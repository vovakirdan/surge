package mono

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Type applies type substitution to a type ID.
func (s *Subst) Type(id types.TypeID) types.TypeID {
	if s == nil || s.Types == nil || id == types.NoTypeID {
		return id
	}
	if s.cache == nil {
		s.cache = make(map[types.TypeID]types.TypeID, 32)
	} else if cached, ok := s.cache[id]; ok {
		return cached
	}

	out := s.typeNoCache(id)
	s.cache[id] = out
	return out
}

func (s *Subst) typeNoCache(id types.TypeID) types.TypeID {
	tt, ok := s.Types.Lookup(id)
	if !ok {
		return id
	}

	switch tt.Kind {
	case types.KindGenericParam:
		info, ok := s.Types.TypeParamInfo(id)
		if !ok || info == nil {
			return id
		}
		if !s.ownerMatches(symbols.SymbolID(info.Owner)) {
			if s.NameArgs != nil {
				if repl, ok := s.NameArgs[info.Name]; ok && repl != types.NoTypeID {
					return repl
				}
			}
			return id
		}
		idx := int(info.Index)
		if idx < 0 || idx >= len(s.TypeArgs) {
			if s.NameArgs != nil {
				if repl, ok := s.NameArgs[info.Name]; ok && repl != types.NoTypeID {
					return repl
				}
			}
			return id
		}
		if s.TypeArgs[idx] == types.NoTypeID {
			if s.NameArgs != nil {
				if repl, ok := s.NameArgs[info.Name]; ok && repl != types.NoTypeID {
					return repl
				}
			}
			return id
		}
		return s.TypeArgs[idx]

	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		elem := s.Type(tt.Elem)
		if elem == tt.Elem {
			return id
		}
		clone := tt
		clone.Elem = elem
		return s.Types.Intern(clone)

	case types.KindTuple:
		info, ok := s.Types.TupleInfo(id)
		if !ok || info == nil || len(info.Elems) == 0 {
			return id
		}
		elems := make([]types.TypeID, len(info.Elems))
		changed := false
		for i := range info.Elems {
			elems[i] = s.Type(info.Elems[i])
			changed = changed || elems[i] != info.Elems[i]
		}
		if !changed {
			return id
		}
		return s.Types.RegisterTuple(elems)

	case types.KindFn:
		info, ok := s.Types.FnInfo(id)
		if !ok || info == nil {
			return id
		}
		params := make([]types.TypeID, len(info.Params))
		changed := false
		for i := range info.Params {
			params[i] = s.Type(info.Params[i])
			changed = changed || params[i] != info.Params[i]
		}
		result := s.Type(info.Result)
		changed = changed || result != info.Result
		if !changed {
			return id
		}
		return s.Types.RegisterFn(params, result)

	case types.KindStruct:
		info, ok := s.Types.StructInfo(id)
		if !ok || info == nil || len(info.TypeArgs) == 0 {
			return id
		}
		newArgs := make([]types.TypeID, len(info.TypeArgs))
		changed := false
		for i := range info.TypeArgs {
			newArgs[i] = s.Type(info.TypeArgs[i])
			changed = changed || newArgs[i] != info.TypeArgs[i]
		}
		if !changed {
			return id
		}
		if existing, ok := s.Types.FindStructInstance(info.Name, newArgs); ok {
			return existing
		}
		inst := s.Types.RegisterStructInstanceWithValues(info.Name, info.Decl, newArgs, s.Types.StructValueArgs(id))
		fields := s.Types.StructFields(id)
		for i := range fields {
			fields[i].Type = s.Type(fields[i].Type)
		}
		s.Types.SetStructFields(inst, fields)
		return inst

	case types.KindUnion:
		info, ok := s.Types.UnionInfo(id)
		if !ok || info == nil || len(info.TypeArgs) == 0 {
			return id
		}
		newArgs := make([]types.TypeID, len(info.TypeArgs))
		changed := false
		for i := range info.TypeArgs {
			newArgs[i] = s.Type(info.TypeArgs[i])
			changed = changed || newArgs[i] != info.TypeArgs[i]
		}
		if !changed {
			return id
		}
		if existing, ok := s.Types.FindUnionInstance(info.Name, newArgs); ok {
			return existing
		}
		inst := s.Types.RegisterUnionInstance(info.Name, info.Decl, newArgs)
		members := make([]types.UnionMember, len(info.Members))
		copy(members, info.Members)
		for i := range members {
			members[i].Type = s.Type(members[i].Type)
			if len(members[i].TagArgs) > 0 {
				tagArgs := make([]types.TypeID, len(members[i].TagArgs))
				for j := range members[i].TagArgs {
					tagArgs[j] = s.Type(members[i].TagArgs[j])
				}
				members[i].TagArgs = tagArgs
			}
		}
		s.Types.SetUnionMembers(inst, members)
		return inst

	case types.KindAlias:
		info, ok := s.Types.AliasInfo(id)
		if !ok || info == nil || len(info.TypeArgs) == 0 {
			return id
		}
		newArgs := make([]types.TypeID, len(info.TypeArgs))
		changed := false
		for i := range info.TypeArgs {
			newArgs[i] = s.Type(info.TypeArgs[i])
			changed = changed || newArgs[i] != info.TypeArgs[i]
		}
		if !changed {
			return id
		}
		if existing, ok := s.Types.FindAliasInstance(info.Name, newArgs); ok {
			return existing
		}
		target := s.Type(info.Target)
		if target == types.NoTypeID {
			return types.NoTypeID
		}
		inst := s.Types.RegisterAliasInstance(info.Name, info.Decl, newArgs)
		s.Types.SetAliasTarget(inst, target)
		return inst

	default:
		return id
	}
}

func inferOwnership(typesIn *types.Interner, ty types.TypeID) hir.Ownership {
	if typesIn == nil || ty == types.NoTypeID {
		return hir.OwnershipNone
	}
	t, ok := typesIn.Lookup(ty)
	if !ok {
		return hir.OwnershipNone
	}
	switch t.Kind {
	case types.KindReference:
		if t.Mutable {
			return hir.OwnershipRefMut
		}
		return hir.OwnershipRef
	case types.KindPointer:
		return hir.OwnershipPtr
	case types.KindOwn:
		return hir.OwnershipOwn
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool:
		return hir.OwnershipCopy
	default:
		return hir.OwnershipNone
	}
}

func (s *Subst) ownerMatches(owner symbols.SymbolID) bool {
	if owner == s.OwnerSym {
		return true
	}
	for _, alt := range s.OwnerSyms {
		if owner == alt {
			return true
		}
	}
	return false
}

// DebugString returns a debug string representation of the substitution.
func (s *Subst) DebugString() string {
	if s == nil {
		return "<nil>"
	}
	return fmt.Sprintf("owner=%d args=%d", s.OwnerSym, len(s.TypeArgs))
}
