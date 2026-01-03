package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
)

func (fe *funcEmitter) emitTerminator(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	switch term.Kind {
	case mir.TermReturn:
		if term.Return.HasValue {
			val, ty, err := fe.emitOperand(&term.Return.Value)
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  ret %s %s\n", ty, val)
			return nil
		}
		fmt.Fprintf(&fe.emitter.buf, "  ret void\n")
		return nil
	case mir.TermGoto:
		fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", term.Goto.Target)
		return nil
	case mir.TermIf:
		condVal, condTy, err := fe.emitOperand(&term.If.Cond)
		if err != nil {
			return err
		}
		if condTy != "i1" {
			return fmt.Errorf("if condition must be i1, got %s", condTy)
		}
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb%d\n", condVal, term.If.Then, term.If.Else)
		return nil
	case mir.TermSwitchTag:
		return fe.emitSwitchTag(&term.SwitchTag)
	case mir.TermUnreachable:
		fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
		return nil
	default:
		return fmt.Errorf("unsupported terminator kind %v", term.Kind)
	}
}

func (fe *funcEmitter) emitSwitchTag(term *mir.SwitchTagTerm) error {
	if term == nil {
		return nil
	}
	tagVal, err := fe.emitTagDiscriminant(&term.Value)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%bb%d [\n", tagVal, term.Default)
	for _, c := range term.Cases {
		idx, err := fe.emitter.tagCaseIndex(term.Value.Type, c.TagName, symbols.NoSymbolID)
		if err != nil {
			return err
		}
		fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%bb%d\n", idx, c.Target)
	}
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	return nil
}

func (fe *funcEmitter) emitOperand(op *mir.Operand) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandConst:
		return fe.emitConst(&op.Const)
	case mir.OperandCopy, mir.OperandMove:
		ptr, ty, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, ty, ptr)
		return tmp, ty, nil
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		return ptr, "ptr", nil
	default:
		return "", "", fmt.Errorf("unsupported operand kind %v", op.Kind)
	}
}

func (fe *funcEmitter) emitValueOperand(op *mir.Operand) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		elemType, ok := derefType(fe.emitter.types, op.Type)
		if !ok {
			return "", "", fmt.Errorf("unsupported address-of operand type")
		}
		llvmTy, err := llvmValueType(fe.emitter.types, elemType)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, llvmTy, ptr)
		return tmp, llvmTy, nil
	default:
		return fe.emitOperand(op)
	}
}

func (fe *funcEmitter) emitOperandAddr(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut, mir.OperandCopy, mir.OperandMove:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", err
		}
		return ptr, nil
	case mir.OperandConst:
		val, ty, err := fe.emitConst(&op.Const)
		if err != nil {
			return "", err
		}
		ptr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca %s\n", ptr, ty)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
		return ptr, nil
	default:
		return "", fmt.Errorf("unsupported operand kind %v", op.Kind)
	}
}

func (fe *funcEmitter) emitConst(c *mir.Const) (val, ty string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("nil const")
	}
	switch c.Kind {
	case mir.ConstInt:
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%d", c.IntValue), ty, nil
	case mir.ConstUint:
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%d", c.UintValue), ty, nil
	case mir.ConstBool:
		return boolValue(c.BoolValue), "i1", nil
	case mir.ConstNothing:
		if fe.emitter.hasTagLayout(c.Type) {
			ptr, err := fe.emitTagValue(c.Type, "nothing", symbols.NoSymbolID, nil)
			if err != nil {
				return "", "", err
			}
			return ptr, "ptr", nil
		}
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		return "0", ty, nil
	case mir.ConstString:
		return fe.emitStringConst(c.StringValue)
	default:
		return "", "", fmt.Errorf("unsupported const kind %v", c.Kind)
	}
}

func (fe *funcEmitter) emitStringConst(raw string) (val, ty string, err error) {
	sc, ok := fe.emitter.stringConsts[raw]
	if !ok {
		return "", "", fmt.Errorf("missing string const %q", raw)
	}
	ptrTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i8], ptr @%s, i64 0, i64 0\n", ptrTmp, sc.arrayLen, sc.globalName)
	handleTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_bytes(ptr %s, i64 %d)\n", handleTmp, ptrTmp, sc.dataLen)
	return handleTmp, "ptr", nil
}
