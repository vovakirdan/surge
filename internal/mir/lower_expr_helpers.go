package mir

import (
	"fmt"

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

// lowerCallExpr lowers a HIR call expression to MIR.
func (l *funcLowerer) lowerCallExpr(e *hir.Expr, consume bool) (Operand, error) {
	if l == nil || e == nil || e.Kind != hir.ExprCall {
		return Operand{}, nil
	}
	data, ok := e.Data.(hir.CallData)
	if !ok {
		return Operand{}, errorf("mir: call: unexpected payload %T", e.Data)
	}

	args := make([]Operand, 0, len(data.Args))
	for _, a := range data.Args {
		op, err := l.lowerExpr(a, true)
		if err != nil {
			return Operand{}, err
		}
		args = append(args, op)
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
		name := ""
		if data.Callee != nil && data.Callee.Kind == hir.ExprFieldAccess {
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
		}
		if name == "" && data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
			if vr, ok := data.Callee.Data.(hir.VarRefData); ok {
				name = vr.Name
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

// lowerAssignExpr lowers an assignment expression.
func (l *funcLowerer) lowerAssignExpr(e *hir.Expr, data hir.BinaryOpData, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	dst, err := l.lowerPlace(data.Left)
	if err != nil {
		return Operand{}, err
	}
	rhs, err := l.lowerExpr(data.Right, true)
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

	left := l.placeOperand(dst, resultTy, false)
	right, err := l.lowerExpr(data.Right, true)
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
