package mir

import "surge/internal/sema"

// blockLiveness holds use/def/in/out sets for liveness analysis.
type blockLiveness struct {
	use localSet
	def localSet
	in  localSet
	out localSet
}

// computeLiveness computes liveness information for all blocks in a function.
func computeLiveness(f *Func) []blockLiveness {
	if f == nil {
		return nil
	}
	info := make([]blockLiveness, len(f.Blocks))
	for i := range f.Blocks {
		use, def := computeBlockUseDef(&f.Blocks[i])
		info[i].use = use
		info[i].def = def
	}

	changed := true
	for changed {
		changed = false
		for i := len(f.Blocks) - 1; i >= 0; i-- {
			out := localSet{}
			for _, succ := range succBlocks(f, BlockID(i), false) { //nolint:gosec // bounded by block count
				out = unionSet(out, info[succ].in)
			}
			in := unionSet(cloneSet(info[i].use), subtractSet(out, info[i].def))

			if !setEqual(out, info[i].out) || !setEqual(in, info[i].in) {
				info[i].out = out
				info[i].in = in
				changed = true
			}
		}
	}
	return info
}

func computeBlockUseDef(bb *Block) (use, def localSet) {
	use = localSet{}
	def = localSet{}
	if bb == nil {
		return use, def
	}
	addUse := func(id LocalID) {
		if id == NoLocalID {
			return
		}
		if def.has(id) {
			return
		}
		use.add(id)
	}
	addDef := func(id LocalID) {
		if id == NoLocalID {
			return
		}
		def.add(id)
	}

	for i := range bb.Instrs {
		ins := &bb.Instrs[i]
		switch ins.Kind {
		case InstrAssign:
			addUsesFromRValue(&ins.Assign.Src, addUse, addDef)
			addUsesFromPlaceWrite(ins.Assign.Dst, addUse)
			addDefFromPlace(ins.Assign.Dst, addDef)
		case InstrCall:
			if ins.Call.Callee.Kind == CalleeValue {
				addUsesFromOperand(&ins.Call.Callee.Value, addUse, addDef)
			}
			for i := range ins.Call.Args {
				addUsesFromOperand(&ins.Call.Args[i], addUse, addDef)
			}
			if ins.Call.HasDst {
				addUsesFromPlaceWrite(ins.Call.Dst, addUse)
				addDefFromPlace(ins.Call.Dst, addDef)
			}
		case InstrDrop:
			addUsesFromPlace(ins.Drop.Place, addUse)
			addDefFromPlace(ins.Drop.Place, addDef)
		case InstrEnvelopeRelease:
			addUsesFromPlace(ins.EnvelopeRelease.Place, addUse)
			addDefFromPlace(ins.EnvelopeRelease.Place, addDef)
		case InstrEndBorrow:
			addUsesFromPlace(ins.EndBorrow.Place, addUse)
			addDefFromPlace(ins.EndBorrow.Place, addDef)
		case InstrAwait:
			addUsesFromOperand(&ins.Await.Task, addUse, addDef)
			addUsesFromPlaceWrite(ins.Await.Dst, addUse)
			addDefFromPlace(ins.Await.Dst, addDef)
		case InstrSpawn:
			addUsesFromOperand(&ins.Spawn.Value, addUse, addDef)
			addUsesFromPlaceWrite(ins.Spawn.Dst, addUse)
			addDefFromPlace(ins.Spawn.Dst, addDef)
		case InstrCrossing:
			addUsesFromCrossing(&ins.Crossing, addUse, addDef)
		case InstrBlocking:
			for i := range ins.Blocking.State.Fields {
				addUsesFromOperand(&ins.Blocking.State.Fields[i].Value, addUse, addDef)
			}
			addUsesFromPlaceWrite(ins.Blocking.Dst, addUse)
			addDefFromPlace(ins.Blocking.Dst, addDef)
		case InstrPoll:
			addUsesFromOperand(&ins.Poll.Task, addUse, addDef)
			addUsesFromPlaceWrite(ins.Poll.Dst, addUse)
			addDefFromPlace(ins.Poll.Dst, addDef)
		case InstrJoinAll:
			addUsesFromOperand(&ins.JoinAll.Scope, addUse, addDef)
			addUsesFromPlaceWrite(ins.JoinAll.Dst, addUse)
			addDefFromPlace(ins.JoinAll.Dst, addDef)
		case InstrChanSend:
			addUsesFromOperand(&ins.ChanSend.Channel, addUse, addDef)
			addUsesFromOperand(&ins.ChanSend.Value, addUse, addDef)
		case InstrChanRecv:
			addUsesFromOperand(&ins.ChanRecv.Channel, addUse, addDef)
			addUsesFromPlaceWrite(ins.ChanRecv.Dst, addUse)
			addDefFromPlace(ins.ChanRecv.Dst, addDef)
		case InstrNetWait:
			addUsesFromOperand(&ins.NetWait.Handle, addUse, addDef)
		case InstrTimeout:
			addUsesFromOperand(&ins.Timeout.Task, addUse, addDef)
			addUsesFromOperand(&ins.Timeout.Ms, addUse, addDef)
			addUsesFromPlaceWrite(ins.Timeout.Dst, addUse)
			addDefFromPlace(ins.Timeout.Dst, addDef)
		case InstrSelect:
			addUsesFromPlaceWrite(ins.Select.Dst, addUse)
			addDefFromPlace(ins.Select.Dst, addDef)
			for i := range ins.Select.Arms {
				arm := &ins.Select.Arms[i]
				switch arm.Kind {
				case SelectArmTask:
					addUsesFromOperand(&arm.Task, addUse, addDef)
				case SelectArmChanRecv:
					addUsesFromOperand(&arm.Channel, addUse, addDef)
				case SelectArmChanSend:
					addUsesFromOperand(&arm.Channel, addUse, addDef)
					addUsesFromOperand(&arm.Value, addUse, addDef)
				case SelectArmTimeout:
					addUsesFromOperand(&arm.Task, addUse, addDef)
					addUsesFromOperand(&arm.Ms, addUse, addDef)
				}
			}
		}
	}

	switch bb.Term.Kind {
	case TermReturn:
		if bb.Term.Return.HasValue {
			addUsesFromOperand(&bb.Term.Return.Value, addUse, addDef)
		}
	case TermAsyncYield:
		addUsesFromOperand(&bb.Term.AsyncYield.State, addUse, addDef)
	case TermAsyncReturn:
		addUsesFromOperand(&bb.Term.AsyncReturn.State, addUse, addDef)
		if bb.Term.AsyncReturn.HasValue {
			addUsesFromOperand(&bb.Term.AsyncReturn.Value, addUse, addDef)
		}
	case TermAsyncReturnCancelled:
		addUsesFromOperand(&bb.Term.AsyncReturnCancelled.State, addUse, addDef)
	case TermIf:
		addUsesFromOperand(&bb.Term.If.Cond, addUse, addDef)
	case TermSwitchTag:
		addUsesFromOperand(&bb.Term.SwitchTag.Value, addUse, addDef)
	}

	return use, def
}

func addUsesFromCrossing(ins *CrossingInstr, addUse, addDef func(LocalID)) {
	if ins == nil {
		return
	}
	addUsesFromPlaceWrite(ins.Dst, addUse)
	addDefFromPlace(ins.Dst, addDef)
	addUsesFromPlace(ins.Pending, addUse)
	if ins.Kind == sema.CrossingLoweringSpawnOn || ins.Kind == sema.CrossingLoweringChannelCreate ||
		ins.Kind == sema.CrossingLoweringChannelShare {
		addUsesFromPlace(ins.Handle, addUse)
	}
	addUsesFromOperand(&ins.Destination.Value, addUse, addDef)
	for i := range ins.State.Fields {
		addUsesFromOperand(&ins.State.Fields[i].Value, addUse, addDef)
	}
	for i := range ins.Captures {
		addUsesFromOperand(&ins.Captures[i].Value, addUse, addDef)
	}
	for i := range ins.RemoteOps {
		addUsesFromOperand(&ins.RemoteOps[i].Receiver, addUse, addDef)
		addUsesFromOperand(&ins.RemoteOps[i].Value, addUse, addDef)
	}
	addUsesFromOperand(&ins.Receiver, addUse, addDef)
}

func addUsesFromOperand(op *Operand, addUse, addDef func(LocalID)) {
	if op == nil {
		return
	}
	switch op.Kind {
	case OperandCopy, OperandMove, OperandAddrOf, OperandAddrOfMut:
		addUsesFromPlace(op.Place, addUse)
		if op.Kind == OperandMove && addDef != nil {
			addDefFromMovePlace(op.Place, addDef)
		}
	}
}

func addUsesFromRValue(rv *RValue, addUse, addDef func(LocalID)) {
	if rv == nil {
		return
	}
	switch rv.Kind {
	case RValueUse:
		addUsesFromOperand(&rv.Use, addUse, addDef)
	case RValueUnaryOp:
		addUsesFromOperand(&rv.Unary.Operand, addUse, addDef)
	case RValueBinaryOp:
		addUsesFromOperand(&rv.Binary.Left, addUse, addDef)
		addUsesFromOperand(&rv.Binary.Right, addUse, addDef)
	case RValueCast:
		addUsesFromOperand(&rv.Cast.Value, addUse, addDef)
	case RValueStructLit:
		for i := range rv.StructLit.Fields {
			addUsesFromOperand(&rv.StructLit.Fields[i].Value, addUse, addDef)
		}
	case RValueArrayLit:
		for i := range rv.ArrayLit.Elems {
			addUsesFromOperand(&rv.ArrayLit.Elems[i], addUse, addDef)
		}
	case RValueTupleLit:
		for i := range rv.TupleLit.Elems {
			addUsesFromOperand(&rv.TupleLit.Elems[i], addUse, addDef)
		}
	case RValueField:
		addUsesFromOperand(&rv.Field.Object, addUse, addDef)
	case RValueIndex:
		addUsesFromOperand(&rv.Index.Object, addUse, addDef)
		addUsesFromOperand(&rv.Index.Index, addUse, addDef)
	case RValueTagTest:
		addUsesFromOperand(&rv.TagTest.Value, addUse, addDef)
	case RValueTagPayload:
		addUsesFromOperand(&rv.TagPayload.Value, addUse, addDef)
	case RValueIterInit:
		addUsesFromOperand(&rv.IterInit.Iterable, addUse, addDef)
	case RValueIterNext:
		addUsesFromOperand(&rv.IterNext.Iter, addUse, addDef)
	case RValueTypeTest:
		addUsesFromOperand(&rv.TypeTest.Value, addUse, addDef)
	case RValueHeirTest:
		addUsesFromOperand(&rv.HeirTest.Value, addUse, addDef)
	}
}

func addUsesFromPlace(p Place, addUse func(LocalID)) {
	if p.Kind == PlaceLocal {
		addUse(p.Local)
	}
	for _, proj := range p.Proj {
		if proj.Kind == PlaceProjIndex && proj.IndexLocal != NoLocalID {
			addUse(proj.IndexLocal)
		}
	}
}

func addUsesFromPlaceWrite(p Place, addUse func(LocalID)) {
	if len(p.Proj) == 0 {
		return
	}
	addUsesFromPlace(p, addUse)
}

func addDefFromPlace(p Place, addDef func(LocalID)) {
	if p.Kind == PlaceLocal && len(p.Proj) == 0 {
		addDef(p.Local)
	}
}

func addDefFromMovePlace(p Place, addDef func(LocalID)) {
	if p.Kind == PlaceLocal {
		addDef(p.Local)
	}
}
