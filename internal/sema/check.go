package sema

import (
	"context"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

// Options configure a semantic pass over a file.
type Options struct {
	Reporter diag.Reporter
	Symbols  *symbols.Result
	Types    *types.Interner
	Exports  map[string]*symbols.ModuleExports
	// Instantiations records generic instantiation use-sites (optional).
	Instantiations InstantiationRecorder
	// AlienHints toggles emission of optional "alien hints" diagnostics.
	// When false, semantic diagnostics must behave exactly as before.
	AlienHints bool
	Bag        *diag.Bag
}

// Result stores semantic artefacts produced by the checker.
type Result struct {
	TypeInterner *types.Interner
	ExprTypes    map[ast.ExprID]types.TypeID
	// IsOperands captures resolved right operands for `is` expressions.
	IsOperands map[ast.ExprID]IsOperand
	// HeirOperands captures resolved operands for `heir` expressions.
	HeirOperands map[ast.ExprID]HeirOperand
	ExprBorrows  map[ast.ExprID]BorrowID
	Borrows      []BorrowInfo
	// BorrowBindings maps an active borrow (BorrowID) to the binding symbol that
	// holds the reference value (best-effort, for debug/analysis passes).
	BorrowBindings map[BorrowID]symbols.SymbolID
	// BorrowEvents is a best-effort event log produced by the borrow checker
	// (borrow start/end, moves, writes, drops, spawn escapes).
	BorrowEvents []BorrowEvent
	// CopyTypes records nominal types marked as Copy via @copy attribute.
	// Builtin Copy-ness is queried via TypeInterner.
	CopyTypes              map[types.TypeID]struct{}
	FunctionInstantiations map[symbols.SymbolID][][]types.TypeID
	ImplicitConversions    map[ast.ExprID]ImplicitConversion // Tracks implicit __to calls
	ToSymbols              map[ast.ExprID]symbols.SymbolID   // Resolved __to symbols for casts/conversions
	BindingTypes           map[symbols.SymbolID]types.TypeID // Maps symbol IDs to their resolved types
	ItemScopes             map[ast.ItemID]symbols.ScopeID    // Maps items to their scopes (for HIR lowering)
}

// Check performs semantic analysis (type inference, borrow checks, etc.).
// At this stage it handles literal typing and basic operator validation.
func Check(ctx context.Context, builder *ast.Builder, fileID ast.FileID, opts Options) Result {
	res := Result{
		ExprTypes:              make(map[ast.ExprID]types.TypeID),
		IsOperands:             make(map[ast.ExprID]IsOperand),
		HeirOperands:           make(map[ast.ExprID]HeirOperand),
		ExprBorrows:            make(map[ast.ExprID]BorrowID),
		FunctionInstantiations: make(map[symbols.SymbolID][][]types.TypeID),
		ImplicitConversions:    make(map[ast.ExprID]ImplicitConversion),
		ToSymbols:              make(map[ast.ExprID]symbols.SymbolID),
	}
	if opts.Types != nil {
		res.TypeInterner = opts.Types
	} else {
		res.TypeInterner = types.NewInterner()
	}
	if builder == nil || fileID == ast.NoFileID {
		return res
	}
	if res.TypeInterner != nil && res.TypeInterner.Strings == nil && builder.StringsInterner != nil {
		res.TypeInterner.Strings = builder.StringsInterner
	}

	checker := typeChecker{
		builder:  builder,
		fileID:   fileID,
		reporter: opts.Reporter,
		symbols:  opts.Symbols,
		result:   &res,
		types:    res.TypeInterner,
		exports:  opts.Exports,
		insts:    opts.Instantiations,
		tracer:   trace.FromContext(ctx),
	}
	checker.run()
	if opts.AlienHints {
		emitAlienHints(builder, fileID, opts)
	}
	return res
}
