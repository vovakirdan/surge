package hir

import (
	"surge/internal/sema"
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

// synthDropStmtWithPlan builds the drop of one binding — identical in shape to
// an explicit `@drop`, so MIR and the backend treat both the same way — carrying
// the plan that narrows it. An empty plan drops the whole binding; a non-empty
// one reclaims the places it still holds after part of it moved.
func (l *lowerer) synthDropStmtWithPlan(symID symbols.SymbolID, span source.Span, steps []sema.DropStep) Stmt {
	return Stmt{
		Kind: StmtDrop,
		Span: span,
		Data: DropData{Steps: steps, Value: &Expr{
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
		block.Stmts = append(block.Stmts, l.synthDropStmtWithPlan(
			symID, span, l.residualSteps(sema.DropSite{Stmt: stmtID, Symbol: symID})))
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
		out = append(out, DropLocal{
			SymbolID: symID,
			Type:     l.bindingDropType(symID),
			Span:     span,
			Steps:    l.residualSteps(sema.DropSite{Stmt: stmtID, Symbol: symID}),
		})
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
		block.Stmts = append(block.Stmts, l.synthDropStmtWithPlan(
			symID, span, l.residualSteps(sema.DropSite{Stmt: stmtID, Symbol: symID})))
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
		block.Stmts = append(block.Stmts, l.synthDropStmtWithPlan(
			symID, span, l.residualSteps(sema.DropSite{Stmt: stmtID, Symbol: symID})))
	}
	return &Stmt{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: block}}
}

// appendBlockExprEndDrops adds a block EXPRESSION's normal-exit drops
// after its last statement. Value-producing blocks end with a ret (the
// value must escape before its scope frees) and are skipped — those
// locals leak, the recorded safe-direction bound; statement-shaped arm
// blocks reclaim normally.
func (l *lowerer) appendBlockExprEndDrops(block *Block, exprID ast.ExprID, span source.Span) {
	if l.semaRes == nil || l.semaRes.BlockExprEndDrops == nil {
		return
	}
	syms := l.semaRes.BlockExprEndDrops[exprID]
	if len(syms) == 0 {
		return
	}
	if last := block.LastStmt(); last != nil {
		switch last.Kind {
		case StmtReturn, StmtRet, StmtBreak, StmtContinue:
			return
		}
	}
	for _, symID := range syms {
		block.Stmts = append(block.Stmts, l.synthDropStmtWithPlan(
			symID, span, l.residualSteps(sema.DropSite{Expr: exprID, Symbol: symID})))
	}
}

// appendArmDrops frees the per-arm drop obligations of an if-statement
// branch block (droppables moved in the sibling branch but live here).
// A branch whose tail terminates escaped its value already and carries
// no such obligation.
func (l *lowerer) appendArmDrops(block *Block, branch ast.StmtID, span source.Span) {
	if block == nil || l.semaRes == nil || l.semaRes.ArmDropsStmt == nil {
		return
	}
	syms := l.semaRes.ArmDropsStmt[branch]
	if len(syms) == 0 {
		return
	}
	if last := block.LastStmt(); last != nil {
		switch last.Kind {
		case StmtReturn, StmtRet, StmtBreak, StmtContinue:
			return
		}
	}
	for _, symID := range syms {
		block.Stmts = append(block.Stmts, l.synthDropStmtWithPlan(
			symID, span, l.residualSteps(sema.DropSite{Stmt: branch, Symbol: symID})))
	}
}

func (l *lowerer) syntheticElseDrops(ifStmt ast.StmtID) []symbols.SymbolID {
	if l.semaRes == nil || l.semaRes.IfSyntheticElseDrops == nil {
		return nil
	}
	return l.semaRes.IfSyntheticElseDrops[ifStmt]
}

// residualSteps looks up the plan for one binding at one exit. Absent means the
// binding drops whole, which is the ordinary case and the only one reachable
// while partial moves are gated.
func (l *lowerer) residualSteps(site sema.DropSite) []sema.DropStep {
	if l.semaRes == nil || l.semaRes.ResidualDrops == nil {
		return nil
	}
	return l.semaRes.ResidualDrops[site]
}
