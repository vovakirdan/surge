package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Blocking intrinsic methods that cannot be called from @nonblocking functions.
// These are methods on synchronization primitives that may block the caller.
var blockingMethods = map[string]map[string]bool{
	"Mutex": {
		"lock": true,
	},
	"RwLock": {
		"read_lock":  true,
		"write_lock": true,
	},
	"Condition": {
		"wait": true,
	},
	"Semaphore": {
		"acquire": true,
	},
	"Channel": {
		"send": true, // Blocking send - suspends until channel has capacity
		"recv": true, // Blocking receive - suspends until value available
	},
}

// checkNonblockingFunction validates that a @nonblocking function doesn't call
// any blocking operations. Reports SemaLockNonblockingCallsWait for violations.
func (tc *typeChecker) checkNonblockingFunction(fnItem *ast.FnItem, fnSpan source.Span) {
	if fnItem == nil || !fnItem.Body.IsValid() {
		return
	}

	// Walk the function body looking for blocking calls
	tc.walkStmtForBlockingCalls(fnItem.Body, fnSpan)
}

// walkStmtForBlockingCalls walks a statement looking for blocking calls.
func (tc *typeChecker) walkStmtForBlockingCalls(stmtID ast.StmtID, fnSpan source.Span) {
	if !stmtID.IsValid() {
		return
	}

	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return
	}

	switch stmt.Kind {
	case ast.StmtBlock:
		block := tc.builder.Stmts.Block(stmtID)
		if block != nil {
			for _, s := range block.Stmts {
				tc.walkStmtForBlockingCalls(s, fnSpan)
			}
		}

	case ast.StmtExpr:
		exprStmt := tc.builder.Stmts.Expr(stmtID)
		if exprStmt != nil {
			tc.walkExprForBlockingCalls(exprStmt.Expr, fnSpan)
		}

	case ast.StmtLet:
		letStmt := tc.builder.Stmts.Let(stmtID)
		if letStmt != nil && letStmt.Value.IsValid() {
			tc.walkExprForBlockingCalls(letStmt.Value, fnSpan)
		}

	case ast.StmtIf:
		ifStmt := tc.builder.Stmts.If(stmtID)
		if ifStmt != nil {
			tc.walkExprForBlockingCalls(ifStmt.Cond, fnSpan)
			tc.walkStmtForBlockingCalls(ifStmt.Then, fnSpan)
			if ifStmt.Else.IsValid() {
				tc.walkStmtForBlockingCalls(ifStmt.Else, fnSpan)
			}
		}

	case ast.StmtWhile:
		whileStmt := tc.builder.Stmts.While(stmtID)
		if whileStmt != nil {
			tc.walkExprForBlockingCalls(whileStmt.Cond, fnSpan)
			tc.walkStmtForBlockingCalls(whileStmt.Body, fnSpan)
		}

	case ast.StmtForIn:
		forStmt := tc.builder.Stmts.ForIn(stmtID)
		if forStmt != nil {
			tc.walkExprForBlockingCalls(forStmt.Iterable, fnSpan)
			tc.walkStmtForBlockingCalls(forStmt.Body, fnSpan)
		}

	case ast.StmtForClassic:
		forStmt := tc.builder.Stmts.ForClassic(stmtID)
		if forStmt != nil {
			if forStmt.Init.IsValid() {
				tc.walkStmtForBlockingCalls(forStmt.Init, fnSpan)
			}
			if forStmt.Cond.IsValid() {
				tc.walkExprForBlockingCalls(forStmt.Cond, fnSpan)
			}
			if forStmt.Post.IsValid() {
				tc.walkExprForBlockingCalls(forStmt.Post, fnSpan)
			}
			tc.walkStmtForBlockingCalls(forStmt.Body, fnSpan)
		}

	case ast.StmtReturn:
		retStmt := tc.builder.Stmts.Return(stmtID)
		if retStmt != nil && retStmt.Expr.IsValid() {
			tc.walkExprForBlockingCalls(retStmt.Expr, fnSpan)
		}
	}
}

// walkExprForBlockingCalls walks an expression looking for blocking calls.
func (tc *typeChecker) walkExprForBlockingCalls(exprID ast.ExprID, fnSpan source.Span) {
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
			// Check if this is a blocking call
			tc.checkBlockingCall(call, expr.Span, fnSpan)
			// Walk arguments
			for _, arg := range call.Args {
				tc.walkExprForBlockingCalls(arg.Value, fnSpan)
			}
		}

	case ast.ExprBinary:
		binData, ok := tc.builder.Exprs.Binary(exprID)
		if ok && binData != nil {
			tc.walkExprForBlockingCalls(binData.Left, fnSpan)
			tc.walkExprForBlockingCalls(binData.Right, fnSpan)
		}

	case ast.ExprUnary:
		unaryData, ok := tc.builder.Exprs.Unary(exprID)
		if ok && unaryData != nil {
			tc.walkExprForBlockingCalls(unaryData.Operand, fnSpan)
		}

	case ast.ExprMember:
		memberData, ok := tc.builder.Exprs.Member(exprID)
		if ok && memberData != nil {
			tc.walkExprForBlockingCalls(memberData.Target, fnSpan)
		}

	case ast.ExprIndex:
		indexData, ok := tc.builder.Exprs.Index(exprID)
		if ok && indexData != nil {
			tc.walkExprForBlockingCalls(indexData.Target, fnSpan)
			tc.walkExprForBlockingCalls(indexData.Index, fnSpan)
		}
	}
}

// checkBlockingCall checks if a call is a blocking operation and reports an error.
// fnSpan is the span of the @nonblocking function (reserved for future enhanced error messages).
func (tc *typeChecker) checkBlockingCall(call *ast.ExprCallData, callSpan, _ source.Span) {
	if call == nil {
		return
	}

	// Check for blocking method calls (e.g., mutex.lock())
	if methodName, typeName, isBlocking := tc.isBlockingMethodCall(call); isBlocking {
		tc.report(diag.SemaLockNonblockingCallsWait, callSpan,
			"@nonblocking function cannot call blocking method %s.%s", typeName, methodName)
		return
	}

	// Check if callee has @waits_on attribute (may block)
	calleeSym, _ := tc.resolveCalleeAndReceiver(call)
	if calleeSym.IsValid() {
		if tc.calleeHasWaitsOn(calleeSym) {
			tc.report(diag.SemaLockNonblockingCallsWait, callSpan,
				"@nonblocking function cannot call function with @waits_on")
		}
	}
}

// isBlockingMethodCall checks if a call is to a blocking method.
// Returns the method name, type name, and whether it's blocking.
func (tc *typeChecker) isBlockingMethodCall(call *ast.ExprCallData) (methodName, typeName string, isBlocking bool) {
	if call == nil {
		return "", "", false
	}

	calleeExpr := tc.builder.Exprs.Get(call.Target)
	if calleeExpr == nil || calleeExpr.Kind != ast.ExprMember {
		return "", "", false
	}

	member, ok := tc.builder.Exprs.Member(call.Target)
	if !ok || member == nil {
		return "", "", false
	}

	methodName = tc.lookupName(member.Field)
	if methodName == "" {
		return "", "", false
	}

	// Get the receiver type to determine if it's a blocking type
	receiverType := tc.result.ExprTypes[member.Target]
	if receiverType == types.NoTypeID {
		return methodName, "", false
	}

	// Get the base type name (stripping references)
	typeName = tc.baseTypeName(receiverType)
	if typeName == "" {
		return methodName, "", false
	}

	// Check if this type+method combination is blocking
	if methods, ok := blockingMethods[typeName]; ok {
		if methods[methodName] {
			return methodName, typeName, true
		}
	}

	return methodName, typeName, false
}

// calleeHasWaitsOn checks if a function has @waits_on attribute.
func (tc *typeChecker) calleeHasWaitsOn(symID symbols.SymbolID) bool {
	summary := tc.getFnConcurrencySummary(symID)
	return summary != nil && summary.MayBlock()
}

// baseTypeName extracts the base type name from a type, stripping references.
func (tc *typeChecker) baseTypeName(typeID types.TypeID) string {
	if typeID == types.NoTypeID {
		return ""
	}

	tt, ok := tc.types.Lookup(typeID)
	if !ok {
		return ""
	}

	// Handle references - get the element type
	if tt.Kind == types.KindReference {
		if tt.Elem != types.NoTypeID {
			return tc.baseTypeName(tt.Elem)
		}
		return ""
	}

	// For struct types, get the name from StructInfo
	if tt.Kind == types.KindStruct {
		if info, ok := tc.types.StructInfo(typeID); ok && info != nil {
			return tc.lookupName(info.Name)
		}
	}

	// For alias types, get the name from AliasInfo
	if tt.Kind == types.KindAlias {
		if info, ok := tc.types.AliasInfo(typeID); ok && info != nil {
			return tc.lookupName(info.Name)
		}
	}

	return ""
}
