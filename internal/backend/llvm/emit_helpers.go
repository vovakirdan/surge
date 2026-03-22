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
	case mir.OperandCopy, mir.OperandMove:
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

func (fe *funcEmitter) emitHandleOperandPtr(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	opType := op.Type
	if op.Kind == mir.OperandCopy || op.Kind == mir.OperandMove {
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
	if fe.isArrayOrMapType(baseType) || isStringLike(fe.emitter.types, baseType) || isBytesViewType(fe.emitter.types, baseType) {
		if fe.operandIsRef(op, opType) {
			val, ty, err := fe.emitOperand(op)
			if err != nil {
				return "", err
			}
			if ty != "ptr" {
				return "", fmt.Errorf("expected ptr handle, got %s", ty)
			}
			return val, nil
		}
		if ptr, err := fe.emitOperandAddr(op); err == nil {
			return ptr, nil
		}
		val, ty, err := fe.emitOperand(op)
		if err != nil {
			return "", err
		}
		if ty != "ptr" {
			return "", fmt.Errorf("expected ptr handle, got %s", ty)
		}
		return fe.emitHandleAddr(val), nil
	}
	val, ty, err := fe.emitOperand(op)
	if err != nil {
		return "", err
	}
	if ty != "ptr" {
		return "", fmt.Errorf("expected ptr handle, got %s", ty)
	}
	return val, nil
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
