package parser

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

// LetBinding представляет парсированный биндинг let
type LetBinding struct {
	Name       source.StringID
	Type       ast.TypeID
	Value      ast.ExprID
	IsMut      bool
	Span       source.Span
	MutSpan    source.Span
	NameSpan   source.Span
	ColonSpan  source.Span
	TypeSpan   source.Span
	AssignSpan source.Span
	ValueSpan  source.Span
}

// parseTypeExpr распознаёт полные типовые выражения:
//
//	own Type, &Type, &mut Type, *Type (префиксы)
//	Type[] (постфиксы)
//	Type[Expr] (постфиксы)
//	простые типы
func (p *Parser) parseTypeExpr() (ast.TypeID, bool) {
	// Если нет двоеточия, тип не указан
	if !p.at(token.Colon) {
		return ast.NoTypeID, true
	}
	p.advance()

	return p.parseTypePrefix()
}

// parseTypePrimary обрабатывает базовые формы типов:
//
//	идентификатор/квалифицированный.путь
//	nothing
//	( tuple )
//	fn ( сигнатура ) -> ...
func (p *Parser) parseTypePrimary() (ast.TypeID, bool) {
	startSpan := p.lx.Peek().Span

	switch p.lx.Peek().Kind {
	case token.Ident:
		identTok := p.lx.Peek()
		identID, ok := p.parseIdent()
		if !ok {
			return ast.NoTypeID, false
		}
		return p.finishTypePath(identID, identTok.Span)

	case token.NothingLit:
		nothingTok := p.advance()
		return p.parseTypeSuffix(p.makeNothingType(nothingTok.Span))

	case token.LParen:
		openTok := p.advance()

		if p.at(token.RParen) {
			closeTok := p.advance()
			tupleType := p.arenas.Types.NewTuple(openTok.Span.Cover(closeTok.Span), nil)
			return p.parseTypeSuffix(tupleType)
		}

		firstElem, ok := p.parseTypePrefix()
		if !ok {
			return ast.NoTypeID, false
		}
		elements := []ast.TypeID{firstElem}
		sawComma := false

		for p.at(token.Comma) {
			sawComma = true
			p.advance() // съедаем ','

			if p.at(token.RParen) {
				break // допускаем завершающую запятую
			}

			var elem ast.TypeID
			elem, ok = p.parseTypePrefix()
			if !ok {
				return ast.NoTypeID, false
			}
			elements = append(elements, elem)
		}

		closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close tuple type", nil)
		if !ok {
			return ast.NoTypeID, false
		}

		if len(elements) == 1 && !sawComma {
			// скобки — просто группировка
			return p.parseTypeSuffix(elements[0])
		}

		tupleType := p.arenas.Types.NewTuple(openTok.Span.Cover(closeTok.Span), elements)
		return p.parseTypeSuffix(tupleType)

	case token.KwFn:
		fnTok := p.advance()
		if _, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after 'fn' in function type", nil); !ok {
			return ast.NoTypeID, false
		}

		var params []ast.TypeFnParam

		if !p.at(token.RParen) {
			for {
				if p.at(token.DotDotDot) {
					p.advance()
					elemType, ok := p.parseTypePrefix()
					if !ok {
						return ast.NoTypeID, false
					}
					params = append(params, ast.TypeFnParam{
						Type:     elemType,
						Name:     source.NoStringID,
						Variadic: true,
					})
					if p.at(token.Comma) {
						p.err(diag.SynUnexpectedToken, "variadic parameter must be last in function type")
						p.advance()
					}
					break
				}

				elemType, ok := p.parseTypePrefix()
				if !ok {
					return ast.NoTypeID, false
				}
				params = append(params, ast.TypeFnParam{
					Type:     elemType,
					Name:     source.NoStringID,
					Variadic: false,
				})

				if !p.at(token.Comma) {
					break
				}
				p.advance()

				if p.at(token.RParen) {
					break
				}
			}
		}

		closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close function type parameters", nil)
		if !ok {
			return ast.NoTypeID, false
		}

		fnSpan := fnTok.Span.Cover(closeTok.Span)
		var returnType ast.TypeID

		if p.at(token.Arrow) {
			arrowTok := p.advance()
			retType, ok := p.parseTypePrefix()
			if !ok {
				return ast.NoTypeID, false
			}
			returnType = retType
			retSpan := p.arenas.Types.Get(returnType).Span
			fnSpan = fnSpan.Cover(arrowTok.Span.Cover(retSpan))
		} else {
			retSpan := source.Span{
				File:  closeTok.Span.File,
				Start: closeTok.Span.End,
				End:   closeTok.Span.End,
			}
			returnType = p.makeNothingType(retSpan)
		}

		fnType := p.arenas.Types.NewFn(fnSpan, params, returnType)
		return p.parseTypeSuffix(fnType)

	default:
		// p.err(diag.SynExpectType, "expected type")
		// так как := это токен, то мы можем уверенно сдвигаться
		spanColon := p.lastSpan
		p.emitDiagnostic(
			diag.SynExpectType,
			diag.SevError,
			spanColon,
			"expected type after colon",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				fixID := fix.MakeFixID(diag.SynExpectType, spanColon)
				suggestion := fix.DeleteSpan(
					"remove colon to simplify the type expression",
					spanColon,
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe), // todo подумать безопасно ли это
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(startSpan, "remove colon to simplify the type expression")
			},
		)
		return ast.NoTypeID, false
	}
}

func (p *Parser) makeNothingType(span source.Span) ast.TypeID {
	nameID := p.arenas.StringsInterner.Intern("nothing")
	segments := []ast.TypePathSegment{{
		Name:     nameID,
		Generics: nil,
	}}
	return p.arenas.Types.NewPath(span, segments)
}

func (p *Parser) finishTypePath(firstID source.StringID, startSpan source.Span) (ast.TypeID, bool) {
	segments := []ast.TypePathSegment{{
		Name:     firstID,
		Generics: nil,
	}}

	if p.at(token.Lt) {
		args, ok := p.parseTypeArgs()
		if !ok {
			return ast.NoTypeID, false
		}
		segments[0].Generics = args
	}

	for p.at(token.Dot) {
		p.advance()
		if !p.at(token.Ident) {
			p.err(diag.SynExpectIdentifier, "expected identifier after '.'")
			return ast.NoTypeID, false
		}
		identID, ok := p.parseIdent()
		if !ok {
			return ast.NoTypeID, false
		}
		var generics []ast.TypeID
		if p.at(token.Lt) {
			generics, ok = p.parseTypeArgs()
			if !ok {
				return ast.NoTypeID, false
			}
		}
		segments = append(segments, ast.TypePathSegment{
			Name:     identID,
			Generics: generics,
		})
	}

	pathSpan := startSpan.Cover(p.lastSpan)
	baseType := p.arenas.Types.NewPath(pathSpan, segments)
	return p.parseTypeSuffix(baseType)
}

func (p *Parser) parseTypeArgs() ([]ast.TypeID, bool) {
	if !p.at(token.Lt) {
		return nil, true
	}
	p.advance()

	args := make([]ast.TypeID, 0, 2)
	for {
		arg, ok := p.parseTypePrefix()
		if !ok {
			p.resyncUntil(token.Gt, token.Comma, token.Semicolon, token.KwLet, token.KwFn, token.KwType, token.KwImport, token.EOF)
			if p.at(token.Gt) {
				p.advance()
			}
			return nil, false
		}
		args = append(args, arg)

		if p.at(token.Comma) {
			p.advance()
			if p.at(token.Gt) {
				p.advance()
				break
			}
			continue
		}
		break
	}

	if _, ok := p.expect(token.Gt, diag.SynUnclosedAngleBracket, "expected '>' after type arguments", nil); !ok {
		p.resyncUntil(token.Semicolon, token.Comma, token.KwLet, token.KwFn, token.KwType, token.KwImport, token.EOF)
		return args, false
	}
	return args, true
}

func (p *Parser) parseArraySizeLiteral(tok token.Token) (uint64, bool) {
	clean := strings.ReplaceAll(tok.Text, "_", "")
	if clean == "" {
		p.err(diag.SynUnexpectedToken, "array size literal is empty")
		return 0, false
	}
	if strings.HasPrefix(clean, "+") || strings.HasPrefix(clean, "-") {
		p.err(diag.SynUnexpectedToken, "array size literal must be a non-negative integer")
		return 0, false
	}

	body, suffix, err := splitNumericLiteral(clean)
	if err != nil {
		p.err(diag.SynUnexpectedToken, fmt.Sprintf("invalid array size literal %q: %v", tok.Text, err))
		return 0, false
	}
	if suffix != "" && !isValidIntegerSuffix(suffix) {
		p.err(diag.SynUnexpectedToken, fmt.Sprintf("invalid array size suffix %q", suffix))
		return 0, false
	}

	value, err := strconv.ParseUint(body, 0, 64)
	if err != nil {
		p.err(diag.SynUnexpectedToken, fmt.Sprintf("array size literal %q is out of range", tok.Text))
		return 0, false
	}
	return value, true
}
