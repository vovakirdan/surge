package sema

import (
	"cmp"
	"slices"

	"surge/internal/types"
)

// A view reads existing descriptors in their original vocabulary. Substitution
// switches to the supplied argument's owner once; it never interns a wrapper.
// Function pointers retain source objects, but equality uses canonical values.
type returnOriginTypeView struct {
	owner, caller *returnOriginFunction
	params, args  []types.TypeID
	concrete      bool
}

func returnOriginView(fn *returnOriginFunction) returnOriginTypeView {
	return returnOriginTypeView{owner: fn}
}

func returnOriginBoundView(fn, caller *returnOriginFunction, args []types.TypeID) returnOriginTypeView {
	return returnOriginTypeView{owner: fn, caller: caller,
		params: slices.Clone(fn.candidate.TemplateParams), args: slices.Clone(args)}
}

func (v returnOriginTypeView) clone() returnOriginTypeView {
	v.params, v.args = slices.Clone(v.params), slices.Clone(v.args)
	return v
}

func compareReturnOriginView(a, b returnOriginTypeView) int {
	for i, left := range []*returnOriginFunction{a.owner, a.caller} {
		right := []*returnOriginFunction{b.owner, b.caller}[i]
		if left == nil || right == nil {
			if left != right {
				if left == nil {
					return -1
				}
				return 1
			}
			continue
		}
		for j, key := range []string{left.key, left.canonicalSourceKey, left.unit.SourceKey} {
			other := []string{right.key, right.canonicalSourceKey, right.unit.SourceKey}[j]
			if order := cmp.Compare(key, other); order != 0 {
				return order
			}
		}
		if order := cmp.Compare(left.candidate.Symbol, right.candidate.Symbol); order != 0 {
			return order
		}
	}
	if order := slices.Compare(a.params, b.params); order != 0 {
		return order
	}
	if order := slices.Compare(a.args, b.args); order != 0 {
		return order
	}
	if a.concrete != b.concrete {
		if a.concrete {
			return 1
		}
		return -1
	}
	return 0
}

func (fn *returnOriginFunction) templateSlot(id types.TypeID) (int, bool) {
	if fn == nil || fn.candidate == nil {
		return 0, false
	}
	slot := slices.Index(fn.candidate.TemplateParams, id)
	info, ok := fn.unit.Sema.TypeInterner.TypeParamInfo(id)
	if slot < 0 || !ok || info == nil {
		return 0, false
	}
	return slot, fn.templateParameterAuthority(slot, id, info)
}

func (v returnOriginTypeView) resolve(id types.TypeID) (types.TypeID, returnOriginTypeView, bool) {
	if v.owner == nil || v.owner.candidate == nil || len(v.params) != len(v.args) {
		return id, v, false
	}
	if len(v.params) != 0 && !slices.Equal(v.params, v.owner.candidate.TemplateParams) {
		return id, v, false
	}
	if slot := slices.Index(v.params, id); slot >= 0 {
		if _, ok := v.owner.templateSlot(id); !ok {
			return id, v, false
		}
		id = v.args[slot]
		if v.caller == nil {
			v = returnOriginTypeView{owner: v.owner, concrete: true}
		} else {
			v = returnOriginView(v.caller)
		}
	}
	_, ok := v.owner.unit.Sema.TypeInterner.Lookup(id)
	return id, v, ok
}

// children uses logical stored payloads, not nominal type arguments as fields.
// Opaque handles need their separate operation certificate even with no fields.
func returnOriginTypeChildren(owner *returnOriginFunction, id types.TypeID) ([]types.TypeID, bool) {
	in := owner.unit.Sema.TypeInterner
	typ, ok := in.Lookup(id)
	if !ok {
		return nil, false
	}
	switch typ.Kind {
	case types.KindAlias:
		target, found := in.AliasTarget(id)
		return []types.TypeID{target}, found
	case types.KindReference, types.KindOwn, types.KindFar, types.KindArray, types.KindPointer:
		return []types.TypeID{typ.Elem}, true
	case types.KindFn:
		info, found := in.FnInfo(id)
		if found && info != nil {
			return append(slices.Clone(info.Params), info.Result), true
		}
	case types.KindTuple:
		info, found := in.TupleInfo(id)
		if found && info != nil {
			return slices.Clone(info.Elems), true
		}
	case types.KindStruct:
		info, found := in.StructInfo(id)
		if found && info != nil {
			if _, nominal := returnOriginNominalShape(in, id, info, nil); nominal {
				// A counted channel's ring holds only its payloads; other handles keep their loans.
				if payloads, known := in.RuntimeHandlePayloads(id); known && len(payloads) != 0 && in.IsRuntimeHandleType(id) && in.IsRefCountedHandle(id) {
					return payloads, true
				}
				return nil, false
			}
			out := make([]types.TypeID, 0, len(info.Fields))
			for _, field := range info.Fields {
				out = append(out, field.Type)
			}
			return out, returnOriginPlainStruct(owner, info)
		}
	case types.KindUnion:
		info, found := in.UnionInfo(id)
		if found && info != nil {
			var out []types.TypeID
			for _, member := range info.Members {
				if member.Type != types.NoTypeID {
					out = append(out, member.Type)
				}
				out = append(out, member.TagArgs...)
			}
			return out, len(info.Members) != 0
		}
	default:
		return nil, typ.Kind != types.KindInvalid
	}
	return nil, false
}

// A supported symbolic content shape may carry incoming borrows. This is not
// NoBorrowedState and cannot certify an empty opaque/container value.
func (v returnOriginTypeView) shape(id types.TypeID) returnOriginShape {
	var walk func(returnOriginTypeView, types.TypeID, map[types.TypeID]bool) returnOriginShape
	walk = func(view returnOriginTypeView, id types.TypeID, active map[types.TypeID]bool) returnOriginShape {
		id, view, ok := view.resolve(id)
		if !ok || active[id] {
			return returnOriginShapeUnknown
		}
		in := view.owner.unit.Sema.TypeInterner
		typ, _ := in.Lookup(id)
		if typ.Kind == types.KindGenericParam {
			if _, valid := view.owner.templateSlot(id); valid && !view.concrete {
				return returnOriginCarriesRef
			}
			return returnOriginShapeUnknown
		}
		if typ.Kind == types.KindReference {
			return returnOriginCarriesRef
		}
		if typ.Kind == types.KindFn {
			return returnOriginShapeUnknown
		}
		if !types.ContainsGenericParam(in, id) {
			return returnOriginTypeShape(in, id, nil)
		}
		active[id] = true
		defer delete(active, id)
		// A canonical array's payload is its element; a fixed length is not a
		// payload. A template never lets a container erase its loans.
		if c, canonical := returnOriginIndexContainer(in, id); canonical && !c.reference && c.family != in.Builtins().String {
			shape := walk(view, c.element, active)
			if len(view.params) == 0 && !view.concrete {
				shape = max(shape, returnOriginCarriesRef)
			}
			return shape
		}
		// A cursor's payload is its element; a template never lets it erase its loans.
		if elem, cursor := returnOriginRangeElement(view.owner, id); cursor {
			shape := walk(view, elem, active)
			if len(view.params) == 0 && !view.concrete {
				shape = max(shape, returnOriginCarriesRef)
			}
			return shape
		}
		children, valid := returnOriginTypeChildren(view.owner, id)
		if !valid {
			return returnOriginShapeUnknown
		}
		shape := returnOriginRefFree
		for _, child := range children {
			shape = max(shape, walk(view, child, active))
		}
		return shape
	}
	return walk(v, id, make(map[types.TypeID]bool))
}

func (v returnOriginTypeView) validType(id types.TypeID) bool {
	var walk func(returnOriginTypeView, types.TypeID, map[types.TypeID]bool) bool
	walk = func(view returnOriginTypeView, id types.TypeID, active map[types.TypeID]bool) bool {
		id, view, ok := view.resolve(id)
		if !ok || active[id] {
			return false
		}
		in := view.owner.unit.Sema.TypeInterner
		if typ, _ := in.Lookup(id); typ.Kind == types.KindGenericParam {
			_, valid := view.owner.templateSlot(id)
			return valid && !view.concrete
		}
		if !types.ContainsGenericParam(in, id) {
			return true
		}
		active[id] = true
		defer delete(active, id)
		children, valid := returnOriginTypeChildren(view.owner, id)
		// Type authority is weaker than a value-state certificate. A canonical
		// handle's generic arguments can be valid even though opaque creation
		// of that handle remains unsupported by NoBorrowedState.
		if info, found := in.StructInfo(id); found && info != nil {
			if _, nominal := returnOriginNominalShape(in, id, info, nil); nominal {
				children, valid = slices.Clone(info.TypeArgs), len(info.TypeArgs) != 0
			}
		}
		for _, child := range children {
			valid = walk(view, child, active) && valid
		}
		return valid
	}
	return walk(v, id, make(map[types.TypeID]bool))
}

// Eligibility follows returnSourceBearing: only references and the admitted
// union/tag payload surface qualify. Symbolic leaves need original authority.
func (v returnOriginTypeView) bearing(id types.TypeID, active map[types.TypeID]bool) (ReturnSourceValidationStatus, bool) {
	id, v, ok := v.resolve(id)
	if !ok || active[id] {
		return ReturnSourcesDeferred, false
	}
	in := v.owner.unit.Sema.TypeInterner
	typ, _ := in.Lookup(id)
	if typ.Kind == types.KindReference {
		return ReturnSourcesValid, true
	}
	if typ.Kind == types.KindGenericParam {
		_, valid := v.owner.templateSlot(id)
		return ReturnSourcesDeferred, valid && !v.concrete
	}
	if typ.Kind != types.KindAlias && typ.Kind != types.KindUnion {
		return ReturnSourcesInvalid, true
	}
	if active == nil {
		active = make(map[types.TypeID]bool)
	}
	active[id] = true
	defer delete(active, id)
	children, valid := returnOriginTypeChildren(v.owner, id)
	state := ReturnSourcesInvalid
	for _, child := range children {
		next, known := v.bearing(child, active)
		valid = valid && known
		if next == ReturnSourcesValid || state == ReturnSourcesValid {
			state = ReturnSourcesValid
		} else if next == ReturnSourcesDeferred {
			state = ReturnSourcesDeferred
		}
	}
	return state, valid
}
