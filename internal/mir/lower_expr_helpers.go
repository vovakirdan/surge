package mir

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// lowerLiteral converts a HIR literal to an MIR operand.
func (l *funcLowerer) lowerLiteral(ty types.TypeID, lit hir.LiteralData) Operand {
	out := Operand{Kind: OperandConst, Type: ty}
	out.Const.Type = ty

	switch lit.Kind {
	case hir.LiteralInt:
		out.Const.Text = lit.Text
		isUint := false
		if l.types != nil && ty != types.NoTypeID {
			if tt, ok := l.types.Lookup(resolveAlias(l.types, ty)); ok && tt.Kind == types.KindUint {
				isUint = true
			}
		}
		if isUint {
			out.Const.Kind = ConstUint
			val, err := safecast.Conv[uint64](lit.IntValue)
			if err != nil {
				out.Const.Kind = ConstInt
				out.Const.IntValue = lit.IntValue
			} else {
				out.Const.UintValue = val
			}
		} else {
			out.Const.Kind = ConstInt
			out.Const.IntValue = lit.IntValue
		}
	case hir.LiteralFloat:
		out.Const.Text = lit.Text
		out.Const.Kind = ConstFloat
		out.Const.FloatValue = lit.FloatValue
	case hir.LiteralBool:
		out.Const.Kind = ConstBool
		out.Const.BoolValue = lit.BoolValue
	case hir.LiteralString:
		out.Const.Kind = ConstString
		out.Const.StringValue = lit.StringValue
	case hir.LiteralNothing:
		out.Const.Kind = ConstNothing
	default:
		out.Const.Kind = ConstNothing
	}

	return out
}

// constNothing creates a nothing constant operand.
func (l *funcLowerer) constNothing(ty types.TypeID) Operand {
	return Operand{Kind: OperandConst, Type: ty, Const: Const{Kind: ConstNothing, Type: ty}}
}

// placeOperand creates an operand for a place.
func (l *funcLowerer) placeOperand(place Place, ty types.TypeID, consume bool) Operand {
	kind := OperandCopy
	if consume && !l.isCopyType(ty) {
		kind = OperandMove
	}
	return Operand{Kind: kind, Type: ty, Place: place}
}

func (l *funcLowerer) unwrapReferenceType(id types.TypeID) types.TypeID {
	if l == nil || l.types == nil || id == types.NoTypeID {
		return id
	}
	tt, ok := l.types.Lookup(resolveAlias(l.types, id))
	if !ok || tt.Kind != types.KindReference {
		return id
	}
	return tt.Elem
}

func (l *funcLowerer) lowerValueExpr(e *hir.Expr, consume bool) (Operand, error) {
	if e == nil {
		return l.constNothing(types.NoTypeID), nil
	}
	if l.types != nil && e.Type != types.NoTypeID {
		if tt, ok := l.types.Lookup(resolveAlias(l.types, e.Type)); ok && tt.Kind == types.KindReference {
			deref := &hir.Expr{
				Kind: hir.ExprUnaryOp,
				Type: tt.Elem,
				Span: e.Span,
				Data: hir.UnaryOpData{
					Op:      ast.ExprUnaryDeref,
					Operand: e,
				},
			}
			return l.lowerExpr(deref, consume)
		}
	}
	return l.lowerExpr(e, consume)
}

func (l *funcLowerer) lowerExprForType(e *hir.Expr, expected types.TypeID) (Operand, error) {
	if e == nil {
		return l.constNothing(types.NoTypeID), nil
	}
	if e.Type == types.NoTypeID && expected != types.NoTypeID {
		// Fallback to expected type when sema didn't populate Expr.Type,
		// so we don't drop call results in return/assignment contexts.
		clone := *e
		clone.Type = expected
		e = &clone
	}
	consume := true
	if expected != types.NoTypeID && l.types != nil {
		if tt, ok := l.types.Lookup(resolveAlias(l.types, expected)); ok && tt.Kind == types.KindReference {
			return l.lowerExpr(e, consume)
		}
	}
	return l.lowerValueExpr(e, consume)
}

func (l *funcLowerer) lowerExprForSideEffects(e *hir.Expr) error {
	if e == nil {
		return nil
	}
	if e.Kind == hir.ExprIndex && l.types != nil && e.Type != types.NoTypeID {
		if tt, ok := l.types.Lookup(resolveAlias(l.types, e.Type)); ok && tt.Kind == types.KindReference {
			_, err := l.lowerValueExpr(e, false)
			return err
		}
	}
	_, err := l.lowerExpr(e, false)
	return err
}

func (l *funcLowerer) isSharedStringRefType(id types.TypeID) bool {
	if l == nil || l.types == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := l.types.Lookup(resolveAlias(l.types, id))
	if !ok || tt.Kind != types.KindReference || tt.Mutable {
		return false
	}
	return resolveAlias(l.types, tt.Elem) == l.types.Builtins().String
}

func (l *funcLowerer) staticStringGlobal(raw string) GlobalID {
	if l == nil || l.out == nil || l.types == nil {
		return NoGlobalID
	}
	if l.staticStringGlobals != nil {
		if id, ok := l.staticStringGlobals[raw]; ok {
			return id
		}
	}
	gidRaw, err := safecast.Conv[int32](len(l.out.Globals))
	if err != nil {
		panic(fmt.Errorf("mir: global id overflow: %w", err))
	}
	id := GlobalID(gidRaw)
	name := fmt.Sprintf("strlit$%d", id)
	l.out.Globals = append(l.out.Globals, Global{
		Sym:  symbols.NoSymbolID,
		Type: l.types.Builtins().String,
		Name: name,
	})
	if l.staticStringGlobals != nil {
		l.staticStringGlobals[raw] = id
	}
	if l.staticStringInits != nil {
		l.staticStringInits[id] = raw
	}
	return id
}

func (l *funcLowerer) lowerConstValue(symID symbols.SymbolID, consume bool) (Operand, bool, error) {
	if l == nil || !symID.IsValid() || l.consts == nil {
		return Operand{}, false, nil
	}
	decl := l.consts[symID]
	if decl == nil {
		return Operand{}, false, nil
	}
	if decl.Value == nil {
		return Operand{}, true, fmt.Errorf("mir: const %q has no value", decl.Name)
	}
	if l.constStack == nil {
		l.constStack = make(map[symbols.SymbolID]bool)
	}
	if l.constStack[symID] {
		return Operand{}, true, fmt.Errorf("mir: cyclic const evaluation for %q", decl.Name)
	}
	l.constStack[symID] = true
	op, err := l.lowerExpr(decl.Value, consume)
	delete(l.constStack, symID)
	if err != nil {
		return Operand{}, true, err
	}
	if op.Type == types.NoTypeID && decl.Type != types.NoTypeID {
		op.Type = decl.Type
	}
	return op, true, nil
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
	if fn == nil || len(data.Args) >= len(fn.Params) {
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
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: op},
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
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: op},
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

// lowerAssignExpr lowers an assignment expression.
func (l *funcLowerer) lowerAssignExpr(e *hir.Expr, data hir.BinaryOpData, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	dst, err := l.lowerPlace(data.Left)
	if err != nil {
		return Operand{}, err
	}
	expected := l.exprType(data.Left)
	if data.Left != nil && data.Left.Kind == hir.ExprIndex {
		expected = l.unwrapReferenceType(expected)
	}
	rhs, err := l.lowerExprForType(data.Right, expected)
	if err != nil {
		return Operand{}, err
	}
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: dst,
			Src: RValue{Kind: RValueUse, Use: rhs},
		},
	})

	resultTy := e.Type
	if resultTy == types.NoTypeID {
		if leftTy := l.exprType(data.Left); leftTy != types.NoTypeID {
			resultTy = leftTy
		} else {
			resultTy = rhs.Type
		}
	}
	return l.placeOperand(dst, resultTy, consume), nil
}

// lowerCompoundAssignExpr lowers a compound assignment expression (+=, -=, etc.).
func (l *funcLowerer) lowerCompoundAssignExpr(e *hir.Expr, data hir.BinaryOpData, base ast.ExprBinaryOp, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	dst, err := l.lowerPlace(data.Left)
	if err != nil {
		return Operand{}, err
	}

	resultTy := e.Type
	if resultTy == types.NoTypeID {
		resultTy = l.exprType(data.Left)
	}
	if data.Left != nil && data.Left.Kind == hir.ExprIndex {
		resultTy = l.unwrapReferenceType(resultTy)
	}

	left := l.placeOperand(dst, resultTy, false)
	right, err := l.lowerExprForType(data.Right, resultTy)
	if err != nil {
		return Operand{}, err
	}

	tmp := l.newTemp(resultTy, "cassign", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueBinaryOp, Binary: BinaryOp{Op: base, Left: left, Right: right}},
		},
	})
	tmpOp := l.placeOperand(Place{Local: tmp}, resultTy, true)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: dst,
			Src: RValue{Kind: RValueUse, Use: tmpOp},
		},
	})

	return l.placeOperand(dst, resultTy, consume), nil
}

// assignmentBaseOp returns the base binary operator for a compound assignment.
func assignmentBaseOp(op ast.ExprBinaryOp) (ast.ExprBinaryOp, bool) {
	switch op {
	case ast.ExprBinaryAddAssign:
		return ast.ExprBinaryAdd, true
	case ast.ExprBinarySubAssign:
		return ast.ExprBinarySub, true
	case ast.ExprBinaryMulAssign:
		return ast.ExprBinaryMul, true
	case ast.ExprBinaryDivAssign:
		return ast.ExprBinaryDiv, true
	case ast.ExprBinaryModAssign:
		return ast.ExprBinaryMod, true
	case ast.ExprBinaryBitAndAssign:
		return ast.ExprBinaryBitAnd, true
	case ast.ExprBinaryBitOrAssign:
		return ast.ExprBinaryBitOr, true
	case ast.ExprBinaryBitXorAssign:
		return ast.ExprBinaryBitXor, true
	case ast.ExprBinaryShlAssign:
		return ast.ExprBinaryShiftLeft, true
	case ast.ExprBinaryShrAssign:
		return ast.ExprBinaryShiftRight, true
	default:
		return 0, false
	}
}

// errorf is a helper to create formatted errors.
func errorf(format string, args ...any) error {
	return &mirError{msg: format, args: args}
}

type mirError struct {
	msg  string
	args []any
}

func (e *mirError) Error() string {
	return e.msg
}
