package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) nextTemp() string {
	fe.tmpID++
	return fmt.Sprintf("%%t%d", fe.tmpID)
}

func (fe *funcEmitter) nextInlineBlock() string {
	fe.inlineBlock++
	return fmt.Sprintf("bb.inline%d", fe.inlineBlock)
}

func boolValue(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func formatLLVMBytes(data []byte, arrayLen int) string {
	var sb strings.Builder
	sb.WriteString("c\"")
	for i := range arrayLen {
		b := byte(0)
		if i < len(data) {
			b = data[i]
		}
		fmt.Fprintf(&sb, "\\%02X", b)
	}
	sb.WriteString("\"")
	return sb.String()
}

func decodeStringLiteral(raw string) []byte {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch != '\\' {
			out = append(out, ch)
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		i++
		switch raw[i] {
		case '\\':
			out = append(out, '\\')
		case '"':
			out = append(out, '"')
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		default:
			out = append(out, raw[i])
		}
	}
	return out
}

func (fe *funcEmitter) operandIsRef(op *mir.Operand, opType types.TypeID) bool {
	if op == nil {
		return false
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		return true
	case mir.OperandCopy, mir.OperandCopyValue, mir.OperandRetain, mir.OperandMove:
		if op.Place.Kind == mir.PlaceLocal && int(op.Place.Local) >= 0 && int(op.Place.Local) < len(fe.f.Locals) {
			if len(op.Place.Proj) == 0 || op.Place.Proj[0].Kind != mir.PlaceProjDeref {
				flags := fe.f.Locals[op.Place.Local].Flags
				if flags&(mir.LocalFlagRef|mir.LocalFlagRefMut) != 0 {
					return true
				}
			}
		}
	}
	return isRefType(fe.emitter.types, opType)
}

// emitHandleOperandPtr addresses the storage an operand names, discarding what
// that address is aligned to. Use emitHandleOperandStorage at any site that
// indexes into a FIXED array through the address, because a fixed array's
// elements are only as aligned as the array itself is.
func (fe *funcEmitter) emitHandleOperandPtr(op *mir.Operand) (string, error) {
	ptr, _, err := fe.emitHandleOperandStorage(op)
	return ptr, err
}

// emitHandleOperandStorage addresses the storage an operand names and reports
// what that address is really aligned to.
func (fe *funcEmitter) emitHandleOperandStorage(op *mir.Operand) (ptr string, align uint64, err error) {
	if op == nil {
		return "", 0, fmt.Errorf("nil operand")
	}
	opType := op.Type
	if op.Kind == mir.OperandCopy || op.Kind == mir.OperandCopyValue || op.Kind == mir.OperandMove {
		if base, err := fe.placeBaseType(op.Place); err == nil {
			opType = base
		}
	}
	baseType := opType
	if isRefType(fe.emitter.types, baseType) {
		if next, ok := derefType(fe.emitter.types, baseType); ok {
			baseType = next
		}
	}
	if isBytesViewType(fe.emitter.types, baseType) {
		// A bytes view is stored inline, so the caller wants the address of
		// the view itself. Spilling a loaded word into a fresh slot would
		// hand back the address of the view's first field instead.
		if fe.operandIsRef(op, opType) {
			return fe.emitRefOperandStorage(op, baseType, "ptr to a bytes view")
		}
		return fe.emitOperandStorage(op)
	}
	if fe.isArrayOrMapType(baseType) || isStringLike(fe.emitter.types, baseType) {
		if fe.operandIsRef(op, opType) {
			return fe.emitRefOperandStorage(op, baseType, "ptr handle")
		}
		if ptr, align, err := fe.emitOperandStorage(op); err == nil {
			return ptr, align, nil
		}
		val, ty, err := fe.emitOperand(op)
		if err != nil {
			return "", 0, err
		}
		if ty != "ptr" {
			return "", 0, fmt.Errorf("expected ptr handle, got %s", ty)
		}
		// emitHandleAddr reserves the spill slot, so its alignment is a fact
		// this emission just wrote, not one inferred from a type.
		return fe.emitHandleAddr(val), alignPtr, nil
	}
	return fe.emitRefOperandStorage(op, baseType, "ptr handle")
}

// emitRefOperandStorage addresses what a reference-shaped operand points at,
// and reports what that address is aligned to.
//
// The two shapes part company on where the address comes from. `&place` is not
// a pointer read out of anywhere: it IS the place walk, so the walk's own
// folded answer describes it exactly — and that is the difference between
// `p.cells[i] = v` inside a `@packed` container claiming align 1 and claiming
// the element type's 8. A reference VALUE — a ref-typed local, a parameter —
// has no walk behind it here and falls back on its pointee type.
func (fe *funcEmitter) emitRefOperandStorage(op *mir.Operand, baseType types.TypeID, want string) (ptr string, align uint64, err error) {
	if op != nil && (op.Kind == mir.OperandAddrOf || op.Kind == mir.OperandAddrOfMut) {
		placePtr, _, placeAlign, placeErr := fe.emitPlaceStorage(op.Place)
		if placeErr != nil {
			return "", 0, placeErr
		}
		return placePtr, placeAlign, nil
	}
	val, ty, err := fe.emitOperand(op)
	if err != nil {
		return "", 0, err
	}
	if ty != "ptr" {
		return "", 0, fmt.Errorf("expected %s, got %s", want, ty)
	}
	return val, fe.borrowedValueAlign(baseType), nil
}

// borrowedValueAlign is what a pointer that arrived as a VALUE — a borrow, a
// handle, a runtime word — points at.
//
// A borrow points at a whole value, so the pointee is aligned to that value's
// own alignment; this is the same reading emitPlaceStorage takes at a deref,
// and it is written once here so the two cannot drift. A borrow of a `@packed`
// member is the one shape it does not describe, and whether the language hands
// one out is an open question rather than something this function may assume
// away — see RV2-DEBT-226's borrow finding. A type whose alignment cannot be
// read answers 1, which under-claims and is always safe.
func (fe *funcEmitter) borrowedValueAlign(baseType types.TypeID) uint64 {
	if fe == nil || fe.emitter == nil {
		return 1
	}
	llvmTy, err := fe.emitter.llvmValueType(baseType)
	if err != nil {
		return 1
	}
	align, err := fe.emitter.storageAlignOf(baseType, llvmTy)
	if err != nil || align == 0 {
		return 1
	}
	return align
}

func (fe *funcEmitter) isArrayOrMapType(id types.TypeID) bool {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil || id == types.NoTypeID {
		return false
	}
	id = resolveValueType(fe.emitter.types, id)
	if _, ok := fe.emitter.types.ArrayInfo(id); ok {
		return true
	}
	if _, _, ok := fe.emitter.types.ArrayFixedInfo(id); ok {
		return true
	}
	if _, _, ok := fe.emitter.types.MapInfo(id); ok {
		return true
	}
	if tt, ok := fe.emitter.types.Lookup(id); ok && tt.Kind == types.KindArray {
		return true
	}
	return false
}
