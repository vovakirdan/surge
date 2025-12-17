package vm

import (
	"fmt"
	"math"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/types"
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
		return MakeInt(c.IntValue, c.Type)
	case mir.ConstUint:
		intVal, err := safecast.Convert[int64](c.UintValue)
		if err != nil {
			// For now, saturate to max int64.
			return Value{Kind: VKInvalid}
		}
		return MakeInt(intVal, c.Type)
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
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		res, ok := AddInt64Checked(left.Int, right.Int)
		if !ok {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(res, left.TypeID), nil

	case ast.ExprBinarySub:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		res, ok := SubInt64Checked(left.Int, right.Int)
		if !ok {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(res, left.TypeID), nil

	case ast.ExprBinaryMul:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		res, ok := MulInt64Checked(left.Int, right.Int)
		if !ok {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(res, left.TypeID), nil

	case ast.ExprBinaryDiv:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		if right.Int == 0 {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, "division by zero")
		}
		return MakeInt(left.Int/right.Int, left.TypeID), nil

	case ast.ExprBinaryEq:
		if left.Kind != right.Kind {
			return Value{}, vm.eb.typeMismatch(left.Kind.String(), right.Kind.String())
		}
		var result bool
		switch left.Kind {
		case VKInt:
			result = left.Int == right.Int
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
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int < right.Int, types.NoTypeID), nil

	case ast.ExprBinaryLessEq:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int <= right.Int, types.NoTypeID), nil

	case ast.ExprBinaryGreater:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int > right.Int, types.NoTypeID), nil

	case ast.ExprBinaryGreaterEq:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int >= right.Int, types.NoTypeID), nil

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("binary op %s", op))
	}
}

// evalUnaryOp evaluates a unary operation.
func (vm *VM) evalUnaryOp(op ast.ExprUnaryOp, operand Value) (Value, *VMError) {
	switch op {
	case ast.ExprUnaryMinus:
		if operand.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", operand.Kind.String())
		}
		if operand.Int == math.MinInt64 {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(-operand.Int, operand.TypeID), nil

	case ast.ExprUnaryNot:
		if operand.Kind != VKBool {
			return Value{}, vm.eb.typeMismatch("bool", operand.Kind.String())
		}
		return MakeBool(!operand.Bool, operand.TypeID), nil

	case ast.ExprUnaryPlus:
		if operand.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", operand.Kind.String())
		}
		return operand, nil

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
	if idx.Kind != VKInt {
		return Value{}, vm.eb.typeMismatch("int", idx.Kind.String())
	}
	if idx.Int < 0 || idx.Int > int64(^uint(0)>>1) {
		return Value{}, vm.eb.outOfBounds(int(idx.Int), 0)
	}
	index := int(idx.Int)

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
	for _, f := range lit.Fields {
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
