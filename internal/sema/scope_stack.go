package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) buildScopeIndex() {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
		return
	}
	data := tc.symbols.Table.Scopes.Data()
	if len(data) == 0 {
		return
	}
	tc.scopeByItem = make(map[ast.ItemID]symbols.ScopeID)
	tc.scopeByStmt = make(map[ast.StmtID]symbols.ScopeID)
	for idx := range data {
		scope := data[idx]
		value, err := safecast.Conv[uint32](idx + 1)
		if err != nil {
			panic(fmt.Errorf("scope index overflow: %w", err))
		}
		id := symbols.ScopeID(value)
		owner := scope.Owner
		if owner.ASTFile.IsValid() && owner.ASTFile != tc.fileID {
			continue
		}
		if owner.Extern.IsValid() {
			if tc.scopeByExtern == nil {
				tc.scopeByExtern = make(map[ast.ExternMemberID]symbols.ScopeID)
			}
			tc.scopeByExtern[owner.Extern] = id
			continue
		}
		switch owner.Kind {
		case symbols.ScopeOwnerItem:
			if owner.Item.IsValid() {
				tc.scopeByItem[owner.Item] = id
			}
		case symbols.ScopeOwnerStmt:
			if owner.Stmt.IsValid() {
				tc.scopeByStmt[owner.Stmt] = id
			}
		}
	}
}

func (tc *typeChecker) buildSymbolIndex() {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return
	}
	data := tc.symbols.Table.Symbols.Data()
	if len(data) == 0 {
		return
	}
	tc.stmtSymbols = make(map[ast.StmtID]symbols.SymbolID)
	for idx := range data {
		sym := data[idx]
		value, err := safecast.Conv[uint32](idx + 1)
		if err != nil {
			panic(fmt.Errorf("symbol index overflow: %w", err))
		}
		id := symbols.SymbolID(value)
		if sym.Decl.ASTFile.IsValid() && sym.Decl.ASTFile != tc.fileID {
			continue
		}
		if sym.Decl.Stmt.IsValid() {
			// Prefer the latest symbol when a stmt is re-resolved (locals aren't reused).
			tc.stmtSymbols[sym.Decl.Stmt] = id
		}
	}
}

func (tc *typeChecker) fileScope() symbols.ScopeID {
	if tc.symbols == nil {
		return symbols.NoScopeID
	}
	return tc.symbols.FileScope
}

func (tc *typeChecker) pushScope(scope symbols.ScopeID) bool {
	if !scope.IsValid() {
		return false
	}
	tc.scopeStack = append(tc.scopeStack, scope)
	return true
}

func (tc *typeChecker) leaveScope() {
	if len(tc.scopeStack) == 0 {
		return
	}
	top := tc.scopeStack[len(tc.scopeStack)-1]
	tc.scopeStack = tc.scopeStack[:len(tc.scopeStack)-1]
	if tc.borrow != nil {
		if ids := tc.borrow.ScopeBorrows(top); len(ids) > 0 {
			for _, id := range ids {
				var place Place
				if info := tc.borrow.Info(id); info != nil {
					place = info.Place
				}
				var binding symbols.SymbolID
				if tc.borrowBindings != nil {
					binding = tc.borrowBindings[id]
				}
				tc.recordBorrowEvent(&BorrowEvent{
					Kind:    BorrowEvBorrowEnd,
					Borrow:  id,
					Place:   place,
					Binding: binding,
					Scope:   top,
					Note:    "scope_end",
				})
			}
		}
		tc.borrow.EndScope(top)
	}
	// Check for task leaks (structured concurrency)
	if tc.taskTracker != nil {
		leaks := tc.taskTracker.EndScope(top)
		for _, leak := range leaks {
			if leak.InAsyncBlock {
				continue
			}
			tc.report(diag.SemaTaskNotAwaited, leak.Span,
				"task is neither awaited nor returned")
		}
	}
	tc.releaseScopeBindings(top)
}

func (tc *typeChecker) currentScope() symbols.ScopeID {
	if len(tc.scopeStack) == 0 {
		return symbols.NoScopeID
	}
	return tc.scopeStack[len(tc.scopeStack)-1]
}

func (tc *typeChecker) scopeForItem(id ast.ItemID) symbols.ScopeID {
	if tc.scopeByItem == nil {
		return symbols.NoScopeID
	}
	return tc.scopeByItem[id]
}

func (tc *typeChecker) scopeForStmt(id ast.StmtID) symbols.ScopeID {
	if tc.scopeByStmt == nil {
		return symbols.NoScopeID
	}
	return tc.scopeByStmt[id]
}

func (tc *typeChecker) scopeForExtern(id ast.ExternMemberID) symbols.ScopeID {
	if tc.scopeByExtern == nil {
		return symbols.NoScopeID
	}
	return tc.scopeByExtern[id]
}

func (tc *typeChecker) flushBorrowResults() {
	if tc.result == nil || tc.borrow == nil {
		return
	}
	if snapshot := tc.borrow.ExprBorrowSnapshot(); len(snapshot) > 0 {
		tc.result.ExprBorrows = snapshot
	}
	if infos := tc.borrow.Infos(); len(infos) > 0 {
		tc.result.Borrows = infos
	}
	if len(tc.borrowEvents) > 0 {
		tc.result.BorrowEvents = append([]BorrowEvent(nil), tc.borrowEvents...)
	}
	if len(tc.borrowBindings) > 0 {
		out := make(map[BorrowID]symbols.SymbolID, len(tc.borrowBindings))
		for k, v := range tc.borrowBindings {
			out[k] = v
		}
		tc.result.BorrowBindings = out
	}
	if len(tc.copyTypes) > 0 {
		out := make(map[types.TypeID]struct{}, len(tc.copyTypes))
		for k, v := range tc.copyTypes {
			out[k] = v
		}
		tc.result.CopyTypes = out
	}
}

func (tc *typeChecker) releaseScopeBindings(scope symbols.ScopeID) {
	if tc.bindingBorrow == nil || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
		return
	}
	scopeData := tc.symbols.Table.Scopes.Get(scope)
	if scopeData == nil {
		return
	}
	for _, symID := range scopeData.Symbols {
		if symID == symbols.NoSymbolID {
			continue
		}
		if bid := tc.bindingBorrow[symID]; bid != NoBorrowID && tc.borrow != nil {
			tc.borrow.DropBorrow(bid)
		}
		delete(tc.bindingBorrow, symID)
	}
}
