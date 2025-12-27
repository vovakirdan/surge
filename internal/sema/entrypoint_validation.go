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
// - argv mode: params without defaults must implement FromArgv
// - stdin mode: params without defaults must implement FromStdin
func (tc *typeChecker) validateEntrypoint(fnItem *ast.FnItem, sym *symbols.Symbol) {
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
	tc.validateEntrypointReturn(fnItem, sym, scope)

	// 3. Check param contracts based on mode
	switch mode {
	case symbols.EntrypointModeArgv:
		tc.validateEntrypointParams(fnItem, sym, scope, "FromArgv", diag.SemaEntrypointParamNoFromArgv)
	case symbols.EntrypointModeStdin:
		tc.validateEntrypointParams(fnItem, sym, scope, "FromStdin", diag.SemaEntrypointParamNoFromStdin)
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
func (tc *typeChecker) validateEntrypointReturn(fnItem *ast.FnItem, _ *symbols.Symbol, scope symbols.ScopeID) {
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

// validateEntrypointParams checks that params without defaults implement the required contract.
func (tc *typeChecker) validateEntrypointParams(fnItem *ast.FnItem, sym *symbols.Symbol, scope symbols.ScopeID, contractName string, errCode diag.Code) {
	if sym.Signature == nil {
		return
	}
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	for i, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}
		// Params with defaults don't need to implement the contract
		hasDefault := i < len(sym.Signature.Defaults) && sym.Signature.Defaults[i]
		if hasDefault {
			continue
		}
		paramType := tc.resolveTypeExprWithScope(param.Type, scope)
		if paramType == types.NoTypeID {
			continue
		}
		// Check if param type implements the required contract via from_str method
		if tc.typeHasFromStr(paramType) {
			continue
		}
		tc.report(errCode, param.Span,
			"parameter '%s' of type '%s' does not implement %s contract (missing from_str method)",
			tc.lookupName(param.Name), tc.typeLabel(paramType), contractName)
	}
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

// typeHasFromStr checks if a type has from_str(&string) -> Erring<T, Error> static method (for FromArgv/FromStdin).
func (tc *typeChecker) typeHasFromStr(typeID types.TypeID) bool {
	if typeID == types.NoTypeID {
		return false
	}

	// Resolve aliases
	resolved := tc.resolveAlias(typeID)

	// Get type key candidates for the target type
	candidates := tc.typeKeyCandidates(resolved)
	if len(candidates) == 0 {
		return false
	}

	// Look for from_str static method in symbol table
	fromStrLiteral := "from_str"
	stringType := tc.types.Builtins().String

	// Search through symbols for static method with matching ReceiverKey
	if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Symbols != nil {
		if data := tc.symbols.Table.Symbols.Data(); data != nil {
			for i := range data {
				sym := &data[i]
				if sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
					continue
				}
				if sym.ReceiverKey == "" {
					continue
				}
				if tc.symbolName(sym.Name) != fromStrLiteral {
					continue
				}
				// Check if ReceiverKey matches any candidate
				for _, cand := range candidates {
					if !typeKeyEqual(sym.ReceiverKey, cand.key) {
						continue
					}
					// Check signature: fn from_str(s: &string) -> Erring<T, Error>
					if len(sym.Signature.Params) == 1 {
						paramType := tc.typeFromKey(sym.Signature.Params[0])
						if tc.isSharedStringRef(paramType, stringType) && sym.Signature.Result != "" {
							return true
						}
					}
					break
				}
			}
		}
	}

	// Also check exported symbols from other modules
	if tc.exports != nil {
		for _, module := range tc.exports {
			if module == nil {
				continue
			}
			for _, list := range module.Symbols {
				for i := range list {
					exp := &list[i]
					if exp.Kind != symbols.SymbolFunction || exp.Signature == nil {
						continue
					}
					if exp.ReceiverKey == "" {
						continue
					}
					if exp.Name != fromStrLiteral {
						continue
					}
					// Check if ReceiverKey matches any candidate
					for _, cand := range candidates {
						if !typeKeyEqual(exp.ReceiverKey, cand.key) {
							continue
						}
						// Check signature: fn from_str(s: &string) -> Erring<T, Error>
						if len(exp.Signature.Params) == 1 {
							paramType := tc.typeFromKey(exp.Signature.Params[0])
							if tc.isSharedStringRef(paramType, stringType) && exp.Signature.Result != "" {
								return true
							}
						}
						break
					}
				}
			}
		}
	}

	return false
}

func (tc *typeChecker) isSharedStringRef(paramType, stringType types.TypeID) bool {
	if paramType == types.NoTypeID || stringType == types.NoTypeID || tc.types == nil {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(paramType))
	if !ok || tt.Kind != types.KindReference || tt.Mutable {
		return false
	}
	return tt.Elem == stringType
}
