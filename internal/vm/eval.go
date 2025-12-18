package vm

import (
	"fmt"
	"math"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
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

// evalBinaryOp evaluates a binary operation.
func (vm *VM) evalBinaryOp(op ast.ExprBinaryOp, left, right Value) (Value, *VMError) {
	switch op {
	case ast.ExprBinaryAdd:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.IntAdd(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, res), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.UintAdd(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatAdd(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			res, ok := AddInt64Checked(left.Int, right.Int)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(res, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinarySub:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.IntSub(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, res), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.UintSub(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatSub(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			res, ok := SubInt64Checked(left.Int, right.Int)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(res, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryMul:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.IntMul(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, res), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.UintMul(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatMul(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			res, ok := MulInt64Checked(left.Int, right.Int)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(res, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryDiv:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			q, _, err := bignum.IntDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, q), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			q, _, err := bignum.UintDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, q), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatDiv(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			if right.Int == 0 {
				return Value{}, vm.eb.divisionByZero()
			}
			return MakeInt(left.Int/right.Int, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryMod:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			_, r, err := bignum.IntDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, r), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			_, r, err := bignum.UintDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, r), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			if right.Int == 0 {
				return Value{}, vm.eb.divisionByZero()
			}
			return MakeInt(left.Int%right.Int, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryEq:
		if left.Kind != right.Kind {
			return Value{}, vm.eb.typeMismatch(left.Kind.String(), right.Kind.String())
		}
		var result bool
		switch left.Kind {
		case VKInt:
			result = left.Int == right.Int
		case VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) == 0
		case VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) == 0
		case VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) == 0
		case VKBool:
			result = left.Bool == right.Bool
		case VKHandleString:
			lObj := vm.Heap.Get(left.H)
			rObj := vm.Heap.Get(right.H)
			if lObj == nil || rObj == nil {
				return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
			}
			result = lObj.Str == rObj.Str
		default:
			result = left.H == right.H
		}
		return MakeBool(result, types.NoTypeID), nil

	case ast.ExprBinaryNotEq:
		if left.Kind != right.Kind {
			return Value{}, vm.eb.typeMismatch(left.Kind.String(), right.Kind.String())
		}
		var result bool
		switch left.Kind {
		case VKInt:
			result = left.Int != right.Int
		case VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) != 0
		case VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) != 0
		case VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) != 0
		case VKBool:
			result = left.Bool != right.Bool
		case VKHandleString:
			lObj := vm.Heap.Get(left.H)
			rObj := vm.Heap.Get(right.H)
			if lObj == nil || rObj == nil {
				return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
			}
			result = lObj.Str != rObj.Str
		default:
			result = left.H != right.H
		}
		return MakeBool(result, types.NoTypeID), nil

	case ast.ExprBinaryLess:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) < 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) < 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) < 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int < right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryLessEq:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) <= 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) <= 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) <= 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int <= right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryGreater:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) > 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) > 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) > 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int > right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryGreaterEq:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) >= 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) >= 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) >= 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int >= right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("binary op %s", op))
	}
}

// evalUnaryOp evaluates a unary operation.
func (vm *VM) evalUnaryOp(op ast.ExprUnaryOp, operand Value) (Value, *VMError) {
	switch op {
	case ast.ExprUnaryMinus:
		switch operand.Kind {
		case VKBigInt:
			i, vmErr := vm.mustBigInt(operand)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigInt(operand.TypeID, i.Negated()), nil
		case VKBigFloat:
			f, vmErr := vm.mustBigFloat(operand)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigFloat(operand.TypeID, bignum.FloatNeg(f)), nil
		case VKInt:
			if operand.Int == math.MinInt64 {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(-operand.Int, operand.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", operand.Kind.String())
		}

	case ast.ExprUnaryNot:
		if operand.Kind != VKBool {
			return Value{}, vm.eb.typeMismatch("bool", operand.Kind.String())
		}
		return MakeBool(!operand.Bool, operand.TypeID), nil

	case ast.ExprUnaryPlus:
		switch operand.Kind {
		case VKBigInt, VKBigUint, VKBigFloat, VKInt:
			return operand, nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", operand.Kind.String())
		}

	case ast.ExprUnaryDeref:
		switch operand.Kind {
		case VKRef, VKRefMut:
			return vm.loadLocationRaw(operand.Loc)
		default:
			return Value{}, vm.eb.derefOnNonRef(operand.Kind.String())
		}

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("unary op %s", op))
	}
}

func (vm *VM) evalArrayLit(frame *Frame, lit *mir.ArrayLit) (Value, *VMError) {
	if lit == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil array literal")
	}
	elems := make([]Value, 0, len(lit.Elems))
	for i := range lit.Elems {
		v, vmErr := vm.evalOperand(frame, &lit.Elems[i])
		if vmErr != nil {
			return Value{}, vmErr
		}
		elems = append(elems, v)
	}

	h := vm.Heap.AllocArray(types.NoTypeID, elems)
	return MakeHandleArray(h, types.NoTypeID), nil
}

// evalIndex evaluates an index operation.
func (vm *VM) evalIndex(obj, idx Value) (Value, *VMError) {
	maxIndex := int(^uint(0) >> 1)
	maxInt := int64(maxIndex)
	maxUint := uint64(^uint(0) >> 1)
	var index int
	switch idx.Kind {
	case VKInt:
		if idx.Int < 0 || idx.Int > maxInt {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		n, err := safecast.Conv[int](idx.Int)
		if err != nil {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		index = n
	case VKBigInt:
		i, vmErr := vm.mustBigInt(idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > maxInt {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		index = ni
	case VKBigUint:
		u, vmErr := vm.mustBigUint(idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > maxUint {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		index = ni
	default:
		return Value{}, vm.eb.typeMismatch("int", idx.Kind.String())
	}

	if obj.Kind != VKHandleArray {
		return Value{}, vm.eb.typeMismatch("array", obj.Kind.String())
	}
	arrObj := vm.Heap.Get(obj.H)
	if arrObj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid array handle")
	}
	if arrObj.Kind != OKArray {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("expected array handle, got %v", arrObj.Kind))
	}
	if index < 0 || index >= len(arrObj.Arr) {
		return Value{}, vm.eb.outOfBounds(index, len(arrObj.Arr))
	}
	return vm.cloneForShare(arrObj.Arr[index])
}

func (vm *VM) evalStructLit(frame *Frame, lit *mir.StructLit) (Value, *VMError) {
	if lit == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil struct literal")
	}
	layout, vmErr := vm.layouts.Struct(lit.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, len(layout.FieldNames))
	for i := range fields {
		fields[i] = Value{Kind: VKInvalid}
	}
	for i := range lit.Fields {
		f := &lit.Fields[i]
		val, vmErr := vm.evalOperand(frame, &f.Value)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx, ok := layout.IndexByName[f.Name]
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("struct type#%d has no field %q", layout.TypeID, f.Name))
		}
		fields[idx] = val
	}
	h := vm.Heap.AllocStruct(layout.TypeID, fields)
	return MakeHandleStruct(h, lit.TypeID), nil
}

func (vm *VM) evalFieldAccess(frame *Frame, fa *mir.FieldAccess) (Value, *VMError) {
	if fa == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil field access")
	}
	if fa.FieldIdx >= 0 {
		return Value{}, vm.eb.unimplemented("tuple field access")
	}
	if fa.FieldName == "" {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "missing field name")
	}
	obj, vmErr := vm.evalOperand(frame, &fa.Object)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if obj.Kind != VKHandleStruct {
		return Value{}, vm.eb.typeMismatch("struct", obj.Kind.String())
	}
	sobj := vm.Heap.Get(obj.H)
	if sobj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid struct handle")
	}
	if sobj.Kind != OKStruct {
		return Value{}, vm.eb.typeMismatch("struct", fmt.Sprintf("%v", sobj.Kind))
	}
	layout, vmErr := vm.layouts.Struct(sobj.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	idx, ok := layout.IndexByName[fa.FieldName]
	if !ok {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("unknown field %q on type#%d", fa.FieldName, sobj.TypeID))
	}
	if idx < 0 || idx >= len(sobj.Fields) {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("field index %d out of bounds for type#%d", idx, sobj.TypeID))
	}
	return vm.cloneForShare(sobj.Fields[idx])
}

func (vm *VM) cloneForShare(v Value) (Value, *VMError) {
	switch v.Kind {
	case VKHandleString:
		obj := vm.Heap.Get(v.H)
		if obj == nil {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
		}
		h := vm.Heap.AllocString(obj.TypeID, obj.Str)
		return MakeHandleString(h, obj.TypeID), nil
	case VKHandleArray, VKHandleStruct:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("cloning %s", v.Kind))
	default:
		return v, nil
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
