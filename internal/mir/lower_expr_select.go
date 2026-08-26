package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

type loweredSelectArm struct {
	arm          SelectArm
	kind         SelectArmKind
	task         Operand
	channelLocal LocalID
	valueLocal   LocalID
	msLocal      LocalID
}

func (l *funcLowerer) lowerSelectExpr(e *hir.Expr, data hir.SelectData, isRace, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}

	hasResult := e.Type != types.NoTypeID && !l.isNothingType(e.Type)
	resultLocal := NoLocalID
	if hasResult {
		resultLocal = l.newTemp(e.Type, "select", e.Span)
	}

	selIndexType := types.NoTypeID
	boolType := types.NoTypeID
	if l.types != nil {
		selIndexType = l.types.Builtins().Int32
		boolType = l.types.Builtins().Bool
	}
	selIndexLocal := l.newTemp(selIndexType, "select_index", e.Span)

	// Remote select: the winner index ships from the arms' owner shard
	// through the ChannelSelect crossing; the arm dispatch below is shared
	// with the local path unchanged.
	if data.Crossing != nil {
		if err := l.lowerRemoteSelect(
			data.Crossing, Place{Local: selIndexLocal}, selIndexType, e.Span); err != nil {
			return Operand{}, err
		}
		return l.lowerSelectArmDispatch(e, data, nil, isRace, selIndexLocal, selIndexType,
			boolType, resultLocal, hasResult, consume)
	}

	lowered := make([]loweredSelectArm, len(data.Arms))
	for i, arm := range data.Arms {
		if arm.IsDefault {
			lowered[i] = loweredSelectArm{
				arm:  SelectArm{Kind: SelectArmDefault},
				kind: SelectArmDefault,
			}
			continue
		}
		armInstr, info, err := l.lowerSelectAwaitExpr(arm.Await)
		if err != nil {
			return Operand{}, err
		}
		lowered[i] = info
		lowered[i].arm = armInstr
	}

	arms := make([]SelectArm, len(lowered))
	for i := range lowered {
		arms[i] = lowered[i].arm
	}

	l.emit(&Instr{Kind: InstrSelect, Select: SelectInstr{
		Dst:     Place{Local: selIndexLocal},
		Arms:    arms,
		ReadyBB: NoBlockID,
		PendBB:  NoBlockID,
	}})

	return l.lowerSelectArmDispatch(e, data, lowered, isRace, selIndexLocal, selIndexType,
		boolType, resultLocal, hasResult, consume)
}

// lowerSelectArmDispatch branches on the winner-index local into the arm
// result blocks — shared verbatim by the local select (rt_select_poll) and
// the remote select (ChannelSelect crossing reply). `lowered` is nil for the
// remote path: a remote select has no task arms to race-cancel (SEM3176).
func (l *funcLowerer) lowerSelectArmDispatch(
	e *hir.Expr,
	data hir.SelectData,
	lowered []loweredSelectArm,
	isRace bool,
	selIndexLocal LocalID,
	selIndexType types.TypeID,
	boolType types.TypeID,
	resultLocal LocalID,
	hasResult bool,
	consume bool,
) (Operand, error) {
	armBBs := make([]BlockID, len(data.Arms))
	for i := range armBBs {
		armBBs[i] = l.newBlock()
	}
	joinBB := l.newBlock()

	nextDispatch := l.cur
	for i := range armBBs {
		l.startBlock(nextDispatch)
		selOp := l.placeOperand(Place{Local: selIndexLocal}, selIndexType, false)
		constOp := Operand{Kind: OperandConst, Type: selIndexType, Const: Const{
			Kind:     ConstInt,
			Type:     selIndexType,
			IntValue: int64(i),
		}}
		condLocal := l.newTemp(boolType, "select_match", e.Span)
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: condLocal},
			Src: RValue{Kind: RValueBinaryOp, Binary: BinaryOp{
				Op:    ast.ExprBinaryEq,
				Left:  selOp,
				Right: constOp,
			}},
		}})
		condOp := l.placeOperand(Place{Local: condLocal}, boolType, false)
		elseBB := l.newBlock()
		l.setTerm(&Terminator{Kind: TermIf, If: IfTerm{
			Cond: condOp,
			Then: armBBs[i],
			Else: elseBB,
		}})
		nextDispatch = elseBB
	}
	l.startBlock(nextDispatch)
	l.setTerm(&Terminator{Kind: TermUnreachable})

	for i, arm := range data.Arms {
		l.startBlock(armBBs[i])
		if isRace {
			for j := range lowered {
				if i == j {
					continue
				}
				if lowered[j].kind != SelectArmTask {
					continue
				}
				l.emit(&Instr{Kind: InstrCall, Call: CallInstr{
					HasDst: false,
					Callee: Callee{Kind: CalleeSym, Name: "cancel"},
					Args:   []Operand{lowered[j].task},
					// `cancel` signals a task it still does not own.
					ArgContracts: borrowArgContracts(1),
				}})
			}
		}
		if arm.Result != nil {
			if hasResult {
				op, err := l.lowerExpr(arm.Result, true)
				if err != nil {
					return Operand{}, err
				}
				l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
					Dst: Place{Local: resultLocal},
					Src: RValue{Kind: RValueUse, Use: op},
				}})
			} else {
				if err := l.lowerExprForSideEffects(arm.Result); err != nil {
					return Operand{}, err
				}
			}
		}
		if !l.curBlock().Terminated() {
			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
		}
	}

	l.startBlock(joinBB)
	if !hasResult {
		return l.constNothing(e.Type), nil
	}
	return l.placeOperand(Place{Local: resultLocal}, e.Type, consume), nil
}

func (l *funcLowerer) lowerSelectAwaitExpr(expr *hir.Expr) (SelectArm, loweredSelectArm, error) {
	if l == nil || expr == nil {
		return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await missing expression")
	}
	expr = l.unwrapSelectAwaitExpr(expr)
	if expr == nil {
		return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await expects awaitable expression")
	}
	switch expr.Kind {
	case hir.ExprAwait:
		data, ok := expr.Data.(hir.AwaitData)
		if !ok || data.Value == nil {
			return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: invalid await expr")
		}
		task, err := l.lowerExpr(data.Value, false)
		if err != nil {
			return SelectArm{}, loweredSelectArm{}, err
		}
		return SelectArm{
				Kind: SelectArmTask,
				Task: task,
			}, loweredSelectArm{
				kind: SelectArmTask,
				task: task,
			}, nil
	case hir.ExprCall:
		data, ok := expr.Data.(hir.CallData)
		if !ok {
			return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: invalid call payload")
		}
		name := l.selectCallName(data.Callee)
		name = baseSymbolName(name)
		recvExpr := l.selectCallReceiver(data.Callee)
		switch name {
		case "await":
			var taskExpr *hir.Expr
			switch {
			case recvExpr != nil:
				if len(data.Args) != 0 {
					return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: await expects no explicit arguments")
				}
				taskExpr = recvExpr
			case len(data.Args) == 1:
				taskExpr = data.Args[0]
			default:
				return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: await expects 1 argument")
			}
			task, err := l.lowerExpr(taskExpr, false)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			return SelectArm{
					Kind: SelectArmTask,
					Task: task,
				}, loweredSelectArm{
					kind: SelectArmTask,
					task: task,
				}, nil
		case "recv":
			var chExpr *hir.Expr
			switch {
			case recvExpr != nil:
				if len(data.Args) != 0 {
					return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: recv expects no explicit arguments")
				}
				chExpr = recvExpr
			case len(data.Args) == 1:
				chExpr = data.Args[0]
			default:
				return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: recv expects 1 argument")
			}
			ch, chLocal, err := l.lowerLocalSelectChannelOperand(chExpr)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			return SelectArm{
					Kind:    SelectArmChanRecv,
					Channel: ch,
				}, loweredSelectArm{
					kind:         SelectArmChanRecv,
					channelLocal: chLocal,
				}, nil
		case "send":
			var (
				chExpr  *hir.Expr
				valExpr *hir.Expr
			)
			switch {
			case recvExpr != nil:
				if len(data.Args) != 1 {
					return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: send expects 1 argument")
				}
				chExpr = recvExpr
				valExpr = data.Args[0]
			case len(data.Args) == 2:
				chExpr = data.Args[0]
				valExpr = data.Args[1]
			default:
				return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: send expects 2 arguments")
			}
			ch, chLocal, err := l.lowerLocalSelectChannelOperand(chExpr)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			val, directOwnedBinding := l.localSelectOwnedBindingOperand(valExpr)
			if !directOwnedBinding {
				val, err = l.lowerExpr(valExpr, true)
				if err != nil {
					return SelectArm{}, loweredSelectArm{}, err
				}
				valTmp := l.newTemp(val.Type, "select_val", expr.Span)
				l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
					Dst: Place{Local: valTmp},
					Src: RValue{Kind: RValueUse, Use: val},
				}})
				val = l.placeOperand(Place{Local: valTmp}, val.Type, true)
			}
			return SelectArm{
					Kind:    SelectArmChanSend,
					Channel: ch,
					Value:   val,
				}, loweredSelectArm{
					kind:         SelectArmChanSend,
					channelLocal: chLocal,
					valueLocal:   val.Place.Local,
				}, nil
		case "timeout":
			if len(data.Args) != 2 {
				return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: timeout expects 2 arguments")
			}
			task, err := l.lowerExpr(data.Args[0], false)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			ms, err := l.lowerExpr(data.Args[1], false)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			msTmp := l.newTemp(ms.Type, "select_ms", expr.Span)
			l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: msTmp},
				Src: RValue{Kind: RValueUse, Use: ms},
			}})
			return SelectArm{
					Kind: SelectArmTimeout,
					Task: task,
					Ms:   l.placeOperand(Place{Local: msTmp}, ms.Type, false),
				}, loweredSelectArm{
					kind:    SelectArmTimeout,
					task:    task,
					msLocal: msTmp,
				}, nil
		}
	}
	return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: unsupported expression")
}

// lowerLocalSelectChannelOperand keeps an already-stable bare channel binding
// as the select arm operand. A select only borrows the receiver while polling,
// so copying that binding into tmp_select_ch manufactures an alias that the
// pending async state later treats as an owner. Only the exact HIR VarRef plus
// bare-local COPY shape takes this path; projections and all other expressions
// retain the evaluate-once temp fallback.
//
// That fallback temp is a HOLDER, not an alias. The select is re-polled from
// the temp after every resume, so the temp is live across the suspension and
// is packed into the pending frame as an owner (`operandForAsyncStateStore`
// moves a heap-owning local); a cancellation that abandons that frame then
// releases what the temp holds. Copied bare, it would give back a reference
// the source binding still counts on — one channel, two releases. So the temp
// takes a reference of its own, and being born through `newTemp` it is also
// registered for the release at the end of the select statement, which is the
// balance for the winner and loser paths alike.
func (l *funcLowerer) lowerLocalSelectChannelOperand(receiver *hir.Expr) (Operand, LocalID, error) {
	if receiver == nil {
		return Operand{}, NoLocalID, fmt.Errorf("mir: local select channel receiver is missing")
	}
	ch, err := l.lowerExpr(receiver, false)
	if err != nil {
		return Operand{}, NoLocalID, err
	}
	if receiver.Kind == hir.ExprVarRef && ch.Kind == OperandCopy &&
		ch.Place.Kind == PlaceLocal && len(ch.Place.Proj) == 0 && ch.Place.Local != NoLocalID {
		return ch, ch.Place.Local, nil
	}
	if ch.Kind == OperandCopy && l.isRefCounted(ch.Type) {
		ch.Kind = OperandRetain
	}
	tmp := l.newTemp(ch.Type, "select_ch", receiver.Span)
	l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: tmp},
		Src: RValue{Kind: RValueUse, Use: ch},
	}})
	return l.placeOperand(Place{Local: tmp}, ch.Type, false), tmp, nil
}

// localSelectOwnedBindingOperand recognizes the one select-send shape whose
// ownership is conditional on which arm wins. rt_select_poll only borrows the
// bits while it is pending; a successful SEND transfers them to the channel,
// while a losing arm keeps using the same binding. Generic unary lowering
// would turn `own job` into an alias temp and the select lowering would then
// add another value temp, putting multiple alleged owners in the cancelled
// async state. Keep the exact bare local as the one MOVE operand instead.
//
// Sema already requires this shape for a non-Copy select payload. Staying
// deliberately exact here makes projections and all other expressions retain
// their ordinary lowering rather than silently widening the protocol.
func (l *funcLowerer) localSelectOwnedBindingOperand(value *hir.Expr) (Operand, bool) {
	if l == nil || l.f == nil || value == nil || value.Kind != hir.ExprUnaryOp {
		return Operand{}, false
	}
	unary, ok := value.Data.(hir.UnaryOpData)
	if !ok || unary.Op != ast.ExprUnaryOwn || unary.Operand == nil || unary.Operand.Kind != hir.ExprVarRef {
		return Operand{}, false
	}
	ref, ok := unary.Operand.Data.(hir.VarRefData)
	if !ok || !ref.SymbolID.IsValid() {
		return Operand{}, false
	}
	local, ok := l.symToLocal[ref.SymbolID]
	if !ok || local == NoLocalID || int(local) < 0 || int(local) >= len(l.f.Locals) {
		return Operand{}, false
	}
	localInfo := l.f.Locals[local]
	localType := localInfo.Type
	if localType == types.NoTypeID || localInfo.Flags&LocalFlagOwnsHeap == 0 ||
		localInfo.Flags&LocalFlagCopy != 0 {
		return Operand{}, false
	}
	return Operand{
		Kind:  OperandMove,
		Type:  localType,
		Place: Place{Local: local},
	}, true
}

func (l *funcLowerer) unwrapSelectAwaitExpr(expr *hir.Expr) *hir.Expr {
	for expr != nil {
		switch expr.Kind {
		case hir.ExprBlock:
			data, ok := expr.Data.(hir.BlockExprData)
			if !ok || data.Block == nil || len(data.Block.Stmts) != 1 {
				return nil
			}
			stmt := data.Block.Stmts[0]
			value := l.unwrapSelectBlockResult(&stmt)
			if value == nil {
				return nil
			}
			expr = value
		default:
			return expr
		}
	}
	return nil
}

func (l *funcLowerer) unwrapSelectBlockResult(stmt *hir.Stmt) *hir.Expr {
	if stmt == nil {
		return nil
	}
	switch stmt.Kind {
	case hir.StmtReturn:
		ret, ok := stmt.Data.(hir.ReturnData)
		if !ok {
			return nil
		}
		return ret.Value
	case hir.StmtRet:
		ret, ok := stmt.Data.(hir.RetData)
		if !ok {
			return nil
		}
		return ret.Value
	default:
		return nil
	}
}

func (l *funcLowerer) selectCallName(expr *hir.Expr) string {
	if expr == nil {
		return ""
	}
	switch expr.Kind {
	case hir.ExprVarRef:
		if data, ok := expr.Data.(hir.VarRefData); ok {
			return data.Name
		}
	case hir.ExprFieldAccess:
		if data, ok := expr.Data.(hir.FieldAccessData); ok {
			return data.FieldName
		}
	}
	return ""
}

func (l *funcLowerer) selectCallReceiver(expr *hir.Expr) *hir.Expr {
	if expr == nil {
		return nil
	}
	if expr.Kind != hir.ExprFieldAccess {
		return nil
	}
	data, ok := expr.Data.(hir.FieldAccessData)
	if !ok {
		return nil
	}
	return data.Object
}
