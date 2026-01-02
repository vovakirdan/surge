package mir

import (
	"fmt"

	"surge/internal/hir"
)

// lowerTagTestExpr lowers a tag test expression.
func (l *funcLowerer) lowerTagTestExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.TagTestData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: tag_test: unexpected payload %T", e.Data)
	}
	val, err := l.lowerExpr(data.Value, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "tagtest", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueTagTest, TagTest: TagTest{Value: val, TagName: data.TagName}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerTagPayloadExpr lowers a tag payload extraction expression.
func (l *funcLowerer) lowerTagPayloadExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.TagPayloadData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: tag_payload: unexpected payload %T", e.Data)
	}
	val, err := l.lowerExpr(data.Value, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "payload", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueTagPayload, TagPayload: TagPayload{Value: val, TagName: data.TagName, Index: data.Index}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerIterInitExpr lowers an iterator initialization expression.
func (l *funcLowerer) lowerIterInitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.IterInitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: iter_init: unexpected payload %T", e.Data)
	}
	iterable, err := l.lowerExpr(data.Iterable, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "iter", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueIterInit, IterInit: IterInit{Iterable: iterable}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerIterNextExpr lowers an iterator next expression.
func (l *funcLowerer) lowerIterNextExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.IterNextData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: iter_next: unexpected payload %T", e.Data)
	}
	iter, err := l.lowerExpr(data.Iter, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "next", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueIterNext, IterNext: IterNext{Iter: iter}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerAwaitExpr lowers an await expression.
func (l *funcLowerer) lowerAwaitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.AwaitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: await: unexpected payload %T", e.Data)
	}
	task, err := l.lowerExpr(data.Value, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "await", e.Span)
	l.emit(&Instr{Kind: InstrAwait, Await: AwaitInstr{Dst: Place{Local: tmp}, Task: task}})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerTaskExpr lowers a task expression into a spawn instruction.
func (l *funcLowerer) lowerTaskExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.TaskData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: task: unexpected payload %T", e.Data)
	}
	value, err := l.lowerExpr(data.Value, true)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "task", e.Span)
	l.emit(&Instr{Kind: InstrSpawn, Spawn: SpawnInstr{Dst: Place{Local: tmp}, Value: value}})
	if l.scopeLocal != NoLocalID {
		l.emit(&Instr{Kind: InstrCall, Call: CallInstr{
			HasDst: false,
			Callee: Callee{Kind: CalleeValue, Name: "rt_scope_register_child"},
			Args: []Operand{
				{Kind: OperandCopy, Place: Place{Local: l.scopeLocal}},
				{Kind: OperandCopy, Place: Place{Local: tmp}},
			},
		}})
	}
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerAsyncExpr lowers an async block to a placeholder Task value.
func (l *funcLowerer) lowerAsyncExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.AsyncData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: async: unexpected payload %T", e.Data)
	}
	payload, ok := l.taskPayloadType(e.Type)
	if !ok {
		return Operand{}, fmt.Errorf("mir: async: expected Task<T> type, got %v", e.Type)
	}
	asyncID := l.allocFuncID()
	if asyncID == NoFuncID {
		return Operand{}, fmt.Errorf("mir: async: failed to allocate function id")
	}
	name := fmt.Sprintf("__async_block$%d", asyncID)
	fl := l.forkLowerer()
	if fl == nil {
		return Operand{}, fmt.Errorf("mir: async: failed to fork lowerer")
	}
	fn, err := fl.lowerSyntheticFunc(asyncID, name, data.Body, payload, e.Span, true, data.Failfast)
	if err != nil {
		return Operand{}, err
	}
	if l.out != nil {
		l.out.Funcs[asyncID] = fn
	}

	tmp := l.newTemp(e.Type, "async", e.Span)
	l.emit(&Instr{
		Kind: InstrCall,
		Call: CallInstr{
			HasDst: true,
			Dst:    Place{Local: tmp},
			Callee: Callee{
				Kind: CalleeValue,
				Name: name,
			},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}
