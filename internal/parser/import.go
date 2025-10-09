package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/token"
)

// parseImportItem распознаёт формы:
//
//	import module;                                	// module/submodule
//	import module :: Ident ;                      	// конкретный элемент
//	import module :: Ident as Ident ;             	// элемент с алиасом
//	import module/subpath ;                       	// module/submodule с подпапками
//	import module/subpath :: Ident ;              	// конкретный элемент с подпапками
//	import module/subpath :: Ident as Ident ;     	// элемент с алиасом с подпапками
//	import module as Ident ;                      	// module с алиасом
//	import module::{Ident, Ident} ;               	// элементы с подпапками
//	import module::{Ident as Ident, Ident as Ident} ; // элементы с алиасами с подпапками
func (p *Parser) parseImportItem() (ast.ItemID, bool) {
	importTok := p.lx.Next() // съедаем KwImport; если мы здесь, то это точно KwImport

	// Парсим путь модуля (module/subpath/...)
	moduleSegs, ok := p.parseImportModule()
	if !ok {
		return ast.NoItemID, false
	}

	// Переменные для различных форм импорта, объявленные вне switch
	var (
		moduleAlias string
		one         *ast.ImportOne
		pairs       []ast.ImportPair
	)

	// Смотрим, что идёт после пути модуля
	switch p.lx.Peek().Kind {
	case token.ColonColon:
		p.lx.Next() // съедаем '::'

		// После '::' может быть либо идентификатор, либо группа {Ident, ...}
		if p.at(token.Ident) {
			// import module::Ident [as Alias];
			name, ok := p.parseIdent()
			if !ok {
				return ast.NoItemID, false
			}

			// Проверяем, есть ли алиас
			alias := ""
			if p.at(token.KwAs) {
				p.lx.Next() // съедаем 'as'

				// Проверяем, что после 'as' идёт идентификатор
				if !p.at(token.Ident) {
					p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
					return ast.NoItemID, false
				}

				alias, ok = p.parseIdent()
				if !ok {
					return ast.NoItemID, false
				}
			}

			one = &ast.ImportOne{Name: name, Alias: alias}

		} else if p.at(token.LBrace) {
			// import module::{Ident [as Alias], ...};
			p.lx.Next() // съедаем '{'
			pairs = make([]ast.ImportPair, 0, 2)

			for !p.at(token.RBrace) && !p.at(token.EOF) {
				name, ok := p.parseIdent()
				if !ok {
					// Ошибка уже зарепортирована в parseIdent
					break
				}

				alias := ""
				if p.at(token.KwAs) {
					p.lx.Next() // съедаем 'as'

					// Проверяем, что после 'as' идёт идентификатор
					if !p.at(token.Ident) {
						p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
						break
					}

					alias, ok = p.parseIdent()
					if !ok {
						break
					}
				}

				pairs = append(pairs, ast.ImportPair{Name: name, Alias: alias})

				// Если есть запятая, съедаем и продолжаем
				if p.at(token.Comma) {
					p.lx.Next()
					continue
				}
				// Иначе должна быть закрывающая скобка
				break
			}

			_, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close import group")
			if !ok {
				return ast.NoItemID, false
			}
			// если здесь только один Ident, то кидаем info что можно без {}
			if len(pairs) == 1 {
				p.warn(diag.SynInfoImportGroup, "import group with only one item can be written without braces")
			}
		} else {
			// Ни идентификатор, ни '{'
			p.err(diag.SynExpectItemAfterDbl, "expected identifier or '{' after '::'")
			return ast.NoItemID, false
		}

	case token.KwAs:
		// import module as Alias;
		p.lx.Next() // съедаем 'as'

		// Проверяем, что после 'as' идёт идентификатор
		if !p.at(token.Ident) {
			p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
			return ast.NoItemID, false
		}

		alias, ok := p.parseIdent()
		if !ok {
			return ast.NoItemID, false
		}
		moduleAlias = alias

	case token.Semicolon:
		// import module;
		// Ничего не делаем, всё уже готово

	default:
		// Неожиданный токен после пути модуля
		p.err(diag.SynUnexpectedToken, "expected '::' or 'as' or ';' after module path, got '"+p.lx.Peek().Text+"'")
		return ast.NoItemID, false
	}

	// Ожидаем точку с запятой в конце
	semi, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected semicolon after import item")
	if !ok {
		return ast.NoItemID, false
	}

	// Финальный span от начала import до точки с запятой
	span := importTok.Span.Cover(semi.Span)
	id := p.arenas.NewImport(span, moduleSegs, moduleAlias, one, pairs)
	return id, true
}

// parseImportModule — собирает последовательность идентификаторов через '/'.
// Возвращает список сегментов и успех.
func (p *Parser) parseImportModule() ([]string, bool) {
	// Ожидаем как минимум один идентификатор (первый сегмент модуля)
	if !p.at(token.Ident) {
		p.err(diag.SynExpectModuleSeg, "expected module segment, got '"+p.lx.Peek().Text+"'")
		return nil, false
	}

	firstSeg, ok := p.parseIdent()
	if !ok {
		return nil, false
	}
	segments := []string{firstSeg}

	// Затем цикл: ('/' Ident)*
	for p.at(token.Slash) {
		p.lx.Next() // съедаем '/'

		// После '/' обязан быть идентификатор
		if !p.at(token.Ident) {
			p.err(diag.SynExpectModuleSeg, "expected module segment after '/'")
			return nil, false
		}

		seg, ok := p.parseIdent()
		if !ok {
			return nil, false
		}
		segments = append(segments, seg)
	}

	return segments, true
}

// parseIdent — утилита: ожидает Ident и возвращает его текст.
// На ошибке — репорт SynExpectIdentifier.
func (p *Parser) parseIdent() (string, bool) {
	if p.at(token.Ident) {
		tok := p.lx.Next()
		return tok.Text, true
	}
	p.err(diag.SynExpectIdentifier, "expected identifier, got \""+p.lx.Peek().Text+"\"")
	return "", false
}
