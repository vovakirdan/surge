package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

// freshGenericArraySummary recognizes a generic body whose returned array
// header is the same local initialized by `[]`. Unknown roots may then describe
// only values pushed into that fresh buffer; a concrete NoBorrowedState element
// erases those content roots without erasing a possible view/header loan.
func (a *returnOriginAnalyzer) freshGenericArraySummary(fn *returnOriginFunction, view returnOriginTypeView,
	value returnOriginValue,
) (returnOriginValue, bool) {
	if fn == nil || fn.candidate == nil || len(fn.candidate.TemplateParams) == 0 || !fn.item.Body.IsValid() ||
		!value.normal || len(value.roots) != 1 || value.roots[0].kind != returnOriginUnknown || len(value.callables) != 0 {
		return value, false
	}
	in := fn.unit.Sema.TypeInterner
	result, canonical := returnOriginContainer(in, fn.info.Result)
	if !canonical || result.reference || result.family != in.ArrayNominalType() {
		return value, false
	}
	bound, _, resolved := view.resolve(result.element)
	required := view.requirement(returnOriginNoBorrowedState, result.element)
	if !resolved || required.failed() || len(required.atoms) != 0 || returnOriginTypeShape(in, bound, nil) != returnOriginRefFree ||
		a.loanCarrier(bound) {
		return value, false
	}
	block := fn.unit.Builder.Stmts.Block(fn.item.Body)
	if block == nil || len(block.Stmts) == 0 {
		return value, false
	}
	var returned ast.ExprID
	for _, stmtID := range block.Stmts {
		stmt := fn.unit.Builder.Stmts.Get(stmtID)
		if stmt != nil && stmt.Kind == ast.StmtReturn {
			if returned.IsValid() {
				return value, false
			}
			returned = fn.unit.Builder.Stmts.Return(stmtID).Expr
		}
	}
	if !returned.IsValid() || fn.unit.Builder.Exprs.Get(returned).Kind != ast.ExprIdent {
		return value, false
	}
	symID := fn.unit.Symbols.ExprSymbols[returned]
	sym := fn.unit.Symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolLet || sym.Type != fn.info.Result || !bWithinFunction(fn, sym) {
		return value, false
	}
	decl := fn.unit.Builder.Stmts.Let(sym.Decl.Stmt)
	if decl == nil {
		return value, false
	}
	array, literal := fn.unit.Builder.Exprs.Array(decl.Value)
	if !literal || array == nil || len(array.Elements) != 0 || fn.unit.stmtSymbols[sym.Decl.Stmt] != symID {
		return value, false
	}
	for id := range fn.unit.Sema.ExprTypes {
		node := fn.unit.Builder.Exprs.Get(id)
		data, binary := fn.unit.Builder.Exprs.Binary(id)
		if node != nil && node.Span.File == fn.item.Span.File && node.Span.Start >= fn.item.Span.Start && node.Span.End <= fn.item.Span.End &&
			binary && data != nil && data.Op == ast.ExprBinaryAssign && fn.unit.Symbols.ExprSymbols[data.Left] == symID {
			return value, false
		}
	}
	value.roots = nil
	return value, true
}

func bWithinFunction(fn *returnOriginFunction, sym *symbols.Symbol) bool {
	return fn != nil && sym != nil && sym.Span.File == fn.item.Span.File && sym.Span.Start >= fn.item.Span.Start && sym.Span.End <= fn.item.Span.End
}
