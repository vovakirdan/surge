package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) recordMagicUnarySymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.MagicUnarySymbols == nil {
		tc.result.MagicUnarySymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.MagicUnarySymbols[exprID] = symID
}

func (tc *typeChecker) recordMagicOpInstantiation(symID symbols.SymbolID, recv types.TypeID, span source.Span) {
	if tc == nil || !symID.IsValid() {
		return
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || len(sym.TypeParams) == 0 {
		return
	}
	recvArgs := tc.receiverTypeArgs(recv)
	if len(recvArgs) == 0 || len(recvArgs) != len(sym.TypeParams) {
		return
	}
	tc.rememberFunctionInstantiation(symID, recvArgs, span, "magic-op")
}

func (tc *typeChecker) recordMagicBinarySymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.MagicBinarySymbols == nil {
		tc.result.MagicBinarySymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.MagicBinarySymbols[exprID] = symID
}

func (tc *typeChecker) recordBoolSymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.BoolSymbols == nil {
		tc.result.BoolSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.BoolSymbols[exprID] = symID
}

func (tc *typeChecker) recordBoolBoundMethod(exprID ast.ExprID) {
	if tc.result == nil {
		return
	}
	if tc.result.BoolBoundMethods == nil {
		tc.result.BoolBoundMethods = make(map[ast.ExprID]struct{})
	}
	tc.result.BoolBoundMethods[exprID] = struct{}{}
}

func (tc *typeChecker) recordRangeSymbol(exprID ast.ExprID, symID symbols.SymbolID, rangeType types.TypeID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.RangeSymbols == nil {
		tc.result.RangeSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	if tc.result.RangeTypes == nil {
		tc.result.RangeTypes = make(map[ast.ExprID]types.TypeID)
	}
	tc.result.RangeSymbols[exprID] = symID
	tc.result.RangeTypes[exprID] = rangeType
}

func (tc *typeChecker) recordIndexSymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.IndexSymbols == nil {
		tc.result.IndexSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.IndexSymbols[exprID] = symID
}

func (tc *typeChecker) recordIndexSetSymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.IndexSetSymbols == nil {
		tc.result.IndexSetSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.IndexSetSymbols[exprID] = symID
}
