package mir

import (
	"fmt"
	"strings"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (l *funcLowerer) lowerCallArgExpr(argExpr *hir.Expr, paramType types.TypeID, calleeStores bool) (Operand, error) {
	if op, ok := l.lowerSharedRefReborrowArg(argExpr, paramType); ok {
		return op, nil
	}
	// Ownership transfers on a move, not on a copy. A by-value `string` param
	// is owned because passing it moved it; a by-value reference-counted
	// scalar is BORROWED, because passing it copied it and the caller stayed
	// the owner for the whole call. So the argument must not retain, and the
	// callee must not drop the parameter (sema's paramTransfersOwnership).
	//
	// This is also what keeps the arithmetic honest: `a + b` lowers to a magic
	// call whose runtime implementation takes `const void*` and frees nothing.
	// A retain here would leak on every operation.
	//
	// `calleeStores` is the exception: a tag constructor looks like a call but
	// is a STORE — `Some(x)` keeps the payload inside the union it builds,
	// which outlives the call, so the union needs its own reference exactly
	// like a struct-literal field does.
	if !calleeStores && argExpr != nil && l.isRefCountedScalar(argExpr.Type) {
		return l.lowerExpr(argExpr, false)
	}
	return l.lowerExpr(argExpr, true)
}

// calleeStoresArguments reports whether the call target keeps its arguments
// beyond the call rather than borrowing them for its duration. Tag
// constructors do: the value they build owns the payload.
func (l *funcLowerer) calleeStoresArguments(symID symbols.SymbolID) bool {
	if l == nil || !symID.IsValid() || l.symbols == nil ||
		l.symbols.Table == nil || l.symbols.Table.Symbols == nil {
		return false
	}
	sym := l.symbols.Table.Symbols.Get(symID)
	return sym != nil && sym.Kind == symbols.SymbolTag
}

func (l *funcLowerer) lowerSharedRefReborrowArg(argExpr *hir.Expr, paramType types.TypeID) (Operand, bool) {
	if l == nil || l.types == nil || argExpr == nil || argExpr.Type == types.NoTypeID || paramType == types.NoTypeID {
		return Operand{}, false
	}

	paramInfo, ok := l.types.Lookup(resolveAlias(l.types, paramType))
	if !ok || paramInfo.Kind != types.KindReference || paramInfo.Mutable {
		return Operand{}, false
	}

	argInfo, ok := l.types.Lookup(resolveAlias(l.types, argExpr.Type))
	if !ok || argInfo.Kind != types.KindReference || !argInfo.Mutable {
		return Operand{}, false
	}

	place, err := l.lowerPlace(argExpr)
	if err != nil {
		return Operand{}, false
	}
	if l.reborrowPlaceNeedsDeref(place) {
		place.Proj = append(place.Proj, PlaceProj{Kind: PlaceProjDeref})
	}
	return Operand{Kind: OperandAddrOf, Type: paramType, Place: place}, true
}

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
	stores := l.calleeStoresArguments(data.SymbolID)
	if fn == nil || fn.IsIntrinsic() || len(data.Args) >= len(fn.Params) {
		args := make([]Operand, 0, len(data.Args))
		for i, a := range data.Args {
			paramType := types.NoTypeID
			if fn != nil && i < len(fn.Params) {
				paramType = fn.Params[i].Type
			}
			op, err := l.lowerCallArgExpr(a, paramType, stores)
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
		op, err := l.lowerCallArgExpr(argExpr, params[i].Type, l.calleeStoresArguments(data.SymbolID))
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
		op, err := l.lowerExprForType(param.Default, param.Type)
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

	// Anchored crossing bodies: channel operations on the far anchor lower to
	// the runtime helpers that reach the channel through the current task's
	// pending and park by re-entering the body from the top (sema pinned the
	// operation as the body's first statement, so the replayed prefix is
	// empty). Returning from a helper means the operation completed. The far
	// member call carries no resolved symbol, so it is intercepted on its
	// HIR shape before the generic callee handling.
	if l.anchoredBody && data.Callee != nil && data.Callee.Kind == hir.ExprFieldAccess {
		if fa, ok := data.Callee.Data.(hir.FieldAccessData); ok && fa.Object != nil &&
			l.isFarChannelType(fa.Object.Type) {
			switch fa.FieldName {
			case "send":
				opArgs, argErr := l.lowerCallArgs(e, data)
				if argErr != nil {
					return Operand{}, argErr
				}
				if len(opArgs) == 1 {
					l.emit(&Instr{Kind: InstrCall, Call: CallInstr{
						Callee: Callee{Kind: CalleeValue, Name: "rt_anchored_channel_send"},
						Args:   opArgs,
					}})
					return l.constNothing(e.Type), nil
				}
			case "recv":
				tmp := l.newTemp(e.Type, "anchored_recv", e.Span)
				l.emit(&Instr{Kind: InstrChanRecv, ChanRecv: ChanRecvInstr{
					Dst:      Place{Local: tmp},
					Anchored: true,
					ReadyBB:  NoBlockID,
					PendBB:   NoBlockID,
				}})
				return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
			case "close":
				l.emit(&Instr{Kind: InstrCall, Call: CallInstr{
					Callee: Callee{Kind: CalleeValue, Name: "rt_anchored_channel_close"},
				}})
				return l.constNothing(e.Type), nil
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
						// Recover receiver type from symbol/local metadata when HIR lost ref info.
						needRecvType := recvType == types.NoTypeID
						if !needRecvType && l.types != nil {
							if tt, ok := l.types.Lookup(resolveAlias(l.types, recvType)); ok && tt.Kind != types.KindReference {
								needRecvType = true
							}
						}
						if needRecvType {
							if vr, ok := fa.Object.Data.(hir.VarRefData); ok && vr.SymbolID.IsValid() {
								if l.f != nil {
									if local, ok := l.symToLocal[vr.SymbolID]; ok {
										idx := int(local)
										if idx >= 0 && idx < len(l.f.Locals) && l.f.Locals[idx].Type != types.NoTypeID {
											recvType = l.f.Locals[idx].Type
										}
									}
								}
								if recvType == types.NoTypeID && l.out != nil {
									if global, ok := l.symToGlobal[vr.SymbolID]; ok {
										idx := int(global)
										if idx >= 0 && idx < len(l.out.Globals) && l.out.Globals[idx].Type != types.NoTypeID {
											recvType = l.out.Globals[idx].Type
										}
									}
								}
								if recvType == types.NoTypeID && l.symbols != nil && l.symbols.Table != nil && l.symbols.Table.Symbols != nil {
									if sym := l.symbols.Table.Symbols.Get(vr.SymbolID); sym != nil && sym.Type != types.NoTypeID {
										recvType = sym.Type
									}
								}
							}
						}
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
							if recvType != types.NoTypeID && op.Type != recvType {
								op.Type = recvType
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
		case "rt_net_wait_accept":
			if len(args) == 1 {
				l.emit(&Instr{Kind: InstrNetWait, NetWait: NetWaitInstr{
					Kind:    NetWaitAccept,
					Handle:  args[0],
					ReadyBB: NoBlockID,
					PendBB:  NoBlockID,
				}})
				return l.constNothing(e.Type), nil
			}
		case "rt_net_wait_readable":
			if len(args) == 1 {
				l.emit(&Instr{Kind: InstrNetWait, NetWait: NetWaitInstr{
					Kind:    NetWaitRead,
					Handle:  args[0],
					ReadyBB: NoBlockID,
					PendBB:  NoBlockID,
				}})
				return l.constNothing(e.Type), nil
			}
		case "rt_net_wait_writable":
			if len(args) == 1 {
				l.emit(&Instr{Kind: InstrNetWait, NetWait: NetWaitInstr{
					Kind:    NetWaitWrite,
					Handle:  args[0],
					ReadyBB: NoBlockID,
					PendBB:  NoBlockID,
				}})
				return l.constNothing(e.Type), nil
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
