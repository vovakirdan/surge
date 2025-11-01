package format

import (
	"surge/internal/ast"
	"surge/internal/source"
)

func (p *printer) string(id source.StringID) string {
	if id == source.NoStringID || p.builder.StringsInterner == nil {
		return ""
	}
	return p.builder.StringsInterner.MustLookup(id)
}

func (p *printer) printTypeID(id ast.TypeID) {
	if !id.IsValid() {
		p.writer.WriteString("nothing")
		return
	}
	typ := p.builder.Types.Get(id)
	if typ == nil {
		return
	}
	p.writer.CopySpan(typ.Span)
}

func (p *printer) printExpr(id ast.ExprID) {
	if !id.IsValid() {
		return
	}
	expr := p.builder.Exprs.Get(id)
	if expr == nil {
		return
	}

	switch expr.Kind {
	case ast.ExprCall:
		p.printCallExpr(id, expr)
	case ast.ExprTuple:
		p.printTupleExpr(id, expr)
	case ast.ExprArray:
		p.printArrayExpr(id, expr)
	default:
		p.writer.CopySpan(expr.Span)
	}
}

func (p *printer) printTupleExpr(id ast.ExprID, expr *ast.Expr) {
	tuple, ok := p.builder.Exprs.Tuple(id)
	if !ok || tuple == nil {
		p.writer.CopySpan(expr.Span)
		return
	}

	if err := p.writer.WriteByte('('); err != nil {
		panic(err)
	}

	for i, elem := range tuple.Elements {
		if i > 0 {
			p.writer.WriteString(", ")
		}
		p.printExpr(elem)
	}

	if tuple.HasTrailingComma && len(tuple.Elements) > 0 {
		p.writer.WriteString(",")
	}

	if err := p.writer.WriteByte(')'); err != nil {
		panic(err)
	}
}

func (p *printer) printArrayExpr(id ast.ExprID, expr *ast.Expr) {
	array, ok := p.builder.Exprs.Array(id)
	if !ok || array == nil {
		p.writer.CopySpan(expr.Span)
		return
	}

	if err := p.writer.WriteByte('['); err != nil {
		panic(err)
	}

	for i, elem := range array.Elements {
		if i > 0 {
			p.writer.WriteString(", ")
		}
		p.printExpr(elem)
	}

	if array.HasTrailingComma && len(array.Elements) > 0 {
		p.writer.WriteString(",")
	}

	if err := p.writer.WriteByte(']'); err != nil {
		panic(err)
	}
}
