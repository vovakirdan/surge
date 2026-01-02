package mir

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
			addUsesFromRValue(&ins.Assign.Src, addUse)
			addUsesFromPlaceWrite(ins.Assign.Dst, addUse)
			addDefFromPlace(ins.Assign.Dst, addDef)
		case InstrCall:
			if ins.Call.Callee.Kind == CalleeValue {
				addUsesFromOperand(&ins.Call.Callee.Value, addUse)
			}
			for i := range ins.Call.Args {
				addUsesFromOperand(&ins.Call.Args[i], addUse)
			}
			if ins.Call.HasDst {
				addUsesFromPlaceWrite(ins.Call.Dst, addUse)
				addDefFromPlace(ins.Call.Dst, addDef)
			}
		case InstrDrop:
			addUsesFromPlace(ins.Drop.Place, addUse)
			addDefFromPlace(ins.Drop.Place, addDef)
		case InstrEndBorrow:
			addUsesFromPlace(ins.EndBorrow.Place, addUse)
			addDefFromPlace(ins.EndBorrow.Place, addDef)
		case InstrAwait:
			addUsesFromOperand(&ins.Await.Task, addUse)
			addUsesFromPlaceWrite(ins.Await.Dst, addUse)
			addDefFromPlace(ins.Await.Dst, addDef)
		case InstrSpawn:
			addUsesFromOperand(&ins.Spawn.Value, addUse)
			addUsesFromPlaceWrite(ins.Spawn.Dst, addUse)
			addDefFromPlace(ins.Spawn.Dst, addDef)
		case InstrPoll:
			addUsesFromOperand(&ins.Poll.Task, addUse)
			addUsesFromPlaceWrite(ins.Poll.Dst, addUse)
			addDefFromPlace(ins.Poll.Dst, addDef)
		case InstrJoinAll:
			addUsesFromOperand(&ins.JoinAll.Scope, addUse)
			addUsesFromPlaceWrite(ins.JoinAll.Dst, addUse)
			addDefFromPlace(ins.JoinAll.Dst, addDef)
		case InstrChanSend:
			addUsesFromOperand(&ins.ChanSend.Channel, addUse)
			addUsesFromOperand(&ins.ChanSend.Value, addUse)
		case InstrChanRecv:
			addUsesFromOperand(&ins.ChanRecv.Channel, addUse)
			addUsesFromPlaceWrite(ins.ChanRecv.Dst, addUse)
			addDefFromPlace(ins.ChanRecv.Dst, addDef)
		case InstrTimeout:
			addUsesFromOperand(&ins.Timeout.Task, addUse)
			addUsesFromOperand(&ins.Timeout.Ms, addUse)
			addUsesFromPlaceWrite(ins.Timeout.Dst, addUse)
			addDefFromPlace(ins.Timeout.Dst, addDef)
		case InstrSelect:
			addUsesFromPlaceWrite(ins.Select.Dst, addUse)
			addDefFromPlace(ins.Select.Dst, addDef)
			for i := range ins.Select.Arms {
				arm := &ins.Select.Arms[i]
				switch arm.Kind {
				case SelectArmTask:
					addUsesFromOperand(&arm.Task, addUse)
				case SelectArmChanRecv:
					addUsesFromOperand(&arm.Channel, addUse)
				case SelectArmChanSend:
					addUsesFromOperand(&arm.Channel, addUse)
					addUsesFromOperand(&arm.Value, addUse)
				case SelectArmTimeout:
					addUsesFromOperand(&arm.Task, addUse)
					addUsesFromOperand(&arm.Ms, addUse)
				}
			}
		}
	}

	switch bb.Term.Kind {
	case TermReturn:
		if bb.Term.Return.HasValue {
			addUsesFromOperand(&bb.Term.Return.Value, addUse)
		}
	case TermAsyncYield:
		addUsesFromOperand(&bb.Term.AsyncYield.State, addUse)
	case TermAsyncReturn:
		addUsesFromOperand(&bb.Term.AsyncReturn.State, addUse)
		if bb.Term.AsyncReturn.HasValue {
			addUsesFromOperand(&bb.Term.AsyncReturn.Value, addUse)
		}
	case TermAsyncReturnCancelled:
		addUsesFromOperand(&bb.Term.AsyncReturnCancelled.State, addUse)
	case TermIf:
		addUsesFromOperand(&bb.Term.If.Cond, addUse)
	case TermSwitchTag:
		addUsesFromOperand(&bb.Term.SwitchTag.Value, addUse)
	}

	return use, def
}

func addUsesFromOperand(op *Operand, addUse func(LocalID)) {
	if op == nil {
		return
	}
	switch op.Kind {
	case OperandCopy, OperandMove, OperandAddrOf, OperandAddrOfMut:
		addUsesFromPlace(op.Place, addUse)
	}
}

func addUsesFromRValue(rv *RValue, addUse func(LocalID)) {
	if rv == nil {
		return
	}
	switch rv.Kind {
	case RValueUse:
		addUsesFromOperand(&rv.Use, addUse)
	case RValueUnaryOp:
		addUsesFromOperand(&rv.Unary.Operand, addUse)
	case RValueBinaryOp:
		addUsesFromOperand(&rv.Binary.Left, addUse)
		addUsesFromOperand(&rv.Binary.Right, addUse)
	case RValueCast:
		addUsesFromOperand(&rv.Cast.Value, addUse)
	case RValueStructLit:
		for i := range rv.StructLit.Fields {
			addUsesFromOperand(&rv.StructLit.Fields[i].Value, addUse)
		}
	case RValueArrayLit:
		for i := range rv.ArrayLit.Elems {
			addUsesFromOperand(&rv.ArrayLit.Elems[i], addUse)
		}
	case RValueTupleLit:
		for i := range rv.TupleLit.Elems {
			addUsesFromOperand(&rv.TupleLit.Elems[i], addUse)
		}
	case RValueField:
		addUsesFromOperand(&rv.Field.Object, addUse)
	case RValueIndex:
		addUsesFromOperand(&rv.Index.Object, addUse)
		addUsesFromOperand(&rv.Index.Index, addUse)
	case RValueTagTest:
		addUsesFromOperand(&rv.TagTest.Value, addUse)
	case RValueTagPayload:
		addUsesFromOperand(&rv.TagPayload.Value, addUse)
	case RValueIterInit:
		addUsesFromOperand(&rv.IterInit.Iterable, addUse)
	case RValueIterNext:
		addUsesFromOperand(&rv.IterNext.Iter, addUse)
	case RValueTypeTest:
		addUsesFromOperand(&rv.TypeTest.Value, addUse)
	case RValueHeirTest:
		addUsesFromOperand(&rv.HeirTest.Value, addUse)
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
