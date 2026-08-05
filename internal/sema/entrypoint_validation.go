package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// validateEntrypoint validates entrypoint function signature based on its mode.
// Rules:
// - No mode: all params must have defaults (callable with no args)
// - Return type must be nothing, int, or implement ExitCode contract
// - argv mode: every parameter must implement FromArgv
// - stdin mode: exactly one non-default parameter must implement FromStdin
func (tc *typeChecker) validateEntrypoint(fnItem *ast.FnItem, symID symbols.SymbolID, sym *symbols.Symbol) {
	if sym == nil || fnItem == nil {
		return
	}
	if sym.Flags&symbols.SymbolFlagEntrypoint == 0 {
		return
	}

	mode := sym.EntrypointMode
	scope := tc.scopeForItem(sym.Decl.Item)

	// 1. Check "no-mode" callable without arguments rule
	if mode == symbols.EntrypointModeNone {
		tc.validateEntrypointNoMode(fnItem, sym, scope)
	}

	// 2. Check return type convertibility
	tc.validateEntrypointReturn(fnItem, symID, scope)

	// 3. Check param contracts based on mode
	switch mode {
	case symbols.EntrypointModeArgv:
		tc.validateEntrypointArgvParams(fnItem, symID, sym, scope)
	case symbols.EntrypointModeStdin:
		tc.validateEntrypointStdinParam(fnItem, symID, sym, scope)
	}
}

// validateEntrypointNoMode checks that @entrypoint without mode has all params with defaults.
func (tc *typeChecker) validateEntrypointNoMode(fnItem *ast.FnItem, sym *symbols.Symbol, _ symbols.ScopeID) {
	if sym.Signature == nil {
		return
	}
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	for i, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}
		// Check if param has a default value
		hasDefault := i < len(sym.Signature.Defaults) && sym.Signature.Defaults[i]
		if !hasDefault {
			tc.report(diag.SemaEntrypointNoModeRequiresNoArgs, param.Span,
				"@entrypoint without mode requires all parameters to have default values; parameter '%s' has no default",
				tc.lookupName(param.Name))
		}
	}
}

// validateEntrypointReturn checks that return type is nothing, int, or implements ExitCode.
func (tc *typeChecker) validateEntrypointReturn(fnItem *ast.FnItem, symID symbols.SymbolID, scope symbols.ScopeID) {
	returnType := tc.functionReturnType(fnItem, scope, false)
	nothingType := tc.types.Builtins().Nothing
	intType := tc.types.Builtins().Int

	// nothing is always OK
	if returnType == nothingType || returnType == types.NoTypeID {
		return
	}

	// int is always OK
	if returnType == intType {
		return
	}

	// Check if type has __to(self, int) -> int method (implements ExitCode)
	if tc.typeHasToInt(returnType) {
		returnSpan := fnItem.ReturnSpan
		if returnSpan == (source.Span{}) {
			returnSpan = fnItem.Span
		}
		tc.recordEntrypointCallableRequest(EntrypointCallableRequest{
			Entrypoint:     symID,
			Role:           EntrypointReturnToInt,
			Receiver:       returnType,
			Args:           []types.TypeID{intType},
			ExpectedResult: intType,
			Method:         "__to",
			AccessModule:   tc.modulePath,
			Site:           returnSpan,
		})
		return
	}

	// Report error
	returnSpan := fnItem.ReturnSpan
	if returnSpan == (source.Span{}) {
		returnSpan = fnItem.Span
	}
	tc.report(diag.SemaEntrypointReturnNotConvertible, returnSpan,
		"@entrypoint return type must be 'nothing', 'int', or implement ExitCode contract (have __to(self, int) -> int); got '%s'",
		tc.typeLabel(returnType))
}

// typeHasToInt checks if a type has __to(self, int) -> int method (for ExitCode).
func (tc *typeChecker) typeHasToInt(typeID types.TypeID) bool {
	if typeID == types.NoTypeID {
		return false
	}

	// Resolve aliases
	resolved := tc.resolveAlias(typeID)

	// Check for Option<T> or Erring<T, E> types which have built-in __to(int)
	if _, ok := tc.optionPayload(resolved); ok {
		return true
	}
	if _, _, ok := tc.resultPayload(resolved); ok {
		return true
	}

	// Check for __to method via methodsForType
	toName := tc.builder.StringsInterner.Intern("__to")
	methods := tc.methodsForType(resolved, toName)
	intType := tc.types.Builtins().Int
	for _, m := range methods {
		// Check signature: fn __to(self: T, target: int) -> int
		// The __to method has 2 params (self, target) and returns int
		if len(m.params) == 2 {
			// Check that target param is int and return is int
			if m.params[1] == intType && m.result == intType {
				return true
			}
		}
	}
	return false
}
