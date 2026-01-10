package mir

import (
	"sort"

	"surge/internal/source"
	"surge/internal/symbols"
)

func paramLocalSet(f *Func, symTable *symbols.Table) localSet {
	set := localSet{}
	if f == nil {
		return set
	}
	paramNames := map[string]struct{}{}
	paramCount := -1
	if symTable != nil && symTable.Symbols != nil {
		if sym := symTable.Symbols.Get(f.Sym); sym != nil && sym.Signature != nil {
			paramCount = len(sym.Signature.Params)
			if symTable.Strings != nil {
				for _, nameID := range sym.Signature.ParamNames {
					if nameID == source.NoStringID {
						continue
					}
					if name := symTable.Strings.MustLookup(nameID); name != "" {
						paramNames[name] = struct{}{}
					}
				}
			}
		}
	}
	if f.ScopeLocal != NoLocalID {
		scopeCount := int(f.ScopeLocal)
		if scopeCount > paramCount {
			paramCount = scopeCount
		}
	}
	for id, loc := range f.Locals {
		include := false
		if loc.Sym.IsValid() {
			if symTable != nil && symTable.Symbols != nil {
				sym := symTable.Symbols.Get(loc.Sym)
				if sym != nil && sym.Kind == symbols.SymbolParam {
					include = true
				}
			} else {
				include = true
			}
		}
		if !include {
			if paramCount >= 0 && id < paramCount {
				include = true
			} else if len(paramNames) > 0 {
				if _, ok := paramNames[loc.Name]; ok {
					include = true
				}
			}
		}
		if include {
			set.add(LocalID(id)) //nolint:gosec // bounded by locals length
		}
	}
	return set
}

func localsAssignedInBlock(f *Func, bbID BlockID) localSet {
	set := localSet{}
	if f == nil || bbID == NoBlockID || int(bbID) >= len(f.Blocks) {
		return set
	}
	bb := &f.Blocks[bbID]
	for i := range bb.Instrs {
		ins := &bb.Instrs[i]
		switch ins.Kind {
		case InstrAssign:
			if len(ins.Assign.Dst.Proj) == 0 && ins.Assign.Dst.Kind == PlaceLocal {
				set.add(ins.Assign.Dst.Local)
			}
		case InstrCall:
			if ins.Call.HasDst && len(ins.Call.Dst.Proj) == 0 && ins.Call.Dst.Kind == PlaceLocal {
				set.add(ins.Call.Dst.Local)
			}
		case InstrSpawn:
			if len(ins.Spawn.Dst.Proj) == 0 && ins.Spawn.Dst.Kind == PlaceLocal {
				set.add(ins.Spawn.Dst.Local)
			}
		case InstrBlocking:
			if len(ins.Blocking.Dst.Proj) == 0 && ins.Blocking.Dst.Kind == PlaceLocal {
				set.add(ins.Blocking.Dst.Local)
			}
		case InstrJoinAll:
			if len(ins.JoinAll.Dst.Proj) == 0 && ins.JoinAll.Dst.Kind == PlaceLocal {
				set.add(ins.JoinAll.Dst.Local)
			}
		case InstrChanRecv:
			if len(ins.ChanRecv.Dst.Proj) == 0 && ins.ChanRecv.Dst.Kind == PlaceLocal {
				set.add(ins.ChanRecv.Dst.Local)
			}
		case InstrTimeout:
			if len(ins.Timeout.Dst.Proj) == 0 && ins.Timeout.Dst.Kind == PlaceLocal {
				set.add(ins.Timeout.Dst.Local)
			}
		case InstrSelect:
			if len(ins.Select.Dst.Proj) == 0 && ins.Select.Dst.Kind == PlaceLocal {
				set.add(ins.Select.Dst.Local)
			}
		}
	}
	return set
}

func localsUsedFrom(f *Func, start BlockID) localSet {
	set := localSet{}
	if f == nil || start == NoBlockID {
		return set
	}
	for _, id := range reachableBlocksFrom(f, start) {
		collectLocalsInBlock(&f.Blocks[id], set)
	}
	return set
}

func reachableBlocksFrom(f *Func, start BlockID) []BlockID {
	if f == nil || start == NoBlockID {
		return nil
	}
	seen := make(map[BlockID]struct{})
	var order []BlockID
	var visit func(id BlockID)
	visit = func(id BlockID) {
		if id < 0 || int(id) >= len(f.Blocks) {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		order = append(order, id)
		bb := &f.Blocks[id]
		if len(bb.Instrs) > 0 {
			last := &bb.Instrs[len(bb.Instrs)-1]
			switch last.Kind {
			case InstrPoll:
				visit(last.Poll.ReadyBB)
				visit(last.Poll.PendBB)
				return
			case InstrJoinAll:
				visit(last.JoinAll.ReadyBB)
				visit(last.JoinAll.PendBB)
				return
			case InstrChanSend:
				visit(last.ChanSend.ReadyBB)
				visit(last.ChanSend.PendBB)
				return
			case InstrChanRecv:
				visit(last.ChanRecv.ReadyBB)
				visit(last.ChanRecv.PendBB)
				return
			case InstrTimeout:
				visit(last.Timeout.ReadyBB)
				visit(last.Timeout.PendBB)
				return
			case InstrSelect:
				visit(last.Select.ReadyBB)
				visit(last.Select.PendBB)
				return
			}
		}
		switch bb.Term.Kind {
		case TermGoto:
			visit(bb.Term.Goto.Target)
		case TermIf:
			visit(bb.Term.If.Then)
			visit(bb.Term.If.Else)
		case TermSwitchTag:
			for _, c := range bb.Term.SwitchTag.Cases {
				visit(c.Target)
			}
			visit(bb.Term.SwitchTag.Default)
		}
	}

	visit(start)
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	return order
}

func collectLocalsInBlock(bb *Block, set localSet) {
	if bb == nil {
		return
	}
	for i := range bb.Instrs {
		collectLocalsInInstr(&bb.Instrs[i], set)
	}
	switch bb.Term.Kind {
	case TermReturn:
		if bb.Term.Return.HasValue {
			collectLocalsFromOperand(&bb.Term.Return.Value, set)
		}
	case TermAsyncYield:
		collectLocalsFromOperand(&bb.Term.AsyncYield.State, set)
	case TermAsyncReturn:
		collectLocalsFromOperand(&bb.Term.AsyncReturn.State, set)
		if bb.Term.AsyncReturn.HasValue {
			collectLocalsFromOperand(&bb.Term.AsyncReturn.Value, set)
		}
	case TermAsyncReturnCancelled:
		collectLocalsFromOperand(&bb.Term.AsyncReturnCancelled.State, set)
	case TermIf:
		collectLocalsFromOperand(&bb.Term.If.Cond, set)
	case TermSwitchTag:
		collectLocalsFromOperand(&bb.Term.SwitchTag.Value, set)
	}
}

func collectLocalsInInstr(ins *Instr, set localSet) {
	if ins == nil {
		return
	}
	switch ins.Kind {
	case InstrAssign:
		collectLocalsFromRValue(&ins.Assign.Src, set)
		collectLocalsFromPlace(ins.Assign.Dst, set)
	case InstrCall:
		for i := range ins.Call.Args {
			collectLocalsFromOperand(&ins.Call.Args[i], set)
		}
		if ins.Call.HasDst {
			collectLocalsFromPlace(ins.Call.Dst, set)
		}
	case InstrDrop:
		collectLocalsFromPlace(ins.Drop.Place, set)
	case InstrEndBorrow:
		collectLocalsFromPlace(ins.EndBorrow.Place, set)
	case InstrAwait:
		collectLocalsFromOperand(&ins.Await.Task, set)
	case InstrSpawn:
		collectLocalsFromOperand(&ins.Spawn.Value, set)
		collectLocalsFromPlace(ins.Spawn.Dst, set)
	case InstrBlocking:
		for i := range ins.Blocking.State.Fields {
			collectLocalsFromOperand(&ins.Blocking.State.Fields[i].Value, set)
		}
		collectLocalsFromPlace(ins.Blocking.Dst, set)
	case InstrPoll:
		collectLocalsFromOperand(&ins.Poll.Task, set)
		collectLocalsFromPlace(ins.Poll.Dst, set)
	case InstrJoinAll:
		collectLocalsFromOperand(&ins.JoinAll.Scope, set)
		collectLocalsFromPlace(ins.JoinAll.Dst, set)
	case InstrChanSend:
		collectLocalsFromOperand(&ins.ChanSend.Channel, set)
		collectLocalsFromOperand(&ins.ChanSend.Value, set)
	case InstrChanRecv:
		collectLocalsFromOperand(&ins.ChanRecv.Channel, set)
		collectLocalsFromPlace(ins.ChanRecv.Dst, set)
	case InstrTimeout:
		collectLocalsFromOperand(&ins.Timeout.Task, set)
		collectLocalsFromOperand(&ins.Timeout.Ms, set)
		collectLocalsFromPlace(ins.Timeout.Dst, set)
	case InstrSelect:
		collectLocalsFromPlace(ins.Select.Dst, set)
		for i := range ins.Select.Arms {
			arm := &ins.Select.Arms[i]
			switch arm.Kind {
			case SelectArmTask:
				collectLocalsFromOperand(&arm.Task, set)
			case SelectArmChanRecv:
				collectLocalsFromOperand(&arm.Channel, set)
			case SelectArmChanSend:
				collectLocalsFromOperand(&arm.Channel, set)
				collectLocalsFromOperand(&arm.Value, set)
			case SelectArmTimeout:
				collectLocalsFromOperand(&arm.Task, set)
				collectLocalsFromOperand(&arm.Ms, set)
			}
		}
	}
}

func collectLocalsFromRValue(rv *RValue, set localSet) {
	if rv == nil {
		return
	}
	switch rv.Kind {
	case RValueUse:
		collectLocalsFromOperand(&rv.Use, set)
	case RValueUnaryOp:
		collectLocalsFromOperand(&rv.Unary.Operand, set)
	case RValueBinaryOp:
		collectLocalsFromOperand(&rv.Binary.Left, set)
		collectLocalsFromOperand(&rv.Binary.Right, set)
	case RValueCast:
		collectLocalsFromOperand(&rv.Cast.Value, set)
	case RValueStructLit:
		for i := range rv.StructLit.Fields {
			collectLocalsFromOperand(&rv.StructLit.Fields[i].Value, set)
		}
	case RValueArrayLit:
		for i := range rv.ArrayLit.Elems {
			collectLocalsFromOperand(&rv.ArrayLit.Elems[i], set)
		}
	case RValueTupleLit:
		for i := range rv.TupleLit.Elems {
			collectLocalsFromOperand(&rv.TupleLit.Elems[i], set)
		}
	case RValueField:
		collectLocalsFromOperand(&rv.Field.Object, set)
	case RValueIndex:
		collectLocalsFromOperand(&rv.Index.Object, set)
		collectLocalsFromOperand(&rv.Index.Index, set)
	case RValueTagTest:
		collectLocalsFromOperand(&rv.TagTest.Value, set)
	case RValueTagPayload:
		collectLocalsFromOperand(&rv.TagPayload.Value, set)
	case RValueIterInit:
		collectLocalsFromOperand(&rv.IterInit.Iterable, set)
	case RValueIterNext:
		collectLocalsFromOperand(&rv.IterNext.Iter, set)
	case RValueTypeTest:
		collectLocalsFromOperand(&rv.TypeTest.Value, set)
	case RValueHeirTest:
		collectLocalsFromOperand(&rv.HeirTest.Value, set)
	}
}

func collectLocalsFromOperand(op *Operand, set localSet) {
	if op == nil {
		return
	}
	switch op.Kind {
	case OperandCopy, OperandMove, OperandAddrOf, OperandAddrOfMut:
		collectLocalsFromPlace(op.Place, set)
	}
}

func collectLocalsFromPlace(p Place, set localSet) {
	if p.Kind == PlaceLocal {
		set.add(p.Local)
	}
	for _, proj := range p.Proj {
		if proj.Kind == PlaceProjIndex && proj.IndexLocal != NoLocalID {
			set.add(proj.IndexLocal)
		}
	}
}

func operandForLocal(f *Func, id LocalID) Operand {
	if f == nil || id == NoLocalID {
		return Operand{}
	}
	kind := OperandCopy
	if int(id) >= 0 && int(id) < len(f.Locals) {
		if f.Locals[id].Flags&LocalFlagCopy == 0 {
			kind = OperandMove
		}
	}
	return Operand{Kind: kind, Place: Place{Local: id}}
}
