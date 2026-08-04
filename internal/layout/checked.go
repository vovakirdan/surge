package layout

import (
	"math"

	"surge/internal/types"
)

func targetLimit(target Target) (uint64, bool) {
	switch {
	case target.AddressBits == 0 || target.AddressBits > 64:
		return 0, false
	case target.AddressBits == 64:
		return math.MaxUint64, true
	default:
		return (uint64(1) << target.AddressBits) - 1, true
	}
}

func validateTarget(target Target, id types.TypeID) *LayoutError {
	limit, ok := targetLimit(target)
	if !ok {
		return &LayoutError{Kind: ErrInvalidTarget, Type: id, Operation: "address bits", Value: uint64(target.AddressBits), Limit: 64}
	}
	if target.Triple == "" {
		return &LayoutError{Kind: ErrInvalidTarget, Type: id, Operation: "empty triple"}
	}
	if target.PointerSize == 0 || target.PointerSize > limit {
		return &LayoutError{Kind: ErrInvalidTarget, Type: id, Operation: "pointer size", Value: target.PointerSize, Limit: limit}
	}
	if err := validateAlign(target, id, target.PointerAlign, "pointer alignment"); err != nil {
		err.Kind = ErrInvalidTarget
		return err
	}
	if target.MaxABIAlign == 0 || !isPowerOfTwo(target.MaxABIAlign) || target.MaxABIAlign > limit {
		return &LayoutError{Kind: ErrInvalidTarget, Type: id, Operation: "maximum ABI alignment", Value: target.MaxABIAlign, Limit: limit}
	}
	return nil
}

func validateAlign(target Target, id types.TypeID, align uint64, operation string) *LayoutError {
	if align == 0 || !isPowerOfTwo(align) {
		return &LayoutError{Kind: ErrUnsupportedAlignment, Type: id, Operation: operation, Value: align, Limit: target.MaxABIAlign}
	}
	limit, ok := targetLimit(target)
	if !ok || align > limit || align > target.MaxABIAlign {
		if target.MaxABIAlign < limit || !ok {
			limit = target.MaxABIAlign
		}
		return &LayoutError{Kind: ErrUnsupportedAlignment, Type: id, Operation: operation, Value: align, Limit: limit}
	}
	return nil
}

func isPowerOfTwo(value uint64) bool { return value != 0 && value&(value-1) == 0 }

func checkedAdd(target Target, id types.TypeID, a, b uint64, operation string) (uint64, *LayoutError) {
	limit, ok := targetLimit(target)
	if !ok || a > limit || b > limit || b > limit-a {
		return 0, &LayoutError{Kind: ErrOverflow, Type: id, Operation: operation, Value: a, Limit: limit}
	}
	return a + b, nil
}

func checkedMul(target Target, id types.TypeID, a, b uint64, operation string) (uint64, *LayoutError) {
	limit, ok := targetLimit(target)
	if !ok || a > limit || b > limit || (a != 0 && b > limit/a) {
		return 0, &LayoutError{Kind: ErrOverflow, Type: id, Operation: operation, Value: a, Limit: limit}
	}
	return a * b, nil
}

func checkedRoundUp(target Target, id types.TypeID, value, align uint64, operation string) (uint64, *LayoutError) {
	if err := validateAlign(target, id, align, operation+" alignment"); err != nil {
		return 0, err
	}
	if value == 0 || align == 1 {
		return value, nil
	}
	remainder := value % align
	if remainder == 0 {
		return value, nil
	}
	return checkedAdd(target, id, value, align-remainder, operation)
}

func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func makePhysical(
	target Target,
	id types.TypeID,
	size uint64,
	align uint64,
	fieldOffsets []uint64,
	fieldAligns []uint64,
	unionCases []UnionCaseLayout,
) (PhysicalLayout, *LayoutError) {
	if err := validateAlign(target, id, align, "layout alignment"); err != nil {
		return errorLayout(), err
	}
	if size == 0 {
		return PhysicalLayout{
			state: StateZST,
			facts: PhysicalFacts{
				Align:        align,
				fieldOffsets: append([]uint64(nil), fieldOffsets...),
				fieldAligns:  append([]uint64(nil), fieldAligns...),
				unionCases:   cloneUnionCases(unionCases),
			},
		}, nil
	}
	stride, err := checkedRoundUp(target, id, size, align, "stride round-up")
	if err != nil {
		return errorLayout(), err
	}
	return PhysicalLayout{
		state: StateConcrete,
		facts: PhysicalFacts{
			Size:         size,
			Align:        align,
			Stride:       stride,
			fieldOffsets: append([]uint64(nil), fieldOffsets...),
			fieldAligns:  append([]uint64(nil), fieldAligns...),
			unionCases:   cloneUnionCases(unionCases),
		},
	}, nil
}
