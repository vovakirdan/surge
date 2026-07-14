package sema

import (
	"context"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
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
	// ModulePath is the normalized module path for the file currently being
	// checked, interned in the current AST builder. Runtime ABI markers use it
	// to distinguish core definitions from user-defined lookalikes.
	ModulePath source.StringID
	// Instantiations records generic instantiation use-sites (optional).
	Instantiations InstantiationRecorder
	// AlienHints toggles emission of optional "alien hints" diagnostics.
	// When false, semantic diagnostics must behave exactly as before.
	AlienHints bool
}

func optionModulePath(builder *ast.Builder, opts Options) string {
	if builder == nil || builder.StringsInterner == nil || opts.ModulePath == source.NoStringID {
		return ""
	}
	path, ok := builder.StringsInterner.Lookup(opts.ModulePath)
	if !ok {
		return ""
	}
	return path
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
	// (borrow start/end, moves, writes, drops, task escapes).
	BorrowEvents []BorrowEvent
	// Drop obligations for scope-exit synthesis (HIR consumes; MIR emits).
	// ScopeEndDrops: droppable bindings live at a block's normal end,
	// keyed by the block statement (a function body block also carries
	// the live by-value params). EarlyExitDrops: live droppables on a
	// return/ret/break/continue, keyed by that statement, innermost
	// scope first. ReassignOldDrops: whole-binding assignments whose
	// overwritten value is live after the RHS evaluated (RHS-moved
	// targets are suppressed by construction), keyed by the assign expr.
	ScopeEndDrops    map[ast.StmtID][]symbols.SymbolID
	EarlyExitDrops   map[ast.StmtID][]symbols.SymbolID
	ReassignOldDrops map[ast.ExprID]symbols.SymbolID
	// TempDrops flags evaluations producing an owned value that nothing
	// consumes; they free at their evaluation region's end.
	TempDrops map[ast.ExprID]struct{}
	// BlockExprEndDrops: block-expression locals live at the block's
	// normal end (keyed by the block expression).
	BlockExprEndDrops map[ast.ExprID][]symbols.SymbolID
	// ArmDropsExpr: per-arm drop synthesis for partial-path moves. A
	// droppable moved on some compare/if/ternary arms but LIVE on others
	// is dropped at the end of each arm where it stays live (keyed by the
	// arm's result expression), instead of being rejected. Using the
	// value after the join stays a use-of-moved error (the binding is in
	// the union moved-set), so no maybe-dropped value is ever readable.
	ArmDropsExpr map[ast.ExprID][]symbols.SymbolID
	// ArmDropsStmt: same, for if-STATEMENT branch blocks (keyed by the
	// branch block statement).
	ArmDropsStmt map[ast.StmtID][]symbols.SymbolID
	// IfSyntheticElseDrops: an if-statement WITHOUT else that moves a
	// droppable in its then-branch needs the binding dropped on the
	// fall-through; HIR synthesizes an else block with these drops
	// (keyed by the if statement).
	IfSyntheticElseDrops map[ast.StmtID][]symbols.SymbolID
	// CopyTypes records nominal types marked as Copy via @copy attribute.
	// Builtin Copy-ness is queried via TypeInterner.
	CopyTypes              map[types.TypeID]struct{}
	FunctionInstantiations map[symbols.SymbolID][][]types.TypeID
	ImplicitConversions    map[ast.ExprID]ImplicitConversion // Tracks implicit __to calls
	ToSymbols              map[ast.ExprID]symbols.SymbolID   // Resolved __to symbols for casts/conversions
	CloneSymbols           map[ast.ExprID]symbols.SymbolID   // Resolved __clone symbols for clone() calls
	BoolSymbols            map[ast.ExprID]symbols.SymbolID   // Resolved __bool symbols for boolean contexts
	BoolBoundMethods       map[ast.ExprID]struct{}           // Generic bound __bool calls resolved after monomorphization
	RangeSymbols           map[ast.ExprID]symbols.SymbolID   // Resolved __range symbols for for-in iterables
	RangeTypes             map[ast.ExprID]types.TypeID       // Result Range<T> types for __range symbols
	MagicUnarySymbols      map[ast.ExprID]symbols.SymbolID   // Resolved magic symbols for unary operators
	MagicBinarySymbols     map[ast.ExprID]symbols.SymbolID   // Resolved magic symbols for binary operators
	IndexSymbols           map[ast.ExprID]symbols.SymbolID   // Resolved magic symbols for index expressions
	IndexSetSymbols        map[ast.ExprID]symbols.SymbolID   // Resolved magic symbols for index assignment
	BindingTypes           map[symbols.SymbolID]types.TypeID // Maps symbol IDs to their resolved types
	ItemScopes             map[ast.ItemID]symbols.ScopeID    // Maps items to their scopes (for HIR lowering)
	BlockingCaptures       map[ast.ExprID][]symbols.SymbolID // Captures for blocking { ... } expressions
	FunctionEffects        map[symbols.SymbolID]FunctionEffect
	// FarTaskAwaitSpans / FarTaskCancelSpans record `far Task<T>` await/cancel
	// call sites (Block 3) so the backend guard can emit FUT7016 /
	// FUT7017 until the Phase 4 transport backend can lower them. They are
	// type-directed (recorded only where sema resolved a `far Task<T>` receiver),
	// so a `far Task` obtained via a parameter is guarded without any `spawn on`
	// in the same file.
	FarTaskAwaitSpans  []source.Span
	FarTaskCancelSpans []source.Span
	CrossingLowering   []CrossingLoweringInfo
}

// FunctionEffect stores inferred function-level effects used by later lowering
// phases. Crossing is inferred from sema-resolved operations rather than a
// programmer-written surface marker.
type FunctionEffect struct {
	MayCross bool
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
		CloneSymbols:           make(map[ast.ExprID]symbols.SymbolID),
		BoolSymbols:            make(map[ast.ExprID]symbols.SymbolID),
		BoolBoundMethods:       make(map[ast.ExprID]struct{}),
		RangeSymbols:           make(map[ast.ExprID]symbols.SymbolID),
		RangeTypes:             make(map[ast.ExprID]types.TypeID),
		MagicUnarySymbols:      make(map[ast.ExprID]symbols.SymbolID),
		MagicBinarySymbols:     make(map[ast.ExprID]symbols.SymbolID),
		IndexSymbols:           make(map[ast.ExprID]symbols.SymbolID),
		IndexSetSymbols:        make(map[ast.ExprID]symbols.SymbolID),
		BlockingCaptures:       make(map[ast.ExprID][]symbols.SymbolID),
		FunctionEffects:        make(map[symbols.SymbolID]FunctionEffect),
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
		builder:    builder,
		fileID:     fileID,
		symbols:    opts.Symbols,
		result:     &res,
		types:      res.TypeInterner,
		exports:    opts.Exports,
		modulePath: optionModulePath(builder, opts),
		insts:      opts.Instantiations,
		tracer:     trace.FromContext(ctx),
	}
	if opts.Reporter != nil {
		checker.reporter = &diagnosticCountingReporter{
			inner:      opts.Reporter,
			errorCount: &checker.errorCount,
		}
	}
	checker.run()
	if opts.AlienHints {
		emitAlienHints(builder, fileID, opts)
	}
	return res
}
