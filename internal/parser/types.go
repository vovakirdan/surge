package parser

import "surge/internal/ast"

func (p *Parser) parseTypeExpr() (ast.TypeID, bool) {

	return ast.NoTypeID, false
}

func (p *Parser) parseSimpleType() (ast.TypeID, bool) {
	return ast.NoTypeID, false
}

func (p *Parser) parseArrowType() (ast.TypeID, bool) {
	return ast.NoTypeID, false
}
