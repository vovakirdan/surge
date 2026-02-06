package mir

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/source"
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
	if expected != types.NoTypeID && e != nil && e.Kind == hir.ExprTupleLit && l != nil && l.types != nil {
		if _, ok := l.types.TupleInfo(resolveAlias(l.types, expected)); ok {
			clone := *e
			clone.Type = expected
			e = &clone
		}
	}
	if expected != types.NoTypeID && e != nil && e.Kind == hir.ExprArrayLit && l != nil && l.types != nil {
		resolved := resolveAliasType(l.types, expected)
		if _, ok := l.types.ArrayInfo(resolved); ok {
			clone := *e
			clone.Type = expected
			e = &clone
		} else if _, _, ok := l.types.ArrayFixedInfo(resolved); ok {
			clone := *e
			clone.Type = expected
			e = &clone
		} else if tt, ok := l.types.Lookup(resolved); ok && tt.Kind == types.KindArray {
			clone := *e
			clone.Type = expected
			e = &clone
		}
	}
	consume := true
	if expected != types.NoTypeID && l.types != nil {
		if tt, ok := l.types.Lookup(resolveAlias(l.types, expected)); ok && tt.Kind == types.KindReference {
			return l.lowerExpr(e, consume)
		}
	}
	op, err := l.lowerValueExpr(e, consume)
	if err != nil {
		return op, err
	}
	if expected != types.NoTypeID {
		l.coerceNothingOperand(&op, expected)
		op = l.unionCastOperand(&op, expected, e.Span)
	}
	return op, nil
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

func resolveAliasType(in *types.Interner, id types.TypeID) types.TypeID {
	if in == nil || id == types.NoTypeID {
		return id
	}
	const maxDepth = 32
	for range maxDepth {
		tt, ok := in.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := in.AliasTarget(id)
			if !ok || target == types.NoTypeID || target == id {
				return id
			}
			id = target
		case types.KindOwn:
			if tt.Elem == types.NoTypeID {
				return id
			}
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func isUnionType(in *types.Interner, id types.TypeID) bool {
	if in == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAliasType(in, id)
	if id == types.NoTypeID {
		return false
	}
	tt, ok := in.Lookup(id)
	return ok && tt.Kind == types.KindUnion
}

func (l *funcLowerer) unionHasNothing(id types.TypeID) bool {
	if l == nil || l.types == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAliasType(l.types, id)
	if id == types.NoTypeID {
		return false
	}
	info, ok := l.types.UnionInfo(id)
	if !ok || info == nil {
		return false
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberNothing {
			return true
		}
	}
	return false
}

func (l *funcLowerer) needsUnionCast(src, target types.TypeID) bool {
	if l == nil || l.types == nil || src == types.NoTypeID || target == types.NoTypeID {
		return false
	}
	src = resolveAliasType(l.types, src)
	target = resolveAliasType(l.types, target)
	if src == types.NoTypeID || target == types.NoTypeID || src == target {
		return false
	}
	return isUnionType(l.types, src) && isUnionType(l.types, target)
}

func (l *funcLowerer) coerceNothingOperand(op *Operand, expected types.TypeID) {
	if expected == types.NoTypeID || l == nil {
		return
	}
	if !l.isNothingType(op.Type) {
		return
	}
	if !l.unionHasNothing(expected) {
		return
	}
	op.Type = expected
	if op.Kind == OperandConst && op.Const.Kind == ConstNothing {
		op.Const.Type = expected
	}
}

func (l *funcLowerer) unionCastOperand(op *Operand, target types.TypeID, span source.Span) Operand {
	if !l.needsUnionCast(op.Type, target) {
		return *op
	}
	tmp := l.newTemp(target, "cast", span)
	l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: tmp},
		Src: RValue{Kind: RValueCast, Cast: CastOp{Value: *op, TargetTy: target}},
	}})
	return l.placeOperand(Place{Local: tmp}, target, true)
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
