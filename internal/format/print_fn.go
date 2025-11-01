package format

import (
	"surge/internal/ast"
	"surge/internal/source"
)

func (p *printer) printFnItem(id ast.ItemID, item *ast.Item, fn *ast.FnItem) {
	if !spanValid(fn.FnKeywordSpan) {
		p.writer.CopySpan(item.Span)
		return
	}

	// Preserve trivia/attributes before `fn`.
	p.writer.CopyRange(int(item.Span.Start), int(fn.FnKeywordSpan.Start))

	p.writer.WriteString("fn")
	p.writer.Space()
	p.writer.WriteString(p.string(fn.Name))
	p.printGenerics(fn.Generics)

	var err error
	err = p.writer.WriteByte('(')
	if err != nil {
		panic(err)
	}
	p.printFnParams(fn)
	err = p.writer.WriteByte(')')
	if err != nil {
		panic(err)
	}

	if spanValid(fn.ReturnSpan) && fn.ReturnType != ast.NoTypeID {
		p.writer.Space()
		p.writer.WriteString("->")
		p.writer.Space()
		p.printTypeID(fn.ReturnType)
	}

	if fn.Body.IsValid() {
		stmt := p.builder.Stmts.Get(fn.Body)
		if stmt == nil {
			p.writer.WriteString(" { }")
			p.writer.CopyRange(int(fn.Span.End), int(item.Span.End))
			return
		}
		// Ensure at least a single space before the body if previous rune wasn't newline.
		if len(p.writer.buf) > 0 && p.writer.buf[len(p.writer.buf)-1] != '\n' {
			p.writer.Space()
		}
		p.writer.CopySpan(stmt.Span)
		p.writer.CopyRange(int(stmt.Span.End), int(item.Span.End))
		return
	}

	// Semicolon for declarations.
	if spanValid(fn.SemicolonSpan) {
		p.writer.CopySpan(fn.SemicolonSpan)
		tailStart := int(fn.SemicolonSpan.End)
		p.writer.CopyRange(tailStart, int(item.Span.End))
		return
	}

	err = p.writer.WriteByte(';')
	if err != nil {
		panic(err)
	}
	p.writer.CopyRange(int(fn.ParamsSpan.End), int(item.Span.End))
}

func (p *printer) printGenerics(names []source.StringID) {
	if len(names) == 0 {
		return
	}
	var err error
	err = p.writer.WriteByte('<')
	if err != nil {
		panic(err)
	}
	for i, id := range names {
		if i > 0 {
			p.writer.WriteString(", ")
		}
		p.writer.WriteString(p.string(id))
	}
	err = p.writer.WriteByte('>')
	if err != nil {
		panic(err)
	}
}

func (p *printer) printFnParams(fn *ast.FnItem) {
	paramIDs := p.builder.Items.GetFnParamIDs(fn)
	for i, pid := range paramIDs {
		if i > 0 {
			p.writer.WriteString(", ")
		}

		param := p.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}

		if param.Variadic {
			p.writer.WriteString("...")
		}

		p.writer.WriteString(p.paramName(param.Name))
		p.writer.WriteString(": ")
		p.printTypeID(param.Type)

		if param.Default.IsValid() {
			p.writer.WriteString(" = ")
			p.printExpr(param.Default)
		}
	}

	if fn.ParamsTrailingComma && len(paramIDs) > 0 {
		p.writer.WriteString(",")
	}
}

func (p *printer) paramName(id source.StringID) string {
	if id == source.NoStringID {
		return "_"
	}
	return p.string(id)
}
