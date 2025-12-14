package hir

import (
	"context"
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// Lower transforms an AST module with semantic analysis results into HIR.
// It performs minimal desugaring:
// - Inserts explicit return for last expression in non-void functions
// - Removes ExprGroup (parentheses) by unwrapping
//
// Lower returns normalized HIR: `compare` and `for` are desugared into a smaller core,
// while async/spawn remain as separate nodes (lowered later).
func Lower(
	ctx context.Context,
	builder *ast.Builder,
	fileID ast.FileID,
	semaRes *sema.Result,
	symRes *symbols.Result,
) (*Module, error) {
	if builder == nil || fileID == ast.NoFileID || semaRes == nil {
		return nil, nil
	}

	l := &lowerer{
		ctx:      ctx,
		builder:  builder,
		semaRes:  semaRes,
		symRes:   symRes,
		strings:  builder.StringsInterner,
		nextFnID: 1,
		module: &Module{
			SourceAST:    fileID,
			TypeInterner: semaRes.TypeInterner,
			BindingTypes: semaRes.BindingTypes,
			Symbols:      symRes,
		},
	}
	l.stmtSymbols = buildStmtSymbolIndex(symRes, fileID)

	l.lowerFile(fileID)

	// Normalize HIR by desugaring high-level constructs (compare, for, etc).
	// This must run before lifting borrow artefacts so that LocalID mappings stay coherent.
	_ = NormalizeModule(l.module) //nolint:errcheck // best-effort debug artefact

	// Lift borrow checker artefacts into stable HIR-side structures.
	for _, fn := range l.module.Funcs {
		borrow, movePlan, _ := BuildBorrowGraph(ctx, fn, semaRes) //nolint:errcheck // best-effort debug artefact
		fn.Borrow = borrow
		fn.MovePlan = movePlan
	}

	return l.module, nil
}

// lowerer holds context for the lowering pass.
type lowerer struct {
	ctx         context.Context
	builder     *ast.Builder
	semaRes     *sema.Result
	symRes      *symbols.Result
	strings     *source.Interner
	module      *Module
	nextFnID    FuncID
	stmtSymbols map[ast.StmtID]symbols.SymbolID
}

func buildStmtSymbolIndex(symRes *symbols.Result, fileID ast.FileID) map[ast.StmtID]symbols.SymbolID {
	if symRes == nil || symRes.Table == nil || symRes.Table.Symbols == nil {
		return nil
	}
	data := symRes.Table.Symbols.Data()
	if len(data) == 0 {
		return nil
	}
	out := make(map[ast.StmtID]symbols.SymbolID)
	for idx := range data {
		sym := data[idx]
		value, err := safecast.Conv[uint32](idx + 1)
		if err != nil {
			panic(fmt.Errorf("symbol index overflow: %w", err))
		}
		id := symbols.SymbolID(value)
		if sym.Decl.ASTFile.IsValid() && sym.Decl.ASTFile != fileID {
			continue
		}
		if sym.Decl.Stmt.IsValid() {
			if _, exists := out[sym.Decl.Stmt]; exists {
				continue
			}
			out[sym.Decl.Stmt] = id
		}
	}
	return out
}

func (l *lowerer) symbolForStmt(id ast.StmtID) symbols.SymbolID {
	if l == nil || l.stmtSymbols == nil {
		return symbols.NoSymbolID
	}
	if sym, ok := l.stmtSymbols[id]; ok {
		return sym
	}
	return symbols.NoSymbolID
}

// lowerFile processes all items in an AST file.
func (l *lowerer) lowerFile(fileID ast.FileID) {
	file := l.builder.Files.Get(fileID)
	if file == nil {
		return
	}

	for _, itemID := range file.Items {
		item := l.builder.Items.Arena.Get(uint32(itemID))
		if item == nil {
			continue
		}

		switch item.Kind {
		case ast.ItemFn:
			if fn := l.lowerFnItem(itemID); fn != nil {
				l.module.Funcs = append(l.module.Funcs, fn)
			}
		case ast.ItemLet:
			if v := l.lowerLetItem(itemID); v != nil {
				l.module.Globals = append(l.module.Globals, *v)
			}
		case ast.ItemConst:
			if c := l.lowerConstItem(itemID); c != nil {
				l.module.Consts = append(l.module.Consts, *c)
			}
		case ast.ItemType:
			if t := l.lowerTypeItem(itemID); t != nil {
				l.module.Types = append(l.module.Types, *t)
			}
		case ast.ItemTag:
			if t := l.lowerTagItem(itemID); t != nil {
				l.module.Types = append(l.module.Types, *t)
			}
		case ast.ItemContract:
			if c := l.lowerContractItem(itemID); c != nil {
				l.module.Types = append(l.module.Types, *c)
			}
			// ItemImport, ItemPragma, ItemExtern, ItemMacro are not lowered to HIR
		}
	}
}
