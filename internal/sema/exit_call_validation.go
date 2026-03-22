package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) validateSpecialCall(sym *symbols.Symbol, call *ast.ExprCallData, argTypes []types.TypeID, span source.Span) bool {
	if !tc.isNamedCall(sym, call, "exit") {
		return true
	}
	return tc.validateExitCall(call, argTypes, span)
}

func (tc *typeChecker) isNamedCall(sym *symbols.Symbol, call *ast.ExprCallData, want string) bool {
	if sym != nil {
		if name := tc.lookupName(sym.Name); name == want {
			return true
		}
	}
	if call == nil || tc.builder == nil {
		return false
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return false
	}
	return tc.lookupName(ident.Name) == want
}

func (tc *typeChecker) validateExitCall(call *ast.ExprCallData, argTypes []types.TypeID, span source.Span) bool {
	if len(argTypes) != 1 {
		return true
	}
	argType := argTypes[0]
	if tc.exitArgIsErrorLike(argType) {
		return true
	}
	reportSpan := span
	if call != nil && len(call.Args) == 1 {
		reportSpan = tc.exprSpan(call.Args[0].Value)
	}
	tc.report(
		diag.SemaTypeMismatch,
		reportSpan,
		"exit requires ErrorLike-compatible argument with fields 'message: string' and 'code: int/uint'; got %s",
		tc.typeLabel(argType),
	)
	return false
}

func (tc *typeChecker) exitArgIsErrorLike(argType types.TypeID) bool {
	msgType, codeType := tc.exitArgFieldTypes(argType)
	if msgType == types.NoTypeID || codeType == types.NoTypeID {
		return false
	}
	if tc.familyOf(msgType) != types.FamilyString {
		return false
	}
	switch tc.familyOf(codeType) {
	case types.FamilySignedInt, types.FamilyUnsignedInt:
		return true
	default:
		return false
	}
}

func (tc *typeChecker) exitArgFieldTypes(argType types.TypeID) (msgType, codeType types.TypeID) {
	if tc == nil || tc.types == nil || tc.builder == nil || tc.builder.StringsInterner == nil {
		return types.NoTypeID, types.NoTypeID
	}
	target := tc.valueType(argType)
	if target == types.NoTypeID {
		target = tc.resolveAlias(argType)
	}
	messageName := tc.builder.StringsInterner.Intern("message")
	codeName := tc.builder.StringsInterner.Intern("code")

	fields := tc.collectTypeFields(target)
	msgType = fields[messageName]
	codeType = fields[codeName]
	if msgType != types.NoTypeID || codeType != types.NoTypeID {
		return msgType, codeType
	}

	return tc.boundFieldType(target, messageName), tc.boundFieldType(target, codeName)
}
