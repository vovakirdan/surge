package layout

import (
	"fortio.org/safecast"

	"surge/internal/types"
)

func canonicalType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil || id == types.NoTypeID {
		return id
	}
	seen := make(map[types.TypeID]struct{}, 8)
	for id != types.NoTypeID {
		if _, ok := seen[id]; ok {
			return id
		}
		seen[id] = struct{}{}
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return id
			}
			id = target
		case types.KindOwn:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func (e *LayoutEngine) computeLayout(id types.TypeID, state *layoutState) (TypeLayout, *LayoutError) {
	if id == types.NoTypeID {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	if e == nil {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	typesIn := e.Types
	if typesIn == nil {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return TypeLayout{Size: 0, Align: 1}, nil
	}

	switch tt.Kind {
	case types.KindUnit, types.KindNothing:
		return TypeLayout{Size: 0, Align: 1}, nil

	case types.KindBool:
		return TypeLayout{Size: 1, Align: 1}, nil

	case types.KindInt:
		if tt.Width == types.WidthAny {
			return e.ptrLayout(), nil
		}
		return scalarLayoutBytes(int(tt.Width) / 8), nil

	case types.KindUint:
		if tt.Width == types.WidthAny {
			return e.ptrLayout(), nil
		}
		return scalarLayoutBytes(int(tt.Width) / 8), nil

	case types.KindFloat:
		if tt.Width == types.WidthAny {
			return e.ptrLayout(), nil
		}
		return scalarLayoutBytes(int(tt.Width) / 8), nil

	case types.KindString:
		return e.ptrLayout(), nil

	case types.KindPointer, types.KindReference, types.KindFn:
		return e.ptrLayout(), nil

	case types.KindStruct:
		if elem, length, ok := typesIn.ArrayFixedInfo(id); ok {
			return e.arrayFixedLayout(elem, length, state)
		}
		if _, ok := typesIn.ArrayInfo(id); ok {
			// Dynamic Array<T> is a handle in the v1 VM/ABI contract.
			return e.ptrLayout(), nil
		}
		return e.structLayoutWithAttrs(id, state)

	case types.KindTuple:
		return e.tupleLayout(id, state)

	case types.KindUnion:
		return e.tagUnionLayout(id, state)

	case types.KindEnum:
		if info, ok := typesIn.EnumInfo(id); ok && info != nil && info.BaseType != types.NoTypeID {
			l, err := e.layoutOf(info.BaseType, state)
			return l, err
		}
		return scalarLayoutBytes(4), nil // default v1: uint32

	case types.KindConst, types.KindGenericParam:
		return TypeLayout{Size: 0, Align: 1}, nil

	case types.KindArray:
		if tt.Count == types.ArrayDynamicLength {
			return e.ptrLayout(), nil
		}
		return e.arrayFixedLayout(tt.Elem, tt.Count, state)

	default:
		return TypeLayout{Size: 0, Align: 1}, nil
	}
}

func (e *LayoutEngine) ptrLayout() TypeLayout {
	ptrSize := e.Target.PtrSize
	ptrAlign := e.Target.PtrAlign
	if ptrSize <= 0 {
		ptrSize = 8
	}
	if ptrAlign <= 0 {
		ptrAlign = ptrSize
	}
	return TypeLayout{Size: ptrSize, Align: ptrAlign}
}

func scalarLayoutBytes(size int) TypeLayout {
	if size <= 0 {
		return TypeLayout{Size: 0, Align: 1}
	}
	return TypeLayout{Size: size, Align: size}
}

func roundUp(n, align int) int {
	if align <= 1 {
		return n
	}
	r := n % align
	if r == 0 {
		return n
	}
	return n + (align - r)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (e *LayoutEngine) arrayFixedLayout(elem types.TypeID, length uint32, state *layoutState) (TypeLayout, *LayoutError) {
	elemLayout, err := e.layoutOf(elem, state)
	if err != nil {
		return TypeLayout{Size: 0, Align: 1}, err
	}
	elemSize := elemLayout.Size
	elemAlign := elemLayout.Align
	if elemAlign <= 0 {
		elemAlign = 1
	}
	stride := roundUp(elemSize, elemAlign)
	n, convErr := safecast.Conv[int](length)
	if convErr != nil || n < 0 {
		n = 0
	}
	return TypeLayout{
		Size:  stride * n,
		Align: elemAlign,
	}, nil
}

func (e *LayoutEngine) structLayoutWithAttrs(id types.TypeID, state *layoutState) (TypeLayout, *LayoutError) {
	if e == nil || e.Types == nil {
		return TypeLayout{Size: 0, Align: 1}, nil
	}

	attrs, _ := e.Types.TypeLayoutAttrs(id)
	if attrs.Packed && attrs.AlignOverride != nil {
		panic("invalid layout attrs: @packed conflicts with @align")
	}

	info, ok := e.Types.StructInfo(id)
	if !ok || info == nil || len(info.Fields) == 0 {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	fields := info.Fields
	offsets := make([]int, len(fields))
	aligns := make([]int, len(fields))

	if attrs.Packed {
		size := 0
		for i := range fields {
			fl, err := e.layoutOf(fields[i].Type, state)
			if err != nil {
				return TypeLayout{Size: 0, Align: 1}, err
			}
			offsets[i] = size
			aligns[i] = 1
			size += fl.Size
		}
		return TypeLayout{
			Size:         size,
			Align:        1,
			FieldOffsets: offsets,
			FieldAligns:  aligns,
		}, nil
	}

	size := 0
	align := 1
	for i := range fields {
		fl, err := e.layoutOf(fields[i].Type, state)
		if err != nil {
			return TypeLayout{Size: 0, Align: 1}, err
		}
		fAlign := fl.Align
		if fields[i].Layout.AlignOverride != nil {
			fAlign = maxInt(fAlign, *fields[i].Layout.AlignOverride)
		}
		if fAlign <= 0 {
			fAlign = 1
		}
		size = roundUp(size, fAlign)
		offsets[i] = size
		aligns[i] = fAlign
		size += fl.Size
		align = maxInt(align, fAlign)
	}
	size = roundUp(size, align)

	if attrs.AlignOverride != nil {
		align = maxInt(align, *attrs.AlignOverride)
		size = roundUp(size, align)
	}
	return TypeLayout{
		Size:         size,
		Align:        align,
		FieldOffsets: offsets,
		FieldAligns:  aligns,
	}, nil
}

func (e *LayoutEngine) tupleLayout(id types.TypeID, state *layoutState) (TypeLayout, *LayoutError) {
	if e == nil || e.Types == nil {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	info, ok := e.Types.TupleInfo(id)
	if !ok || info == nil || len(info.Elems) == 0 {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	size := 0
	align := 1
	for _, elem := range info.Elems {
		el, err := e.layoutOf(elem, state)
		if err != nil {
			return TypeLayout{Size: 0, Align: 1}, err
		}
		a := el.Align
		if a <= 0 {
			a = 1
		}
		size = roundUp(size, a)
		size += el.Size
		align = maxInt(align, a)
	}
	size = roundUp(size, align)
	return TypeLayout{Size: size, Align: align}, nil
}

func (e *LayoutEngine) tagUnionLayout(id types.TypeID, state *layoutState) (TypeLayout, *LayoutError) {
	if e == nil || e.Types == nil {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	info, ok := e.Types.UnionInfo(id)
	if !ok || info == nil || len(info.Members) == 0 {
		return TypeLayout{Size: 0, Align: 1}, nil
	}

	maxPayloadSize := 0
	payloadAlign := 1
	for _, m := range info.Members {
		switch m.Kind {
		case types.UnionMemberNothing:
			// payload: size 0 align 1
		case types.UnionMemberType:
			pl, err := e.layoutOf(m.Type, state)
			if err != nil {
				return TypeLayout{Size: 0, Align: 1}, err
			}
			maxPayloadSize = maxInt(maxPayloadSize, pl.Size)
			payloadAlign = maxInt(payloadAlign, pl.Align)
		case types.UnionMemberTag:
			if len(m.TagArgs) == 0 {
				continue
			}
			if len(m.TagArgs) == 1 {
				pl, err := e.layoutOf(m.TagArgs[0], state)
				if err != nil {
					return TypeLayout{Size: 0, Align: 1}, err
				}
				maxPayloadSize = maxInt(maxPayloadSize, pl.Size)
				payloadAlign = maxInt(payloadAlign, pl.Align)
				continue
			}
			// Multiple payload values: lay them out like a tuple.
			size := 0
			align := 1
			for _, a := range m.TagArgs {
				al, err := e.layoutOf(a, state)
				if err != nil {
					return TypeLayout{Size: 0, Align: 1}, err
				}
				size = roundUp(size, maxInt(1, al.Align))
				size += al.Size
				align = maxInt(align, maxInt(1, al.Align))
			}
			size = roundUp(size, align)
			maxPayloadSize = maxInt(maxPayloadSize, size)
			payloadAlign = maxInt(payloadAlign, align)
		default:
		}
	}

	// v1 layout: tag:uint32 then payload aligned up to payloadAlign.
	tagSize := 4
	tagAlign := 4
	if payloadAlign <= 0 {
		payloadAlign = 1
	}
	payloadOffset := roundUp(tagSize, payloadAlign)
	overallAlign := maxInt(tagAlign, payloadAlign)
	size := roundUp(payloadOffset+maxPayloadSize, overallAlign)
	return TypeLayout{
		Size:          size,
		Align:         overallAlign,
		TagSize:       tagSize,
		TagAlign:      tagAlign,
		PayloadOffset: payloadOffset,
	}, nil
}
