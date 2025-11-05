package diagfmt

import (
	"fmt"
	"sort"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

// SemanticsInput carries the data required to build a semantic dump.
type SemanticsInput struct {
	Builder *ast.Builder
	FileID  ast.FileID
	Result  *symbols.Result
}

// SemanticsOutput represents semantic data emitted alongside diagnostics.
type SemanticsOutput struct {
	Scopes       []ScopeJSON       `json:"scopes"`
	Symbols      []SymbolJSON      `json:"symbols"`
	ExprBindings []ExprBindingJSON `json:"expr_bindings"`
}

type ScopeJSON struct {
	ID     uint32         `json:"id"`
	Kind   string         `json:"kind"`
	Parent uint32         `json:"parent,omitempty"`
	Span   source.Span    `json:"span"`
	Owner  ScopeOwnerJSON `json:"owner"`
}

type ScopeOwnerJSON struct {
	Kind string `json:"kind"`
	Item uint32 `json:"item,omitempty"`
	Stmt uint32 `json:"stmt,omitempty"`
	Expr uint32 `json:"expr,omitempty"`
}

type SymbolJSON struct {
	ID      uint32      `json:"id"`
	Name    string      `json:"name"`
	Kind    string      `json:"kind"`
	Scope   uint32      `json:"scope"`
	Span    source.Span `json:"span"`
	Flags   []string    `json:"flags,omitempty"`
	Aliases []string    `json:"aliases,omitempty"`
}

type ExprBindingJSON struct {
	ExprID   uint32      `json:"expr_id"`
	SymbolID uint32      `json:"symbol_id"`
	Span     source.Span `json:"span"`
	Name     string      `json:"name"`
}

func buildSemanticsOutput(in *SemanticsInput) (*SemanticsOutput, error) {
	if in == nil || in.Result == nil || in.Result.Table == nil {
		return nil, nil
	}

	table := in.Result.Table
	if table.Scopes == nil || table.Symbols == nil {
		return nil, nil
	}

	output := &SemanticsOutput{
		Scopes:       make([]ScopeJSON, 0, table.Scopes.Len()),
		Symbols:      make([]SymbolJSON, 0, table.Symbols.Len()),
		ExprBindings: make([]ExprBindingJSON, 0, len(in.Result.ExprSymbols)),
	}

	strings := table.Strings
	if strings == nil && in.Builder != nil {
		strings = in.Builder.StringsInterner
	}
	if strings == nil {
		return nil, fmt.Errorf("semantics: missing string interner")
	}

	// Scopes are stored with sentinel at index 0.
	scopes := table.Scopes.Data()
	for idx, scope := range scopes {
		scopeValue, err := safecast.Conv[uint32](idx + 1)
		if err != nil {
			return nil, fmt.Errorf("semantics: scope id overflow: %w", err)
		}
		id := scopeValue
		parent := uint32(scope.Parent)
		owner := ScopeOwnerJSON{
			Kind: scopeOwnerKindString(scope.Owner.Kind),
		}
		if scope.Owner.Item.IsValid() {
			owner.Item = uint32(scope.Owner.Item)
		}
		if scope.Owner.Stmt.IsValid() {
			owner.Stmt = uint32(scope.Owner.Stmt)
		}
		if scope.Owner.Expr.IsValid() {
			owner.Expr = uint32(scope.Owner.Expr)
		}
		output.Scopes = append(output.Scopes, ScopeJSON{
			ID:     id,
			Kind:   scope.Kind.String(),
			Parent: parent,
			Span:   scope.Span,
			Owner:  owner,
		})
	}

	// Symbols stored with sentinel at index 0.
	syms := table.Symbols.Data()
	for idx, sym := range syms {
		symValue, err := safecast.Conv[uint32](idx + 1)
		if err != nil {
			return nil, fmt.Errorf("semantics: symbol id overflow: %w", err)
		}
		id := symValue
		name := strings.MustLookup(sym.Name)
		flagStrings := sym.Flags.Strings()
		aliases := make([]string, 0, len(sym.Aliases))
		for _, aliasID := range sym.Aliases {
			if aliasID == source.NoStringID {
				continue
			}
			aliases = append(aliases, strings.MustLookup(aliasID))
		}

		output.Symbols = append(output.Symbols, SymbolJSON{
			ID:      id,
			Name:    name,
			Kind:    sym.Kind.String(),
			Scope:   uint32(sym.Scope),
			Span:    sym.Span,
			Flags:   flagStrings,
			Aliases: aliases,
		})
	}

	// Expression bindings
	if len(in.Result.ExprSymbols) > 0 && in.Builder != nil {
		exprIDs := make([]int, 0, len(in.Result.ExprSymbols))
		for exprID := range in.Result.ExprSymbols {
			exprIDs = append(exprIDs, int(exprID))
		}
		sort.Ints(exprIDs)

		for _, exprInt := range exprIDs {
			exprValue, err := safecast.Conv[uint32](exprInt)
			if err != nil {
				return nil, fmt.Errorf("semantics: expr id overflow: %w", err)
			}
			exprID := ast.ExprID(exprValue)
			symID := in.Result.ExprSymbols[exprID]
			expr := in.Builder.Exprs.Get(exprID)
			if expr == nil {
				continue
			}
			name := ""
			if sym := table.Symbols.Get(symID); sym != nil {
				name = strings.MustLookup(sym.Name)
			}
			output.ExprBindings = append(output.ExprBindings, ExprBindingJSON{
				ExprID:   exprValue,
				SymbolID: uint32(symID),
				Span:     expr.Span,
				Name:     name,
			})
		}
	}

	return output, nil
}

func scopeOwnerKindString(kind symbols.ScopeOwnerKind) string {
	switch kind {
	case symbols.ScopeOwnerFile:
		return "file"
	case symbols.ScopeOwnerItem:
		return "item"
	case symbols.ScopeOwnerStmt:
		return "stmt"
	case symbols.ScopeOwnerExpr:
		return "expr"
	default:
		return "unknown"
	}
}
