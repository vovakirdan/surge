package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

var debugCallContract = false

// checkCallConcurrencyContract validates lock contracts at a call site.
// This implements inter-procedural lock contract checking for:
// - @requires_lock: caller must hold the lock
// - @acquires_lock: caller must NOT hold the lock (it will be acquired)
// - @releases_lock: caller must hold the lock (it will be released)
func (tc *typeChecker) checkCallConcurrencyContract(la *lockAnalyzer, call *ast.ExprCallData, span source.Span) {
	if call == nil {
		return
	}

	// Try to resolve the callee function symbol and its receiver
	calleeSym, receiverSym := tc.resolveCalleeAndReceiver(call)
	if debugCallContract {
		// Get symbol name if available
		symName := ""
		symKind := ""
		symReceiverKey := ""
		if calleeSym.IsValid() && tc.symbols != nil && tc.symbols.Table != nil {
			if sym := tc.symbols.Table.Symbols.Get(calleeSym); sym != nil {
				symName = tc.symbolName(sym.Name)
				symKind = sym.Kind.String()
				symReceiverKey = string(sym.ReceiverKey)
			}
		}
		fmt.Printf("checkCallConcurrencyContract: calleeSym=%v (name=%s, kind=%s, receiver=%s), receiverSym=%v, span=%v\n",
			calleeSym, symName, symKind, symReceiverKey, receiverSym, span)
	}
	if !calleeSym.IsValid() {
		return
	}

	// Get the concurrency summary for the callee
	summary := tc.getFnConcurrencySummary(calleeSym)
	if summary == nil || !summary.HasConcurrencyContract() {
		return
	}

	// Check @requires_lock contracts
	for _, lockInfo := range summary.RequiresLocks {
		// For requires_lock, we check if any kind of the lock is held
		key := LockKey{
			Base:      receiverSym,
			FieldName: lockInfo.FieldName,
			Kind:      lockInfo.Kind,
		}

		if !tc.isAnyLockHeld(la, key) {
			name := tc.lookupName(lockInfo.FieldName)
			tc.report(diag.SemaLockRequiresNotHeld, span,
				"calling function requires holding lock '%s'", name)
		}
	}

	// Check @acquires_lock contracts
	for _, lockInfo := range summary.AcquiresLocks {
		// For acquires_lock, check if already held (any kind), then acquire with correct kind
		key := LockKey{
			Base:      receiverSym,
			FieldName: lockInfo.FieldName,
			Kind:      lockInfo.Kind,
		}

		if tc.isAnyLockHeld(la, key) {
			name := tc.lookupName(lockInfo.FieldName)
			tc.report(diag.SemaLockDoubleAcquire, span,
				"function will acquire lock '%s' which is already held", name)
		} else {
			// Record that the callee acquires this lock with the correct kind
			la.state.Acquire(key, span)
		}
	}

	// Check @releases_lock contracts
	for _, lockInfo := range summary.ReleasesLocks {
		// For releases_lock, check if held (any kind), then release with correct kind
		key := LockKey{
			Base:      receiverSym,
			FieldName: lockInfo.FieldName,
			Kind:      lockInfo.Kind,
		}

		if !tc.isAnyLockHeld(la, key) {
			name := tc.lookupName(lockInfo.FieldName)
			tc.report(diag.SemaLockReleaseNotHeld, span,
				"function will release lock '%s' which is not held", name)
		} else {
			// Record that the callee releases this lock with the correct kind
			la.state.Release(key)
		}
	}
}

// isAnyLockHeld checks if a lock is held with any lock kind (mutex, read, write).
func (tc *typeChecker) isAnyLockHeld(la *lockAnalyzer, key LockKey) bool {
	for _, kind := range []LockKind{LockKindMutex, LockKindRwRead, LockKindRwWrite} {
		k := LockKey{Base: key.Base, FieldName: key.FieldName, Kind: kind}
		if la.state.IsHeld(k) {
			return true
		}
	}
	return false
}

// resolveCalleeAndReceiver resolves the callee symbol and receiver symbol for a call.
// For method calls like self.method(), returns (method_symbol, self_symbol).
// For direct calls, returns (function_symbol, NoSymbolID).
func (tc *typeChecker) resolveCalleeAndReceiver(call *ast.ExprCallData) (callee, receiver symbols.SymbolID) {
	if call == nil {
		return symbols.NoSymbolID, symbols.NoSymbolID
	}

	if debugCallContract {
		targetExpr := tc.builder.Exprs.Get(call.Target)
		if targetExpr != nil {
			fmt.Printf("resolveCalleeAndReceiver: target ExprID=%v, Kind=%d\n", call.Target, targetExpr.Kind)
		}
	}

	// Try to get member expression directly - this works for method calls
	if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
		if debugCallContract {
			fieldName := tc.lookupName(member.Field)
			fmt.Printf("resolveCalleeAndReceiver: method call, field=%s\n", fieldName)
		}

		// Get the receiver symbol (e.g., 'self')
		receiverSym := tc.getExprSymbol(member.Target)

		// Resolve the method symbol based on receiver type and method name
		methodSym := tc.resolveMethodSymbol(member.Target, member.Field)

		if debugCallContract {
			fmt.Printf("resolveCalleeAndReceiver: methodSym=%v, receiverSym=%v\n", methodSym, receiverSym)
		}

		return methodSym, receiverSym
	}

	// Case 2: Direct function call (e.g., some_func())
	if debugCallContract {
		fmt.Println("resolveCalleeAndReceiver: direct call")
	}
	calleeSym := tc.symbolForExpr(call.Target)
	return calleeSym, symbols.NoSymbolID
}

// resolveMethodSymbol finds the symbol for a method given receiver expression and method name.
func (tc *typeChecker) resolveMethodSymbol(receiverExpr ast.ExprID, methodName source.StringID) symbols.SymbolID {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		if debugCallContract {
			fmt.Println("resolveMethodSymbol: symbols table nil")
		}
		return symbols.NoSymbolID
	}

	// Get the receiver type
	receiverType := tc.result.ExprTypes[receiverExpr]
	if receiverType == types.NoTypeID {
		if debugCallContract {
			fmt.Println("resolveMethodSymbol: receiver type not found")
		}
		return symbols.NoSymbolID
	}

	// Get the type key for the receiver
	receiverKey := tc.typeKeyForType(receiverType)
	if receiverKey == "" {
		if debugCallContract {
			fmt.Println("resolveMethodSymbol: receiver key empty")
		}
		return symbols.NoSymbolID
	}

	// Convert method name to string
	methodNameStr := tc.lookupName(methodName)
	if methodNameStr == "" {
		if debugCallContract {
			fmt.Println("resolveMethodSymbol: method name empty")
		}
		return symbols.NoSymbolID
	}

	if debugCallContract {
		fmt.Printf("resolveMethodSymbol: looking for method '%s' on receiver key '%s'\n", methodNameStr, receiverKey)
	}

	// Search for the method symbol in the symbol table
	data := tc.symbols.Table.Symbols.Data()
	if data == nil {
		if debugCallContract {
			fmt.Println("resolveMethodSymbol: symbols data nil")
		}
		return symbols.NoSymbolID
	}

	if debugCallContract {
		fmt.Printf("resolveMethodSymbol: searching %d symbols\n", len(data))
	}

	// Note: Data() returns s.data[1:], so data[i] corresponds to SymbolID(i+1)
	for i := range data {
		sym := &data[i]
		if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" {
			continue
		}

		// Check if this is the method we're looking for
		symName := tc.symbolName(sym.Name)
		if debugCallContract && symName == methodNameStr {
			fmt.Printf("resolveMethodSymbol: found method '%s' with ReceiverKey='%s' at index %d\n", symName, sym.ReceiverKey, i)
		}
		if symName != methodNameStr {
			continue
		}

		// Check if receiver type matches (handles references like &T matching T)
		if typeKeyMatches(sym.ReceiverKey, receiverKey) {
			// Symbol IDs are bounded by the arena size, which is always < MaxUint32
			symID := symbols.SymbolID(i + 1) //nolint:gosec // Add 1 because Data() returns s.data[1:]
			if debugCallContract {
				fmt.Printf("resolveMethodSymbol: matched! returning symbol %d\n", symID)
			}
			return symID
		}
	}

	if debugCallContract {
		fmt.Println("resolveMethodSymbol: no match found")
	}
	return symbols.NoSymbolID
}

// typeKeyMatches checks if two type keys match (handles references and borrowing).
func typeKeyMatches(key1, key2 symbols.TypeKey) bool {
	if key1 == key2 {
		return true
	}

	// Strip reference prefixes for comparison
	s1, s2 := string(key1), string(key2)
	s1 = stripRefPrefix(s1)
	s2 = stripRefPrefix(s2)

	return s1 == s2
}

// stripRefPrefix removes reference prefixes (&, &mut) from a type key string.
func stripRefPrefix(s string) string {
	if len(s) > 5 && s[:5] == "&mut " {
		return s[5:]
	}
	if len(s) > 1 && s[0] == '&' {
		return s[1:]
	}
	return s
}
