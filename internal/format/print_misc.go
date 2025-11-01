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
	default:
		p.writer.CopySpan(expr.Span)
	}
}
