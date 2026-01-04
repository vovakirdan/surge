package llvm

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitInstr(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	switch ins.Kind {
	case mir.InstrAssign:
		return fe.emitAssign(ins)
	case mir.InstrCall:
		return fe.emitCall(ins)
	case mir.InstrDrop, mir.InstrEndBorrow, mir.InstrNop:
		return nil
	default:
		return fmt.Errorf("unsupported instruction kind %v", ins.Kind)
	}
}

func (fe *funcEmitter) emitAssign(ins *mir.Instr) error {
	if ins.Assign.Src.Kind == mir.RValueArrayLit {
		dstType, err := fe.placeBaseType(ins.Assign.Dst)
		if err != nil {
			return err
		}
		val, ty, err := fe.emitArrayLit(&ins.Assign.Src.ArrayLit, dstType)
		if err != nil {
			return err
		}
		ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			ty = dstTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
		return nil
	}
	if ins.Assign.Src.Kind == mir.RValueTupleLit {
		dstType, err := fe.placeBaseType(ins.Assign.Dst)
		if err != nil {
			return err
		}
		val, ty, err := fe.emitTupleLit(&ins.Assign.Src.TupleLit, dstType)
		if err != nil {
			return err
		}
		ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
		if err != nil {
			return err
		}
		if dstTy != ty {
			ty = dstTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
		return nil
	}
	if ins.Assign.Src.Kind == mir.RValueUse && ins.Assign.Src.Use.Kind == mir.OperandConst && ins.Assign.Src.Use.Const.Kind == mir.ConstNothing {
		dstType, err := fe.placeBaseType(ins.Assign.Dst)
		if err == nil && dstType != types.NoTypeID {
			op := ins.Assign.Src.Use
			if op.Type == types.NoTypeID || isNothingType(fe.emitter.types, op.Type) {
				op.Type = dstType
			}
			if op.Const.Type == types.NoTypeID || isNothingType(fe.emitter.types, op.Const.Type) {
				op.Const.Type = dstType
			}
			val, ty, err := fe.emitOperand(&op)
			if err != nil {
				return err
			}
			ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
			if err != nil {
				return err
			}
			if dstTy != ty {
				ty = dstTy
			}
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
			return nil
		}
	}
	val, ty, err := fe.emitRValue(&ins.Assign.Src)
	if err != nil {
		return err
	}
	ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
	if err != nil {
		return err
	}
	if dstTy != ty {
		ty = dstTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
	return nil
}

func (fe *funcEmitter) emitRValue(rv *mir.RValue) (val, ty string, err error) {
	if rv == nil {
		return "", "", fmt.Errorf("nil rvalue")
	}
	switch rv.Kind {
	case mir.RValueUse:
		return fe.emitOperand(&rv.Use)
	case mir.RValueStructLit:
		return fe.emitStructLit(&rv.StructLit)
	case mir.RValueField:
		return fe.emitFieldAccess(&rv.Field)
	case mir.RValueIndex:
		return fe.emitIndexAccess(&rv.Index)
	case mir.RValueUnaryOp:
		return fe.emitUnary(&rv.Unary)
	case mir.RValueBinaryOp:
		return fe.emitBinary(&rv.Binary)
	case mir.RValueCast:
		return fe.emitCast(&rv.Cast)
	case mir.RValueTagTest:
		return fe.emitTagTest(&rv.TagTest)
	case mir.RValueTagPayload:
		return fe.emitTagPayload(&rv.TagPayload)
	case mir.RValueArrayLit, mir.RValueTupleLit:
		return "", "", fmt.Errorf("literal rvalue must be handled in assignment")
	default:
		return "", "", fmt.Errorf("unsupported rvalue kind %v", rv.Kind)
	}
}

func (fe *funcEmitter) emitCast(c *mir.CastOp) (val, ty string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("nil cast")
	}
	val, srcTy, err := fe.emitOperand(&c.Value)
	if err != nil {
		return "", "", err
	}
	dstTy, err := llvmValueType(fe.emitter.types, c.TargetTy)
	if err != nil {
		return "", "", err
	}
	if srcTy == dstTy {
		return val, dstTy, nil
	}
	srcInfo, srcOK := intInfo(fe.emitter.types, c.Value.Type)
	dstInfo, dstOK := intInfo(fe.emitter.types, c.TargetTy)
	if srcOK && dstOK {
		if srcInfo.bits < dstInfo.bits {
			op := "zext"
			if srcInfo.signed {
				op = "sext"
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to %s\n", tmp, op, srcTy, val, dstTy)
			return tmp, dstTy, nil
		}
		if srcInfo.bits > dstInfo.bits {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = trunc %s %s to %s\n", tmp, srcTy, val, dstTy)
			return tmp, dstTy, nil
		}
		return val, dstTy, nil
	}
	return "", "", fmt.Errorf("unsupported cast to %s", dstTy)
}

func (fe *funcEmitter) emitUnary(op *mir.UnaryOp) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil unary op")
	}
	switch op.Op {
	case ast.ExprUnaryPlus:
		return fe.emitValueOperand(&op.Operand)
	case ast.ExprUnaryMinus:
		val, ty, err := fe.emitValueOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		info, ok := intInfo(fe.emitter.types, op.Operand.Type)
		if !ok || !info.signed {
			return "", "", fmt.Errorf("unsupported unary minus type")
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = sub %s 0, %s\n", tmp, ty, val)
		return tmp, ty, nil
	case ast.ExprUnaryNot:
		val, ty, err := fe.emitValueOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		if ty != "i1" {
			return "", "", fmt.Errorf("unary not requires i1, got %s", ty)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, val)
		return tmp, "i1", nil
	case ast.ExprUnaryDeref:
		ptrVal, _, err := fe.emitOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		elemType, ok := derefType(fe.emitter.types, op.Operand.Type)
		if !ok {
			return "", "", fmt.Errorf("unsupported deref type")
		}
		elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, elemLLVM, ptrVal)
		return tmp, elemLLVM, nil
	default:
		return "", "", fmt.Errorf("unsupported unary op %v", op.Op)
	}
}

func (fe *funcEmitter) emitBinary(op *mir.BinaryOp) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil binary op")
	}
	switch op.Op {
	case ast.ExprBinaryRange, ast.ExprBinaryRangeInclusive:
		leftVal, leftTy, leftErr := fe.emitValueOperand(&op.Left)
		if leftErr != nil {
			return "", "", leftErr
		}
		rightVal, rightTy, rightErr := fe.emitValueOperand(&op.Right)
		if rightErr != nil {
			return "", "", rightErr
		}
		start64, startErr := fe.coerceIntToI64(leftVal, leftTy, op.Left.Type)
		if startErr != nil {
			return "", "", startErr
		}
		end64, endErr := fe.coerceIntToI64(rightVal, rightTy, op.Right.Type)
		if endErr != nil {
			return "", "", endErr
		}
		inclusive := "0"
		if op.Op == ast.ExprBinaryRangeInclusive {
			inclusive = "1"
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_range_int_new(i64 %s, i64 %s, i1 %s)\n", tmp, start64, end64, inclusive)
		return tmp, "ptr", nil
	}
	if isStringLike(fe.emitter.types, op.Left.Type) || isStringLike(fe.emitter.types, op.Right.Type) {
		if !isStringLike(fe.emitter.types, op.Left.Type) || !isStringLike(fe.emitter.types, op.Right.Type) {
			return "", "", fmt.Errorf("mixed string and non-string operands")
		}
		return fe.emitStringBinary(op)
	}
	leftVal, leftTy, err := fe.emitValueOperand(&op.Left)
	if err != nil {
		return "", "", err
	}
	rightVal, rightTy, err := fe.emitValueOperand(&op.Right)
	if err != nil {
		return "", "", err
	}
	if leftTy != rightTy {
		return "", "", fmt.Errorf("binary operand type mismatch: %s vs %s", leftTy, rightTy)
	}

	switch op.Op {
	case ast.ExprBinaryLogicalAnd:
		if leftTy != "i1" {
			return "", "", fmt.Errorf("logical and requires i1, got %s", leftTy)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", tmp, leftVal, rightVal)
		return tmp, "i1", nil
	case ast.ExprBinaryLogicalOr:
		if leftTy != "i1" {
			return "", "", fmt.Errorf("logical or requires i1, got %s", leftTy)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", tmp, leftVal, rightVal)
		return tmp, "i1", nil
	case ast.ExprBinaryAdd, ast.ExprBinarySub, ast.ExprBinaryMul, ast.ExprBinaryDiv, ast.ExprBinaryMod,
		ast.ExprBinaryBitAnd, ast.ExprBinaryBitOr, ast.ExprBinaryBitXor, ast.ExprBinaryShiftLeft, ast.ExprBinaryShiftRight:
		info, ok := intInfo(fe.emitter.types, op.Left.Type)
		if !ok {
			return "", "", fmt.Errorf("unsupported numeric op on type")
		}
		var opcode string
		switch op.Op {
		case ast.ExprBinaryAdd:
			opcode = "add"
		case ast.ExprBinarySub:
			opcode = "sub"
		case ast.ExprBinaryMul:
			opcode = "mul"
		case ast.ExprBinaryDiv:
			if info.signed {
				opcode = "sdiv"
			} else {
				opcode = "udiv"
			}
		case ast.ExprBinaryMod:
			if info.signed {
				opcode = "srem"
			} else {
				opcode = "urem"
			}
		case ast.ExprBinaryBitAnd:
			opcode = "and"
		case ast.ExprBinaryBitOr:
			opcode = "or"
		case ast.ExprBinaryBitXor:
			opcode = "xor"
		case ast.ExprBinaryShiftLeft:
			opcode = "shl"
		case ast.ExprBinaryShiftRight:
			if info.signed {
				opcode = "ashr"
			} else {
				opcode = "lshr"
			}
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
		return tmp, leftTy, nil
	case ast.ExprBinaryEq, ast.ExprBinaryNotEq, ast.ExprBinaryLess, ast.ExprBinaryLessEq, ast.ExprBinaryGreater, ast.ExprBinaryGreaterEq:
		return fe.emitCompare(op, leftVal, rightVal, leftTy)
	default:
		return "", "", fmt.Errorf("unsupported binary op %v", op.Op)
	}
}

func (fe *funcEmitter) emitCompare(op *mir.BinaryOp, leftVal, rightVal, leftTy string) (val, ty string, err error) {
	info, ok := intInfo(fe.emitter.types, op.Left.Type)
	if !ok && leftTy != "ptr" {
		return "", "", fmt.Errorf("unsupported compare type")
	}
	if leftTy == "ptr" {
		pred := "eq"
		if op.Op == ast.ExprBinaryNotEq {
			pred = "ne"
		} else if op.Op != ast.ExprBinaryEq {
			return "", "", fmt.Errorf("unsupported pointer comparison %v", op.Op)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s ptr %s, %s\n", tmp, pred, leftVal, rightVal)
		return tmp, "i1", nil
	}
	pred := ""
	switch op.Op {
	case ast.ExprBinaryEq:
		pred = "eq"
	case ast.ExprBinaryNotEq:
		pred = "ne"
	case ast.ExprBinaryLess:
		if info.signed {
			pred = "slt"
		} else {
			pred = "ult"
		}
	case ast.ExprBinaryLessEq:
		if info.signed {
			pred = "sle"
		} else {
			pred = "ule"
		}
	case ast.ExprBinaryGreater:
		if info.signed {
			pred = "sgt"
		} else {
			pred = "ugt"
		}
	case ast.ExprBinaryGreaterEq:
		if info.signed {
			pred = "sge"
		} else {
			pred = "uge"
		}
	default:
		return "", "", fmt.Errorf("unsupported compare op %v", op.Op)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitStringBinary(op *mir.BinaryOp) (val, ty string, err error) {
	leftPtr, err := fe.emitOperandAddr(&op.Left)
	if err != nil {
		return "", "", err
	}
	rightPtr, err := fe.emitOperandAddr(&op.Right)
	if err != nil {
		return "", "", err
	}
	switch op.Op {
	case ast.ExprBinaryAdd:
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_concat(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
		return tmp, "ptr", nil
	case ast.ExprBinaryEq:
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
		return tmp, "i1", nil
	case ast.ExprBinaryNotEq:
		eqTmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", eqTmp, leftPtr, rightPtr)
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, eqTmp)
		return tmp, "i1", nil
	default:
		return "", "", fmt.Errorf("unsupported string op %v", op.Op)
	}
}

func (fe *funcEmitter) emitTagTest(tt *mir.TagTest) (val, ty string, errTagTest error) {
	if tt == nil {
		return "", "", fmt.Errorf("nil tag test")
	}
	var tagVal string
	tagVal, errTagTest = fe.emitTagDiscriminant(&tt.Value)
	if errTagTest != nil {
		return "", "", errTagTest
	}
	typeID := tt.Value.Type
	if typeID == types.NoTypeID && tt.Value.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(tt.Value.Place); err == nil {
			typeID = baseType
		}
	}
	var idx int
	idx, errTagTest = fe.emitter.tagCaseIndex(typeID, tt.TagName, symbols.NoSymbolID)
	if errTagTest != nil {
		return "", "", errTagTest
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", tmp, tagVal, idx)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitTagPayload(tp *mir.TagPayload) (val, ty string, errTagPayload error) {
	if tp == nil {
		return "", "", fmt.Errorf("nil tag payload")
	}
	typeID := tp.Value.Type
	if typeID == types.NoTypeID && tp.Value.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(tp.Value.Place); err == nil {
			typeID = baseType
		}
	}
	var meta mir.TagCaseMeta
	_, meta, errTagPayload = fe.emitter.tagCaseMeta(typeID, tp.TagName, symbols.NoSymbolID)
	if errTagPayload != nil {
		return "", "", errTagPayload
	}
	if tp.Index < 0 || tp.Index >= len(meta.PayloadTypes) {
		return "", "", fmt.Errorf("tag payload index out of range")
	}
	layoutInfo, errLayout := fe.emitter.layoutOf(typeID)
	if errLayout != nil {
		return "", "", errLayout
	}
	payloadOffsets, errPayloadOffsets := fe.emitter.payloadOffsets(meta.PayloadTypes)
	if errPayloadOffsets != nil {
		return "", "", errPayloadOffsets
	}
	offset := layoutInfo.PayloadOffset + payloadOffsets[tp.Index]
	var (
		basePtr string
		baseTy  string
	)
	basePtr, baseTy, errTagPayload = fe.emitValueOperand(&tp.Value)
	if errTagPayload != nil {
		return "", "", errTagPayload
	}
	if baseTy != "ptr" {
		return "", "", fmt.Errorf("tag payload requires ptr base, got %s", baseTy)
	}
	payloadType := meta.PayloadTypes[tp.Index]
	payloadLLVM, errPayloadLLVM := llvmValueType(fe.emitter.types, payloadType)
	if errPayloadLLVM != nil {
		return "", "", errPayloadLLVM
	}
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, basePtr, offset)
	val = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", val, payloadLLVM, bytePtr)
	return val, payloadLLVM, nil
}
