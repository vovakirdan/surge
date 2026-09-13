package mono

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// PrepareBorrowedParamBody resolves cleanup candidates after concrete callable
// rewriting. The caller supplies candidates using its existing borrowed ABI
// classifier; this function decides only which bodies need a working owner.
// It never mutates fn. A private clone is made only when readonly obligations
// must be removed, before MIR can use them to materialize a return value.
func PrepareBorrowedParamBody(fn *hir.Func, typesIn *types.Interner, candidates []symbols.SymbolID) (*hir.Func, []symbols.SymbolID, error) {
	if fn == nil || typesIn == nil {
		return nil, nil, fmt.Errorf("mono: borrowed parameter preparation requires a function and types")
	}
	if fn.IsAsync() || fn.IsIntrinsic() || fn.Body == nil {
		if len(candidates) != 0 {
			return nil, nil, fmt.Errorf("mono: borrowed parameter candidates require a synchronous body in %s", fn.Name)
		}
		return fn, nil, nil
	}
	paramTypes := make([]types.TypeID, len(fn.Params))
	params := make(map[symbols.SymbolID]types.TypeID, len(fn.Params))
	for i, p := range fn.Params {
		paramTypes[i] = p.Type
		if _, ok := typesIn.Lookup(p.Type); !ok {
			return nil, nil, fmt.Errorf("mono: parameter %s has no concrete descriptor in %s", p.Name, fn.Name)
		}
		if !p.SymbolID.IsValid() {
			if p.Name != "" && p.Name != "_" {
				return nil, nil, fmt.Errorf("mono: parameter %s has no symbol in %s", p.Name, fn.Name)
			}
			continue
		}
		if _, exists := params[p.SymbolID]; exists {
			return nil, nil, fmt.Errorf("mono: duplicate parameter symbol %d in %s", p.SymbolID, fn.Name)
		}
		params[p.SymbolID] = p.Type
	}
	if fn.IsGeneric() || !typeArgsAreConcrete(typesIn, paramTypes) {
		return nil, nil, fmt.Errorf("mono: borrowed parameter preparation requires concrete parameters in %s", fn.Name)
	}
	plan := borrowedParamPlan{
		types: typesIn, candidates: make(map[symbols.SymbolID]bool, len(candidates)),
		working: make(map[symbols.SymbolID]bool), cleanup: make(map[symbols.SymbolID]bool),
		residual: make(map[symbols.SymbolID]bool),
	}
	for _, sym := range candidates {
		ty, exists := params[sym]
		if !sym.IsValid() || !exists || !typesIn.IsRefCounted(ty) || plan.candidates[sym] {
			return nil, nil, fmt.Errorf("mono: invalid or duplicate borrowed parameter candidate %d in %s", sym, fn.Name)
		}
		plan.candidates[sym] = true
	}
	if len(candidates) == 0 {
		return fn, nil, nil
	}
	observe := borrowedBodyWalker{expr: plan.observeExpr, stmt: plan.observeStmt}
	if err := observe.block(fn.Body); err != nil {
		return nil, nil, fmt.Errorf("mono: borrowed parameter body %s: %w", fn.Name, err)
	}
	var working []symbols.SymbolID
	readonly := make(map[symbols.SymbolID]bool)
	needsCopy := false
	for _, p := range fn.Params {
		if !plan.candidates[p.SymbolID] {
			continue
		}
		if plan.working[p.SymbolID] {
			working = append(working, p.SymbolID)
			continue
		}
		if plan.residual[p.SymbolID] {
			return nil, nil, fmt.Errorf("mono: readonly counted parameter %d has residual cleanup in %s", p.SymbolID, fn.Name)
		}
		readonly[p.SymbolID] = true
		needsCopy = needsCopy || plan.cleanup[p.SymbolID]
	}
	if !needsCopy {
		return fn, working, nil
	}
	prepared := cloneFunc(fn)
	prune := borrowedBodyWalker{prune: true, stmt: func(st *hir.Stmt) (bool, error) {
		switch data := st.Data.(type) {
		case hir.DropData:
			return !data.Synthetic || !readonly[borrowedParamSlot(data.Value)], nil
		case hir.ReturnData:
			data.DropsAfterValue = withoutBorrowedParamDrops(data.DropsAfterValue, readonly)
			st.Data = data
		case hir.RetData:
			data.DropsAfterValue = withoutBorrowedParamDrops(data.DropsAfterValue, readonly)
			st.Data = data
		}
		return true, nil
	}}
	if err := prune.block(prepared.Body); err != nil {
		return nil, nil, fmt.Errorf("mono: borrowed parameter cleanup %s: %w", fn.Name, err)
	}
	return prepared, working, nil
}

type borrowedParamPlan struct {
	types      *types.Interner
	candidates map[symbols.SymbolID]bool
	working    map[symbols.SymbolID]bool
	cleanup    map[symbols.SymbolID]bool
	residual   map[symbols.SymbolID]bool
}

func (p *borrowedParamPlan) mark(expr *hir.Expr) {
	if sym := borrowedParamSlot(expr); p.candidates[sym] {
		p.working[sym] = true
	}
}

func (p *borrowedParamPlan) observeExpr(expr *hir.Expr) error {
	switch data := expr.Data.(type) {
	case hir.BinaryOpData:
		if borrowedParamAssignment(data.Op) {
			p.mark(data.Left)
		}
	case hir.UnaryOpData:
		if data.Op == ast.ExprUnaryRefMut {
			tt, ok := p.types.Lookup(resolveAlias(p.types, expr.Type))
			if !ok || tt.Kind != types.KindReference || !tt.Mutable || tt.Elem == types.NoTypeID ||
				types.ContainsGenericParam(p.types, expr.Type) || data.Operand == nil {
				return fmt.Errorf("mutable address has no concrete mutable reference descriptor")
			}
			p.mark(data.Operand)
		}
	}
	return nil
}

func (p *borrowedParamPlan) observeStmt(st *hir.Stmt) (bool, error) {
	switch data := st.Data.(type) {
	case hir.AssignData:
		p.mark(data.Target)
	case hir.DropData:
		if !data.Synthetic {
			p.mark(data.Value)
		} else if sym := borrowedParamSlot(data.Value); p.candidates[sym] {
			p.cleanup[sym] = true
			p.residual[sym] = p.residual[sym] || len(data.Steps) != 0
		}
	case hir.ReturnData:
		p.noteDrops(data.DropsAfterValue)
	case hir.RetData:
		p.noteDrops(data.DropsAfterValue)
	}
	return true, nil
}

func (p *borrowedParamPlan) noteDrops(drops []hir.DropLocal) {
	for _, drop := range drops {
		if p.candidates[drop.SymbolID] {
			p.cleanup[drop.SymbolID] = true
			p.residual[drop.SymbolID] = p.residual[drop.SymbolID] || len(drop.Steps) != 0
		}
	}
}

// Match the whole slot, not every symbol in an address expression. An owned
// temporary denotes another local; dereference/projection denotes another place.
// RaiseReleaseGuard alone preserves the inner place (mir.lowerPlace).
func borrowedParamSlot(expr *hir.Expr) symbols.SymbolID {
	for expr != nil {
		switch data := expr.Data.(type) {
		case hir.VarRefData:
			if expr.Kind == hir.ExprVarRef {
				return data.SymbolID
			}
		case hir.RaiseReleaseGuardData:
			if expr.Kind == hir.ExprRaiseReleaseGuard {
				expr = data.Inner
				continue
			}
		}
		break
	}
	return symbols.NoSymbolID
}

func borrowedParamAssignment(op ast.ExprBinaryOp) bool {
	switch op {
	case ast.ExprBinaryAssign, ast.ExprBinaryAddAssign, ast.ExprBinarySubAssign,
		ast.ExprBinaryMulAssign, ast.ExprBinaryDivAssign, ast.ExprBinaryModAssign,
		ast.ExprBinaryBitAndAssign, ast.ExprBinaryBitOrAssign, ast.ExprBinaryBitXorAssign,
		ast.ExprBinaryShlAssign, ast.ExprBinaryShrAssign:
		return true
	default:
		return false
	}
}

func withoutBorrowedParamDrops(drops []hir.DropLocal, readonly map[symbols.SymbolID]bool) []hir.DropLocal {
	out := drops[:0]
	for _, drop := range drops {
		if !readonly[drop.SymbolID] {
			out = append(out, drop)
		}
	}
	clear(drops[len(out):])
	return out
}
