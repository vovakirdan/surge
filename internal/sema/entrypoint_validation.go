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
// - Return type must be nothing, int, or implement the ExitCode contract
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

// validateEntrypointReturn accepts `nothing` and `int` directly. Any other
// result must implement the ExitCode contract (`fn __exit_code(self: &T) -> int`,
// core/entrypoint.sg); the exact callable is selected once, after all modules
// are merged, by FinalizeEntrypointCallables, which also reports its absence.
func (tc *typeChecker) validateEntrypointReturn(fnItem *ast.FnItem, symID symbols.SymbolID, scope symbols.ScopeID) {
	returnType := tc.functionReturnType(fnItem, scope, false)
	if returnType == types.NoTypeID || returnType == tc.types.Builtins().Nothing || returnType == tc.types.Builtins().Int {
		return
	}
	returnSpan := fnItem.ReturnSpan
	if returnSpan == (source.Span{}) {
		returnSpan = fnItem.Span
	}
	tc.recordEntrypointCallableRequest(&EntrypointCallableRequest{
		Entrypoint:     symID,
		Role:           EntrypointReturnExitCode,
		TypeLabel:      tc.typeLabel(returnType),
		CanDefineHere:  tc.canDefineEntrypointParserHere(returnType),
		Receiver:       returnType,
		ExpectedResult: tc.types.Builtins().Int,
		Method:         entrypointExitCodeMethod,
		AccessModule:   tc.modulePath,
		Site:           returnSpan,
	})
}
