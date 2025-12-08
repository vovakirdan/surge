package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// LockKind represents the type of lock operation
type LockKind int

const (
	LockKindMutex   LockKind = iota // Mutex.lock()
	LockKindRwRead                  // RwLock.read_lock()
	LockKindRwWrite                 // RwLock.write_lock()
)

func (k LockKind) String() string {
	switch k {
	case LockKindMutex:
		return "mutex"
	case LockKindRwRead:
		return "read"
	case LockKindRwWrite:
		return "write"
	default:
		return "unknown"
	}
}

// LockKey uniquely identifies a held lock
type LockKey struct {
	Base      symbols.SymbolID // Root variable (self, parameter, local)
	FieldName source.StringID  // Field name of the lock
	Kind      LockKind
}

// LockAcquisition records where a lock was acquired
type LockAcquisition struct {
	Key  LockKey
	Span source.Span // Location where lock was acquired
}

// LockState tracks currently held locks within a function
type LockState struct {
	held []LockAcquisition // Stack of held locks (in acquisition order)
}

// NewLockState creates a new empty lock state
func NewLockState() *LockState {
	return &LockState{
		held: make([]LockAcquisition, 0, 4),
	}
}

// Clone creates a copy of the lock state (for future branch analysis)
func (s *LockState) Clone() *LockState {
	clone := &LockState{
		held: make([]LockAcquisition, len(s.held)),
	}
	copy(clone.held, s.held)
	return clone
}

// IsHeld checks if a lock is currently held
func (s *LockState) IsHeld(key LockKey) bool {
	for _, acq := range s.held {
		if acq.Key == key {
			return true
		}
	}
	return false
}

// FindAcquisition returns the acquisition info for a held lock, if any
func (s *LockState) FindAcquisition(key LockKey) (LockAcquisition, bool) {
	for _, acq := range s.held {
		if acq.Key == key {
			return acq, true
		}
	}
	return LockAcquisition{}, false
}

// Acquire attempts to acquire a lock. Returns error info if double-lock detected.
func (s *LockState) Acquire(key LockKey, span source.Span) (prevSpan source.Span, doubleLock bool) {
	if prev, found := s.FindAcquisition(key); found {
		return prev.Span, true
	}
	s.held = append(s.held, LockAcquisition{Key: key, Span: span})
	return source.Span{}, false
}

// Release attempts to release a lock. Returns false if lock was not held.
func (s *LockState) Release(key LockKey) bool {
	for i, acq := range s.held {
		if acq.Key == key {
			// Remove from held list
			s.held = append(s.held[:i], s.held[i+1:]...)
			return true
		}
	}
	return false
}

// HeldLocks returns all currently held locks
func (s *LockState) HeldLocks() []LockAcquisition {
	return s.held
}

// IsEmpty returns true if no locks are held
func (s *LockState) IsEmpty() bool {
	return len(s.held) == 0
}

// lockAnalyzer performs lock analysis on function bodies
type lockAnalyzer struct {
	tc          *typeChecker
	state       *LockState
	selfSym     symbols.SymbolID // Symbol for 'self' parameter if method
	hasTryLocks bool             // True if function uses try_lock methods (skip linear analysis)
}

// newLockAnalyzer creates a new lock analyzer for a function
func (tc *typeChecker) newLockAnalyzer() *lockAnalyzer {
	return &lockAnalyzer{
		tc:    tc,
		state: NewLockState(),
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
	for _, info := range infos {
		if (info.Spec.Name == "requires_lock" || info.Spec.Name == "releases_lock") && len(info.Args) > 0 {
			fieldName := la.extractFieldName(info)
			if fieldName != 0 {
				key := LockKey{
					Base:      la.selfSym,
					FieldName: fieldName,
					Kind:      LockKindMutex, // Assume mutex for now
				}
				la.state.Acquire(key, info.Span)
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

	la := tc.newLockAnalyzer()
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

	// Check for unreleased locks at function exit
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

// walkStmtForLocks analyzes a statement for lock operations
func (tc *typeChecker) walkStmtForLocks(la *lockAnalyzer, stmtID ast.StmtID) {
	if !stmtID.IsValid() {
		return
	}

	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return
	}

	switch stmt.Kind {
	case ast.StmtExpr:
		// Expression statement - check for lock/unlock calls
		if exprStmt := tc.builder.Stmts.Expr(stmtID); exprStmt != nil {
			tc.checkExprForLockOps(la, exprStmt.Expr)
		}

	case ast.StmtLet:
		// Let statement - check initializer for lock calls
		if letStmt := tc.builder.Stmts.Let(stmtID); letStmt != nil && letStmt.Value.IsValid() {
			tc.checkExprForLockOps(la, letStmt.Value)
		}

	case ast.StmtBlock:
		// Nested block
		if block := tc.builder.Stmts.Block(stmtID); block != nil {
			for _, child := range block.Stmts {
				tc.walkStmtForLocks(la, child)
			}
		}

	case ast.StmtIf:
		// Branch analysis: clone state, analyze branches, merge conservatively
		if ifStmt := tc.builder.Stmts.If(stmtID); ifStmt != nil {
			// Check condition expression for lock ops
			tc.checkExprForLockOps(la, ifStmt.Cond)

			// Clone state before entering branches
			stateBefore := la.state.Clone()

			// Analyze then branch
			tc.walkStmtForLocks(la, ifStmt.Then)
			thenState := la.state.Clone()

			// Analyze else branch (if exists)
			if ifStmt.Else.IsValid() {
				la.state = stateBefore.Clone()
				tc.walkStmtForLocks(la, ifStmt.Else)
				elseState := la.state

				// Merge states and check for imbalance
				la.state = tc.mergeLockStates(la, thenState, elseState, stmt.Span)
			} else {
				// No else: merge with original state (as if else was empty)
				la.state = tc.mergeLockStates(la, thenState, stateBefore, stmt.Span)
			}
		}

	case ast.StmtWhile:
		// Branch analysis for loops: loop might execute 0 or more times
		if whileStmt := tc.builder.Stmts.While(stmtID); whileStmt != nil {
			// Check condition expression for lock ops
			tc.checkExprForLockOps(la, whileStmt.Cond)

			// Clone state before loop
			stateBefore := la.state.Clone()

			// Analyze loop body
			tc.walkStmtForLocks(la, whileStmt.Body)

			// Merge states: loop might execute 0 or more times
			la.state = tc.mergeLockStates(la, stateBefore, la.state, stmt.Span)
		}

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
			tc.walkStmtForLocks(la, forStmt.Body)

			// Merge states: loop might execute 0 or more times
			la.state = tc.mergeLockStates(la, stateBefore, la.state, stmt.Span)
		}

	case ast.StmtForIn:
		// Branch analysis for loops: loop might execute 0 or more times
		if forIn := tc.builder.Stmts.ForIn(stmtID); forIn != nil {
			// Check iterable expression
			tc.checkExprForLockOps(la, forIn.Iterable)

			// Clone state before loop
			stateBefore := la.state.Clone()

			// Analyze loop body
			tc.walkStmtForLocks(la, forIn.Body)

			// Merge states: loop might execute 0 or more times
			la.state = tc.mergeLockStates(la, stateBefore, la.state, stmt.Span)
		}

	case ast.StmtReturn:
		// On return, check for unreleased locks
		// (handled at function level for now)
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
				// try_ methods require branch analysis (Step Б)
				// Mark that this function uses conditional locking
				la.hasTryLocks = true
			}
		}
	}

	// Recursively check sub-expressions
	tc.walkExprForLockOps(la, exprID)
}

// isAssignmentOp checks if the binary operator is an assignment operator
func isAssignmentOp(op ast.ExprBinaryOp) bool {
	switch op {
	case ast.ExprBinaryAssign,
		ast.ExprBinaryAddAssign,
		ast.ExprBinarySubAssign,
		ast.ExprBinaryMulAssign,
		ast.ExprBinaryDivAssign,
		ast.ExprBinaryModAssign,
		ast.ExprBinaryBitAndAssign,
		ast.ExprBinaryBitOrAssign,
		ast.ExprBinaryBitXorAssign,
		ast.ExprBinaryShlAssign,
		ast.ExprBinaryShrAssign:
		return true
	}
	return false
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
	}
}

// handleLockAcquire handles a lock acquisition (e.g., self.lock.lock())
func (tc *typeChecker) handleLockAcquire(la *lockAnalyzer, targetExpr ast.ExprID, kind LockKind, span source.Span) {
	key, ok := tc.extractLockKey(targetExpr, kind)
	if !ok {
		return // Could not determine lock - skip analysis
	}

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
