package hir

import (
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
// The lowering preserves high-level constructs like compare, for, async/spawn.
func Lower(
	builder *ast.Builder,
	fileID ast.FileID,
	semaRes *sema.Result,
	symRes *symbols.Result,
) (*Module, error) {
	if builder == nil || fileID == ast.NoFileID || semaRes == nil {
		return nil, nil
	}

	l := &lowerer{
		builder:  builder,
		semaRes:  semaRes,
		symRes:   symRes,
		strings:  builder.StringsInterner,
		nextFnID: 1,
		module:   &Module{SourceAST: fileID},
	}

	l.lowerFile(fileID)

	return l.module, nil
}

// lowerer holds context for the lowering pass.
type lowerer struct {
	builder  *ast.Builder
	semaRes  *sema.Result
	symRes   *symbols.Result
	strings  *source.Interner
	module   *Module
	nextFnID FuncID
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
