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

func (e *LayoutEngine) computeLayout(id types.TypeID) TypeLayout {
	if id == types.NoTypeID {
		return TypeLayout{Size: 0, Align: 1}
	}
	if e == nil {
		return TypeLayout{Size: 0, Align: 1}
	}
	typesIn := e.Types
	if typesIn == nil {
		return TypeLayout{Size: 0, Align: 1}
	}
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return TypeLayout{Size: 0, Align: 1}
	}

	switch tt.Kind {
	case types.KindUnit, types.KindNothing:
		return TypeLayout{Size: 0, Align: 1}

	case types.KindBool:
		return TypeLayout{Size: 1, Align: 1}

	case types.KindInt:
		if tt.Width == types.WidthAny {
			return e.ptrLayout()
		}
		return scalarLayoutBytes(int(tt.Width) / 8)

	case types.KindUint:
		if tt.Width == types.WidthAny {
			return e.ptrLayout()
		}
		return scalarLayoutBytes(int(tt.Width) / 8)

	case types.KindFloat:
		if tt.Width == types.WidthAny {
			return e.ptrLayout()
		}
		return scalarLayoutBytes(int(tt.Width) / 8)

	case types.KindString:
		return e.ptrLayout()

	case types.KindPointer, types.KindReference, types.KindFn:
		return e.ptrLayout()

	case types.KindStruct:
		if elem, length, ok := typesIn.ArrayFixedInfo(id); ok {
			return e.arrayFixedLayout(elem, length)
		}
		if _, ok := typesIn.ArrayInfo(id); ok {
			// Dynamic Array<T> is a handle in the v1 VM/ABI contract.
			return e.ptrLayout()
		}
		return e.structLayoutWithAttrs(id)

	case types.KindTuple:
		return e.tupleLayout(id)

	case types.KindUnion:
		return e.tagUnionLayout(id)

	case types.KindEnum:
		if info, ok := typesIn.EnumInfo(id); ok && info != nil && info.BaseType != types.NoTypeID {
			return e.LayoutOf(info.BaseType)
		}
		return scalarLayoutBytes(4) // default v1: uint32

	case types.KindConst, types.KindGenericParam:
		return TypeLayout{Size: 0, Align: 1}

	case types.KindArray:
		if tt.Count == types.ArrayDynamicLength {
			return e.ptrLayout()
		}
		return e.arrayFixedLayout(tt.Elem, tt.Count)

	default:
		return TypeLayout{Size: 0, Align: 1}
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

func (e *LayoutEngine) arrayFixedLayout(elem types.TypeID, length uint32) TypeLayout {
	elemLayout := e.LayoutOf(elem)
	elemSize := elemLayout.Size
	elemAlign := elemLayout.Align
	if elemAlign <= 0 {
		elemAlign = 1
	}
	stride := roundUp(elemSize, elemAlign)
	n, err := safecast.Conv[int](length)
	if err != nil || n < 0 {
		n = 0
	}
	return TypeLayout{
		Size:  stride * n,
		Align: elemAlign,
	}
}

func (e *LayoutEngine) structLayoutWithAttrs(id types.TypeID) TypeLayout {
	if e == nil || e.Types == nil {
		return TypeLayout{Size: 0, Align: 1}
	}

	attrs, _ := e.Types.TypeLayoutAttrs(id)
	if attrs.Packed && attrs.AlignOverride != nil {
		panic("invalid layout attrs: @packed conflicts with @align")
	}

	info, ok := e.Types.StructInfo(id)
	if !ok || info == nil || len(info.Fields) == 0 {
		return TypeLayout{Size: 0, Align: 1}
	}
	fields := info.Fields
	offsets := make([]int, len(fields))
	aligns := make([]int, len(fields))

	if attrs.Packed {
		size := 0
		for i := range fields {
			fl := e.LayoutOf(fields[i].Type)
			offsets[i] = size
			aligns[i] = 1
			size += fl.Size
		}
		return TypeLayout{
			Size:         size,
			Align:        1,
			FieldOffsets: offsets,
			FieldAligns:  aligns,
		}
	}

	size := 0
	align := 1
	for i := range fields {
		fl := e.LayoutOf(fields[i].Type)
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
	}
}

func (e *LayoutEngine) tupleLayout(id types.TypeID) TypeLayout {
	if e == nil || e.Types == nil {
		return TypeLayout{Size: 0, Align: 1}
	}
	info, ok := e.Types.TupleInfo(id)
	if !ok || info == nil || len(info.Elems) == 0 {
		return TypeLayout{Size: 0, Align: 1}
	}
	size := 0
	align := 1
	for _, elem := range info.Elems {
		el := e.LayoutOf(elem)
		a := el.Align
		if a <= 0 {
			a = 1
		}
		size = roundUp(size, a)
		size += el.Size
		align = maxInt(align, a)
	}
	size = roundUp(size, align)
	return TypeLayout{Size: size, Align: align}
}

func (e *LayoutEngine) tagUnionLayout(id types.TypeID) TypeLayout {
	if e == nil || e.Types == nil {
		return TypeLayout{Size: 0, Align: 1}
	}
	info, ok := e.Types.UnionInfo(id)
	if !ok || info == nil || len(info.Members) == 0 {
		return TypeLayout{Size: 0, Align: 1}
	}

	maxPayloadSize := 0
	payloadAlign := 1
	for _, m := range info.Members {
		switch m.Kind {
		case types.UnionMemberNothing:
			// payload: size 0 align 1
		case types.UnionMemberType:
			pl := e.LayoutOf(m.Type)
			maxPayloadSize = maxInt(maxPayloadSize, pl.Size)
			payloadAlign = maxInt(payloadAlign, pl.Align)
		case types.UnionMemberTag:
			if len(m.TagArgs) == 0 {
				continue
			}
			if len(m.TagArgs) == 1 {
				pl := e.LayoutOf(m.TagArgs[0])
				maxPayloadSize = maxInt(maxPayloadSize, pl.Size)
				payloadAlign = maxInt(payloadAlign, pl.Align)
				continue
			}
			// Multiple payload values: lay them out like a tuple.
			size := 0
			align := 1
			for _, a := range m.TagArgs {
				al := e.LayoutOf(a)
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
	}
}
