package mir

import "fmt"

// forEachPlace visits every PLACE a function mentions, in place, so a caller can
// rewrite one.
//
// It exists because a pass that redirects a local has to reach ALL of its uses:
// one missed instruction leaves that use addressing the old storage while every
// other use addresses the new one, and the two silently disagree. There is no
// crash and no diagnostic — the program simply reads a value nobody wrote. That
// is the failure mode the async resident promotion cannot tolerate, since the
// whole point of a resident is that a parent and the child it lent a place to
// see ONE storage.
//
// So this walker is exhaustive by construction and by test: every kind switch
// ends in a `default` that reports the kind it did not know, and
// place_walk_test.go walks every kind by value against the enum sentinels
// (instrKindCount and friends) so a kind added later cannot be forgotten here.
// A silent miss is turned into a loud error and then into a failing test.
//
// The layout root collector next door walks TYPES and deliberately does not
// visit `Assign.Dst` or any bare place, so it cannot serve this purpose; the two
// walks answer different questions and are kept apart rather than merged into
// one that answers neither cleanly.
func forEachPlace(f *Func, visit func(*Place)) error {
	if f == nil || visit == nil {
		return nil
	}
	for bi := range f.Blocks {
		block := &f.Blocks[bi]
		for ii := range block.Instrs {
			if err := instrPlaces(&block.Instrs[ii], visit); err != nil {
				return err
			}
		}
		if err := termPlaces(&block.Term, visit); err != nil {
			return err
		}
	}
	return nil
}

// operandPlace visits the place an operand reads. A constant holds none.
func operandPlace(op *Operand, visit func(*Place)) error {
	if op == nil {
		return nil
	}
	switch op.Kind {
	case OperandConst:
		return nil
	case OperandCopy, OperandMove, OperandAddrOf, OperandAddrOfMut, OperandRetain, OperandCopyValue:
		visit(&op.Place)
		return nil
	default:
		return fmt.Errorf("mir: place walk: unsupported OperandKind %d", op.Kind)
	}
}

func operandsPlaces(ops []Operand, visit func(*Place)) error {
	for i := range ops {
		if err := operandPlace(&ops[i], visit); err != nil {
			return err
		}
	}
	return nil
}

func structLitPlaces(lit *StructLit, visit func(*Place)) error {
	if lit == nil {
		return nil
	}
	for i := range lit.Fields {
		if err := operandPlace(&lit.Fields[i].Value, visit); err != nil {
			return err
		}
	}
	return nil
}

func rvaluePlaces(rv *RValue, visit func(*Place)) error { //nolint:gocyclo
	if rv == nil {
		return nil
	}
	switch rv.Kind {
	case RValueUse:
		return operandPlace(&rv.Use, visit)
	case RValueUnaryOp:
		return operandPlace(&rv.Unary.Operand, visit)
	case RValueBinaryOp:
		if err := operandPlace(&rv.Binary.Left, visit); err != nil {
			return err
		}
		return operandPlace(&rv.Binary.Right, visit)
	case RValueCast:
		return operandPlace(&rv.Cast.Value, visit)
	case RValueStructLit:
		return structLitPlaces(&rv.StructLit, visit)
	case RValueArrayLit:
		return operandsPlaces(rv.ArrayLit.Elems, visit)
	case RValueTupleLit:
		return operandsPlaces(rv.TupleLit.Elems, visit)
	case RValueField:
		return operandPlace(&rv.Field.Object, visit)
	case RValueIndex:
		if err := operandPlace(&rv.Index.Object, visit); err != nil {
			return err
		}
		return operandPlace(&rv.Index.Index, visit)
	case RValueTagTest:
		return operandPlace(&rv.TagTest.Value, visit)
	case RValueTagPayload:
		return operandPlace(&rv.TagPayload.Value, visit)
	case RValueIterInit:
		return operandPlace(&rv.IterInit.Iterable, visit)
	case RValueIterNext:
		return operandPlace(&rv.IterNext.Iter, visit)
	case RValueTypeTest:
		return operandPlace(&rv.TypeTest.Value, visit)
	case RValueHeirTest:
		return operandPlace(&rv.HeirTest.Value, visit)
	default:
		return fmt.Errorf("mir: place walk: unsupported RValueKind %d", rv.Kind)
	}
}

func crossingPlaces(c *CrossingInstr, visit func(*Place)) error {
	visit(&c.Dst)
	visit(&c.Pending)
	visit(&c.Handle)
	if err := operandPlace(&c.Destination.Value, visit); err != nil {
		return err
	}
	if err := operandPlace(&c.Receiver, visit); err != nil {
		return err
	}
	if err := structLitPlaces(&c.State, visit); err != nil {
		return err
	}
	for i := range c.Captures {
		if err := operandPlace(&c.Captures[i].Value, visit); err != nil {
			return err
		}
	}
	for i := range c.RemoteOps {
		op := &c.RemoteOps[i]
		if err := operandPlace(&op.Receiver, visit); err != nil {
			return err
		}
		if err := operandPlace(&op.Value, visit); err != nil {
			return err
		}
		if op.ReturnPlace != nil {
			visit(op.ReturnPlace)
		}
	}
	return nil
}

func selectPlaces(s *SelectInstr, visit func(*Place)) error {
	visit(&s.Dst)
	for i := range s.Arms {
		arm := &s.Arms[i]
		switch arm.Kind {
		case SelectArmTask, SelectArmChanRecv, SelectArmChanSend, SelectArmTimeout, SelectArmDefault:
		default:
			return fmt.Errorf("mir: place walk: unsupported SelectArmKind %d", arm.Kind)
		}
		// Every arm kind leaves the operands it does not use zero, and a zero
		// operand is a constant, which holds no place -- so the four are visited
		// unconditionally rather than per kind. The switch above is still what
		// makes a new arm kind loud here.
		for _, op := range []*Operand{&arm.Task, &arm.Channel, &arm.Value, &arm.Ms} {
			if err := operandPlace(op, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func instrPlaces(instr *Instr, visit func(*Place)) error { //nolint:gocyclo
	if instr == nil {
		return nil
	}
	switch instr.Kind {
	case InstrAssign:
		visit(&instr.Assign.Dst)
		return rvaluePlaces(&instr.Assign.Src, visit)
	case InstrCall:
		if instr.Call.HasDst {
			visit(&instr.Call.Dst)
		}
		switch instr.Call.Callee.Kind {
		case CalleeSym:
		case CalleeValue:
			if err := operandPlace(&instr.Call.Callee.Value, visit); err != nil {
				return err
			}
		default:
			return fmt.Errorf("mir: place walk: unsupported CalleeKind %d", instr.Call.Callee.Kind)
		}
		return operandsPlaces(instr.Call.Args, visit)
	case InstrDrop:
		visit(&instr.Drop.Place)
		return nil
	case InstrEndBorrow:
		visit(&instr.EndBorrow.Place)
		return nil
	case InstrEnvelopeRelease:
		visit(&instr.EnvelopeRelease.Place)
		return nil
	case InstrAwait:
		visit(&instr.Await.Dst)
		return operandPlace(&instr.Await.Task, visit)
	case InstrSpawn:
		visit(&instr.Spawn.Dst)
		return operandPlace(&instr.Spawn.Value, visit)
	case InstrChanRecv:
		visit(&instr.ChanRecv.Dst)
		return operandPlace(&instr.ChanRecv.Channel, visit)
	case InstrCrossing:
		return crossingPlaces(&instr.Crossing, visit)
	case InstrBlocking:
		visit(&instr.Blocking.Dst)
		return structLitPlaces(&instr.Blocking.State, visit)
	case InstrPoll:
		visit(&instr.Poll.Dst)
		return operandPlace(&instr.Poll.Task, visit)
	case InstrJoinAll:
		visit(&instr.JoinAll.Dst)
		return operandPlace(&instr.JoinAll.Scope, visit)
	case InstrChanSend:
		if err := operandPlace(&instr.ChanSend.Channel, visit); err != nil {
			return err
		}
		return operandPlace(&instr.ChanSend.Value, visit)
	case InstrNetWait:
		return operandPlace(&instr.NetWait.Handle, visit)
	case InstrTimeout:
		visit(&instr.Timeout.Dst)
		if err := operandPlace(&instr.Timeout.Task, visit); err != nil {
			return err
		}
		return operandPlace(&instr.Timeout.Ms, visit)
	case InstrSelect:
		return selectPlaces(&instr.Select, visit)
	case InstrNop:
		return nil
	default:
		return fmt.Errorf("mir: place walk: unsupported InstrKind %d", instr.Kind)
	}
}

func termPlaces(term *Terminator, visit func(*Place)) error {
	if term == nil {
		return nil
	}
	switch term.Kind {
	case TermNone, TermGoto, TermUnreachable:
		return nil
	case TermReturn:
		if term.Return.HasValue {
			return operandPlace(&term.Return.Value, visit)
		}
		return nil
	case TermAsyncYield:
		return operandPlace(&term.AsyncYield.State, visit)
	case TermAsyncReturn:
		if err := operandPlace(&term.AsyncReturn.State, visit); err != nil {
			return err
		}
		if term.AsyncReturn.HasValue {
			return operandPlace(&term.AsyncReturn.Value, visit)
		}
		return nil
	case TermAsyncReturnCancelled:
		return operandPlace(&term.AsyncReturnCancelled.State, visit)
	case TermIf:
		return operandPlace(&term.If.Cond, visit)
	case TermSwitchTag:
		return operandPlace(&term.SwitchTag.Value, visit)
	default:
		return fmt.Errorf("mir: place walk: unsupported TermKind %d", term.Kind)
	}
}
