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
		Data: DropData{Synthetic: true, Steps: steps, Value: &Expr{
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
	return l.dropLocalsAt(l.earlyExitDropSymbols(stmtID), sema.DropSite{Stmt: stmtID}, span)
}

func (l *lowerer) dropLocalsAt(syms []symbols.SymbolID, site sema.DropSite, span source.Span) []DropLocal {
	if len(syms) == 0 {
		return nil
	}
	out := make([]DropLocal, 0, len(syms))
	for _, symID := range syms {
		site.Symbol = symID
		out = append(out, DropLocal{
			SymbolID: symID,
			Type:     l.bindingDropType(symID),
			Span:     span,
			Steps:    l.residualSteps(site),
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

// appendBlockExprEndDrops carries normal-tail obligations after result
// evaluation. Source statement identity distinguishes a block value from
// an explicit function return; existing statement-key plans stay authoritative.
func (l *lowerer) appendBlockExprEndDrops(block *Block, exprID ast.ExprID, sourceTail ast.StmtID, span source.Span) {
	if l.semaRes == nil || l.semaRes.BlockExprEndDrops == nil {
		return
	}
	syms := l.semaRes.BlockExprEndDrops[exprID]
	if len(syms) == 0 {
		return
	}
	if last := block.LastStmt(); last != nil {
		switch last.Kind {
		case StmtReturn, StmtRet:
			if l.builder == nil || !sourceTail.IsValid() {
				return
			}
			tail := l.builder.Stmts.Get(sourceTail)
			if tail == nil || tail.Span != last.Span {
				return
			}
			if _, exists := l.semaRes.EarlyExitDrops[sourceTail]; exists {
				return
			}
			switch last.Kind {
			case StmtReturn:
				data, ok := last.Data.(ReturnData)
				if !ok || !data.IsImplicit || tail.Kind != ast.StmtReturn {
					return
				}
				data.DropsAfterValue = l.dropLocalsAt(syms, sema.DropSite{Expr: exprID}, span)
				last.Data = data
			case StmtRet:
				data, ok := last.Data.(RetData)
				if !ok || (tail.Kind != ast.StmtExpr && tail.Kind != ast.StmtRet) {
					return
				}
				data.DropsAfterValue = l.dropLocalsAt(syms, sema.DropSite{Expr: exprID}, span)
				last.Data = data
			}
			return
		case StmtBreak, StmtContinue:
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
