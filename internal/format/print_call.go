package format

import (
	"surge/internal/ast"
	"surge/internal/source"
)

func (p *printer) printCallExpr(id ast.ExprID, expr *ast.Expr) {
	call, ok := p.builder.Exprs.Call(id)
	if !ok || call == nil {
		p.writer.CopySpan(expr.Span)
		return
	}
	var err error
	p.printExpr(call.Target)
	err = p.writer.WriteByte('(')
	if err != nil {
		panic(err)
	}
	for i, arg := range call.Args {
		if i > 0 {
			p.writer.WriteString(", ")
		}
		// Print named argument if it has a name
		if arg.Name != source.NoStringID {
			p.writer.WriteString(p.builder.StringsInterner.MustLookup(arg.Name))
			p.writer.WriteString(": ")
		}
		p.printExpr(arg.Value)
	}
	if call.HasTrailingComma && len(call.Args) > 0 {
		p.writer.WriteString(",")
	}
	err = p.writer.WriteByte(')')
	if err != nil {
		panic(err)
	}
}
