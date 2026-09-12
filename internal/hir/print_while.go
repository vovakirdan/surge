package hir

func (p *Printer) printWhile(data WhileData) {
	p.printf("while ")
	p.printExpr(data.Cond)
	p.printf(" {\n")
	p.indent++
	p.printBlock(data.Body)
	p.indent--
	p.printIndent()
	p.printf("}")
	if data.Post != nil {
		p.printf(" post ")
		p.printExpr(data.Post)
	}
	p.printf("\n")
}
