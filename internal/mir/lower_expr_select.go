package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

type loweredSelectArm struct {
	arm         SelectArm
	kind        SelectArmKind
	taskLocal   LocalID
	channelLocal LocalID
	valueLocal  LocalID
	msLocal     LocalID
}

func (l *funcLowerer) lowerSelectExpr(e *hir.Expr, data hir.SelectData, isRace bool, consume bool) (Operand, error) {
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
				taskLocal := lowered[j].taskLocal
				if taskLocal == NoLocalID {
					continue
				}
				l.emit(&Instr{Kind: InstrCall, Call: CallInstr{
					HasDst: false,
					Callee: Callee{Kind: CalleeSym, Name: "cancel"},
					Args: []Operand{
						l.placeOperand(Place{Local: taskLocal}, l.f.Locals[taskLocal].Type, false),
					},
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
				if _, err := l.lowerExpr(arm.Result, false); err != nil {
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
		tmp := l.newTemp(task.Type, "select_task", expr.Span)
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: task},
		}})
		return SelectArm{
				Kind: SelectArmTask,
				Task: l.placeOperand(Place{Local: tmp}, task.Type, false),
			}, loweredSelectArm{
				kind:      SelectArmTask,
				taskLocal: tmp,
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
			tmp := l.newTemp(task.Type, "select_task", expr.Span)
			l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: tmp},
				Src: RValue{Kind: RValueUse, Use: task},
			}})
			return SelectArm{
					Kind: SelectArmTask,
					Task: l.placeOperand(Place{Local: tmp}, task.Type, false),
				}, loweredSelectArm{
					kind:      SelectArmTask,
					taskLocal: tmp,
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
			ch, err := l.lowerExpr(chExpr, false)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			tmp := l.newTemp(ch.Type, "select_ch", expr.Span)
			l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: tmp},
				Src: RValue{Kind: RValueUse, Use: ch},
			}})
			return SelectArm{
					Kind:    SelectArmChanRecv,
					Channel: l.placeOperand(Place{Local: tmp}, ch.Type, false),
				}, loweredSelectArm{
					kind:         SelectArmChanRecv,
					channelLocal: tmp,
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
			ch, err := l.lowerExpr(chExpr, false)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			chTmp := l.newTemp(ch.Type, "select_ch", expr.Span)
			l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: chTmp},
				Src: RValue{Kind: RValueUse, Use: ch},
			}})
			val, err := l.lowerExpr(valExpr, true)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			valTmp := l.newTemp(val.Type, "select_val", expr.Span)
			l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: valTmp},
				Src: RValue{Kind: RValueUse, Use: val},
			}})
			return SelectArm{
					Kind:    SelectArmChanSend,
					Channel: l.placeOperand(Place{Local: chTmp}, ch.Type, false),
					Value:   l.placeOperand(Place{Local: valTmp}, val.Type, true),
				}, loweredSelectArm{
					kind:         SelectArmChanSend,
					channelLocal: chTmp,
					valueLocal:   valTmp,
				}, nil
		case "timeout":
			if len(data.Args) != 2 {
				return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: timeout expects 2 arguments")
			}
			task, err := l.lowerExpr(data.Args[0], false)
			if err != nil {
				return SelectArm{}, loweredSelectArm{}, err
			}
			taskTmp := l.newTemp(task.Type, "select_task", expr.Span)
			l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: taskTmp},
				Src: RValue{Kind: RValueUse, Use: task},
			}})
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
					Task: l.placeOperand(Place{Local: taskTmp}, task.Type, false),
					Ms:   l.placeOperand(Place{Local: msTmp}, ms.Type, false),
				}, loweredSelectArm{
					kind:      SelectArmTimeout,
					taskLocal: taskTmp,
					msLocal:   msTmp,
				}, nil
		}
	}
	return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: unsupported expression")
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
			if stmt.Kind != hir.StmtReturn {
				return nil
			}
			ret, ok := stmt.Data.(hir.ReturnData)
			if !ok || ret.Value == nil {
				return nil
			}
			expr = ret.Value
		default:
			return expr
		}
	}
	return nil
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
