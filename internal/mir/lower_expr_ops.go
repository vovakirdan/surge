package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

func (l *funcLowerer) lowerUnaryOpExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.UnaryOpData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: unary: unexpected payload %T", e.Data)
	}
	if data.Op == ast.ExprUnaryRef || data.Op == ast.ExprUnaryRefMut {
		if data.Op == ast.ExprUnaryRef && data.Operand != nil && data.Operand.Kind == hir.ExprLiteral {
			if lit, ok := data.Operand.Data.(hir.LiteralData); ok && lit.Kind == hir.LiteralString {
				if l.isSharedStringRefType(e.Type) {
					global := l.staticStringGlobal(lit.StringValue)
					if global != NoGlobalID {
						return Operand{Kind: OperandAddrOf, Type: e.Type, Place: Place{Kind: PlaceGlobal, Global: global}}, nil
					}
				}
			}
		}
		place, err := l.lowerPlace(data.Operand)
		if err != nil {
			val, valErr := l.lowerExpr(data.Operand, false)
			if valErr != nil {
				return Operand{}, err
			}
			tmpType := val.Type
			if tmpType == types.NoTypeID && l.types != nil && e.Type != types.NoTypeID {
				if tt, ok := l.types.Lookup(e.Type); ok && tt.Kind == types.KindReference {
					tmpType = tt.Elem
				}
			}
			tmp := l.newTemp(tmpType, "ref", e.Span)
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Local: tmp},
					Src: RValue{Kind: RValueUse, Use: val},
				},
			})
			place = Place{Local: tmp}
		}
		kind := OperandAddrOf
		if data.Op == ast.ExprUnaryRefMut {
			kind = OperandAddrOfMut
		}
		return Operand{Kind: kind, Type: e.Type, Place: place}, nil
	}

	var operand Operand
	var err error
	if data.Op == ast.ExprUnaryDeref {
		operand, err = l.lowerExpr(data.Operand, false)
	} else {
		operand, err = l.lowerValueExpr(data.Operand, false)
	}
	if err != nil {
		return Operand{}, err
	}
	resultTy := e.Type
	if resultTy == types.NoTypeID {
		// For deref operations, get the element type from the operand's reference/pointer type
		if data.Op == ast.ExprUnaryDeref && operand.Type != types.NoTypeID && l.types != nil {
			if tt, ok := l.types.Lookup(operand.Type); ok {
				if tt.Kind == types.KindReference || tt.Kind == types.KindPointer || tt.Kind == types.KindOwn {
					resultTy = tt.Elem
				}
			}
		}
		// Fallback to operand type if deref extraction didn't work
		if resultTy == types.NoTypeID {
			resultTy = operand.Type
		}
	}
	tmp := l.newTemp(resultTy, "un", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUnaryOp, Unary: UnaryOp{Op: data.Op, Operand: operand}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, resultTy, consume), nil
}

// lowerBinaryOpExpr lowers a binary operation expression.
func (l *funcLowerer) lowerBinaryOpExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.BinaryOpData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: binary: unexpected payload %T", e.Data)
	}
	if data.Op == ast.ExprBinaryIs {
		resultTy := e.Type
		if resultTy == types.NoTypeID && l.types != nil {
			resultTy = l.types.Builtins().Bool
		}
		if data.TypeRight == types.NoTypeID {
			return Operand{
				Kind: OperandConst,
				Type: resultTy,
				Const: Const{
					Kind:      ConstBool,
					Type:      resultTy,
					BoolValue: false,
				},
			}, nil
		}
		left, err := l.lowerExpr(data.Left, false)
		if err != nil {
			return Operand{}, err
		}
		tmp := l.newTemp(resultTy, "is", e.Span)
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: tmp},
				Src: RValue{
					Kind:     RValueTypeTest,
					TypeTest: TypeTest{Value: left, TargetTy: data.TypeRight},
				},
			},
		})
		return l.placeOperand(Place{Local: tmp}, resultTy, consume), nil
	}
	if data.Op == ast.ExprBinaryHeir {
		resultTy := e.Type
		if resultTy == types.NoTypeID && l.types != nil {
			resultTy = l.types.Builtins().Bool
		}
		if data.TypeRight == types.NoTypeID {
			return Operand{
				Kind: OperandConst,
				Type: resultTy,
				Const: Const{
					Kind:      ConstBool,
					Type:      resultTy,
					BoolValue: false,
				},
			}, nil
		}
		left, err := l.lowerExpr(data.Left, false)
		if err != nil {
			return Operand{}, err
		}
		tmp := l.newTemp(resultTy, "heir", e.Span)
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: tmp},
				Src: RValue{
					Kind:     RValueHeirTest,
					HeirTest: HeirTest{Value: left, TargetTy: data.TypeRight},
				},
			},
		})
		return l.placeOperand(Place{Local: tmp}, resultTy, consume), nil
	}
	if data.Op == ast.ExprBinaryAssign {
		return l.lowerAssignExpr(e, data, consume)
	}
	if base, ok := assignmentBaseOp(data.Op); ok {
		return l.lowerCompoundAssignExpr(e, data, base, consume)
	}
	if isLogicalShortCircuitOp(data.Op) {
		return l.lowerLogicalShortCircuitExpr(e, data, consume)
	}
	left, err := l.lowerValueExpr(data.Left, false)
	if err != nil {
		return Operand{}, err
	}
	right, err := l.lowerValueExpr(data.Right, false)
	if err != nil {
		return Operand{}, err
	}
	resultTy := e.Type
	if resultTy == types.NoTypeID {
		// Fallback: use the operand types (already computed from lowering)
		if left.Type != types.NoTypeID { //nolint:gocritic // if-else chain is clearer here than switch
			resultTy = left.Type
		} else if right.Type != types.NoTypeID {
			resultTy = right.Type
		} else {
			// Further fallback: try to get type from HIR expressions
			if data.Left != nil && data.Left.Type != types.NoTypeID {
				resultTy = data.Left.Type
			} else if data.Right != nil && data.Right.Type != types.NoTypeID {
				resultTy = data.Right.Type
			}
		}
	}
	// A binary operation MATERIALIZES its result; nothing else holds it.
	tmp := l.markOwningTemp(l.newTemp(resultTy, "bin", e.Span))
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{
				Kind:   RValueBinaryOp,
				Binary: BinaryOp{Op: data.Op, Left: left, Right: right},
			},
		},
	})
	return l.placeOperand(Place{Local: tmp}, resultTy, consume), nil
}

// lowerCastExpr lowers a cast expression.
func (l *funcLowerer) lowerCastExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.CastData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: cast: unexpected payload %T", e.Data)
	}
	resultTy := e.Type
	if resultTy == types.NoTypeID {
		resultTy = data.TargetTy
	}
	targetTy := data.TargetTy
	if targetTy == types.NoTypeID {
		targetTy = resultTy
	}
	valueExpr := data.Value
	if valueExpr != nil && valueExpr.Type == types.NoTypeID && targetTy != types.NoTypeID && valueExpr.Kind == hir.ExprLiteral {
		clone := *valueExpr
		clone.Type = targetTy
		valueExpr = &clone
	}
	// A cast between one type and itself changes NOTHING: every backend hands
	// the source value straight back. Routing it through a temp anyway would
	// give that temp a release — refcounted scalars get one the moment they are
	// born — while the source it aliases is still owned by the binding it was
	// read from, so the release frees storage that binding still holds. Read
	// the source the way the CALLER asked instead, which makes `a to float`
	// exactly as owned, and as cheap, as `a`.
	if l.castKeepsRepresentation(valueExpr, targetTy) {
		return l.lowerCastAsAlias(valueExpr, resultTy, consume)
	}
	value, err := l.lowerValueExpr(valueExpr, false)
	if err != nil {
		return Operand{}, err
	}
	// A cast that changes representation builds a new value — a tag cast
	// allocates a fresh box and frees the source. Deliberately NOT extended to
	// the unary temp above: a deref temp ALIASES its pointee, and treating one
	// as owned hands away storage the borrow still points at.
	tmp := l.markOwningTemp(l.newTemp(resultTy, "cast", e.Span))
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueCast, Cast: CastOp{Value: value, TargetTy: targetTy}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, resultTy, consume), nil
}

// castKeepsRepresentation reports whether a cast leaves the value alone: its
// source and target name the same type, so nothing is built and nothing is
// converted.
//
// It answers only for the INTRINSIC cast, which is the only thing that reaches
// here — a `to` that resolves to a user `__to` is already a call by the time
// HIR is built, and a call's result is a fresh value it really does own.
//
// Aliases resolve, so `type Celsius = float` is the same representation as
// `float`. `own` and `&` deliberately do NOT: they say something about who
// holds the value, which is the very question this predicate feeds.
func (l *funcLowerer) castKeepsRepresentation(value *hir.Expr, target types.TypeID) bool {
	if l == nil || l.types == nil || value == nil {
		return false
	}
	if value.Type == types.NoTypeID || target == types.NoTypeID {
		return false
	}
	return resolveAlias(l.types, value.Type) == resolveAlias(l.types, target)
}

// lowerCastAsAlias lowers a cast that converts nothing as the read it really
// is. The result carries the cast's own type so later stages see the name the
// source wrote, which for an alias type is the only thing the cast changed.
func (l *funcLowerer) lowerCastAsAlias(value *hir.Expr, resultTy types.TypeID, consume bool) (Operand, error) {
	op, err := l.lowerValueExpr(value, consume)
	if err != nil {
		return Operand{}, err
	}
	if resultTy != types.NoTypeID {
		op.Type = resultTy
		if op.Kind == OperandConst {
			op.Const.Type = resultTy
		}
	}
	return op, nil
}

// lowerFieldAccessExpr lowers a field access expression.
