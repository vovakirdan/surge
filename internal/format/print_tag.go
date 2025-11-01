package format

import "surge/internal/ast"

func (p *printer) printTagItem(item *ast.Item, tag *ast.TagItem) {
	if !spanValid(tag.TagKeywordSpan) {
		p.writer.CopySpan(item.Span)
		return
	}

	contentLen := len(p.writer.sf.Content)
	start := clampToContent(int(item.Span.Start), contentLen)
	kwStart := clampToContent(int(tag.TagKeywordSpan.Start), contentLen)
	if start < kwStart {
		p.writer.CopyRange(start, kwStart)
	}

	p.writer.WriteString("tag")
	p.writer.Space()
	p.writer.WriteString(p.string(tag.Name))

	if len(tag.Generics) > 0 {
		p.printGenerics(tag.Generics, tag.GenericsTrailingComma)
	}

	if err := p.writer.WriteByte('('); err != nil {
		panic(err)
	}
	for i, typ := range tag.Payload {
		if i > 0 {
			p.writer.WriteString(", ")
		}
		p.printTypeID(typ)
	}
	if tag.PayloadTrailingComma && len(tag.Payload) > 0 {
		p.writer.WriteString(",")
	}
	if err := p.writer.WriteByte(')'); err != nil {
		panic(err)
	}

	if err := p.writer.WriteByte(';'); err != nil {
		panic(err)
	}

	tailStart := int(item.Span.End)
	if spanValid(tag.SemicolonSpan) {
		tailStart = clampToContent(int(tag.SemicolonSpan.End), contentLen)
	}
	if tailStart < int(item.Span.End) {
		p.writer.CopyRange(tailStart, clampToContent(int(item.Span.End), contentLen))
	}
}
