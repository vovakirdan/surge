package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) checkExternFns(_ ast.ItemID, block *ast.ExternBlock) {
	if block == nil || !block.MembersStart.IsValid() || block.MembersCount == 0 {
		return
	}
	start := uint32(block.MembersStart)
	for offset := range block.MembersCount {
		memberID := ast.ExternMemberID(start + offset)
		member := tc.builder.Items.ExternMember(memberID)
		if member == nil || member.Kind != ast.ExternMemberFn {
			continue
		}
		fn := tc.builder.Items.FnByPayload(member.Fn)
		if fn == nil {
			continue
		}
		tc.typecheckExternFn(memberID, fn)
	}
}

func (tc *typeChecker) typecheckExternFn(memberID ast.ExternMemberID, fn *ast.FnItem) {
	if fn == nil {
		return
	}
	scope := tc.scopeOrFile(tc.scopeForExtern(memberID))
	symID := tc.symbolForExtern(memberID)

	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetFnTypeParamIDs(fn), scope)
	if len(paramSpecs) == 0 && len(fn.Generics) > 0 {
		paramSpecs = specsFromNames(fn.Generics)
	}
	typeParamsPushed := tc.pushTypeParams(symID, paramSpecs, nil)
	if paramIDs := tc.builder.Items.GetFnTypeParamIDs(fn); len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, scope, nil)
		tc.attachTypeParamSymbols(symID, bounds)
		tc.applyTypeParamBounds(symID)
	}

	returnType := tc.functionReturnType(fn, scope)
	returnSpan := fn.ReturnSpan
	if returnSpan == (source.Span{}) {
		returnSpan = fn.Span
	}

	tc.registerExternParamTypes(scope, fn)

	if fn.Body.IsValid() {
		tc.pushReturnContext(returnType, returnSpan)
		pushed := tc.pushScope(scope)
		tc.walkStmt(fn.Body)
		if pushed {
			tc.leaveScope()
		}
		tc.popReturnContext()
	}
	if typeParamsPushed {
		tc.popTypeParams()
	}
}

func (tc *typeChecker) registerExternParamTypes(scope symbols.ScopeID, fnItem *ast.FnItem) {
	if tc.builder == nil || fnItem == nil {
		return
	}
	scope = tc.scopeOrFile(scope)
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	for _, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || param.Name == source.NoStringID {
			continue
		}
		paramType := tc.resolveTypeExprWithScope(param.Type, scope)
		symID := tc.symbolInScope(scope, param.Name, symbols.SymbolParam)
		if paramType != types.NoTypeID {
			tc.setBindingType(symID, paramType)
		}
	}
}
