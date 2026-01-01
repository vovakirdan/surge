package mir

import (
	"fmt"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// LowerAsyncStateMachine lowers async functions to a multi-suspend state machine.
func LowerAsyncStateMachine(m *Module, semaRes *sema.Result, symTable *symbols.Table) error {
	if m == nil {
		return nil
	}
	typesIn := (*types.Interner)(nil)
	if semaRes != nil {
		typesIn = semaRes.TypeInterner
	}
	if typesIn == nil {
		return fmt.Errorf("mir: async lowering requires type interner")
	}

	for _, f := range m.Funcs {
		if f == nil || !f.IsAsync {
			continue
		}
		if err := lowerAsyncStateMachineFunc(m, f, typesIn, semaRes, symTable); err != nil {
			return err
		}
	}
	return nil
}

func lowerAsyncStateMachineFunc(m *Module, f *Func, typesIn *types.Interner, semaRes *sema.Result, symTable *symbols.Table) error {
	if f == nil {
		return nil
	}
	payload := f.Result
	if payload == types.NoTypeID {
		payload = typesIn.Builtins().Nothing
	}

	taskType, err := taskTypeFor(typesIn, payload)
	if err != nil {
		return err
	}

	pollFnID, err := allocAsyncPollFunc(m)
	if err != nil {
		return err
	}

	origLocals := cloneLocals(f.Locals)
	origBlocks := f.Blocks
	origEntry := f.Entry

	pollFn := &Func{
		ID:         pollFnID,
		Sym:        symbols.NoSymbolID,
		Name:       f.Name + "$poll",
		Span:       f.Span,
		Result:     payload,
		IsAsync:    false,
		Failfast:   f.Failfast,
		Locals:     origLocals,
		Blocks:     origBlocks,
		Entry:      origEntry,
		ScopeLocal: f.ScopeLocal,
	}

	if m.Funcs == nil {
		m.Funcs = make(map[FuncID]*Func)
	}
	m.Funcs[pollFnID] = pollFn

	f.Locals = cloneLocals(origLocals)
	f.Blocks = nil
	f.Entry = NoBlockID

	if _, err = splitAsyncAwaits(pollFn); err != nil {
		return err
	}
	if pollFn.ScopeLocal != NoLocalID {
		joinResultLocal := addLocal(pollFn, "__scope_join_failed", typesIn.Builtins().Bool, localFlagsFor(typesIn, semaRes, typesIn.Builtins().Bool))
		insertScopeJoins(pollFn, pollFn.ScopeLocal, joinResultLocal)
	}

	sites := collectSuspendSites(pollFn)
	if loopErr := rejectAwaitInLoops(pollFn, sites); loopErr != nil {
		return loopErr
	}
	live := computeLiveness(pollFn)

	paramLocals := paramLocalSet(pollFn, symTable)
	variants := make([]stateVariant, 0, len(sites)+1)
	variants = append(variants, stateVariant{
		name:     "S0",
		locals:   paramLocals.sorted(),
		resumeBB: origEntry,
	})

	for i := range sites {
		sites[i].stateIndex = i + 1
		sites[i].liveLocals = cloneSet(live[sites[i].pollBB].in)
		if sites[i].pollBB >= 0 && int(sites[i].pollBB) < len(pollFn.Blocks) {
			bb := &pollFn.Blocks[sites[i].pollBB]
			if sites[i].pollInstr >= 0 && sites[i].pollInstr < len(bb.Instrs) {
				ins := &bb.Instrs[sites[i].pollInstr]
				switch ins.Kind {
				case InstrPoll:
					if ins.Poll.Dst.Kind == PlaceLocal && len(ins.Poll.Dst.Proj) == 0 {
						sites[i].liveLocals.delete(ins.Poll.Dst.Local)
					}
				case InstrJoinAll:
					if ins.JoinAll.Dst.Kind == PlaceLocal && len(ins.JoinAll.Dst.Proj) == 0 {
						sites[i].liveLocals.delete(ins.JoinAll.Dst.Local)
					}
				case InstrChanRecv:
					if ins.ChanRecv.Dst.Kind == PlaceLocal && len(ins.ChanRecv.Dst.Proj) == 0 {
						sites[i].liveLocals.delete(ins.ChanRecv.Dst.Local)
					}
				case InstrTimeout:
					if ins.Timeout.Dst.Kind == PlaceLocal && len(ins.Timeout.Dst.Proj) == 0 {
						sites[i].liveLocals.delete(ins.Timeout.Dst.Local)
					}
				}
			}
		}
		variants = append(variants, stateVariant{
			name:     fmt.Sprintf("S%d", sites[i].stateIndex),
			locals:   sites[i].liveLocals.sorted(),
			resumeBB: sites[i].pollBB,
		})
	}

	stateType, err := buildAsyncStateUnion(m, typesIn, symTable, f, pollFn, variants)
	if err != nil {
		return err
	}

	stateLocal := addLocal(pollFn, "__state", stateType, localFlagsFor(typesIn, semaRes, stateType))
	entryBB := buildAsyncPollEntry(pollFn, stateLocal, variants, pollFn.ScopeLocal, pollFn.Failfast, typesIn.Builtins().Bool)
	pollFn.Entry = entryBB

	if err := buildAsyncPendingBlocks(pollFn, stateLocal, sites, variants); err != nil {
		return err
	}
	rewriteAsyncReturns(pollFn, stateLocal)

	if err := buildAsyncConstructorState(f, typesIn, semaRes, taskType, stateType, pollFnID, variants[0]); err != nil {
		return err
	}
	f.IsAsync = false
	f.Result = taskType
	return nil
}

func insertScopeJoins(f *Func, scopeLocal, joinResultLocal LocalID) {
	if f == nil || scopeLocal == NoLocalID || joinResultLocal == NoLocalID {
		return
	}
	origBlocks := len(f.Blocks)
	for bi := range origBlocks {
		if f.Blocks[bi].Term.Kind != TermReturn {
			continue
		}
		term := f.Blocks[bi].Term.Return
		joinBB := newBlock(f)
		doneBB := newBlock(f)
		successBB := newBlock(f)
		cancelBB := newBlock(f)

		f.Blocks[bi].Term = Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}}

		if term.Early {
			appendInstr(f, joinBB, Instr{Kind: InstrCall, Call: CallInstr{
				HasDst: false,
				Callee: Callee{Kind: CalleeValue, Name: "rt_scope_cancel_all"},
				Args:   []Operand{operandForLocal(f, scopeLocal)},
			}})
		}
		appendInstr(f, joinBB, Instr{Kind: InstrJoinAll, JoinAll: JoinAllInstr{
			Dst:     Place{Local: joinResultLocal},
			Scope:   operandForLocal(f, scopeLocal),
			ReadyBB: doneBB,
			PendBB:  NoBlockID,
		}})
		setBlockTerm(f, joinBB, Terminator{Kind: TermUnreachable})

		appendInstr(f, doneBB, Instr{Kind: InstrCall, Call: CallInstr{
			HasDst: false,
			Callee: Callee{Kind: CalleeValue, Name: "rt_scope_exit"},
			Args:   []Operand{operandForLocal(f, scopeLocal)},
		}})
		setBlockTerm(f, doneBB, Terminator{Kind: TermIf, If: IfTerm{
			Cond: operandForLocal(f, joinResultLocal),
			Then: cancelBB,
			Else: successBB,
		}})

		setBlockTerm(f, successBB, Terminator{Kind: TermReturn, Return: ReturnTerm{
			HasValue: term.HasValue,
			Value:    term.Value,
		}})

		setBlockTerm(f, cancelBB, Terminator{Kind: TermReturn, Return: ReturnTerm{
			Cancelled: true,
		}})
	}
}
