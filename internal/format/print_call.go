package format

import "surge/internal/ast"

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
		p.printExpr(arg)
	}
	if call.HasTrailingComma && len(call.Args) > 0 {
		p.writer.WriteString(",")
	}
	err = p.writer.WriteByte(')')
	if err != nil {
		panic(err)
	}
}
