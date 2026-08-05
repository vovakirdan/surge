package sema

import (
	"fmt"
	"slices"

	"surge/internal/types"
)

const instantiationSubstitutionDepthLimit = 256

type instantiationSubstitution struct {
	types  *types.Interner
	exact  map[types.TypeID]uint32
	binds  map[instantiationParamRef]uint32
	args   []types.TypeID
	cache  map[types.TypeID]types.TypeID
	active map[types.TypeID]struct{}
}

type instantiationParamRef struct {
	owner uint32
	index uint32
}

func newInstantiationSubstitution(typesIn *types.Interner, bindings []InstantiationParamBinding, args []types.TypeID) (*instantiationSubstitution, error) {
	exact := make(map[types.TypeID]uint32, len(bindings))
	binds := make(map[instantiationParamRef]uint32, len(bindings))
	for _, binding := range bindings {
		if !binding.Owner.IsValid() {
			return nil, fmt.Errorf("instantiation substitution: binding has no owner")
		}
		if int(binding.ArgIndex) >= len(args) {
			return nil, fmt.Errorf("instantiation substitution: owner %d parameter %d maps to missing caller argument %d", binding.Owner, binding.ParamIndex, binding.ArgIndex)
		}
		if binding.Param != types.NoTypeID {
			if existing, found := exact[binding.Param]; found && existing != binding.ArgIndex {
				return nil, fmt.Errorf("instantiation substitution: type#%d has conflicting caller arguments %d and %d", binding.Param, existing, binding.ArgIndex)
			}
			exact[binding.Param] = binding.ArgIndex
		}
		ref := instantiationParamRef{owner: uint32(binding.Owner), index: binding.ParamIndex}
		if existing, found := binds[ref]; found && existing != binding.ArgIndex {
			return nil, fmt.Errorf("instantiation substitution: owner %d parameter %d has conflicting caller arguments %d and %d", binding.Owner, binding.ParamIndex, existing, binding.ArgIndex)
		}
		binds[ref] = binding.ArgIndex
	}
	return &instantiationSubstitution{
		types:  typesIn,
		exact:  exact,
		binds:  binds,
		args:   slices.Clone(args),
		cache:  make(map[types.TypeID]types.TypeID, 32),
		active: make(map[types.TypeID]struct{}, 8),
	}, nil
}

func (s *instantiationSubstitution) typeID(id types.TypeID) (types.TypeID, error) {
	return s.typeIDAtDepth(id, 0)
}

func (s *instantiationSubstitution) typeIDAtDepth(id types.TypeID, depth int) (types.TypeID, error) {
	if s == nil || s.types == nil {
		return types.NoTypeID, fmt.Errorf("instantiation substitution: missing type interner")
	}
	if id == types.NoTypeID {
		return types.NoTypeID, fmt.Errorf("instantiation substitution: missing type")
	}
	if depth >= instantiationSubstitutionDepthLimit {
		return types.NoTypeID, fmt.Errorf("instantiation substitution: nesting exceeds %d at type#%d", instantiationSubstitutionDepthLimit, id)
	}
	if cached, ok := s.cache[id]; ok {
		return cached, nil
	}
	if _, ok := s.active[id]; ok {
		return types.NoTypeID, fmt.Errorf("instantiation substitution: cyclic structural type at type#%d", id)
	}
	s.active[id] = struct{}{}
	defer delete(s.active, id)

	t, ok := s.types.Lookup(id)
	if !ok {
		return types.NoTypeID, fmt.Errorf("instantiation substitution: unknown type#%d", id)
	}
	out := id
	var err error
	switch t.Kind {
	case types.KindGenericParam:
		info, found := s.types.TypeParamInfo(id)
		if !found || info == nil {
			return types.NoTypeID, fmt.Errorf("instantiation substitution: generic type#%d has no metadata", id)
		}
		argIndex, matches := s.exact[id]
		if !matches {
			argIndex, matches = s.binds[instantiationParamRef{owner: info.Owner, index: info.Index}]
		}
		if !matches {
			return types.NoTypeID, fmt.Errorf("instantiation substitution: owner %d parameter %d is not bound by the caller instance", info.Owner, info.Index)
		}
		if int(argIndex) >= len(s.args) || s.args[argIndex] == types.NoTypeID {
			return types.NoTypeID, fmt.Errorf("instantiation substitution: owner %d parameter %d has no caller argument %d", info.Owner, info.Index, argIndex)
		}
		out = s.args[argIndex]
	case types.KindPointer, types.KindReference, types.KindOwn, types.KindFar, types.KindArray:
		var elem types.TypeID
		elem, err = s.typeIDAtDepth(t.Elem, depth+1)
		if err == nil && elem != t.Elem {
			clone := t
			clone.Elem = elem
			out = s.types.Intern(clone)
		}
	case types.KindTuple:
		info, found := s.types.TupleInfo(id)
		if !found || info == nil {
			return types.NoTypeID, fmt.Errorf("instantiation substitution: tuple type#%d has no metadata", id)
		}
		var changed bool
		var elems []types.TypeID
		elems, changed, err = s.typeList(info.Elems, depth)
		if err == nil && changed {
			out = s.types.RegisterTuple(elems)
		}
	case types.KindFn:
		info, found := s.types.FnInfo(id)
		if !found || info == nil {
			return types.NoTypeID, fmt.Errorf("instantiation substitution: function type#%d has no metadata", id)
		}
		var changed bool
		var params []types.TypeID
		params, changed, err = s.typeList(info.Params, depth)
		if err != nil {
			break
		}
		result, resultErr := s.typeIDAtDepth(info.Result, depth+1)
		if resultErr != nil {
			err = resultErr
			break
		}
		if changed || result != info.Result {
			out = s.types.RegisterFn(params, result)
		}
	case types.KindStruct:
		out, err = s.structType(id, depth)
	case types.KindUnion:
		out, err = s.unionType(id, depth)
	case types.KindAlias:
		out, err = s.aliasType(id, depth)
	case types.KindEnum:
		out, err = s.enumType(id, depth)
	}
	if err != nil {
		return types.NoTypeID, err
	}
	s.cache[id] = out
	return out, nil
}

func (s *instantiationSubstitution) typeList(ids []types.TypeID, depth int) ([]types.TypeID, bool, error) {
	out := make([]types.TypeID, len(ids))
	changed := false
	for i, id := range ids {
		var err error
		out[i], err = s.typeIDAtDepth(id, depth+1)
		if err != nil {
			return nil, false, err
		}
		changed = changed || out[i] != id
	}
	return out, changed, nil
}

func (s *instantiationSubstitution) structType(id types.TypeID, depth int) (types.TypeID, error) {
	info, ok := s.types.StructInfo(id)
	if !ok || info == nil || len(info.TypeArgs) == 0 {
		return id, nil
	}
	args, changed, err := s.typeList(info.TypeArgs, depth)
	if err != nil || !changed {
		return id, err
	}
	if existing, found := s.types.FindStructInstanceWithDecl(info.Name, info.Decl, args); found {
		return existing, nil
	}
	out := s.types.RegisterStructInstanceWithValues(info.Name, info.Decl, args, s.types.StructValueArgs(id))
	// Publish the nominal placeholder before fields are expanded. Recursive and
	// mutually-recursive nominal graphs then point at the concrete instance,
	// while illegal structural cycles are still rejected by active tracking.
	s.cache[id] = out
	if base, ok := s.types.StructBase(id); ok {
		base, err = s.typeIDAtDepth(base, depth+1)
		if err != nil {
			return types.NoTypeID, err
		}
		s.types.SetStructBase(out, base)
	}
	fields := s.types.StructFields(id)
	for i := range fields {
		fields[i].Type, err = s.typeIDAtDepth(fields[i].Type, depth+1)
		if err != nil {
			return types.NoTypeID, err
		}
	}
	s.types.SetStructFields(out, fields)
	return out, nil
}

func (s *instantiationSubstitution) unionType(id types.TypeID, depth int) (types.TypeID, error) {
	info, ok := s.types.UnionInfo(id)
	if !ok || info == nil || len(info.TypeArgs) == 0 {
		return id, nil
	}
	args, changed, err := s.typeList(info.TypeArgs, depth)
	if err != nil || !changed {
		return id, err
	}
	if existing, found := s.types.FindUnionInstanceWithDecl(info.Name, info.Decl, args); found {
		return existing, nil
	}
	out := s.types.RegisterUnionInstance(info.Name, info.Decl, args)
	s.cache[id] = out
	members := slices.Clone(info.Members)
	for i := range members {
		members[i].Type, err = s.typeIDAtDepth(members[i].Type, depth+1)
		if err != nil {
			return types.NoTypeID, err
		}
		members[i].TagArgs, _, err = s.typeList(members[i].TagArgs, depth)
		if err != nil {
			return types.NoTypeID, err
		}
	}
	s.types.SetUnionMembers(out, members)
	return out, nil
}

func (s *instantiationSubstitution) aliasType(id types.TypeID, depth int) (types.TypeID, error) {
	info, ok := s.types.AliasInfo(id)
	if !ok || info == nil || len(info.TypeArgs) == 0 {
		return id, nil
	}
	args, changed, err := s.typeList(info.TypeArgs, depth)
	if err != nil || !changed {
		return id, err
	}
	if existing, found := s.types.FindAliasInstanceWithDecl(info.Name, info.Decl, args); found {
		return existing, nil
	}
	out := s.types.RegisterAliasInstance(info.Name, info.Decl, args)
	s.cache[id] = out
	target, err := s.typeIDAtDepth(info.Target, depth+1)
	if err != nil {
		return types.NoTypeID, err
	}
	s.types.SetAliasTarget(out, target)
	return out, nil
}

func (s *instantiationSubstitution) enumType(id types.TypeID, depth int) (types.TypeID, error) {
	info, ok := s.types.EnumInfo(id)
	if !ok || info == nil || len(info.TypeArgs) == 0 {
		return id, nil
	}
	args, changed, err := s.typeList(info.TypeArgs, depth)
	if err != nil || !changed {
		return id, err
	}
	if existing, found := s.types.FindEnumInstanceWithDecl(info.Name, info.Decl, args); found {
		return existing, nil
	}
	out := s.types.RegisterEnumInstance(info.Name, info.Decl, args)
	s.cache[id] = out
	base, err := s.typeIDAtDepth(info.BaseType, depth+1)
	if err != nil {
		return types.NoTypeID, err
	}
	s.types.SetEnumBaseType(out, base)
	s.types.SetEnumVariants(out, info.Variants)
	return out, nil
}
