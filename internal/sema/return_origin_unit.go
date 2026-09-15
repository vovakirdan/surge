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
	authority    *Result
	stmtSymbols  map[ast.StmtID]symbols.SymbolID
	stmtScopes   map[ast.StmtID]symbols.ScopeID
	exprScopes   map[ast.ExprID]symbols.ScopeID
	itemScopes   map[ast.ItemID]symbols.ScopeID
	externScopes map[ast.ExternMemberID]symbols.ScopeID
	functions    map[symbols.SymbolID]*returnOriginFunction
	pending      []ReturnOriginPending
	declarations int
}

type returnOriginFunction struct {
	unit               *returnOriginUnitIndex
	item               *ast.FnItem
	symbol             symbols.SymbolID
	scope              symbols.ScopeID
	key                string
	canonicalSourceKey string
	name               string
	params             []symbols.SymbolID
	info               *types.FnInfo
	candidate          *CallableCandidate
	// Formal slots admitted as external cells, and their writable subset.
	cellSlots        []uint32
	mutableCellSlots []uint32
}

type returnOriginAnalyzer struct {
	ctx          context.Context
	functions    []*returnOriginFunction
	units        []*returnOriginUnitIndex
	bodies       map[string]*returnOriginFunction
	declarations map[string]*returnOriginFunction
	summaries    map[string]returnOriginSummaryFact
	report       *ReturnOriginAnalysis
	collect      bool
}

type returnOriginBody struct {
	analyzer   *returnOriginAnalyzer
	function   *returnOriginFunction
	conditions []returnOriginCondition
	required   returnOriginRequirements
	postCells  map[uint32]returnOriginValue
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
		bodies:       make(map[string]*returnOriginFunction),
		declarations: make(map[string]*returnOriginFunction),
		summaries:    make(map[string]returnOriginSummaryFact), report: &ReturnOriginAnalysis{}}
	seen := make(map[string]struct{}, len(units))
	declarations := 0
	for _, unit := range units {
		if _, exists := seen[unit.SourceKey]; exists {
			return nil, fmt.Errorf("return origins: duplicate source unit %q", unit.SourceKey)
		}
		seen[unit.SourceKey] = struct{}{}
		index, err := indexReturnOriginUnit(unit, authority)
		if err != nil {
			return nil, err
		}
		a.units = append(a.units, index)
		for _, function := range index.functions {
			if a.bodies[function.key] != nil || a.declarations[function.key] != nil {
				return nil, fmt.Errorf("return origins: duplicate owning body %q", function.key)
			}
			if function.item.Body.IsValid() {
				a.bodies[function.key] = function
				a.functions = append(a.functions, function)
			} else {
				a.declarations[function.key] = function
			}
		}
		declarations += index.declarations
		a.report.Pending = append(a.report.Pending, index.pending...)
	}
	if declarations == 0 {
		return nil, fmt.Errorf("return origins: supplied units contain no typed declarations")
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

func indexReturnOriginUnit(unit ReturnOriginUnit, authority *Result) (*returnOriginUnitIndex, error) {
	if unit.Builder == nil || !unit.FileID.IsValid() || unit.SourceKey == "" ||
		unit.Sema == nil || unit.Sema.TypeInterner != authority.TypeInterner || unit.Symbols == nil ||
		unit.Symbols.Table == nil || unit.Symbols.Table.Scopes == nil || unit.Symbols.Table.Symbols == nil {
		return nil, fmt.Errorf("return origins: incomplete owning unit %q", unit.SourceKey)
	}
	file := unit.Builder.Files.Get(unit.FileID)
	if file == nil {
		return nil, fmt.Errorf("return origins: source unit %q has no AST file", unit.SourceKey)
	}
	if unit.Publication.SourceKey != unit.SourceKey {
		return nil, fmt.Errorf("return origins: missing owning publication for %q", unit.SourceKey)
	}
	u := &returnOriginUnitIndex{ReturnOriginUnit: unit, authority: authority,
		stmtSymbols: make(map[ast.StmtID]symbols.SymbolID), stmtScopes: make(map[ast.StmtID]symbols.ScopeID),
		exprScopes: make(map[ast.ExprID]symbols.ScopeID), itemScopes: make(map[ast.ItemID]symbols.ScopeID),
		externScopes: make(map[ast.ExternMemberID]symbols.ScopeID),
		functions:    make(map[symbols.SymbolID]*returnOriginFunction)}
	for i, scope := range unit.Symbols.Table.Scopes.Data() {
		if scope.Owner.ASTFile != unit.FileID || scope.Owner.SourceFile != file.Span.File {
			continue
		}
		n, err := safecast.Conv[uint32](i + 1)
		if err != nil {
			return nil, fmt.Errorf("return origins: scope index: %w", err)
		}
		id := symbols.ScopeID(n)
		if scope.Owner.Extern.IsValid() {
			u.externScopes[scope.Owner.Extern] = id
			continue
		}
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
		if !ok || fn == nil {
			if block, found := unit.Builder.Items.Extern(itemID); found && block != nil {
				if err := u.addExternFunctions(block); err != nil {
					return nil, err
				}
			}
			continue
		}
		ids := unit.Symbols.ItemSymbols[itemID]
		if len(ids) != 1 {
			return nil, fmt.Errorf("return origins: function at %v has no unique symbol", fn.NameSpan)
		}
		if err := u.addFunction(fn, ids[0], u.itemScopes[itemID]); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func (u *returnOriginUnitIndex) addFunction(fn *ast.FnItem, id symbols.SymbolID, scope symbols.ScopeID) error {
	sym := u.Symbols.Table.Symbols.Get(id)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return fmt.Errorf("return origins: function at %v has no typed signature", fn.NameSpan)
	}
	info, ok := u.Sema.TypeInterner.FnInfo(sym.Type)
	if !ok || info == nil {
		return fmt.Errorf("return origins: function at %v has no typed parameters", fn.NameSpan)
	}
	u.declarations++
	if sym.Signature.HasBody != fn.Body.IsValid() {
		return fmt.Errorf("return origins: declaration/body mismatch at %v", fn.NameSpan)
	}
	if fn.Body.IsValid() && !scope.IsValid() {
		return fmt.Errorf("return origins: function body at %v has no owning scope", fn.NameSpan)
	}
	identity, err := u.owningCallableIdentity(fn, id, info)
	if err != nil {
		return err
	}
	name, _ := u.Builder.StringsInterner.Lookup(fn.Name)
	f := &returnOriginFunction{unit: u, item: fn, symbol: id, scope: scope, key: identity.BodyKey,
		canonicalSourceKey: identity.SourceKey, name: name, info: info}
	for i := range u.authority.CallableCandidates {
		candidate := &u.authority.CallableCandidates[i]
		if candidate.BodyKey == f.key && candidate.SourceKey == f.canonicalSourceKey {
			f.candidate = candidate
			break
		}
	}
	if !fn.Body.IsValid() {
		u.functions[id] = f
		return nil
	}
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
	f.cellSlots, f.mutableCellSlots = u.externalCellRoster(f)
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
