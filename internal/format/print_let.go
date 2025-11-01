package format

import "surge/internal/ast"

func (p *printer) printLetItem(item *ast.Item, let *ast.LetItem) {
	if !spanValid(let.LetSpan) {
		p.writer.CopySpan(item.Span)
		return
	}

	contentLen := len(p.writer.sf.Content)
	prefixEnd := clampToContent(int(let.LetSpan.Start), contentLen)
	p.writer.CopyRange(int(item.Span.Start), prefixEnd)

	p.writer.WriteString("let")

	if spanValid(let.MutSpan) {
		p.writer.Space()
		p.writer.WriteString("mut")
	}

	p.writer.Space()
	p.writer.WriteString(p.string(let.Name))

	if let.Type != ast.NoTypeID {
		p.writer.WriteString(": ")
		p.printTypeID(let.Type)
	}

	if let.Value != ast.NoExprID {
		p.writer.WriteString(" = ")
		p.printExpr(let.Value)
	}

	p.writer.WriteString(";")

	tailStart := int(let.SemicolonSpan.End)
	if !spanValid(let.SemicolonSpan) {
		tailStart = clampToContent(int(let.LetSpan.End), contentLen)
	}
	if tailStart < contentLen && tailStart < int(item.Span.End) {
		p.writer.CopyRange(tailStart, clampToContent(int(item.Span.End), contentLen))
	}
}
