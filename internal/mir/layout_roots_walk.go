package mir

import "fmt"

func (c *layoutRootCollector) walkInstr(instr *Instr) error { //nolint:gocyclo
	if instr == nil {
		return nil
	}
	switch instr.Kind {
	case InstrAssign:
		return c.walkRValue(&instr.Assign.Src)
	case InstrCall:
		switch instr.Call.Callee.Kind {
		case CalleeSym:
		case CalleeValue:
			if err := c.walkOperand(&instr.Call.Callee.Value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported CalleeKind %d", instr.Call.Callee.Kind)
		}
		for i := range instr.Call.Args {
			if err := c.walkOperand(&instr.Call.Args[i]); err != nil {
				return err
			}
		}
	case InstrDrop, InstrEndBorrow, InstrNop, InstrEnvelopeRelease:
	case InstrAwait:
		return c.walkOperand(&instr.Await.Task)
	case InstrSpawn:
		return c.walkOperand(&instr.Spawn.Value)
	case InstrCrossing:
		if err := c.addType(instr.Crossing.Destination.Type, RootValue); err != nil {
			return err
		}
		if err := c.walkOperand(&instr.Crossing.Destination.Value); err != nil {
			return err
		}
		if err := c.addType(instr.Crossing.State.TypeID, RootValue); err != nil {
			return err
		}
		for i := range instr.Crossing.State.Fields {
			if err := c.walkOperand(&instr.Crossing.State.Fields[i].Value); err != nil {
				return err
			}
		}
		for i := range instr.Crossing.Captures {
			capture := &instr.Crossing.Captures[i]
			if err := c.addType(capture.Type, RootValue); err != nil {
				return err
			}
			if err := c.walkOperand(&capture.Value); err != nil {
				return err
			}
		}
		for i := range instr.Crossing.RemoteOps {
			op := &instr.Crossing.RemoteOps[i]
			if err := c.addType(op.ReceiverType, RootValue); err != nil {
				return err
			}
			if err := c.walkOperand(&op.Receiver); err != nil {
				return err
			}
			if err := c.walkOperand(&op.Value); err != nil {
				return err
			}
		}
		if err := c.addType(instr.Crossing.ReceiverType, RootValue); err != nil {
			return err
		}
		if err := c.walkOperand(&instr.Crossing.Receiver); err != nil {
			return err
		}
		if err := c.addType(instr.Crossing.PayloadType, RootValue); err != nil {
			return err
		}
		if err := c.addType(instr.Crossing.ResultType, RootValue); err != nil {
			return err
		}
		return c.addType(instr.Crossing.HandleType, RootValue)
	case InstrBlocking:
		if err := c.addType(instr.Blocking.State.TypeID, RootValue); err != nil {
			return err
		}
		for i := range instr.Blocking.State.Fields {
			if err := c.walkOperand(&instr.Blocking.State.Fields[i].Value); err != nil {
				return err
			}
		}
	case InstrPoll:
		return c.walkOperand(&instr.Poll.Task)
	case InstrJoinAll:
		return c.walkOperand(&instr.JoinAll.Scope)
	case InstrChanSend:
		if err := c.walkOperand(&instr.ChanSend.Channel); err != nil {
			return err
		}
		return c.walkOperand(&instr.ChanSend.Value)
	case InstrChanRecv:
		return c.walkOperand(&instr.ChanRecv.Channel)
	case InstrNetWait:
		return c.walkOperand(&instr.NetWait.Handle)
	case InstrTimeout:
		if err := c.walkOperand(&instr.Timeout.Task); err != nil {
			return err
		}
		return c.walkOperand(&instr.Timeout.Ms)
	case InstrSelect:
		for i := range instr.Select.Arms {
			arm := &instr.Select.Arms[i]
			switch arm.Kind {
			case SelectArmTask, SelectArmChanRecv, SelectArmChanSend, SelectArmTimeout, SelectArmDefault:
			default:
				return fmt.Errorf("unsupported SelectArmKind %d", arm.Kind)
			}
			for _, operand := range []*Operand{&arm.Task, &arm.Channel, &arm.Value, &arm.Ms} {
				if err := c.walkOperand(operand); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("unsupported InstrKind %d", instr.Kind)
	}
	return nil
}

func (c *layoutRootCollector) walkOperand(operand *Operand) error {
	if operand == nil {
		return nil
	}
	switch operand.Kind {
	case OperandConst, OperandCopy, OperandMove, OperandAddrOf, OperandAddrOfMut, OperandRetain, OperandCopyValue:
	default:
		return fmt.Errorf("unsupported OperandKind %d", operand.Kind)
	}
	if err := c.addType(operand.Type, RootValue); err != nil {
		return err
	}
	if operand.Kind == OperandConst {
		return c.addType(operand.Const.Type, RootValue)
	}
	return nil
}

func (c *layoutRootCollector) walkRValue(value *RValue) error { //nolint:gocyclo
	if value == nil {
		return nil
	}
	switch value.Kind {
	case RValueUse:
		return c.walkOperand(&value.Use)
	case RValueUnaryOp:
		return c.walkOperand(&value.Unary.Operand)
	case RValueBinaryOp:
		if err := c.walkOperand(&value.Binary.Left); err != nil {
			return err
		}
		return c.walkOperand(&value.Binary.Right)
	case RValueCast:
		if err := c.walkOperand(&value.Cast.Value); err != nil {
			return err
		}
		return c.addType(value.Cast.TargetTy, RootValue)
	case RValueStructLit:
		if err := c.addType(value.StructLit.TypeID, RootValue); err != nil {
			return err
		}
		for i := range value.StructLit.Fields {
			if err := c.walkOperand(&value.StructLit.Fields[i].Value); err != nil {
				return err
			}
		}
	case RValueArrayLit:
		for i := range value.ArrayLit.Elems {
			if err := c.walkOperand(&value.ArrayLit.Elems[i]); err != nil {
				return err
			}
		}
	case RValueTupleLit:
		for i := range value.TupleLit.Elems {
			if err := c.walkOperand(&value.TupleLit.Elems[i]); err != nil {
				return err
			}
		}
	case RValueField:
		return c.walkOperand(&value.Field.Object)
	case RValueIndex:
		if err := c.walkOperand(&value.Index.Object); err != nil {
			return err
		}
		return c.walkOperand(&value.Index.Index)
	case RValueTagTest:
		return c.walkOperand(&value.TagTest.Value)
	case RValueTagPayload:
		return c.walkOperand(&value.TagPayload.Value)
	case RValueIterInit:
		return c.walkOperand(&value.IterInit.Iterable)
	case RValueIterNext:
		return c.walkOperand(&value.IterNext.Iter)
	case RValueTypeTest:
		if err := c.walkOperand(&value.TypeTest.Value); err != nil {
			return err
		}
		return c.addType(value.TypeTest.TargetTy, RootValue)
	case RValueHeirTest:
		if err := c.walkOperand(&value.HeirTest.Value); err != nil {
			return err
		}
		return c.addType(value.HeirTest.TargetTy, RootValue)
	default:
		return fmt.Errorf("unsupported RValueKind %d", value.Kind)
	}
	return nil
}

func (c *layoutRootCollector) walkTerm(term *Terminator) error {
	if term == nil {
		return nil
	}
	switch term.Kind {
	case TermNone, TermGoto, TermUnreachable:
	case TermReturn:
		if term.Return.HasValue {
			return c.walkOperand(&term.Return.Value)
		}
	case TermAsyncYield:
		return c.walkOperand(&term.AsyncYield.State)
	case TermAsyncReturn:
		if err := c.walkOperand(&term.AsyncReturn.State); err != nil {
			return err
		}
		if term.AsyncReturn.HasValue {
			return c.walkOperand(&term.AsyncReturn.Value)
		}
	case TermAsyncReturnCancelled:
		return c.walkOperand(&term.AsyncReturnCancelled.State)
	case TermIf:
		return c.walkOperand(&term.If.Cond)
	case TermSwitchTag:
		return c.walkOperand(&term.SwitchTag.Value)
	default:
		return fmt.Errorf("unsupported TermKind %d", term.Kind)
	}
	return nil
}
