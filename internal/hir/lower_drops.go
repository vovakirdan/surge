package hir

import (
	"surge/internal/source"
	"surge/internal/symbols"

	"surge/internal/ast"
	"surge/internal/types"
)

// Scope-exit drop synthesis: sema records WHICH bindings drop at each
// exit point (drop obligations in sema.Result — move tracking is the
// single source of truth); this file materializes them as HIR
// statements (or return-carried lists) at the exit edges.

func (l *lowerer) scopeEndDropSymbols(stmtID ast.StmtID) []symbols.SymbolID {
	if l.semaRes == nil || l.semaRes.ScopeEndDrops == nil {
		return nil
	}
	return l.semaRes.ScopeEndDrops[stmtID]
}

func (l *lowerer) earlyExitDropSymbols(stmtID ast.StmtID) []symbols.SymbolID {
	if l.semaRes == nil || l.semaRes.EarlyExitDrops == nil {
		return nil
	}
	return l.semaRes.EarlyExitDrops[stmtID]
}

func (l *lowerer) bindingDropType(symID symbols.SymbolID) types.TypeID {
	if l.semaRes == nil || l.semaRes.BindingTypes == nil {
		return types.NoTypeID
	}
	return l.semaRes.BindingTypes[symID]
}

func (l *lowerer) bindingDropName(symID symbols.SymbolID) string {
	if l.symRes == nil || l.symRes.Table == nil || l.symRes.Table.Symbols == nil || l.strings == nil {
		return ""
	}
	if sym := l.symRes.Table.Symbols.Get(symID); sym != nil && sym.Name != source.NoStringID {
		if name, ok := l.strings.Lookup(sym.Name); ok {
			return name
		}
	}
	return ""
}

// synthDropStmt builds the drop of one binding — identical in shape to
// an explicit `@drop`, so MIR and the backend treat both the same way.
func (l *lowerer) synthDropStmt(symID symbols.SymbolID, span source.Span) Stmt {
	return Stmt{
		Kind: StmtDrop,
		Span: span,
		Data: DropData{Value: &Expr{
			Kind: ExprVarRef,
			Type: l.bindingDropType(symID),
			Span: span,
			Data: VarRefData{Name: l.bindingDropName(symID), SymbolID: symID},
		}},
	}
}

// appendScopeEndDrops adds the block's normal-exit drops after its last
// statement. A block whose tail terminates (return/ret/break/continue)
// never reaches them, so nothing is appended — those exits carry their
// own obligation lists, and dead drops after a tail return would be
// reordered around it by later normalization.
func (l *lowerer) appendScopeEndDrops(block *Block, stmtID ast.StmtID, span source.Span) {
	if last := block.LastStmt(); last != nil {
		switch last.Kind {
		case StmtReturn, StmtRet, StmtBreak, StmtContinue:
			return
		}
	}
	for _, symID := range l.scopeEndDropSymbols(stmtID) {
		block.Stmts = append(block.Stmts, l.synthDropStmt(symID, span))
	}
}

// dropLocalsFor converts a return's obligation list into the carried
// form (MIR emits them between value evaluation and the terminator).
func (l *lowerer) dropLocalsFor(stmtID ast.StmtID, span source.Span) []DropLocal {
	syms := l.earlyExitDropSymbols(stmtID)
	if len(syms) == 0 {
		return nil
	}
	out := make([]DropLocal, 0, len(syms))
	for _, symID := range syms {
		out = append(out, DropLocal{SymbolID: symID, Type: l.bindingDropType(symID), Span: span})
	}
	return out
}

// wrapExitWithDrops prefixes a break/continue with its obligation drops
// inside a wrapper block (the jump escapes through it).
func (l *lowerer) wrapExitWithDrops(exit *Stmt, stmtID ast.StmtID, span source.Span) *Stmt {
	syms := l.earlyExitDropSymbols(stmtID)
	if len(syms) == 0 {
		return exit
	}
	block := &Block{Span: span}
	for _, symID := range syms {
		block.Stmts = append(block.Stmts, l.synthDropStmt(symID, span))
	}
	block.Stmts = append(block.Stmts, *exit)
	return &Stmt{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: block}}
}

// wrapLoopWithScopeDrops places a loop's own-scope drops (classic-for
// init bindings) after the loop statement: both the condition-false
// edge and every break land there.
func (l *lowerer) wrapLoopWithScopeDrops(loop *Stmt, stmtID ast.StmtID, span source.Span) *Stmt {
	syms := l.scopeEndDropSymbols(stmtID)
	if len(syms) == 0 || loop == nil {
		return loop
	}
	block := &Block{Span: span}
	block.Stmts = append(block.Stmts, *loop)
	for _, symID := range syms {
		block.Stmts = append(block.Stmts, l.synthDropStmt(symID, span))
	}
	return &Stmt{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: block}}
}
