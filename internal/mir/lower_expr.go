package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

// lowerPlace lowers a HIR expression to a place (assignable location).
func (l *funcLowerer) lowerPlace(e *hir.Expr) (Place, error) {
	if l == nil || e == nil {
		return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got <nil>")
	}
	switch e.Kind {
	case hir.ExprVarRef:
		data, ok := e.Data.(hir.VarRefData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: var ref: unexpected payload %T", e.Data)
		}
		if !data.SymbolID.IsValid() {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: var ref %q has no symbol id", data.Name)
		}
		if l.consts != nil {
			if decl := l.consts[data.SymbolID]; decl != nil {
				return Place{Local: NoLocalID}, fmt.Errorf("mir: const %q is not assignable", decl.Name)
			}
		}
		local, ok := l.symToLocal[data.SymbolID]
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: unknown local symbol %d (%s)", data.SymbolID, data.Name)
		}
		return Place{Local: local}, nil

	case hir.ExprUnaryOp:
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: unary: unexpected payload %T", e.Data)
		}
		if data.Op != ast.ExprUnaryDeref {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got UnaryOp %s", data.Op)
		}
		base, err := l.lowerPlace(data.Operand)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}
		base.Proj = append(base.Proj, PlaceProj{Kind: PlaceProjDeref})
		return base, nil

	case hir.ExprFieldAccess:
		data, ok := e.Data.(hir.FieldAccessData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: field: unexpected payload %T", e.Data)
		}
		base, err := l.lowerPlace(data.Object)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}
		if l.types != nil && data.Object != nil && data.Object.Type != types.NoTypeID {
			if tt, ok := l.types.Lookup(data.Object.Type); ok && tt.Kind == types.KindReference {
				base.Proj = append(base.Proj, PlaceProj{Kind: PlaceProjDeref})
			}
		}
		base.Proj = append(base.Proj, PlaceProj{
			Kind:      PlaceProjField,
			FieldName: data.FieldName,
			FieldIdx:  data.FieldIdx,
		})
		return base, nil

	case hir.ExprIndex:
		data, ok := e.Data.(hir.IndexData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: index: unexpected payload %T", e.Data)
		}
		base, err := l.lowerPlace(data.Object)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}
		if l.types != nil && data.Object != nil && data.Object.Type != types.NoTypeID {
			if tt, ok := l.types.Lookup(data.Object.Type); ok && tt.Kind == types.KindReference {
				base.Proj = append(base.Proj, PlaceProj{Kind: PlaceProjDeref})
			}
		}
		idxOp, err := l.lowerExpr(data.Index, true)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}

		idxTmp := l.newTemp(idxOp.Type, "idx", e.Span)
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: idxTmp},
				Src: RValue{Kind: RValueUse, Use: idxOp},
			},
		})

		base.Proj = append(base.Proj, PlaceProj{
			Kind:       PlaceProjIndex,
			IndexLocal: idxTmp,
		})
		return base, nil

	default:
		return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got %s", e.Kind)
	}
}

// lowerExpr lowers a HIR expression to an MIR operand.
func (l *funcLowerer) lowerExpr(e *hir.Expr, consume bool) (Operand, error) {
	if l == nil {
		return Operand{}, nil
	}
	if e == nil {
		return l.constNothing(types.NoTypeID), nil
	}

	switch e.Kind {
	case hir.ExprLiteral:
		data, ok := e.Data.(hir.LiteralData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: literal: unexpected payload %T", e.Data)
		}
		return l.lowerLiteral(e.Type, data), nil

	case hir.ExprVarRef:
		data, ok := e.Data.(hir.VarRefData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: var ref: unexpected payload %T", e.Data)
		}
		if !data.SymbolID.IsValid() {
			return Operand{}, fmt.Errorf("mir: unsupported global value reference %q", data.Name)
		}
		local, ok := l.symToLocal[data.SymbolID]
		if !ok {
			if op, handled, err := l.lowerConstValue(data.SymbolID, consume); handled {
				return op, err
			}
			return Operand{}, fmt.Errorf("mir: unknown local symbol %d (%s)", data.SymbolID, data.Name)
		}
		ty := e.Type
		if ty == types.NoTypeID && l.f != nil {
			idx := int(local)
			if idx >= 0 && idx < len(l.f.Locals) {
				if lty := l.f.Locals[idx].Type; lty != types.NoTypeID {
					ty = lty
				}
			}
		}
		return l.placeOperand(Place{Local: local}, ty, consume), nil

	case hir.ExprUnaryOp:
		return l.lowerUnaryOpExpr(e, consume)

	case hir.ExprBinaryOp:
		return l.lowerBinaryOpExpr(e, consume)

	case hir.ExprCast:
		return l.lowerCastExpr(e, consume)

	case hir.ExprFieldAccess:
		return l.lowerFieldAccessExpr(e, consume)

	case hir.ExprIndex:
		return l.lowerIndexExpr(e, consume)

	case hir.ExprStructLit:
		return l.lowerStructLitExpr(e, consume)

	case hir.ExprArrayLit:
		return l.lowerArrayLitExpr(e, consume)

	case hir.ExprTupleLit:
		return l.lowerTupleLitExpr(e, consume)

	case hir.ExprTagTest:
		return l.lowerTagTestExpr(e, consume)

	case hir.ExprTagPayload:
		return l.lowerTagPayloadExpr(e, consume)

	case hir.ExprIterInit:
		return l.lowerIterInitExpr(e, consume)

	case hir.ExprIterNext:
		return l.lowerIterNextExpr(e, consume)

	case hir.ExprCall:
		return l.lowerCallExpr(e, consume)

	case hir.ExprIf:
		data, ok := e.Data.(hir.IfData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: if-expr: unexpected payload %T", e.Data)
		}
		return l.lowerIfExpr(e, data, consume)

	case hir.ExprBlock:
		data, ok := e.Data.(hir.BlockExprData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: block-expr: unexpected payload %T", e.Data)
		}
		return l.lowerBlockExpr(e, data, consume)

	case hir.ExprAwait:
		return l.lowerAwaitExpr(e, consume)

	case hir.ExprSpawn:
		return l.lowerSpawnExpr(e, consume)

	case hir.ExprAsync:
		return Operand{}, fmt.Errorf("mir: async blocks are not supported yet")

	case hir.ExprCompare:
		return Operand{}, fmt.Errorf("mir: compare must be normalized before MIR lowering")

	default:
		return Operand{}, fmt.Errorf("mir: unsupported expr kind %s", e.Kind)
	}
}

// lowerUnaryOpExpr lowers a unary operation expression.
func (l *funcLowerer) lowerUnaryOpExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.UnaryOpData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: unary: unexpected payload %T", e.Data)
	}
	if data.Op == ast.ExprUnaryRef || data.Op == ast.ExprUnaryRefMut {
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

	operand, err := l.lowerExpr(data.Operand, false)
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
		if data.TypeRight == types.NoTypeID {
			return Operand{}, fmt.Errorf("mir: is missing type operand")
		}
		left, err := l.lowerExpr(data.Left, false)
		if err != nil {
			return Operand{}, err
		}
		resultTy := e.Type
		if resultTy == types.NoTypeID && l.types != nil {
			resultTy = l.types.Builtins().Bool
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
		if data.TypeLeft == types.NoTypeID || data.TypeRight == types.NoTypeID {
			return Operand{}, fmt.Errorf("mir: heir missing type operands")
		}
		resultTy := e.Type
		if resultTy == types.NoTypeID && l.types != nil {
			resultTy = l.types.Builtins().Bool
		}
		tmp := l.newTemp(resultTy, "heir", e.Span)
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: tmp},
				Src: RValue{
					Kind:     RValueHeirTest,
					HeirTest: HeirTest{LeftTy: data.TypeLeft, RightTy: data.TypeRight},
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
	left, err := l.lowerExpr(data.Left, false)
	if err != nil {
		return Operand{}, err
	}
	right, err := l.lowerExpr(data.Right, false)
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
	tmp := l.newTemp(resultTy, "bin", e.Span)
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
	value, err := l.lowerExpr(data.Value, false)
	if err != nil {
		return Operand{}, err
	}
	resultTy := e.Type
	if resultTy == types.NoTypeID {
		resultTy = data.TargetTy
	}
	targetTy := data.TargetTy
	if targetTy == types.NoTypeID {
		targetTy = resultTy
	}
	tmp := l.newTemp(resultTy, "cast", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueCast, Cast: CastOp{Value: value, TargetTy: targetTy}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, resultTy, consume), nil
}

// lowerFieldAccessExpr lowers a field access expression.
func (l *funcLowerer) lowerFieldAccessExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.FieldAccessData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: field: unexpected payload %T", e.Data)
	}
	obj, err := l.lowerExpr(data.Object, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "field", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{
				Kind: RValueField,
				Field: FieldAccess{
					Object:    obj,
					FieldName: data.FieldName,
					FieldIdx:  data.FieldIdx,
				},
			},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerIndexExpr lowers an index expression.
func (l *funcLowerer) lowerIndexExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.IndexData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: index: unexpected payload %T", e.Data)
	}
	obj, err := l.lowerExpr(data.Object, false)
	if err != nil {
		return Operand{}, err
	}
	idx, err := l.lowerExpr(data.Index, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "idx", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueIndex, Index: IndexAccess{Object: obj, Index: idx}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerStructLitExpr lowers a struct literal expression.
func (l *funcLowerer) lowerStructLitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.StructLitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: struct lit: unexpected payload %T", e.Data)
	}
	fields := make([]StructLitField, 0, len(data.Fields))
	for _, f := range data.Fields {
		if f.Value == nil {
			continue
		}
		val, err := l.lowerExpr(f.Value, true)
		if err != nil {
			return Operand{}, err
		}
		fields = append(fields, StructLitField{Name: f.Name, Value: val})
	}
	tmp := l.newTemp(e.Type, "struct", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueStructLit, StructLit: StructLit{TypeID: data.TypeID, Fields: fields}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerArrayLitExpr lowers an array literal expression.
func (l *funcLowerer) lowerArrayLitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.ArrayLitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: array lit: unexpected payload %T", e.Data)
	}
	elems := make([]Operand, 0, len(data.Elements))
	for _, el := range data.Elements {
		op, err := l.lowerExpr(el, true)
		if err != nil {
			return Operand{}, err
		}
		elems = append(elems, op)
	}
	tmp := l.newTemp(e.Type, "arr", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueArrayLit, ArrayLit: ArrayLit{Elems: elems}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerTupleLitExpr lowers a tuple literal expression.
func (l *funcLowerer) lowerTupleLitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.TupleLitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: tuple lit: unexpected payload %T", e.Data)
	}
	elems := make([]Operand, 0, len(data.Elements))
	for _, el := range data.Elements {
		op, err := l.lowerExpr(el, true)
		if err != nil {
			return Operand{}, err
		}
		elems = append(elems, op)
	}
	tmp := l.newTemp(e.Type, "tup", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueTupleLit, TupleLit: TupleLit{Elems: elems}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

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

// lowerSpawnExpr lowers a spawn expression.
func (l *funcLowerer) lowerSpawnExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.SpawnData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: spawn: unexpected payload %T", e.Data)
	}
	value, err := l.lowerExpr(data.Value, true)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "spawn", e.Span)
	l.emit(&Instr{Kind: InstrSpawn, Spawn: SpawnInstr{Dst: Place{Local: tmp}, Value: value}})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}
