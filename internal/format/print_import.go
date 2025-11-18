package format

import (
	"surge/internal/ast"
	"surge/internal/source"
)

func (p *printer) printImportItem(imp *ast.ImportItem) {
	p.writer.WriteString("import ")
	p.printModulePath(imp.Module)

	if imp.ModuleAlias != source.NoStringID {
		p.writer.WriteString(" as ")
		p.writer.WriteString(p.string(imp.ModuleAlias))
	}

	if imp.HasOne {
		p.writer.WriteString("::")
		p.writer.WriteString(p.string(imp.One.Name))
		if imp.One.Alias != source.NoStringID {
			p.writer.WriteString(" as ")
			p.writer.WriteString(p.string(imp.One.Alias))
		}
	} else if len(imp.Group) > 0 {
		p.writer.WriteString("::{")
		for i, pair := range imp.Group {
			if i > 0 {
				p.writer.WriteString(", ")
			}
			p.writer.WriteString(p.string(pair.Name))
			if pair.Alias != source.NoStringID {
				p.writer.WriteString(" as ")
				p.writer.WriteString(p.string(pair.Alias))
			}
		}
		p.writer.WriteString("}")
	}

	err := p.writer.WriteByte(';')
	if err != nil {
		panic(err)
	}
}

func (p *printer) printModulePath(parts []source.StringID) {
	for i, part := range parts {
		if i > 0 {
			err := p.writer.WriteByte('/')
			if err != nil {
				panic(err)
			}
		}
		p.writer.WriteString(p.string(part))
	}
}
