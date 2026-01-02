package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// lockAnalyzer performs lock analysis on function bodies
type lockAnalyzer struct {
	tc               *typeChecker
	state            *LockState
	selfSym          symbols.SymbolID         // Symbol for 'self' parameter if method
	receiverTypeName string                   // Type name of the receiver (for deadlock detection)
	hasTryLocks      bool                     // True if function uses try_lock methods (skip linear analysis)
	exemptLocks      map[source.StringID]bool // Locks exempt from leak checking (contract-managed)
}

// newLockAnalyzer creates a new lock analyzer for a function
func (tc *typeChecker) newLockAnalyzer() *lockAnalyzer {
	return &lockAnalyzer{
		tc:          tc,
		state:       NewLockState(),
		exemptLocks: make(map[source.StringID]bool),
	}
}

// findSelfSymbol finds the 'self' parameter symbol for a function/method
func (tc *typeChecker) findSelfSymbol(fnItem *ast.FnItem, scope symbols.ScopeID) symbols.SymbolID {
	if fnItem == nil {
		return symbols.NoSymbolID
	}

	// Get parameters
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	if len(paramIDs) == 0 {
		return symbols.NoSymbolID
	}

	// Check first parameter for 'self'
	firstParam := tc.builder.Items.FnParam(paramIDs[0])
	if firstParam == nil {
		return symbols.NoSymbolID
	}

	// Check if it's named 'self'
	paramName := tc.lookupName(firstParam.Name)
	if paramName != "self" {
		return symbols.NoSymbolID
	}

	// Find the symbol for 'self' in the function's scope
	return tc.symbolInScope(scope, firstParam.Name, symbols.SymbolParam)
}

// initFromAttributes initializes lock state from function attributes
// @requires_lock and @releases_lock both mean lock is held on entry
func (la *lockAnalyzer) initFromAttributes(fnItem *ast.FnItem, selfSym symbols.SymbolID) {
	la.selfSym = selfSym

	infos := la.tc.collectAttrs(fnItem.AttrStart, fnItem.AttrCount)

	// @requires_lock("field") - lock is held on entry (and remains held)
	// @releases_lock("field") - lock is held on entry (and will be released)
	// @acquires_lock("field") - lock will be acquired (exempt from leak check at exit)
	for _, info := range infos {
		switch info.Spec.Name {
		case "requires_lock", "releases_lock":
			if len(info.Args) > 0 {
				fieldName := la.extractFieldName(info)
				if fieldName != 0 {
					key := LockKey{
						Base:      la.selfSym,
						FieldName: fieldName,
						Kind:      LockKindMutex, // Assume mutex for now
					}
					la.state.Acquire(key, info.Span)
					la.exemptLocks[fieldName] = true
				}
			}
		case "acquires_lock":
			if len(info.Args) > 0 {
				fieldName := la.extractFieldName(info)
				if fieldName != 0 {
					la.exemptLocks[fieldName] = true
				}
			}
		}
	}
}

// extractFieldName extracts the field name StringID from an attribute argument
func (la *lockAnalyzer) extractFieldName(info AttrInfo) source.StringID {
	if len(info.Args) == 0 {
		return 0
	}
	argExpr := la.tc.builder.Exprs.Get(info.Args[0])
	if argExpr == nil || argExpr.Kind != ast.ExprLit {
		return 0
	}
	lit, ok := la.tc.builder.Exprs.Literal(info.Args[0])
	if !ok || lit.Kind != ast.ExprLitString {
		return 0
	}
	// Get the field name - strip quotes
	fieldNameRaw := la.tc.lookupName(lit.Value)
	if len(fieldNameRaw) < 2 {
		return 0
	}
	fieldNameStr := fieldNameRaw[1 : len(fieldNameRaw)-1] // Remove quotes
	return la.tc.builder.StringsInterner.Intern(fieldNameStr)
}

// analyzeFunctionLocks performs LINEAR lock analysis on a function body
// This is Step A - no branch analysis, just sequential tracking
func (tc *typeChecker) analyzeFunctionLocks(fnItem *ast.FnItem, selfSym symbols.SymbolID) {
	if fnItem == nil || !fnItem.Body.IsValid() {
		return
	}

	if debugCallContract {
		fnName := tc.lookupName(fnItem.Name)
		fmt.Printf("analyzeFunctionLocks: %s\n", fnName)
	}

	la := tc.newLockAnalyzer()

	// Get receiver type name for deadlock detection
	if selfSym.IsValid() {
		la.receiverTypeName = tc.getSymbolTypeName(selfSym)
	}

	la.initFromAttributes(fnItem, selfSym)

	// Walk the function body looking for lock/unlock calls
	tc.walkStmtForLocks(la, fnItem.Body)

	// Check for unreleased locks at function exit
	infos := tc.collectAttrs(fnItem.AttrStart, fnItem.AttrCount)

	// Collect field names from @requires_lock, @releases_lock, @acquires_lock
	// These locks are expected to remain held (or be acquired) as part of the contract
	exemptLocks := make(map[source.StringID]bool)
	for _, info := range infos {
		switch info.Spec.Name {
		case "requires_lock", "releases_lock", "acquires_lock":
			if fieldName := la.extractFieldName(info); fieldName != 0 {
				exemptLocks[fieldName] = true
			}
		}
	}

	// Check for unreleased locks at function exit (end of body, not return statements)
	// Return statements are checked by checkLocksAtReturn
	for _, acq := range la.state.HeldLocks() {
		// Skip locks that are part of the function's contract
		if exemptLocks[acq.Key.FieldName] {
			continue
		}
		fieldName := tc.lookupName(acq.Key.FieldName)
		tc.report(diag.SemaLockNotReleasedOnExit, acq.Span,
			"lock '%s' acquired here but not released before function exit", fieldName)
	}
}

// checkLocksAtReturn checks for unreleased locks at a return statement.
// Reports SemaLockNotReleasedOnExit for any locks still held.
func (tc *typeChecker) checkLocksAtReturn(la *lockAnalyzer, returnSpan source.Span) {
	if la == nil || la.hasTryLocks {
		return // Skip if using try_lock (requires more sophisticated analysis)
	}

	for _, acq := range la.state.HeldLocks() {
		// Skip locks that are part of the function's contract
		if la.exemptLocks[acq.Key.FieldName] {
			continue
		}
		fieldName := tc.lookupName(acq.Key.FieldName)
		tc.report(diag.SemaLockNotReleasedOnExit, returnSpan,
			"lock '%s' not released before return", fieldName)
	}
}

// mergeLockStates merges two lock states conservatively.
// Reports SemaLockUnbalanced if states differ in held locks.
// Returns the intersection (locks that are held in BOTH states).
func (tc *typeChecker) mergeLockStates(la *lockAnalyzer, s1, s2 *LockState, span source.Span) *LockState {
	merged := NewLockState()
	reported := make(map[LockKey]bool) // Avoid duplicate reports

	// Find locks held in both states
	for _, acq := range s1.HeldLocks() {
		if s2.IsHeld(acq.Key) {
			merged.Acquire(acq.Key, acq.Span)
		} else if !reported[acq.Key] {
			// Lock held in s1 but not s2 - report imbalance
			// Skip reporting for functions using try_lock (deferred to more advanced analysis)
			if !la.hasTryLocks {
				fieldName := tc.lookupName(acq.Key.FieldName)
				if fieldName == "" {
					// Local variable lock
					tc.report(diag.SemaLockUnbalanced, span,
						"lock may be held in one branch but not another")
				} else {
					tc.report(diag.SemaLockUnbalanced, span,
						"lock '%s' may be held in one branch but not another", fieldName)
				}
			}
			reported[acq.Key] = true
		}
	}

	// Check for locks in s2 but not s1
	for _, acq := range s2.HeldLocks() {
		if !s1.IsHeld(acq.Key) && !reported[acq.Key] {
			// Skip reporting for functions using try_lock
			if !la.hasTryLocks {
				fieldName := tc.lookupName(acq.Key.FieldName)
				if fieldName == "" {
					tc.report(diag.SemaLockUnbalanced, span,
						"lock may be held in one branch but not another")
				} else {
					tc.report(diag.SemaLockUnbalanced, span,
						"lock '%s' may be held in one branch but not another", fieldName)
				}
			}
			reported[acq.Key] = true
		}
	}

	return merged
}

// mergePathsAtJoin performs path-sensitive merging of lock states at branch join points.
// Returns the merged state and combined outcome based on how each path exits.
func (tc *typeChecker) mergePathsAtJoin(
	la *lockAnalyzer,
	thenState *LockState, thenOutcome PathOutcome,
	elseState *LockState, elseOutcome PathOutcome,
	span source.Span,
) (*LockState, PathOutcome) {
	// Both paths exit early - need to distinguish between different exit types
	if thenOutcome != PathContinues && elseOutcome != PathContinues {
		// Both return -> truly unreachable code after
		if thenOutcome == PathReturns && elseOutcome == PathReturns {
			return NewLockState(), PathReturns
		}
		// Both break -> code after loop is reachable, merge states
		if thenOutcome == PathBreaks && elseOutcome == PathBreaks {
			return tc.mergeLockStates(la, thenState, elseState, span), PathBreaks
		}
		// Both continue -> next iteration reachable, merge states
		if thenOutcome == PathContinuesLoop && elseOutcome == PathContinuesLoop {
			return tc.mergeLockStates(la, thenState, elseState, span), PathContinuesLoop
		}
		// Mixed: one returns, other breaks/continues -> non-return path's state matters
		if thenOutcome == PathReturns {
			return elseState, elseOutcome
		}
		if elseOutcome == PathReturns {
			return thenState, thenOutcome
		}
		// Mixed break/continue -> conservatively merge, use break (exits loop)
		return tc.mergeLockStates(la, thenState, elseState, span), PathBreaks
	}

	// Only then path continues -> use then state
	if thenOutcome == PathContinues && elseOutcome != PathContinues {
		return thenState, PathContinues
	}

	// Only else path continues -> use else state
	if thenOutcome != PathContinues && elseOutcome == PathContinues {
		return elseState, PathContinues
	}

	// Both paths continue -> conservative merge (existing behavior)
	return tc.mergeLockStates(la, thenState, elseState, span), PathContinues
}

// walkStmtForLocks analyzes a statement for lock operations.
// Returns PathOutcome indicating how the statement exits (continues, returns, etc.)
func (tc *typeChecker) walkStmtForLocks(la *lockAnalyzer, stmtID ast.StmtID) PathOutcome {
	if !stmtID.IsValid() {
		return PathContinues
	}

	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return PathContinues
	}

	switch stmt.Kind {
	case ast.StmtExpr:
		// Expression statement - check for lock/unlock calls
		if exprStmt := tc.builder.Stmts.Expr(stmtID); exprStmt != nil {
			tc.checkExprForLockOps(la, exprStmt.Expr)
		}
		return PathContinues

	case ast.StmtLet:
		// Let statement - check initializer for lock calls
		if letStmt := tc.builder.Stmts.Let(stmtID); letStmt != nil && letStmt.Value.IsValid() {
			tc.checkExprForLockOps(la, letStmt.Value)
		}
		return PathContinues

	case ast.StmtBlock:
		// Nested block - process sequentially, track early exits
		if block := tc.builder.Stmts.Block(stmtID); block != nil {
			for _, child := range block.Stmts {
				outcome := tc.walkStmtForLocks(la, child)
				if outcome != PathContinues {
					return outcome // Early exit from block
				}
			}
		}
		return PathContinues

	case ast.StmtIf:
		// Branch analysis with path-sensitive merging
		if ifStmt := tc.builder.Stmts.If(stmtID); ifStmt != nil {
			// Check condition expression for lock ops
			tc.checkExprForLockOps(la, ifStmt.Cond)

			// Clone state before entering branches
			stateBefore := la.state.Clone()

			// Analyze then branch
			thenOutcome := tc.walkStmtForLocks(la, ifStmt.Then)
			thenState := la.state.Clone()

			// Analyze else branch (if exists)
			var elseState *LockState
			var elseOutcome PathOutcome
			if ifStmt.Else.IsValid() {
				la.state = stateBefore.Clone()
				elseOutcome = tc.walkStmtForLocks(la, ifStmt.Else)
				elseState = la.state
			} else {
				// No else: implicit else continues with original state
				elseState = stateBefore
				elseOutcome = PathContinues
			}

			// Path-sensitive merge - use the merged outcome
			var mergedOutcome PathOutcome
			la.state, mergedOutcome = tc.mergePathsAtJoin(la, thenState, thenOutcome, elseState, elseOutcome, stmt.Span)

			// Propagate early exit (return/break/continue) from merged paths
			if mergedOutcome != PathContinues {
				return mergedOutcome
			}
		}
		return PathContinues

	case ast.StmtWhile:
		// Branch analysis for loops: loop might execute 0 or more times
		if whileStmt := tc.builder.Stmts.While(stmtID); whileStmt != nil {
			// Check condition expression for lock ops
			tc.checkExprForLockOps(la, whileStmt.Cond)

			// Clone state before loop
			stateBefore := la.state.Clone()

			// Analyze loop body (ignoring break/continue for now - conservatively treat as may continue)
			bodyOutcome := tc.walkStmtForLocks(la, whileStmt.Body)

			// For loops: merge with state before (loop might execute 0 times)
			// Break/continue within loop body exit to after the loop
			if bodyOutcome == PathContinues || bodyOutcome == PathBreaks || bodyOutcome == PathContinuesLoop {
				la.state = tc.mergeLockStates(la, stateBefore, la.state, stmt.Span)
			} else {
				// Body always returns - but loop condition may be false initially
				la.state = stateBefore
			}
		}
		return PathContinues

	case ast.StmtForClassic:
		// Branch analysis for loops: loop might execute 0 or more times
		if forStmt := tc.builder.Stmts.ForClassic(stmtID); forStmt != nil {
			// Check init, condition, and post expressions
			tc.walkStmtForLocks(la, forStmt.Init)
			tc.checkExprForLockOps(la, forStmt.Cond)
			tc.checkExprForLockOps(la, forStmt.Post)

			// Clone state before loop
			stateBefore := la.state.Clone()

			// Analyze loop body
			bodyOutcome := tc.walkStmtForLocks(la, forStmt.Body)

			// Same logic as while loop
			if bodyOutcome == PathContinues || bodyOutcome == PathBreaks || bodyOutcome == PathContinuesLoop {
				la.state = tc.mergeLockStates(la, stateBefore, la.state, stmt.Span)
			} else {
				la.state = stateBefore
			}
		}
		return PathContinues

	case ast.StmtForIn:
		// Branch analysis for loops: loop might execute 0 or more times
		if forIn := tc.builder.Stmts.ForIn(stmtID); forIn != nil {
			// Check iterable expression
			tc.checkExprForLockOps(la, forIn.Iterable)

			// Clone state before loop
			stateBefore := la.state.Clone()

			// Analyze loop body
			bodyOutcome := tc.walkStmtForLocks(la, forIn.Body)

			// Same logic as while loop
			if bodyOutcome == PathContinues || bodyOutcome == PathBreaks || bodyOutcome == PathContinuesLoop {
				la.state = tc.mergeLockStates(la, stateBefore, la.state, stmt.Span)
			} else {
				la.state = stateBefore
			}
		}
		return PathContinues

	case ast.StmtReturn:
		// Check return expression for lock ops
		if retStmt := tc.builder.Stmts.Return(stmtID); retStmt != nil && retStmt.Expr.IsValid() {
			tc.checkExprForLockOps(la, retStmt.Expr)
		}
		// Check for unreleased locks at this return point
		tc.checkLocksAtReturn(la, stmt.Span)
		// Return statement exits the function - this path doesn't continue
		return PathReturns

	case ast.StmtBreak:
		return PathBreaks

	case ast.StmtContinue:
		return PathContinuesLoop

	default:
		return PathContinues
	}
}

// checkExprForLockOps checks an expression for lock/unlock method calls
func (tc *typeChecker) checkExprForLockOps(la *lockAnalyzer, exprID ast.ExprID) {
	if !exprID.IsValid() {
		return
	}

	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return
	}

	// Look for method calls: expr.method()
	if expr.Kind == ast.ExprCall {
		call, ok := tc.builder.Exprs.Call(exprID)
		if !ok || call == nil {
			return
		}

		// Check if callee is a member access (e.g., self.lock.lock())
		calleeExpr := tc.builder.Exprs.Get(call.Target)
		if calleeExpr != nil && calleeExpr.Kind == ast.ExprMember {
			member, ok := tc.builder.Exprs.Member(call.Target)
			if !ok || member == nil {
				return
			}

			methodName := tc.lookupName(member.Field)

			// Check for lock methods
			switch methodName {
			case "lock":
				tc.handleLockAcquire(la, member.Target, LockKindMutex, expr.Span)
			case "read_lock":
				tc.handleLockAcquire(la, member.Target, LockKindRwRead, expr.Span)
			case "write_lock":
				tc.handleLockAcquire(la, member.Target, LockKindRwWrite, expr.Span)
			case "unlock", "read_unlock", "write_unlock":
				tc.handleLockRelease(la, member.Target, expr.Span)
			case "try_lock", "try_read_lock", "try_write_lock":
				la.hasTryLocks = true // Mark conditional locking (branch analysis)
			}
		}
	}

	// Recursively check sub-expressions
	tc.walkExprForLockOps(la, exprID)
}

// walkExprForLockOps recursively walks expression children
func (tc *typeChecker) walkExprForLockOps(la *lockAnalyzer, exprID ast.ExprID) {
	if !exprID.IsValid() {
		return
	}

	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return
	}

	switch expr.Kind {
	case ast.ExprCall:
		call, ok := tc.builder.Exprs.Call(exprID)
		if ok && call != nil {
			// Check inter-procedural lock contracts at call site
			tc.checkCallConcurrencyContract(la, call, expr.Span)
			// Walk arguments
			for _, arg := range call.Args {
				tc.checkExprForLockOps(la, arg.Value)
			}
		}

	case ast.ExprBinary:
		binData, ok := tc.builder.Exprs.Binary(exprID)
		if ok && binData != nil {
			// Check for assignment operators - LHS is a write access
			if isAssignmentOp(binData.Op) {
				// Check LHS for guarded field write access
				tc.checkGuardedFieldAccess(la, binData.Left, true, expr.Span)
				// Walk only the base of the LHS member, not the member itself
				// This prevents double-reporting (write + spurious read)
				tc.walkAssignmentTargetBase(la, binData.Left)
			} else {
				tc.checkExprForLockOps(la, binData.Left)
			}
			tc.checkExprForLockOps(la, binData.Right)
		}

	case ast.ExprUnary:
		unaryData, ok := tc.builder.Exprs.Unary(exprID)
		if ok && unaryData != nil {
			tc.checkExprForLockOps(la, unaryData.Operand)
		}

	case ast.ExprMember:
		memberData, ok := tc.builder.Exprs.Member(exprID)
		if ok && memberData != nil {
			// Check guarded field read access
			tc.checkGuardedFieldAccess(la, exprID, false, expr.Span)
			tc.checkExprForLockOps(la, memberData.Target)
		}

	case ast.ExprIndex:
		indexData, ok := tc.builder.Exprs.Index(exprID)
		if ok && indexData != nil {
			tc.checkExprForLockOps(la, indexData.Target)
			tc.checkExprForLockOps(la, indexData.Index)
		}
	case ast.ExprRangeLit:
		rangeData, ok := tc.builder.Exprs.RangeLit(exprID)
		if ok && rangeData != nil {
			tc.checkExprForLockOps(la, rangeData.Start)
			tc.checkExprForLockOps(la, rangeData.End)
		}
	case ast.ExprSelect:
		if data, ok := tc.builder.Exprs.Select(exprID); ok && data != nil {
			for _, arm := range data.Arms {
				tc.checkExprForLockOps(la, arm.Await)
				tc.checkExprForLockOps(la, arm.Result)
			}
		}
	case ast.ExprRace:
		if data, ok := tc.builder.Exprs.Race(exprID); ok && data != nil {
			for _, arm := range data.Arms {
				tc.checkExprForLockOps(la, arm.Await)
				tc.checkExprForLockOps(la, arm.Result)
			}
		}
	}
}

// handleLockAcquire handles a lock acquisition (e.g., self.lock.lock())
func (tc *typeChecker) handleLockAcquire(la *lockAnalyzer, targetExpr ast.ExprID, kind LockKind, span source.Span) {
	key, ok := tc.extractLockKey(targetExpr, kind)
	if !ok {
		return // Could not determine lock - skip analysis
	}

	// Record lock ordering edges for deadlock detection
	newLock := LockIdentity{
		TypeName:  tc.getLockTypeName(la, key),
		FieldName: tc.lookupName(key.FieldName),
	}
	tc.recordLockOrderEdge(la, newLock, span)

	if prevSpan, doubleLock := la.state.Acquire(key, span); doubleLock {
		fieldName := tc.lookupName(key.FieldName)
		tc.report(diag.SemaLockDoubleAcquire, span,
			"lock '%s' is already held (acquired at %s)", fieldName, prevSpan.String())
	}
}

// handleLockRelease handles a lock release (e.g., self.lock.unlock())
func (tc *typeChecker) handleLockRelease(la *lockAnalyzer, targetExpr ast.ExprID, span source.Span) {
	// Try all lock kinds since unlock() applies to any
	for _, kind := range []LockKind{LockKindMutex, LockKindRwRead, LockKindRwWrite} {
		key, ok := tc.extractLockKey(targetExpr, kind)
		if !ok {
			continue
		}

		if la.state.Release(key) {
			return // Successfully released
		}
	}

	// Skip error if function uses try_lock methods (requires branch analysis)
	if la.hasTryLocks {
		return
	}

	// Lock was not held - report error
	// Try to get field name for better error message
	if key, ok := tc.extractLockKey(targetExpr, LockKindMutex); ok {
		fieldName := tc.lookupName(key.FieldName)
		tc.report(diag.SemaLockReleaseNotHeld, span,
			"lock '%s' is not currently held", fieldName)
	} else {
		tc.report(diag.SemaLockReleaseNotHeld, span,
			"attempting to release lock that is not held")
	}
}

// extractLockKey extracts a LockKey from a lock expression
// Handles both local variable locks (mtx.lock()) and field locks (self.lock.lock())
func (tc *typeChecker) extractLockKey(targetExpr ast.ExprID, kind LockKind) (LockKey, bool) {
	if !targetExpr.IsValid() {
		return LockKey{}, false
	}

	expr := tc.builder.Exprs.Get(targetExpr)
	if expr == nil {
		return LockKey{}, false
	}

	switch expr.Kind {
	case ast.ExprIdent:
		// Local variable lock (e.g., mtx.lock())
		// Use the variable's symbol directly, with no field name
		sym := tc.getExprSymbol(targetExpr)
		if !sym.IsValid() {
			return LockKey{}, false
		}
		return LockKey{
			Base:      sym,
			FieldName: 0, // No field - it's the variable itself
			Kind:      kind,
		}, true

	case ast.ExprMember:
		// Field lock (e.g., self.lock.lock())
		memberData, ok := tc.builder.Exprs.Member(targetExpr)
		if !ok || memberData == nil {
			return LockKey{}, false
		}

		// Get base symbol
		baseSym := tc.getExprSymbol(memberData.Target)
		if !baseSym.IsValid() {
			return LockKey{}, false
		}

		return LockKey{
			Base:      baseSym,
			FieldName: memberData.Field,
			Kind:      kind,
		}, true

	default:
		return LockKey{}, false
	}
}

// getExprSymbol returns the symbol for a simple expression (identifier or self)
func (tc *typeChecker) getExprSymbol(exprID ast.ExprID) symbols.SymbolID {
	if !exprID.IsValid() {
		return symbols.NoSymbolID
	}

	// Check if we have a symbol recorded for this expression
	if tc.symbols != nil && tc.symbols.ExprSymbols != nil {
		if sym, ok := tc.symbols.ExprSymbols[exprID]; ok {
			return sym
		}
	}

	return symbols.NoSymbolID
}
