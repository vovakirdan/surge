package sema

import (
	"cmp"
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

type returnOriginConditionKind uint8

const (
	returnOriginNoBorrowedState returnOriginConditionKind = iota
	returnOriginDefaultable
)

type returnOriginAtom struct {
	kind returnOriginConditionKind
	slot int
}

// TRUE is the zero value. The only atoms are predicate × original formal slot;
// no type wrappers or recursively expanded source traces enter this lattice.
type returnOriginRequirements struct {
	atoms                []returnOriginAtom
	refuted, unsupported bool
}

func compareReturnOriginAtom(a, b returnOriginAtom) int {
	if n := cmp.Compare(a.kind, b.kind); n != 0 {
		return n
	}
	return cmp.Compare(a.slot, b.slot)
}

func (r returnOriginRequirements) join(other returnOriginRequirements) returnOriginRequirements {
	out := returnOriginRequirements{atoms: append(slices.Clone(r.atoms), other.atoms...),
		refuted: r.refuted || other.refuted, unsupported: r.unsupported || other.unsupported}
	slices.SortFunc(out.atoms, compareReturnOriginAtom)
	out.atoms = slices.Compact(out.atoms)
	return out
}

func (r returnOriginRequirements) equal(other returnOriginRequirements) bool {
	return r.refuted == other.refuted && r.unsupported == other.unsupported && slices.Equal(r.atoms, other.atoms)
}

func (r returnOriginRequirements) failed() bool { return r.refuted || r.unsupported }

func (v returnOriginTypeView) requirement(kind returnOriginConditionKind, id types.TypeID) returnOriginRequirements {
	var walk func(returnOriginTypeView, types.TypeID, map[types.TypeID]bool) returnOriginRequirements
	unknown := returnOriginRequirements{unsupported: true}
	walk = func(view returnOriginTypeView, id types.TypeID, active map[types.TypeID]bool) returnOriginRequirements {
		id, view, ok := view.resolve(id)
		if !ok || active[id] {
			return unknown
		}
		in := view.owner.unit.Sema.TypeInterner
		typ, _ := in.Lookup(id)
		if typ.Kind == types.KindGenericParam {
			if slot, valid := view.owner.templateSlot(id); valid && !view.concrete {
				return returnOriginRequirements{atoms: []returnOriginAtom{{kind: kind, slot: slot}}}
			}
			return unknown
		}
		active[id] = true
		defer delete(active, id)
		if typ.Kind == types.KindAlias {
			target, found := in.AliasTarget(id)
			if !found {
				return unknown
			}
			return walk(view, target, active)
		}
		if typ.Kind == types.KindStruct && in.IsBorrowedView(id) {
			return returnOriginRequirements{refuted: true}
		}
		if kind == returnOriginNoBorrowedState && typ.Kind == types.KindStruct && returnOriginStdlibTimeDuration(view.owner, id) {
			return returnOriginRequirements{}
		}
		if kind == returnOriginDefaultable {
			if elem, array := returnOriginDefaultArrayElement(in, id); array {
				return walk(view, elem, active)
			}
			switch typ.Kind {
			case types.KindStruct:
				info, found := in.StructInfo(id)
				if !found || info == nil || !returnOriginPlainStruct(view.owner, info) {
					return unknown
				}
				out := returnOriginRequirements{}
				for _, field := range info.Fields {
					out = out.join(walk(view, field.Type, active))
				}
				return out
			case types.KindUnion:
				info, found := in.UnionInfo(id)
				if !found || info == nil {
					return unknown
				}
				for _, member := range info.Members {
					if member.Kind == types.UnionMemberNothing {
						return returnOriginRequirements{}
					}
				}
				return returnOriginRequirements{refuted: true}
			case types.KindReference, types.KindOwn, types.KindFar, types.KindFn, types.KindTuple, types.KindEnum:
				return returnOriginRequirements{refuted: true}
			}
		}
		switch typ.Kind {
		case types.KindUnit, types.KindNothing, types.KindBool, types.KindConst, types.KindString,
			types.KindInt, types.KindUint, types.KindFloat, types.KindEnum, types.KindPointer:
			return returnOriginRequirements{}
		case types.KindReference:
			return returnOriginRequirements{refuted: true}
		case types.KindOwn, types.KindFar: // far X holds what the core runtime handle X holds
			if typ.Kind == types.KindFar && !in.IsRuntimeHandleType(typ.Elem) {
				return unknown
			}
			return walk(view, typ.Elem, active)
		case types.KindFn, types.KindArray:
			return unknown
		case types.KindStruct, types.KindTuple, types.KindUnion:
			children, valid := returnOriginTypeChildren(view.owner, id)
			if !valid {
				return unknown
			}
			out := returnOriginRequirements{}
			for _, child := range children {
				out = out.join(walk(view, child, active))
			}
			return out
		}
		return unknown
	}
	return walk(v, id, make(map[types.TypeID]bool))
}

func returnOriginDefaultArrayElement(in *types.Interner, id types.TypeID) (types.TypeID, bool) {
	if typ, ok := in.Lookup(id); ok && typ.Kind == types.KindArray {
		return typ.Elem, true
	}
	info, ok := in.StructInfo(id)
	if !ok || info == nil || len(info.TypeArgs) == 0 {
		return types.NoTypeID, false
	}
	for _, base := range []types.TypeID{in.ArrayNominalType(), in.ArrayFixedNominalType()} {
		original, found := in.StructInfo(base)
		if found && original != nil && original.Name == info.Name && original.Decl == info.Decl {
			return info.TypeArgs[0], true
		}
	}
	return types.NoTypeID, false
}

func (r returnOriginRequirements) rebase(view returnOriginTypeView) returnOriginRequirements {
	out := returnOriginRequirements{refuted: r.refuted, unsupported: r.unsupported}
	for _, atom := range r.atoms {
		if atom.slot < 0 || atom.slot >= len(view.params) {
			out.unsupported = true
			continue
		}
		out = out.join(view.requirement(atom.kind, view.params[atom.slot]))
	}
	return out
}

// FileID zero is a real source file. Prove the source declaration and completed
// field roster, rather than treating a zero ID or an empty physical layout as
// a nominal certificate. A struct declared in another analyzed unit is proven in
// that unit, found by its exact file among the peers; a unit that is missing or
// ambiguous proves nothing. Inherited and opaque forms, and every attribute
// except a bare `@copy` or `@shard_movable`, stay unsupported.
func returnOriginPlainStruct(fn *returnOriginFunction, info *types.StructInfo) bool {
	u := fn.unit
	if f := u.Builder.Files.Get(u.FileID); f == nil || f.Span.File != info.Decl.File {
		var match *returnOriginUnitIndex
		for _, peer := range u.peers {
			if pf := peer.Builder.Files.Get(peer.FileID); pf != nil && pf.Span.File == info.Decl.File {
				if match != nil {
					return false
				}
				match = peer
			}
		}
		if match == nil {
			return false
		}
		u = match
	}
	file := u.Builder.Files.Get(u.FileID)
	if file == nil || file.Span.File != info.Decl.File || info.Decl.End <= info.Decl.Start {
		return false
	}
	for _, id := range file.Items {
		item, ok := u.Builder.Items.Type(id)
		if !ok || item.Span != info.Decl {
			continue
		}
		decl := u.Builder.Items.TypeStruct(item)
		ids := u.Symbols.ItemSymbols[id]
		if decl == nil || decl.Base.IsValid() || !returnOriginCapabilityOnlyAttributes(u, item) || len(ids) != 1 ||
			uint64(decl.FieldsCount) != uint64(len(info.Fields)) {
			return false
		}
		sym := u.Symbols.Table.Symbols.Get(ids[0])
		if sym == nil || sym.Kind != symbols.SymbolType || sym.Decl.Item != id || sym.Decl.ASTFile != u.FileID || sym.Decl.SourceFile != info.Decl.File {
			return false
		}
		original, found := u.Sema.TypeInterner.StructInfo(sym.Type)
		if !found || original == nil || original.Decl != info.Decl || original.Name != info.Name || len(original.Fields) != len(info.Fields) {
			return false
		}
		for i, field := range info.Fields {
			if field.Name != original.Fields[i].Name {
				return false
			}
		}
		return true
	}
	return false
}

// `@copy` (every field is Copy) and `@shard_movable` (every field may cross a shard
// boundary) only permit uses of a value: neither adds storage nor hides a field, so the
// caller still walks every field. Any other, unknown or unreadable attribute, or an argument, refuses.
func returnOriginCapabilityOnlyAttributes(u *returnOriginUnitIndex, item *ast.TypeItem) bool {
	if item.AttrCount == 0 {
		return true
	}
	attrs := u.Builder.Items.CollectAttrs(item.AttrStart, item.AttrCount)
	if uint64(len(attrs)) != uint64(item.AttrCount) {
		return false
	}
	for _, attr := range attrs {
		spec, known := ast.LookupAttrID(u.Builder.StringsInterner, attr.Name)
		if !known || (spec.Name != "copy" && spec.Name != "shard_movable") || len(attr.Args) != 0 {
			return false
		}
	}
	return true
}
