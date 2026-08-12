package mir

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/numlit"
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
			if val, ok := parseLiteralUint64(lit); ok {
				out.Const.UintValue = val
			} else {
				out.Const.Kind = ConstInt
				out.Const.IntValue = lit.IntValue
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

func parseLiteralUint64(lit hir.LiteralData) (uint64, bool) {
	if lit.Text != "" {
		return numlit.ParseUint64(lit.Text)
	}
	val, err := safecast.Conv[uint64](lit.IntValue)
	if err != nil {
		return 0, false
	}
	return val, true
}

// constNothing creates a nothing constant operand.
func (l *funcLowerer) constNothing(ty types.TypeID) Operand {
	return Operand{Kind: OperandConst, Type: ty, Const: Const{Kind: ConstNothing, Type: ty}}
}

// placeOperand creates an operand for a place. `consume` marks a read whose
// value outlives the expression — it is being stored, returned or handed on —
// as opposed to a borrowing read that only feeds the current operation.
func (l *funcLowerer) placeOperand(place Place, ty types.TypeID, consume bool) Operand {
	kind := OperandCopy
	switch {
	case consume && l.isRefCountedScalar(ty):
		// Copy at the surface, counted underneath: the source stays usable, so
		// this cannot be a move, and the destination needs its own reference,
		// so it cannot be a bare copy.
		kind = OperandRetain
	case consume && !l.isCopyType(ty):
		kind = OperandMove
	case consume && l.isValueComposite(ty) && l.ownsItsValue(place):
		// A composite the lowering itself materialized and is spending here —
		// a literal, a call result. It has exactly one holder, so consuming it
		// TRANSFERS. Duplicating instead would allocate a second box and
		// abandon the first, which is a leak wherever there is no frame
		// teardown to sweep it up.
		kind = OperandMove
	case consume && l.isValueComposite(ty):
		// A Copy value composite consumed here: the destination must get an
		// INDEPENDENT value, not a second name for the same storage. Reached
		// only after the move case above, so this is exactly the Copy
		// composites — a move-only one transfers instead.
		//
		// The borrowing read (consume == false) keeps OperandCopy and must:
		// it looks through to storage someone else owns and has to keep seeing
		// later writes to it. That difference is the whole reason this kind
		// exists — both spellings were OperandCopy, so no backend could tell a
		// duplication from a look.
		kind = OperandCopyValue
	}
	return Operand{Kind: kind, Type: ty, Place: place}
}

// markOwningTemp records that a temp holds a value the lowering materialized,
// so consuming it transfers rather than duplicates. Callers are the sites that
// CREATE a value; everything else stays an alias by default.
func (l *funcLowerer) markOwningTemp(local LocalID) LocalID {
	if l == nil || local == NoLocalID {
		return local
	}
	if l.owningTemps == nil {
		l.owningTemps = make(map[LocalID]struct{})
	}
	l.owningTemps[local] = struct{}{}
	return local
}

// ownsItsValue reports whether consuming this place TRANSFERS a value rather
// than duplicating one.
func (l *funcLowerer) ownsItsValue(place Place) bool {
	if l == nil || place.Kind != PlaceLocal || len(place.Proj) != 0 || place.Local == NoLocalID {
		return false
	}
	if l.f == nil || int(place.Local) < 0 || int(place.Local) >= len(l.f.Locals) {
		return false
	}
	// A named binding stays live and readable after this use, whatever else is
	// true of it.
	if l.f.Locals[place.Local].Sym != symbols.NoSymbolID {
		return false
	}
	_, owns := l.owningTemps[place.Local]
	return owns
}

// isValueComposite reports whether the type is stored inline — a struct,
// tuple, tagged union or fixed array — as opposed to a handle-backed type that
// names runtime-owned storage.
func (l *funcLowerer) isValueComposite(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	return l.types.IsValueComposite(resolveAlias(l.types, ty))
}

// materializeOwnedConst binds a reference-counted scalar constant to a temp so
// that the block it allocates has an owner.
//
// A `float` literal is not a compile-time word the way an in-range `int` is:
// `float` has no inline form, so evaluating one ALLOCATES. Left as a bare
// constant operand it would be a block with no place holding it and therefore
// no release — a leak per evaluation, which in a loop grows without bound.
// Routing it through a temp gives it exactly the ownership every other float
// value has.
func (l *funcLowerer) materializeOwnedConst(op *Operand, span source.Span, consume bool) Operand {
	if op.Kind != OperandConst || !l.isRefCountedScalar(op.Type) {
		return *op
	}
	tmp := l.newTemp(op.Type, "const", span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: *op},
		},
	})
	return l.placeOperand(Place{Local: tmp}, op.Type, consume)
}

// retainExtractedValue gives a temp holding a value read OUT of a container its
// own reference.
//
// A field or element read copies the bare word; the container keeps its
// reference and knows nothing about the copy. Without this the temp would be
// released at the end of its region while the container still points at the
// block — a double release — and reading through the temp after the container
// was dropped would be a use-after-free. Retaining makes the temp a real owner,
// which its own release then balances.
func (l *funcLowerer) retainExtractedValue(local LocalID, ty types.TypeID) {
	if local == NoLocalID {
		return
	}
	if !l.isRefCountedScalar(ty) {
		return
	}
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: local},
			Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandRetain, Type: ty, Place: Place{Local: local}}},
		},
	})
}

// isRefCountedScalar reports whether the type is one of the arbitrary-precision
// scalars whose heap block carries a reference count.
func (l *funcLowerer) isRefCountedScalar(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	return l.types.IsRefCountedScalar(resolveAlias(l.types, ty))
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
	if expected != types.NoTypeID && l.isRuntimePlacementType(expected) && e.Kind == hir.ExprVarRef {
		if data, ok := e.Data.(hir.VarRefData); ok && (data.Name == "pool" || data.Name == "distributed") {
			clone := *e
			clone.Type = expected
			e = &clone
		}
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
	if discarded := l.discardResultExpr(e); discarded != nil {
		_, err := l.lowerExpr(discarded, false)
		return err
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

func (l *funcLowerer) lowerConstValue(symID symbols.SymbolID, consume bool, fallbackType types.TypeID) (Operand, bool, error) {
	if l == nil || !symID.IsValid() || l.consts == nil {
		return Operand{}, false, nil
	}
	decl := l.consts[symID]
	if decl == nil {
		return Operand{}, false, nil
	}
	if decl.Value == nil {
		placementType := decl.Type
		if placementType == types.NoTypeID {
			placementType = fallbackType
		}
		if op, ok := l.lowerPlacementIntrinsicConstSymbol(symID, decl.Name, placementType); ok {
			return op, true, nil
		}
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

const (
	mirPlacementKindBits        = 8
	mirPlacementKindPool        = uint64(1)
	mirPlacementKindDistributed = uint64(2)
)

func (l *funcLowerer) lowerPlacementIntrinsicConstSymbol(symID symbols.SymbolID, name string, placementType types.TypeID) (Operand, bool) {
	if !l.isRuntimePlacementConstSymbol(symID, name) {
		return Operand{}, false
	}
	if placementType == types.NoTypeID && l != nil && l.symbols != nil && l.symbols.Table != nil && l.symbols.Table.Symbols != nil {
		if sym := l.symbols.Table.Symbols.Get(symID); sym != nil {
			placementType = sym.Type
		}
	}
	if !l.isRuntimePlacementType(placementType) {
		return Operand{}, false
	}
	var encoded uint64
	switch name {
	case "pool":
		encoded = mirPlacementKindPool
	case "distributed":
		encoded = mirPlacementKindDistributed
	default:
		return Operand{}, false
	}
	return Operand{
		Kind: OperandConst,
		Type: placementType,
		Const: Const{
			Kind:      ConstUint,
			Type:      placementType,
			UintValue: encoded,
		},
	}, true
}

func (l *funcLowerer) lowerPlacementIntrinsicConstName(name string, placementType types.TypeID) (Operand, bool) {
	if placementType == types.NoTypeID && l != nil && l.f != nil {
		placementType = l.f.Result
	}
	if !l.isRuntimePlacementType(placementType) {
		return Operand{}, false
	}
	var encoded uint64
	switch name {
	case "pool":
		encoded = mirPlacementKindPool
	case "distributed":
		encoded = mirPlacementKindDistributed
	default:
		return Operand{}, false
	}
	return Operand{
		Kind: OperandConst,
		Type: placementType,
		Const: Const{
			Kind:      ConstUint,
			Type:      placementType,
			UintValue: encoded,
		},
	}, true
}

func (l *funcLowerer) isRuntimePlacementType(id types.TypeID) bool {
	if l == nil || l.types == nil || id == types.NoTypeID {
		return false
	}
	if l.types.IsRuntimePlacementType(id) {
		return true
	}
	if mirRuntimePlacementTypeFromSymbols(l.symbols, l.types, id) {
		l.types.MarkRuntimePlacementType(resolveAliasType(l.types, id))
		return true
	}
	return false
}

func (l *funcLowerer) isRuntimePlacementConstSymbol(symID symbols.SymbolID, wantName string) bool {
	if l == nil || l.symbols == nil || l.symbols.Table == nil ||
		l.symbols.Table.Symbols == nil || l.symbols.Table.Strings == nil || !symID.IsValid() {
		return false
	}
	sym := l.symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolConst || sym.Name == source.NoStringID {
		return false
	}
	if !mirIsCoreRuntimeConstSymbol(sym) {
		return false
	}
	name := l.symbols.Table.Strings.MustLookup(sym.Name)
	return name == wantName && (name == "pool" || name == "distributed")
}

func mirIsCoreRuntimeConstSymbol(sym *symbols.Symbol) bool {
	if sym == nil {
		return false
	}
	if sym.ModulePath == "" {
		return false
	}
	trimmed := strings.Trim(sym.ModulePath, "/")
	if trimmed != "core" && !strings.HasPrefix(trimmed, "core/") {
		return false
	}
	return sym.Flags&(symbols.SymbolFlagBuiltin|symbols.SymbolFlagImported|symbols.SymbolFlagPublic) != 0
}

func mirIsCoreRuntimeSymbol(sym *symbols.Symbol) bool {
	if sym == nil || sym.Flags&symbols.SymbolFlagBuiltin == 0 {
		return false
	}
	if sym.ModulePath != "" {
		trimmed := strings.Trim(sym.ModulePath, "/")
		return trimmed == "core" || strings.HasPrefix(trimmed, "core/")
	}
	return sym.Flags&symbols.SymbolFlagImported != 0
}

func mirRuntimePlacementTypeFromSymbols(symRes *symbols.Result, typesIn *types.Interner, id types.TypeID) bool {
	if symRes == nil || symRes.Table == nil || symRes.Table.Symbols == nil || symRes.Table.Strings == nil ||
		typesIn == nil || id == types.NoTypeID {
		return false
	}
	resolved := resolveAliasType(typesIn, id)
	for i := 1; i <= symRes.Table.Symbols.Len(); i++ {
		sym := symRes.Table.Symbols.Get(symbols.SymbolID(i))
		if sym == nil || sym.Kind != symbols.SymbolType || sym.Type == types.NoTypeID || sym.Name == source.NoStringID {
			continue
		}
		if sym.Type != id && sym.Type != resolved {
			continue
		}
		if symRes.Table.Strings.MustLookup(sym.Name) != "Placement" {
			continue
		}
		if mirIsCoreRuntimeSymbol(sym) {
			return true
		}
	}
	return false
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
	tmp := l.markOwningTemp(l.newTemp(target, "cast", span))
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
