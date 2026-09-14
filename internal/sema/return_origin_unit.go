package sema

import (
	"context"
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ReturnOriginUnit keeps local AST and symbol IDs with their owning checker.
// The analysis does not mutate that checker, its interner, or its drop plans.
type ReturnOriginUnit struct {
	Builder   *ast.Builder
	FileID    ast.FileID
	Sema      *Result
	Symbols   *symbols.Result
	SourceKey string
	// Publication preserves the existing canonical/local callable vocabulary
	// before a merged authority replaces per-file callable candidates.
	Publication FinalizationPublication
}

// ReturnOriginPending is an obligation not answered by the private C2 pass.
// It must be resolved before this analysis can authorize public publication.
type ReturnOriginPending struct {
	SourceKey string
	Span      source.Span
	Reason    string
}

// ReturnOriginSummary describes a completed body's possible normal results.
// NoNormalReturn is distinct from a normal result with no borrowed content.
type ReturnOriginSummary struct {
	BodyKey        string
	Name           string
	Source         source.Span
	NoNormalReturn bool
	ParamSlots     []uint32
	Unknown        bool
}

// ReturnOriginAnalysis is detached evidence, not a published acceptance bit.
type ReturnOriginAnalysis struct {
	Diagnostics []diag.Diagnostic
	Pending     []ReturnOriginPending
	Summaries   []ReturnOriginSummary
	finished    bool
}

// Complete reports whether every encountered obligation was answered.
// A complete analysis can still contain source errors.
func (r *ReturnOriginAnalysis) Complete() bool {
	return r != nil && r.finished && len(r.Pending) == 0
}

type returnOriginUnitIndex struct {
	ReturnOriginUnit
	stmtSymbols map[ast.StmtID]symbols.SymbolID
	stmtScopes  map[ast.StmtID]symbols.ScopeID
	exprScopes  map[ast.ExprID]symbols.ScopeID
	itemScopes  map[ast.ItemID]symbols.ScopeID
	functions   map[symbols.SymbolID]*returnOriginFunction
	pending     []ReturnOriginPending
}

type returnOriginFunction struct {
	unit   *returnOriginUnitIndex
	item   *ast.FnItem
	symbol symbols.SymbolID
	scope  symbols.ScopeID
	key    string
	name   string
	params []symbols.SymbolID
	info   *types.FnInfo
}

type returnOriginAnalyzer struct {
	ctx       context.Context
	functions []*returnOriginFunction
	summaries map[string]returnOriginValue
	report    *ReturnOriginAnalysis
	collect   bool
}

type returnOriginBody struct {
	analyzer *returnOriginAnalyzer
	function *returnOriginFunction
}

type returnOriginTargets struct {
	scope symbols.ScopeID
	block symbols.ScopeID
	loop  symbols.ScopeID
}

type returnOriginExprResult struct {
	flow    returnOriginFlow
	value   returnOriginValue
	storage returnOriginValue
}

// AnalyzeReturnOrigins examines already typed source without installing a
// compiler hook. Unknown shapes remain explicit pending obligations; callers
// must not treat the absence of a local-escape diagnostic as acceptance.
func AnalyzeReturnOrigins(ctx context.Context, authority *Result, units []ReturnOriginUnit) (*ReturnOriginAnalysis, error) {
	if ctx == nil || authority == nil || authority.TypeInterner == nil || len(units) == 0 {
		return nil, fmt.Errorf("return origins: missing context, typed authority, or source units")
	}
	a := &returnOriginAnalyzer{ctx: ctx,
		summaries: make(map[string]returnOriginValue), report: &ReturnOriginAnalysis{}}
	seen := make(map[string]struct{}, len(units))
	for _, unit := range units {
		if _, exists := seen[unit.SourceKey]; exists {
			return nil, fmt.Errorf("return origins: duplicate source unit %q", unit.SourceKey)
		}
		seen[unit.SourceKey] = struct{}{}
		index, err := indexReturnOriginUnit(unit, authority.TypeInterner)
		if err != nil {
			return nil, err
		}
		for _, function := range index.functions {
			a.functions = append(a.functions, function)
		}
		a.report.Pending = append(a.report.Pending, index.pending...)
	}
	if len(a.functions) == 0 {
		return nil, fmt.Errorf("return origins: supplied units contain no supported typed bodies")
	}
	slices.SortFunc(a.functions, func(a, b *returnOriginFunction) int {
		if a.key < b.key {
			return -1
		}
		if a.key > b.key {
			return 1
		}
		return 0
	})
	if err := a.solveBodies(); err != nil {
		return nil, err
	}
	a.report.finished = true
	return a.report, nil
}

func indexReturnOriginUnit(unit ReturnOriginUnit, interner *types.Interner) (*returnOriginUnitIndex, error) {
	if unit.Builder == nil || !unit.FileID.IsValid() || unit.SourceKey == "" ||
		unit.Sema == nil || unit.Sema.TypeInterner != interner || unit.Symbols == nil ||
		unit.Symbols.Table == nil || unit.Symbols.Table.Scopes == nil || unit.Symbols.Table.Symbols == nil {
		return nil, fmt.Errorf("return origins: incomplete owning unit %q", unit.SourceKey)
	}
	file := unit.Builder.Files.Get(unit.FileID)
	if file == nil {
		return nil, fmt.Errorf("return origins: source unit %q has no AST file", unit.SourceKey)
	}
	u := &returnOriginUnitIndex{ReturnOriginUnit: unit,
		stmtSymbols: make(map[ast.StmtID]symbols.SymbolID), stmtScopes: make(map[ast.StmtID]symbols.ScopeID),
		exprScopes: make(map[ast.ExprID]symbols.ScopeID), itemScopes: make(map[ast.ItemID]symbols.ScopeID),
		functions: make(map[symbols.SymbolID]*returnOriginFunction)}
	for i, scope := range unit.Symbols.Table.Scopes.Data() {
		if scope.Owner.ASTFile != unit.FileID || scope.Owner.SourceFile != file.Span.File {
			continue
		}
		n, err := safecast.Conv[uint32](i + 1)
		if err != nil {
			return nil, fmt.Errorf("return origins: scope index: %w", err)
		}
		id := symbols.ScopeID(n)
		switch scope.Owner.Kind {
		case symbols.ScopeOwnerItem:
			u.itemScopes[scope.Owner.Item] = id
		case symbols.ScopeOwnerStmt:
			u.stmtScopes[scope.Owner.Stmt] = id
		case symbols.ScopeOwnerExpr:
			u.exprScopes[scope.Owner.Expr] = id
		}
	}
	for i, sym := range unit.Symbols.Table.Symbols.Data() {
		if sym.Decl.ASTFile != unit.FileID || !sym.Decl.Stmt.IsValid() {
			continue
		}
		n, err := safecast.Conv[uint32](i + 1)
		if err != nil {
			return nil, fmt.Errorf("return origins: symbol index: %w", err)
		}
		u.stmtSymbols[sym.Decl.Stmt] = symbols.SymbolID(n)
	}
	for _, itemID := range file.Items {
		fn, ok := unit.Builder.Items.Fn(itemID)
		if !ok || fn == nil || !fn.Body.IsValid() {
			continue
		}
		ids := unit.Symbols.ItemSymbols[itemID]
		if len(ids) != 1 {
			return nil, fmt.Errorf("return origins: function at %v has no unique symbol", fn.NameSpan)
		}
		if err := u.addFunction(itemID, fn, ids[0]); err != nil {
			return nil, err
		}
	}
	for _, scope := range unit.Symbols.Table.Scopes.Data() {
		if scope.Kind == symbols.ScopeFunction && scope.Owner.ASTFile == unit.FileID && scope.Owner.Extern.IsValid() {
			u.pending = append(u.pending, ReturnOriginPending{SourceKey: unit.SourceKey, Span: scope.Span,
				Reason: "extern method body requires its own declaration/body adapter"})
		}
	}
	return u, nil
}

func (u *returnOriginUnitIndex) addFunction(itemID ast.ItemID, fn *ast.FnItem, id symbols.SymbolID) error {
	sym := u.Symbols.Table.Symbols.Get(id)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return fmt.Errorf("return origins: function at %v has no typed signature", fn.NameSpan)
	}
	info, ok := u.Sema.TypeInterner.FnInfo(sym.Type)
	scope := u.itemScopes[itemID]
	if !ok || info == nil || !scope.IsValid() {
		return fmt.Errorf("return origins: function at %v has no typed parameters or scope", fn.NameSpan)
	}
	var key string
	for _, candidate := range u.Sema.CallableCandidates {
		if candidate.Symbol == id {
			candidate.SourceKey = u.SourceKey
			key = canonicalCallableBodyKey(&candidate)
			break
		}
	}
	if key == "" {
		return fmt.Errorf("return origins: function at %v has no declaration identity", fn.NameSpan)
	}
	name, _ := u.Builder.StringsInterner.Lookup(fn.Name)
	f := &returnOriginFunction{unit: u, item: fn, symbol: id, scope: scope, key: key, name: name, info: info}
	for _, paramID := range u.Builder.Items.GetFnParamIDs(fn) {
		param := u.Builder.Items.FnParam(paramID)
		if param == nil {
			return fmt.Errorf("return origins: missing parameter at %v", fn.NameSpan)
		}
		var found symbols.SymbolID
		for _, candidate := range u.Symbols.Table.Scopes.Get(scope).NameIndex[param.Name] {
			if entry := u.Symbols.Table.Symbols.Get(candidate); entry != nil && entry.Kind == symbols.SymbolParam {
				found = candidate
			}
		}
		f.params = append(f.params, found)
	}
	if len(f.params) != len(info.Params) {
		return fmt.Errorf("return origins: parameter arity mismatch at %v", fn.NameSpan)
	}
	u.functions[id] = f
	return nil
}

func (b *returnOriginBody) within(child, ancestor symbols.ScopeID) bool {
	for child.IsValid() {
		if child == ancestor {
			return true
		}
		scope := b.function.unit.Symbols.Table.Scopes.Get(child)
		if scope == nil {
			return false
		}
		child = scope.Parent
	}
	return false
}
