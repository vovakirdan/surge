package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
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
//	import ./module ; // не ошибка, значит "импорт из текущей директории". Info что так не обязательно, но в случае если имеем библиотеку и файл с одинаковым названием это будет явное указание взять файл
//	import ../module ; // импорт с верхнего уровня
//	import ../../module ; // импорт с верхнего верхнего уровня
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
		moduleAlias   source.StringID
		one           ast.ImportOne
		hasOne        bool
		pairs         []ast.ImportPair
		needSemicolon = true // флаг для определения нужности `;` в конце
		groupOpenSpan source.Span
		trailingComma source.Span
	)
	moduleAlias = source.NoStringID

	// Смотрим, что идёт после пути модуля
	switch p.lx.Peek().Kind {
	case token.ColonColon:
		colonColonTok := p.advance() // съедаем '::'

		// После '::' может быть либо идентификатор, либо группа {Ident, ...}
		if p.at(token.Ident) {
			// import module::Ident [as Alias];
			nameID, ok := p.parseIdent()
			if !ok {
				p.resyncStatement()
				return ast.NoItemID, false
			}

			// Проверяем, есть ли алиас
			var aliasID source.StringID
			if p.at(token.KwAs) {
				p.advance() // съедаем 'as'

				// Проверяем, что после 'as' идёт идентификатор
				if !p.at(token.Ident) {
					// todo: убирать 'as'
					p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
					p.resyncStatement()
					return ast.NoItemID, false
				}

				aliasID, ok = p.parseIdent()
				if !ok {
					p.resyncStatement()
					return ast.NoItemID, false
				}
			}

			one = ast.ImportOne{Name: nameID, Alias: aliasID}
			hasOne = true

		} else if p.at(token.LBrace) {
			// import module::{Ident [as Alias], ...};
			openTok := p.advance() // съедаем '{'
			groupOpenSpan = openTok.Span
			pairs = make([]ast.ImportPair, 0, 2)
			broken := false // флаг для обработки ошибок в группе

			for !p.at(token.RBrace) && !p.at(token.EOF) {
				nameID, ok := p.parseIdent()
				if !ok {
					// Ошибка уже зарепортирована в parseIdent
					broken = true
					p.resyncImportGroup()
					break
				}

				var aliasID source.StringID
				if p.at(token.KwAs) {
					p.advance() // съедаем 'as'

					// Проверяем, что после 'as' идёт идентификатор
					if !p.at(token.Ident) {
						p.err(diag.SynExpectIdentAfterAs, "expected identifier after 'as', got '"+p.lx.Peek().Text+"'")
						broken = true
						p.resyncImportGroup()
						break
					}

					aliasID, ok = p.parseIdent()
					if !ok {
						broken = true
						p.resyncImportGroup()
						break
					}
				}

				pairs = append(pairs, ast.ImportPair{Name: nameID, Alias: aliasID})

				// Если есть запятая, съедаем и продолжаем
				if p.at(token.Comma) {
					commaTok := p.advance()
					trailingComma = commaTok.Span
					continue
				}
				if p.at_or(token.RBrace, token.EOF, token.Semicolon) || isTopLevelStarter(p.lx.Peek().Kind) {
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
				// p.warn(diag.SynEmptyImportGroup, "empty import group")
				// тут мы встретили ::{} и, возможно, ;
				// удалим только ::{}
				// а точку с запятой проверяет другой фикс
				// если мы удалим только {}, то нарвемся на другую ошибку - unexpected item after ::
				groupCloseSpan := source.Span{
					File:  groupOpenSpan.File,
					Start: groupOpenSpan.Start + 1,
					End:   groupOpenSpan.End + 1,
				}
				combinedSpan := colonColonTok.Span.Cover(groupCloseSpan)
				p.emitDiagnostic(
					diag.SynEmptyImportGroup,
					diag.SevWarning,
					combinedSpan,
					"empty import group",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynEmptyImportGroup, combinedSpan)
						suggestion := fix.DeleteSpan(
							"remove '::{}' to simplify the import statement",
							combinedSpan,
							"::{}",
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
							fix.WithID(fixID),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(combinedSpan, "remove double colons and braces to simplify the import statement")
					},
				)
			}

			missingClose := false
			var closeTok token.Token
			if p.at(token.RBrace) {
				closeTok = p.advance() // съедаем '}'
			} else {
				anchor := p.lastSpan
				if anchor.End == 0 {
					anchor = colonColonTok.Span
				}
				closeBraceSpan := anchor.ZeroideToEnd()
				p.emitDiagnostic(
					diag.SynUnclosedBrace,
					diag.SevError,
					closeBraceSpan,
					"expected '}' to close import group",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnclosedBrace, closeBraceSpan)
						suggestion := fix.InsertText(
							"add missing '}' to close import group",
							closeBraceSpan,
							"}",
							"",
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
							fix.WithID(fixID),
						)
						b.WithFixSuggestion(suggestion)
					},
				)
				p.lastSpan = closeBraceSpan
				closeTok = token.Token{Kind: token.RBrace, Span: closeBraceSpan}
				missingClose = true
			}

			// если здесь только один Ident, то кидаем info что можно без {}
			// предлагаем так же fix удалить {}
			if len(pairs) == 1 && !missingClose {
				braceSpan := groupOpenSpan
				if braceSpan.File == closeTok.Span.File {
					braceSpan = braceSpan.Cover(closeTok.Span)
				}
				msg := "import group with only one item can be written without braces"
				p.emitDiagnostic(diag.SynInfoImportGroup, diag.SevInfo, braceSpan, msg, func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					removeSpans := []source.Span{groupOpenSpan, closeTok.Span}
					if trailingComma.End > trailingComma.Start {
						removeSpans = append(removeSpans, trailingComma)
					}
					fixID := fix.MakeFixID(diag.SynInfoImportGroup, groupOpenSpan)
					suggestion := fix.DeleteSpans(
						"remove braces around single import",
						removeSpans,
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						fix.WithID(fixID),
					)
					b.WithNote(braceSpan, "remove braces to simplify the import statement").
						WithFixSuggestion(suggestion)
				})
			}
		} else {
			// Ни идентификатор, ни '{'
			// да, p.err удобно но тут мы можем предложить фиксы
			dblSpan := colonColonTok.Span
			p.emitDiagnostic(
				diag.SynExpectItemAfterDbl,
				diag.SevError,
				dblSpan,
				"expected identifier or '{' after '::'",
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fix.MakeFixID(diag.SynExpectItemAfterDbl, dblSpan)
					suggestion := fix.DeleteSpan(
						"remove unexpected '::'",
						dblSpan,
						"::",
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						fix.WithID(fixID),
					)
					b.WithFixSuggestion(suggestion)
				},
			)
			// НЕ делаем resyncStatement() здесь, продолжаем до проверки ';'
			// Устанавливаем флаг, что не нужно ожидать ';' т.к. была ошибка
			needSemicolon = true
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

		_, ok := p.parseIdent()
		if !ok {
			p.resyncStatement()
			return ast.NoItemID, false
		}

	case token.Semicolon:
		// import module;
		// Ничего не делаем, всё уже готово

	default:
		// Неожиданный токен после пути модуля
		peek := p.lx.Peek()
		if peek.Kind != token.EOF {
			if !(needSemicolon && isTopLevelStarter(peek.Kind)) {
				p.err(diag.SynUnexpectedToken, "expected '::' or 'as' or ';' after module path, got '"+peek.Text+"'")
				needSemicolon = false
				p.resyncTop()
				return ast.NoItemID, false
			}
		}
		// Если EOF, то это просто недостающая `;` - продолжаем обычную обработку
	}

	// Проверяем, нужна ли точка с запятой
	if !needSemicolon {
		return ast.NoItemID, false
	}

	// Ожидаем точку с запятой в конце
	// Для вставки ';' используем позицию после последнего токена модуля
	insertSpan := source.Span{File: p.lastSpan.File, Start: p.lastSpan.End, End: p.lastSpan.End}
	semi, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected semicolon after import item", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		insertPos := source.Span{File: insertSpan.File, Start: insertSpan.Start, End: insertSpan.Start}
		fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertPos)
		suggestion := fix.InsertText(
			"insert ';' after import",
			insertPos,
			";",
			"",
			fix.Preferred(),
			fix.WithID(fixID),
			fix.WithKind(diag.FixKindRefactor),
			fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
		)
		b.WithFixSuggestion(suggestion)
		b.WithNote(insertPos, "insert ';' to terminate the import item")
	})
	if !ok {
		// expect уже содержит диагностику с правильным span, просто возвращаем false
		return ast.NoItemID, false
	}

	// Финальный span от начала import до точки с запятой
	span := importTok.Span.Cover(semi.Span)
	id := p.arenas.NewImport(span, moduleSegs, moduleAlias, one, hasOne, pairs)
	return id, true
}

// parseImportModule — собирает последовательность идентификаторов через '/'.
// Возвращает список сегментов, span последнего сегмента и успех.
func (p *Parser) parseImportModule() ([]source.StringID, source.Span, bool) {
	// Ожидаем как минимум один идентификатор (первый сегмент модуля)
	if !p.at_or(token.Ident, token.Dot, token.DotDot) {
		p.err(diag.SynExpectModuleSeg, "expected module segment, got '"+p.lx.Peek().Text+"'")
		return nil, source.Span{}, false
	}

	firstTok := p.advance()
	segments := []source.StringID{p.arenas.StringsInterner.Intern(firstTok.Text)}
	lastSpan := firstTok.Span

	// Затем цикл: ('/' Ident)*
	for p.at(token.Slash) {
		p.advance() // съедаем '/'

		// После '/' обязан быть идентификатор
		if !p.at_or(token.Ident, token.Dot, token.DotDot) {
			p.err(diag.SynExpectModuleSeg, "expected module segment after '/'")
			// Не вызываем resync здесь, так как parseImportModule вызывается из parseImportItem
			// который сам обработает ошибку
			return nil, lastSpan, false
		}

		segTok := p.advance()
		segments = append(segments, p.arenas.StringsInterner.Intern(segTok.Text))
		lastSpan = segTok.Span
	}

	return segments, lastSpan, true
}
