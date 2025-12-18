package vm

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// evalRValue evaluates an rvalue to a Value.
func (vm *VM) evalRValue(frame *Frame, rv *mir.RValue) (Value, *VMError) {
	switch rv.Kind {
	case mir.RValueUse:
		return vm.evalOperand(frame, &rv.Use)

	case mir.RValueBinaryOp:
		left, vmErr := vm.evalOperand(frame, &rv.Binary.Left)
		if vmErr != nil {
			return Value{}, vmErr
		}
		right, vmErr := vm.evalOperand(frame, &rv.Binary.Right)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.evalBinaryOp(rv.Binary.Op, left, right)

	case mir.RValueUnaryOp:
		operand, vmErr := vm.evalOperand(frame, &rv.Unary.Operand)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.evalUnaryOp(rv.Unary.Op, operand)

	case mir.RValueCast:
		v, vmErr := vm.evalOperand(frame, &rv.Cast.Value)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.evalCast(v, rv.Cast.TargetTy)

	case mir.RValueIndex:
		obj, vmErr := vm.evalOperand(frame, &rv.Index.Object)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx, vmErr := vm.evalOperand(frame, &rv.Index.Index)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.evalIndex(obj, idx)

	case mir.RValueStructLit:
		return vm.evalStructLit(frame, &rv.StructLit)

	case mir.RValueArrayLit:
		return vm.evalArrayLit(frame, &rv.ArrayLit)

	case mir.RValueField:
		return vm.evalFieldAccess(frame, &rv.Field)

	case mir.RValueTagTest:
		return vm.evalTagTest(frame, &rv.TagTest)

	case mir.RValueTagPayload:
		return vm.evalTagPayload(frame, &rv.TagPayload)

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("rvalue kind %d", rv.Kind))
	}
}

func (vm *VM) evalCast(v Value, target types.TypeID) (Value, *VMError) {
	if target == types.NoTypeID {
		return v, nil
	}

	if vm.Types == nil {
		v.TypeID = target
		return v, nil
	}
	return vm.evalIntrinsicTo(v, target)
}

// evalOperand evaluates an operand to a Value.
func (vm *VM) evalOperand(frame *Frame, op *mir.Operand) (Value, *VMError) {
	switch op.Kind {
	case mir.OperandConst:
		return vm.evalConst(&op.Const), nil

	case mir.OperandCopy:
		if len(op.Place.Proj) == 0 {
			val, vmErr := vm.readLocal(frame, op.Place.Local)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return val, nil
		}
		loc, vmErr := vm.EvalPlace(frame, op.Place)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.loadLocationRaw(loc)

	case mir.OperandMove:
		if len(op.Place.Proj) == 0 {
			val, vmErr := vm.readLocal(frame, op.Place.Local)
			if vmErr != nil {
				return Value{}, vmErr
			}
			vm.moveLocal(frame, op.Place.Local)
			return val, nil
		}
		return Value{}, vm.eb.unimplemented("move from projected place")

	case mir.OperandAddrOf:
		loc, vmErr := vm.EvalPlace(frame, op.Place)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return MakeRef(loc, op.Type), nil

	case mir.OperandAddrOfMut:
		loc, vmErr := vm.EvalPlace(frame, op.Place)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if !loc.IsMut {
			return Value{}, vm.eb.invalidLocation("addr_of_mut of non-mutable location")
		}
		return MakeRefMut(loc, op.Type), nil

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("operand kind %d", op.Kind))
	}
}

// evalConst converts a MIR constant to a Value.
func (vm *VM) evalConst(c *mir.Const) Value {
	switch c.Kind {
	case mir.ConstInt:
		if kind, width, ok := vm.numericKind(c.Type); ok && kind == types.KindInt && width == types.WidthAny {
			var (
				i   bignum.BigInt
				err error
			)
			if c.Text != "" {
				i, err = bignum.ParseIntLiteral(c.Text)
			} else {
				i = bignum.IntFromInt64(c.IntValue)
			}
			if err != nil {
				vm.panic(PanicInvalidNumericConversion, fmt.Sprintf("invalid int literal %q: %v", c.Text, err))
			}
			return vm.makeBigInt(c.Type, i)
		}
		return MakeInt(c.IntValue, c.Type)
	case mir.ConstUint:
		if kind, width, ok := vm.numericKind(c.Type); ok && kind == types.KindUint && width == types.WidthAny {
			var (
				u   bignum.BigUint
				err error
			)
			if c.Text != "" {
				u, err = bignum.ParseUintLiteral(c.Text)
			} else {
				u = bignum.UintFromUint64(c.UintValue)
			}
			if err != nil {
				vm.panic(PanicInvalidNumericConversion, fmt.Sprintf("invalid uint literal %q: %v", c.Text, err))
			}
			return vm.makeBigUint(c.Type, u)
		}
		intVal, err := safecast.Convert[int64](c.UintValue)
		if err != nil {
			return Value{Kind: VKInvalid}
		}
		return MakeInt(intVal, c.Type)
	case mir.ConstFloat:
		if kind, width, ok := vm.numericKind(c.Type); ok && kind == types.KindFloat && width == types.WidthAny {
			if c.Text == "" {
				vm.panic(PanicFloatUnsupported, "missing float literal text")
			}
			f, err := bignum.ParseFloat(c.Text)
			if err != nil {
				vm.panic(PanicInvalidNumericConversion, fmt.Sprintf("invalid float literal %q: %v", c.Text, err))
			}
			return vm.makeBigFloat(c.Type, f)
		}
		vm.panic(PanicFloatUnsupported, "float constant evaluation is not supported")
		return Value{Kind: VKInvalid}
	case mir.ConstBool:
		return MakeBool(c.BoolValue, c.Type)
	case mir.ConstString:
		s := unescapeStringLiteral(c.StringValue)
		h := vm.Heap.AllocString(c.Type, s)
		return MakeHandleString(h, c.Type)
	case mir.ConstNothing:
		if c.Type != types.NoTypeID && vm.tagLayouts != nil {
			if layout, ok := vm.tagLayouts.Layout(vm.valueType(c.Type)); ok && layout != nil {
				if tc, ok := layout.CaseByName("nothing"); ok {
					h := vm.Heap.AllocTag(c.Type, tc.TagSym, nil)
					return MakeHandleTag(h, c.Type)
				}
			}
		}
		return MakeNothing()
	default:
		return Value{Kind: VKInvalid}
	}
}

func unescapeStringLiteral(raw string) string {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	var sb strings.Builder
	sb.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch != '\\' {
			sb.WriteByte(ch)
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		i++
		switch raw[i] {
		case '\\':
			sb.WriteByte('\\')
		case '"':
			sb.WriteByte('"')
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		default:
			sb.WriteByte(raw[i])
		}
	}
	return sb.String()
}
