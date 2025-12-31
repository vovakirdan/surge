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
		ID:      pollFnID,
		Sym:     symbols.NoSymbolID,
		Name:    f.Name + "$poll",
		Span:    f.Span,
		Result:  payload,
		IsAsync: false,
		Locals:  origLocals,
		Blocks:  origBlocks,
		Entry:   origEntry,
	}

	if m.Funcs == nil {
		m.Funcs = make(map[FuncID]*Func)
	}
	m.Funcs[pollFnID] = pollFn

	f.Locals = cloneLocals(origLocals)
	f.Blocks = nil
	f.Entry = NoBlockID

	awaitSites, err := splitAsyncAwaits(pollFn)
	if err != nil {
		return err
	}
	if loopErr := rejectAwaitInLoops(pollFn, awaitSites); loopErr != nil {
		return loopErr
	}
	live := computeLiveness(pollFn)

	paramLocals := paramLocalSet(pollFn, symTable)
	variants := make([]stateVariant, 0, len(awaitSites)+1)
	variants = append(variants, stateVariant{
		name:     "S0",
		locals:   paramLocals.sorted(),
		resumeBB: origEntry,
	})

	for i := range awaitSites {
		awaitSites[i].stateIndex = i + 1
		awaitSites[i].liveLocals = cloneSet(live[awaitSites[i].pollBB].out)
		variants = append(variants, stateVariant{
			name:     fmt.Sprintf("S%d", awaitSites[i].stateIndex),
			locals:   awaitSites[i].liveLocals.sorted(),
			resumeBB: awaitSites[i].readyBB,
		})
	}

	stateType, err := buildAsyncStateUnion(m, typesIn, symTable, f, pollFn, variants)
	if err != nil {
		return err
	}

	stateLocal := addLocal(pollFn, "__state", stateType, localFlagsFor(typesIn, semaRes, stateType))
	entryBB := buildAsyncPollEntry(pollFn, stateLocal, variants)
	pollFn.Entry = entryBB

	if err := buildAsyncPendingBlocks(pollFn, stateLocal, awaitSites, variants); err != nil {
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
