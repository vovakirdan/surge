package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
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
	importTok := p.advance() // съедаем KwImport; если мы здесь, то это точно KwImport

	// Парсим путь модуля (module/subpath/...)
	moduleSegs, moduleEndSpan, ok := p.parseImportModule()
	if !ok {
		// Если не смогли распарсить модуль, пытаемся восстановиться до конца statement
		p.resyncStatement()
		return ast.NoItemID, false
	}
	// Обновляем lastSpan на основе последнего сегмента модуля
	if moduleEndSpan.End > 0 {
		p.lastSpan = moduleEndSpan
	}

	// Переменные для различных форм импорта, объявленные вне switch
	var (
		moduleAlias   string
		one           *ast.ImportOne
		pairs         []ast.ImportPair
		needSemicolon = true // флаг для определения нужности `;` в конце
	)

	// Смотрим, что идёт после пути модуля
	switch p.lx.Peek().Kind {
	case token.ColonColon:
		p.advance() // съедаем '::'

		// После '::' может быть либо идентификатор, либо группа {Ident, ...}
		if p.at(token.Ident) {
			// import module::Ident [as Alias];
			name, ok := p.parseIdent()
			if !ok {
				p.resyncStatement()
				return ast.NoItemID, false
			}

			// Проверяем, есть ли алиас
			alias := ""
			if p.at(token.KwAs) {
				p.advance() // съедаем 'as'

				// Проверяем, что после 'as' идёт идентификатор
				if !p.at(token.Ident) {
					p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
					p.resyncStatement()
					return ast.NoItemID, false
				}

				alias, ok = p.parseIdent()
				if !ok {
					p.resyncStatement()
					return ast.NoItemID, false
				}
			}

			one = &ast.ImportOne{Name: name, Alias: alias}

		} else if p.at(token.LBrace) {
			// import module::{Ident [as Alias], ...};
			p.advance() // съедаем '{'
			pairs = make([]ast.ImportPair, 0, 2)
			broken := false // флаг для обработки ошибок в группе

			for !p.at(token.RBrace) && !p.at(token.EOF) {
				name, ok := p.parseIdent()
				if !ok {
					// Ошибка уже зарепортирована в parseIdent
					broken = true
					p.resyncImportGroup()
					break
				}

				alias := ""
				if p.at(token.KwAs) {
					p.advance() // съедаем 'as'

					// Проверяем, что после 'as' идёт идентификатор
					if !p.at(token.Ident) {
						p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
						broken = true
						p.resyncImportGroup()
						break
					}

					alias, ok = p.parseIdent()
					if !ok {
						broken = true
						p.resyncImportGroup()
						break
					}
				}

				pairs = append(pairs, ast.ImportPair{Name: name, Alias: alias})

				// Если есть запятая, съедаем и продолжаем
				if p.at(token.Comma) {
					p.advance()
					continue
				}
				if p.at_or(token.RBrace, token.EOF, token.Semicolon) {
					// Если нет запятой, должна быть закрывающая скобка или EOF
					// Если EOF, то это ошибка unclosed brace (обработаем позже)
					// Если видим `;`, это означает, что группа не закрыта
					break
				}
				// Иначе это неожиданный токен
				p.err(diag.SynUnexpectedToken, "expected ',' or '}' in import group, got '"+p.lx.Peek().Text+"'")
				broken = true
				p.resyncImportGroup()
				break
			}

			// Если группа была повреждена, сразу возвращаемся
			if broken {
				return ast.NoItemID, false
			}

			// Проверяем на пустую группу
			if len(pairs) == 0 {
				p.warn(diag.SynEmptyImportGroup, "empty import group")
			}

			// Проверяем, что у нас есть закрывающая скобка
			if !p.at(token.RBrace) {
				p.err(diag.SynUnclosedBrace, "expected '}' to close import group")
				return ast.NoItemID, false
			}

			p.advance() // съедаем '}'
			// если здесь только один Ident, то кидаем info что можно без {}
			if len(pairs) == 1 {
				p.info(diag.SynInfoImportGroup, "import group with only one item can be written without braces")
			}
		} else {
			// Ни идентификатор, ни '{'
			p.err(diag.SynExpectItemAfterDbl, "expected identifier or '{' after '::'")
			p.resyncStatement()
			return ast.NoItemID, false
		}

	case token.KwAs:
		// import module as Alias;
		p.advance() // съедаем 'as'

		// Проверяем, что после 'as' идёт идентификатор
		if !p.at(token.Ident) {
			p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
			p.resyncStatement()
			return ast.NoItemID, false
		}

		alias, ok := p.parseIdent()
		if !ok {
			p.resyncStatement()
			return ast.NoItemID, false
		}
		moduleAlias = alias

	case token.Semicolon:
		// import module;
		// Ничего не делаем, всё уже готово

	default:
		// Неожиданный токен после пути модуля
		peek := p.lx.Peek()
		if peek.Kind != token.EOF {
			p.err(diag.SynUnexpectedToken, "expected '::' or 'as' or ';' after module path, got '"+peek.Text+"'")
			needSemicolon = false
			p.resyncTop()
			return ast.NoItemID, false
		}
		// Если EOF, то это просто недостающая `;` - продолжаем обычную обработку
	}

	// Проверяем, нужна ли точка с запятой
	if !needSemicolon {
		return ast.NoItemID, false
	}

	// Ожидаем точку с запятой в конце
	semi, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected semicolon after import item")
	if !ok {
		// expect уже содержит диагностику с правильным span, просто возвращаем false
		return ast.NoItemID, false
	}

	// Финальный span от начала import до точки с запятой
	span := importTok.Span.Cover(semi.Span)
	id := p.arenas.NewImport(span, moduleSegs, moduleAlias, one, pairs)
	return id, true
}

// parseImportModule — собирает последовательность идентификаторов через '/'.
// Возвращает список сегментов, span последнего сегмента и успех.
func (p *Parser) parseImportModule() ([]string, source.Span, bool) {
	// Ожидаем как минимум один идентификатор (первый сегмент модуля)
	if !p.at(token.Ident) {
		p.err(diag.SynExpectModuleSeg, "expected module segment, got '"+p.lx.Peek().Text+"'")
		return nil, source.Span{}, false
	}

	firstTok := p.advance()
	segments := []string{firstTok.Text}
	lastSpan := firstTok.Span

	// Затем цикл: ('/' Ident)*
	for p.at(token.Slash) {
		p.advance() // съедаем '/'

		// После '/' обязан быть идентификатор
		if !p.at(token.Ident) {
			p.err(diag.SynExpectModuleSeg, "expected module segment after '/'")
			// Не вызываем resync здесь, так как parseImportModule вызывается из parseImportItem
			// который сам обработает ошибку
			return nil, lastSpan, false
		}

		segTok := p.advance()
		segments = append(segments, segTok.Text)
		lastSpan = segTok.Span
	}

	return segments, lastSpan, true
}

// parseIdent — утилита: ожидает Ident и возвращает его текст.
// На ошибке — репорт SynExpectIdentifier.
func (p *Parser) parseIdent() (string, bool) {
	if p.at(token.Ident) {
		tok := p.advance()
		return tok.Text, true
	}
	p.err(diag.SynExpectIdentifier, "expected identifier, got \""+p.lx.Peek().Text+"\"")
	return "", false
}
