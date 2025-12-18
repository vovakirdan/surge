package mir

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

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
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: unary: unexpected payload %T", e.Data)
		}
		if data.Op == ast.ExprUnaryRef || data.Op == ast.ExprUnaryRefMut {
			place, err := l.lowerPlace(data.Operand)
			if err != nil {
				return Operand{}, err
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

	case hir.ExprBinaryOp:
		data, ok := e.Data.(hir.BinaryOpData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: binary: unexpected payload %T", e.Data)
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

	case hir.ExprCast:
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

	case hir.ExprFieldAccess:
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

	case hir.ExprIndex:
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

	case hir.ExprStructLit:
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

	case hir.ExprArrayLit:
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

	case hir.ExprTupleLit:
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

	case hir.ExprTagTest:
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

	case hir.ExprTagPayload:
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

	case hir.ExprIterInit:
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

	case hir.ExprIterNext:
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

	case hir.ExprSpawn:
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

	case hir.ExprAsync:
		return Operand{}, fmt.Errorf("mir: async blocks are not supported yet")

	case hir.ExprCompare:
		return Operand{}, fmt.Errorf("mir: compare must be normalized before MIR lowering")

	default:
		return Operand{}, fmt.Errorf("mir: unsupported expr kind %s", e.Kind)
	}
}

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

func (l *funcLowerer) constNothing(ty types.TypeID) Operand {
	return Operand{Kind: OperandConst, Type: ty, Const: Const{Kind: ConstNothing, Type: ty}}
}

func (l *funcLowerer) placeOperand(place Place, ty types.TypeID, consume bool) Operand {
	kind := OperandCopy
	if consume && !l.isCopyType(ty) {
		kind = OperandMove
	}
	return Operand{Kind: kind, Type: ty, Place: place}
}

func (l *funcLowerer) lowerCallExpr(e *hir.Expr, consume bool) (Operand, error) {
	if l == nil || e == nil || e.Kind != hir.ExprCall {
		return Operand{}, nil
	}
	data, ok := e.Data.(hir.CallData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: call: unexpected payload %T", e.Data)
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
		if data.Callee != nil && data.Callee.Kind == hir.ExprVarRef {
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
