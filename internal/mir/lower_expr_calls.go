package mir

import (
	"fmt"
	"strings"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (l *funcLowerer) calleeFunc(symID symbols.SymbolID) *hir.Func {
	if l == nil || !symID.IsValid() || l.mono == nil || l.mono.FuncBySym == nil {
		return nil
	}
	mf := l.mono.FuncBySym[symID]
	if mf == nil || mf.Func == nil {
		return nil
	}
	return mf.Func
}

func (l *funcLowerer) lowerCallArgs(e *hir.Expr, data hir.CallData) ([]Operand, error) {
	fn := l.calleeFunc(data.SymbolID)
	if fn == nil || fn.IsIntrinsic() || len(data.Args) >= len(fn.Params) {
		args := make([]Operand, 0, len(data.Args))
		for i, a := range data.Args {
			op, err := l.lowerExpr(a, true)
			if err != nil {
				return nil, err
			}
			if fn != nil && i < len(fn.Params) && fn.Params[i].Type != types.NoTypeID {
				span := e.Span
				if a != nil {
					span = a.Span
				}
				op = l.unionCastOperand(&op, fn.Params[i].Type, span)
			}
			args = append(args, op)
		}
		return args, nil
	}
	return l.lowerCallArgsWithDefaults(e, data, fn)
}

func (l *funcLowerer) lowerCallArgsWithDefaults(e *hir.Expr, data hir.CallData, fn *hir.Func) ([]Operand, error) {
	if l == nil || fn == nil {
		return nil, nil
	}
	params := fn.Params
	if len(data.Args) > len(params) {
		args := make([]Operand, 0, len(data.Args))
		for _, a := range data.Args {
			op, err := l.lowerExpr(a, true)
			if err != nil {
				return nil, err
			}
			args = append(args, op)
		}
		return args, nil
	}

	added := make([]symbols.SymbolID, 0, len(params))
	replaced := make(map[symbols.SymbolID]LocalID, len(params))
	bind := func(sym symbols.SymbolID, local LocalID) {
		if !sym.IsValid() {
			return
		}
		if prev, ok := l.symToLocal[sym]; ok {
			if _, recorded := replaced[sym]; !recorded {
				replaced[sym] = prev
			}
		} else {
			added = append(added, sym)
		}
		l.symToLocal[sym] = local
	}
	restore := func() {
		for sym, prev := range replaced {
			l.symToLocal[sym] = prev
		}
		for _, sym := range added {
			delete(l.symToLocal, sym)
		}
	}
	defer restore()

	args := make([]Operand, 0, len(params))
	for i, argExpr := range data.Args {
		op, err := l.lowerExpr(argExpr, true)
		if err != nil {
			return nil, err
		}
		span := e.Span
		if argExpr != nil {
			span = argExpr.Span
		}
		paramType := params[i].Type
		if paramType == types.NoTypeID {
			paramType = op.Type
		}
		tmp := l.newTemp(paramType, "arg", span)
		src := RValue{Kind: RValueUse, Use: op}
		if l.needsUnionCast(op.Type, paramType) {
			src = RValue{Kind: RValueCast, Cast: CastOp{Value: op, TargetTy: paramType}}
		}
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: src,
		}})
		bind(params[i].SymbolID, tmp)
		args = append(args, l.placeOperand(Place{Local: tmp}, paramType, true))
	}

	for i := len(data.Args); i < len(params); i++ {
		param := params[i]
		if !param.HasDefault || param.Default == nil {
			name := param.Name
			if name == "" {
				name = fmt.Sprintf("#%d", i)
			}
			return nil, fmt.Errorf("mir: call: missing default for parameter %q", name)
		}
		op, err := l.lowerExpr(param.Default, true)
		if err != nil {
			return nil, err
		}
		paramType := param.Type
		if paramType == types.NoTypeID {
			paramType = op.Type
		}
		tmp := l.newTemp(paramType, "default", e.Span)
		src := RValue{Kind: RValueUse, Use: op}
		if l.needsUnionCast(op.Type, paramType) {
			src = RValue{Kind: RValueCast, Cast: CastOp{Value: op, TargetTy: paramType}}
		}
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: src,
		}})
		bind(param.SymbolID, tmp)
		args = append(args, l.placeOperand(Place{Local: tmp}, paramType, true))
	}

	return args, nil
}

// lowerCallExpr lowers a HIR call expression to MIR.
func (l *funcLowerer) lowerCallExpr(e *hir.Expr, consume bool) (Operand, error) {
	if l == nil || e == nil || e.Kind != hir.ExprCall {
		return Operand{}, nil
	}
	data, ok := e.Data.(hir.CallData)
	if !ok {
		return Operand{}, errorf("mir: call: unexpected payload %T", e.Data)
	}

	if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef && len(data.Args) == 1 {
		if vr, ok := data.Callee.Data.(hir.VarRefData); ok && isAwaitSymbolName(vr.Name) {
			if recv := data.Args[0]; recv != nil && l.isTaskType(recv.Type) {
				task, err := l.lowerExpr(recv, false)
				if err != nil {
					return Operand{}, err
				}
				tmp := l.newTemp(e.Type, "await", e.Span)
				l.emit(&Instr{Kind: InstrAwait, Await: AwaitInstr{Dst: Place{Local: tmp}, Task: task}})
				return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
			}
		}
	}

	args, err := l.lowerCallArgs(e, data)
	if err != nil {
		return Operand{}, err
	}

	callee := Callee{Kind: CalleeSym, Sym: data.SymbolID}
	if callee.Sym.IsValid() {
		if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
			if vr, ok := data.Callee.Data.(hir.VarRefData); ok && vr.Name != "" {
				callee.Name = vr.Name
			}
		}
	} else {
		// Dynamic callee or unresolved intrinsic.
		resolved := false
		if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
			if vr, ok := data.Callee.Data.(hir.VarRefData); ok && vr.SymbolID.IsValid() {
				if _, ok := l.symToLocal[vr.SymbolID]; !ok {
					if _, ok := l.symToGlobal[vr.SymbolID]; !ok {
						if l.consts == nil || l.consts[vr.SymbolID] == nil {
							callee = Callee{Kind: CalleeSym, Sym: vr.SymbolID, Name: vr.Name}
							resolved = true
						}
					}
				}
			}
		}
		if !resolved {
			name := ""
			if data.Callee != nil {
				switch data.Callee.Kind {
				case hir.ExprFieldAccess:
					if fa, ok := data.Callee.Data.(hir.FieldAccessData); ok && fa.Object != nil && fa.FieldName == "__len" {
						recvType := fa.Object.Type
						isRef := false
						if l.types != nil && recvType != types.NoTypeID {
							if tt, ok := l.types.Lookup(resolveAlias(l.types, recvType)); ok && tt.Kind == types.KindReference {
								isRef = true
							}
						}
						var recvOp Operand
						if isRef {
							op, err := l.lowerExpr(fa.Object, true)
							if err != nil {
								return Operand{}, err
							}
							recvOp = op
						} else {
							place, err := l.lowerPlace(fa.Object)
							if err != nil {
								val, valErr := l.lowerExpr(fa.Object, false)
								if valErr != nil {
									return Operand{}, err
								}
								tmp := l.newTemp(val.Type, "ref", e.Span)
								l.emit(&Instr{
									Kind: InstrAssign,
									Assign: AssignInstr{
										Dst: Place{Local: tmp},
										Src: RValue{Kind: RValueUse, Use: val},
									},
								})
								place = Place{Local: tmp}
								recvType = val.Type
							}
							refType := recvType
							if l.types != nil && recvType != types.NoTypeID {
								refType = l.types.Intern(types.MakeReference(recvType, false))
							}
							recvOp = Operand{Kind: OperandAddrOf, Type: refType, Place: place}
						}
						args = append([]Operand{recvOp}, args...)
						name = fa.FieldName
					}
				case hir.ExprVarRef:
					if vr, ok := data.Callee.Data.(hir.VarRefData); ok && !vr.SymbolID.IsValid() && vr.Name != "" {
						name = vr.Name
					}
				}
			}
			if name != "" {
				callee.Name = name
			} else if data.Callee != nil {
				val, err := l.lowerExpr(data.Callee, false)
				if err != nil {
					return Operand{}, err
				}
				callee = Callee{Kind: CalleeValue, Value: val}
			}
		}
	}

	if l.f != nil && l.f.IsAsync {
		baseName := baseSymbolName(callee.Name)
		switch baseName {
		case "send":
			if len(args) == 2 && l.isChannelType(args[0].Type) {
				l.emit(&Instr{Kind: InstrChanSend, ChanSend: ChanSendInstr{
					Channel: args[0],
					Value:   args[1],
					ReadyBB: NoBlockID,
					PendBB:  NoBlockID,
				}})
				return l.constNothing(e.Type), nil
			}
		case "recv":
			if len(args) == 1 && l.isChannelType(args[0].Type) {
				tmp := l.newTemp(e.Type, "recv", e.Span)
				l.emit(&Instr{Kind: InstrChanRecv, ChanRecv: ChanRecvInstr{
					Dst:     Place{Local: tmp},
					Channel: args[0],
					ReadyBB: NoBlockID,
					PendBB:  NoBlockID,
				}})
				return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
			}
		case "timeout":
			if len(args) == 2 && l.isTaskType(args[0].Type) {
				task := args[0]
				if task.Kind == OperandMove {
					taskTmp := l.newTemp(task.Type, "timeout_task", e.Span)
					l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
						Dst: Place{Local: taskTmp},
						Src: RValue{Kind: RValueUse, Use: task},
					}})
					task = l.placeOperand(Place{Local: taskTmp}, task.Type, false)
				}
				ms := args[1]
				if ms.Kind == OperandMove {
					msTmp := l.newTemp(ms.Type, "timeout_ms", e.Span)
					l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
						Dst: Place{Local: msTmp},
						Src: RValue{Kind: RValueUse, Use: ms},
					}})
					ms = l.placeOperand(Place{Local: msTmp}, ms.Type, false)
				}
				tmp := l.newTemp(e.Type, "timeout", e.Span)
				l.emit(&Instr{Kind: InstrTimeout, Timeout: TimeoutInstr{
					Dst:     Place{Local: tmp},
					Task:    task,
					Ms:      ms,
					ReadyBB: NoBlockID,
					PendBB:  NoBlockID,
				}})
				return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
			}
		}
	}

	if e.Type == types.NoTypeID || l.isNothingType(e.Type) {
		l.emit(&Instr{Kind: InstrCall, Call: CallInstr{HasDst: false, Callee: callee, Args: args}})
		return l.constNothing(e.Type), nil
	}

	tmp := l.newTemp(e.Type, "call", e.Span)
	l.emit(&Instr{
		Kind: InstrCall,
		Call: CallInstr{
			HasDst: true,
			Dst:    Place{Local: tmp},
			Callee: callee,
			Args:   args,
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

func isAwaitSymbolName(name string) bool {
	if name == "await" {
		return true
	}
	return strings.HasPrefix(name, "await::<")
}

func baseSymbolName(name string) string {
	if idx := strings.Index(name, "::<"); idx >= 0 {
		return name[:idx]
	}
	return name
}
