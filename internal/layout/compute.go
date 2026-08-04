package layout

import "surge/internal/types"

func (e *LayoutEngine) computeLayout(id types.TypeID, state *layoutState) (PhysicalLayout, *LayoutError) {
	t, ok := e.Types.Lookup(id)
	if !ok {
		return errorLayout(), &LayoutError{Kind: ErrUnknownType, Type: id}
	}
	if _, handleBacked := e.Types.RuntimeHandlePayloads(id); handleBacked {
		return e.pointerLayout(id)
	}

	switch t.Kind {
	case types.KindUnit, types.KindNothing:
		return makePhysical(e.Target, id, 0, 1, nil, nil, nil)
	case types.KindBool:
		return makePhysical(e.Target, id, 1, 1, nil, nil, nil)
	case types.KindInt, types.KindUint, types.KindFloat:
		if t.Width == types.WidthAny {
			return e.pointerLayout(id)
		}
		if t.Width == 0 || uint64(t.Width)%8 != 0 {
			return errorLayout(), &LayoutError{Kind: ErrUnsupportedKind, Type: id, Operation: "numeric width"}
		}
		return makePhysical(e.Target, id, uint64(t.Width)/8, uint64(t.Width)/8, nil, nil, nil)
	case types.KindString, types.KindPointer, types.KindReference, types.KindFar, types.KindFn:
		return e.pointerLayout(id)
	case types.KindStruct:
		if elem, length, ok := e.Types.ArrayFixedInfo(id); ok {
			return e.arrayFixedLayout(id, elem, uint64(length), state)
		}
		return e.structLayout(id, state)
	case types.KindTuple:
		return e.tupleLayout(id, state)
	case types.KindUnion:
		return e.unionLayout(id, state)
	case types.KindEnum:
		if info, ok := e.Types.EnumInfo(id); ok && info != nil && info.BaseType != types.NoTypeID {
			return e.childLayout(id, info.BaseType, state, PathElement{Kind: PathEnumBase})
		}
		return makePhysical(e.Target, id, 4, 4, nil, nil, nil)
	case types.KindConst, types.KindGenericParam:
		return deferredLayout(), nil
	case types.KindArray:
		return e.arrayFixedLayout(id, t.Elem, uint64(t.Count), state)
	case types.KindInvalid, types.KindAlias, types.KindOwn:
		return errorLayout(), &LayoutError{Kind: ErrUnsupportedKind, Type: id, Operation: t.Kind.String()}
	default:
		return errorLayout(), &LayoutError{Kind: ErrUnsupportedKind, Type: id, Operation: t.Kind.String()}
	}
}

func (e *LayoutEngine) pointerLayout(id types.TypeID) (PhysicalLayout, *LayoutError) {
	return makePhysical(e.Target, id, e.Target.PointerSize, e.Target.PointerAlign, nil, nil, nil)
}

func (e *LayoutEngine) childLayout(
	root types.TypeID,
	child types.TypeID,
	state *layoutState,
	path PathElement,
) (PhysicalLayout, *LayoutError) {
	l, err := e.layoutOf(child, state)
	if err != nil {
		return errorLayout(), err.prepend(root, path)
	}
	return l, nil
}

func (e *LayoutEngine) arrayFixedLayout(
	id types.TypeID,
	elem types.TypeID,
	length uint64,
	state *layoutState,
) (PhysicalLayout, *LayoutError) {
	elemLayout, err := e.childLayout(id, elem, state, PathElement{Kind: PathArrayElement})
	if err != nil {
		return errorLayout(), err
	}
	if elemLayout.state == StateDeferred {
		return deferredLayout(), nil
	}
	size, mulErr := checkedMul(e.Target, id, elemLayout.facts.Stride, length, "array stride * length")
	if mulErr != nil {
		return errorLayout(), mulErr.prepend(id, PathElement{Kind: PathArrayElement})
	}
	return makePhysical(e.Target, id, size, elemLayout.facts.Align, nil, nil, nil)
}

func (e *LayoutEngine) structLayout(id types.TypeID, state *layoutState) (PhysicalLayout, *LayoutError) {
	info, ok := e.Types.StructInfo(id)
	if !ok || info == nil {
		return errorLayout(), &LayoutError{Kind: ErrUnknownType, Type: id, Operation: "struct metadata"}
	}
	attrs, _ := e.Types.TypeLayoutAttrs(id)
	if attrs.Packed && attrs.AlignOverride != nil {
		return errorLayout(), &LayoutError{Kind: ErrUnsupportedAlignment, Type: id, Operation: "@packed with @align", Value: *attrs.AlignOverride, Limit: e.Target.MaxABIAlign}
	}

	offsets := make([]uint64, len(info.Fields))
	aligns := make([]uint64, len(info.Fields))
	size := uint64(0)
	align := uint64(1)
	for i := range info.Fields {
		field := &info.Fields[i]
		path := PathElement{Kind: PathStructField, Index: uint32(i)} //nolint:gosec // field count is arena-bounded
		fl, err := e.childLayout(id, field.Type, state, path)
		if err != nil {
			return errorLayout(), err
		}
		if fl.state == StateDeferred {
			return deferredLayout(), nil
		}
		fieldAlign := fl.facts.Align
		if field.Layout.AlignOverride != nil {
			if attrs.Packed {
				return errorLayout(), (&LayoutError{Kind: ErrUnsupportedAlignment, Type: id, Operation: "@packed field @align", Value: *field.Layout.AlignOverride, Limit: e.Target.MaxABIAlign}).prepend(id, path)
			}
			fieldAlign = maxU64(fieldAlign, *field.Layout.AlignOverride)
		}
		if err := validateAlign(e.Target, id, fieldAlign, "field alignment"); err != nil {
			return errorLayout(), err.prepend(id, path)
		}
		if attrs.Packed {
			fieldAlign = 1
		} else {
			var roundErr *LayoutError
			size, roundErr = checkedRoundUp(e.Target, id, size, fieldAlign, "field offset round-up")
			if roundErr != nil {
				return errorLayout(), roundErr.prepend(id, path)
			}
			align = maxU64(align, fieldAlign)
		}
		offsets[i] = size
		aligns[i] = fieldAlign
		var addErr *LayoutError
		size, addErr = checkedAdd(e.Target, id, size, fl.facts.Size, "field offset + size")
		if addErr != nil {
			return errorLayout(), addErr.prepend(id, path)
		}
	}

	if attrs.Packed {
		align = 1
	} else {
		if attrs.AlignOverride != nil {
			if err := validateAlign(e.Target, id, *attrs.AlignOverride, "type alignment"); err != nil {
				return errorLayout(), err
			}
			align = maxU64(align, *attrs.AlignOverride)
		}
		var roundErr *LayoutError
		size, roundErr = checkedRoundUp(e.Target, id, size, align, "struct tail padding")
		if roundErr != nil {
			return errorLayout(), roundErr
		}
	}
	return makePhysical(e.Target, id, size, align, offsets, aligns, nil)
}

func (e *LayoutEngine) tupleLayout(id types.TypeID, state *layoutState) (PhysicalLayout, *LayoutError) {
	info, ok := e.Types.TupleInfo(id)
	if !ok || info == nil {
		return errorLayout(), &LayoutError{Kind: ErrUnknownType, Type: id, Operation: "tuple metadata"}
	}
	offsets := make([]uint64, len(info.Elems))
	aligns := make([]uint64, len(info.Elems))
	size := uint64(0)
	align := uint64(1)
	for i, elem := range info.Elems {
		path := PathElement{Kind: PathTupleElement, Index: uint32(i)} //nolint:gosec // tuple count is arena-bounded
		el, err := e.childLayout(id, elem, state, path)
		if err != nil {
			return errorLayout(), err
		}
		if el.state == StateDeferred {
			return deferredLayout(), nil
		}
		var roundErr *LayoutError
		size, roundErr = checkedRoundUp(e.Target, id, size, el.facts.Align, "tuple offset round-up")
		if roundErr != nil {
			return errorLayout(), roundErr.prepend(id, path)
		}
		offsets[i] = size
		aligns[i] = el.facts.Align
		var addErr *LayoutError
		size, addErr = checkedAdd(e.Target, id, size, el.facts.Size, "tuple offset + size")
		if addErr != nil {
			return errorLayout(), addErr.prepend(id, path)
		}
		align = maxU64(align, el.facts.Align)
	}
	var roundErr *LayoutError
	size, roundErr = checkedRoundUp(e.Target, id, size, align, "tuple tail padding")
	if roundErr != nil {
		return errorLayout(), roundErr
	}
	return makePhysical(e.Target, id, size, align, offsets, aligns, nil)
}

type unionPayload struct {
	size         uint64
	align        uint64
	fieldOffsets []uint64
}

func (e *LayoutEngine) unionLayout(id types.TypeID, state *layoutState) (PhysicalLayout, *LayoutError) {
	info, ok := e.Types.UnionInfo(id)
	if !ok || info == nil || len(info.Members) == 0 {
		return errorLayout(), &LayoutError{Kind: ErrUnknownType, Type: id, Operation: "union metadata"}
	}
	payloads := make([]unionPayload, len(info.Members))
	payloadAlign := uint64(1)
	for i := range info.Members {
		path := PathElement{Kind: PathUnionCase, Index: uint32(i)} //nolint:gosec // member count is arena-bounded
		payload, deferred, err := e.unionMemberPayload(id, &info.Members[i], state, path)
		if err != nil {
			return errorLayout(), err
		}
		if deferred {
			return deferredLayout(), nil
		}
		payloads[i] = payload
		payloadAlign = maxU64(payloadAlign, payload.align)
	}

	const tagSize uint64 = 4
	const tagAlign uint64 = 4
	payloadOffset, err := checkedRoundUp(e.Target, id, tagSize, payloadAlign, "union payload offset")
	if err != nil {
		return errorLayout(), err
	}
	cases := make([]UnionCaseLayout, len(payloads))
	maxEnd := tagSize
	for i, payload := range payloads {
		cases[i] = UnionCaseLayout{
			PayloadOffset: payloadOffset,
			PayloadSize:   payload.size,
			PayloadAlign:  payload.align,
			fieldOffsets:  append([]uint64(nil), payload.fieldOffsets...),
		}
		end, addErr := checkedAdd(e.Target, id, payloadOffset, payload.size, "union payload offset + size")
		if addErr != nil {
			return errorLayout(), addErr.prepend(id, PathElement{Kind: PathUnionCase, Index: uint32(i)}) //nolint:gosec
		}
		maxEnd = maxU64(maxEnd, end)
	}
	overallAlign := maxU64(tagAlign, payloadAlign)
	size, roundErr := checkedRoundUp(e.Target, id, maxEnd, overallAlign, "union tail padding")
	if roundErr != nil {
		return errorLayout(), roundErr
	}
	l, makeErr := makePhysical(e.Target, id, size, overallAlign, nil, nil, cases)
	if makeErr != nil {
		return errorLayout(), makeErr
	}
	l.facts.TagSize = tagSize
	l.facts.TagAlign = tagAlign
	return l, nil
}

func (e *LayoutEngine) unionMemberPayload(
	root types.TypeID,
	member *types.UnionMember,
	state *layoutState,
	path PathElement,
) (unionPayload, bool, *LayoutError) {
	switch member.Kind {
	case types.UnionMemberNothing:
		return unionPayload{align: 1}, false, nil
	case types.UnionMemberType:
		l, err := e.childLayout(root, member.Type, state, path)
		if err != nil {
			return unionPayload{}, false, err
		}
		if l.state == StateDeferred {
			return unionPayload{}, true, nil
		}
		return unionPayload{size: l.facts.Size, align: l.facts.Align, fieldOffsets: []uint64{0}}, false, nil
	case types.UnionMemberTag:
		return e.unionTagPayload(root, member.TagArgs, state, path)
	default:
		return unionPayload{}, false, (&LayoutError{Kind: ErrUnsupportedKind, Type: root, Operation: "union member"}).prepend(root, path)
	}
}

func (e *LayoutEngine) unionTagPayload(
	root types.TypeID,
	args []types.TypeID,
	state *layoutState,
	casePath PathElement,
) (unionPayload, bool, *LayoutError) {
	size := uint64(0)
	align := uint64(1)
	offsets := make([]uint64, len(args))
	for i, arg := range args {
		path := PathElement{Kind: PathUnionPayload, Index: uint32(i)} //nolint:gosec // payload count is arena-bounded
		l, err := e.childLayout(root, arg, state, path)
		if err != nil {
			return unionPayload{}, false, err.prepend(root, casePath)
		}
		if l.state == StateDeferred {
			return unionPayload{}, true, nil
		}
		var roundErr *LayoutError
		size, roundErr = checkedRoundUp(e.Target, root, size, l.facts.Align, "union tuple offset round-up")
		if roundErr != nil {
			return unionPayload{}, false, roundErr.prepend(root, path).prepend(root, casePath)
		}
		offsets[i] = size
		var addErr *LayoutError
		size, addErr = checkedAdd(e.Target, root, size, l.facts.Size, "union tuple offset + size")
		if addErr != nil {
			return unionPayload{}, false, addErr.prepend(root, path).prepend(root, casePath)
		}
		align = maxU64(align, l.facts.Align)
	}
	var roundErr *LayoutError
	size, roundErr = checkedRoundUp(e.Target, root, size, align, "union tuple tail padding")
	if roundErr != nil {
		return unionPayload{}, false, roundErr.prepend(root, casePath)
	}
	return unionPayload{size: size, align: align, fieldOffsets: offsets}, false, nil
}
