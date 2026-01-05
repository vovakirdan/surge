package llvm

import (
	"fmt"

	"surge/internal/mir"
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
	case mir.InstrAwait:
		return fe.emitInstrAwait(ins)
	case mir.InstrSpawn:
		return fe.emitInstrSpawn(ins)
	case mir.InstrPoll:
		return fe.emitInstrPoll(ins)
	case mir.InstrJoinAll:
		return fe.emitInstrJoinAll(ins)
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
	case mir.RValueIterInit:
		return fe.emitIterInit(&rv.IterInit)
	case mir.RValueIterNext:
		return fe.emitIterNext(&rv.IterNext)
	case mir.RValueArrayLit, mir.RValueTupleLit:
		return "", "", fmt.Errorf("literal rvalue must be handled in assignment")
	default:
		return "", "", fmt.Errorf("unsupported rvalue kind %v", rv.Kind)
	}
}
