package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

// LetBinding представляет парсированный биндинг let
type LetBinding struct {
	Name  source.StringID
	Type  ast.TypeID
	Value ast.ExprID
	IsMut bool
	Span  source.Span
}

// parseLetBinding парсит биндинг let: [mut] name : Type? = Expr?
// Этот метод переиспользуется в let items, параметрах функций, полях структур
func (p *Parser) parseLetBinding() (LetBinding, bool) {
	startSpan := p.lx.Peek().Span

	// Парсим модификатор mut (если есть)
	var isMut bool
	if p.at(token.KwMut) {
		isMut = true
		p.advance()
	}

	// Парсим имя переменной
	nameText, ok := p.parseIdent()
	if !ok {
		return LetBinding{}, false
	}
	nameID := p.arenas.StringsInterner.Intern(nameText)

	// Парсим тип (если есть двоеточие)
	typeID, ok := p.parseTypeExpr()
	if !ok {
		return LetBinding{}, false
	}

	// Парсим инициализацию (если есть =)
	var valueID ast.ExprID = ast.NoExprID
	if p.at(token.Assign) {
		p.advance() // съедаем '='
		// TODO: реализовать parseExpr()
		p.err(diag.SynUnexpectedToken, "expression parsing not yet implemented")
		return LetBinding{}, false
	}

	// Проверяем, что хотя бы тип или значение указано
	if typeID == ast.NoTypeID && valueID == ast.NoExprID {
		p.err(diag.SynExpectType, "let binding must have either type annotation or initializer")
		return LetBinding{}, false
	}

	binding := LetBinding{
		Name:  nameID,
		Type:  typeID,
		Value: valueID,
		IsMut: isMut,
		Span:  startSpan.Cover(p.lastSpan),
	}

	return binding, true
}

// parseLetItem распознаёт let items верхнего уровня:
//
//	let [mut] name: Type = Expr;
//	let [mut] name: Type;
//	let [mut] name = Expr;
func (p *Parser) parseLetItem() (ast.ItemID, bool) {
	letTok := p.advance() // съедаем KwLet

	// Парсим биндинг
	binding, ok := p.parseLetBinding()
	if !ok {
		return ast.NoItemID, false
	}

	// Ожидаем точку с запятой
	semi, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected semicolon after let item", nil)
	if !ok {
		return ast.NoItemID, false
	}

	// Создаем LetItem в AST
	finalSpan := letTok.Span.Cover(semi.Span)
	itemID := p.arenas.Items.NewLet(
		binding.Name,
		binding.Type,
		binding.Value,
		binding.IsMut,
		finalSpan,
	)

	return itemID, true
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

// parseTypePrefix обрабатывает цепочки префиксов: own, &, &mut, *
// Поддерживает множественные префиксы типа **int, &&mut Payload, own &T
func (p *Parser) parseTypePrefix() (ast.TypeID, bool) {
	type prefixInfo struct {
		op   ast.TypeUnaryOp
		span source.Span
	}

	var prefixes []prefixInfo

prefixLoop:
	for {
		switch p.lx.Peek().Kind {
		case token.KwOwn:
			start := p.lx.Peek().Span
			p.advance()
			prefixes = append(prefixes, prefixInfo{
				op:   ast.TypeUnaryOwn,
				span: start.Cover(p.lastSpan),
			})
		case token.Amp:
			start := p.lx.Peek().Span
			p.advance()
			end := p.lastSpan
			if p.at(token.KwMut) {
				p.advance()
				end = p.lastSpan
				prefixes = append(prefixes, prefixInfo{
					op:   ast.TypeUnaryRefMut,
					span: start.Cover(end),
				})
			} else {
				prefixes = append(prefixes, prefixInfo{
					op:   ast.TypeUnaryRef,
					span: start.Cover(end),
				})
			}
		case token.AndAnd:
			start := p.lx.Peek().Span
			p.advance()
			end := p.lastSpan
			if p.at(token.KwMut) {
				// &&mut = & + &mut
				prefixes = append(prefixes,
					prefixInfo{op: ast.TypeUnaryRef, span: start.Cover(end)},
				)
				p.advance()
				end = p.lastSpan
				prefixes = append(prefixes,
					prefixInfo{op: ast.TypeUnaryRefMut, span: start.Cover(end)},
				)
			} else {
				// && = & + &
				prefixes = append(prefixes,
					prefixInfo{op: ast.TypeUnaryRef, span: start.Cover(end)},
					prefixInfo{op: ast.TypeUnaryRef, span: start.Cover(end)},
				)
			}
		case token.Star:
			start := p.lx.Peek().Span
			p.advance()
			prefixes = append(prefixes, prefixInfo{
				op:   ast.TypeUnaryPointer,
				span: start.Cover(p.lastSpan),
			})
		default:
			// Больше префиксов нет, выходим из цикла
			break prefixLoop
		}
	}

	// Парсим базовый тип
	baseType, ok := p.parseTypePrimary()
	if !ok {
		return ast.NoTypeID, false
	}

	// Применяем префиксы справа налево (последний префикс - ближе к базовому типу)
	currentType := baseType
	for i := len(prefixes) - 1; i >= 0; i-- {
		// Получаем span текущего типа для правильного объединения
		currentSpan := p.arenas.Types.Get(currentType).Span
		finalSpan := prefixes[i].span.Cover(currentSpan)
		currentType = p.arenas.Types.NewUnary(finalSpan, prefixes[i].op, currentType)
	}

	return currentType, true
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
		// Квалифицированный путь к типу: Ident ( "." Ident )*
		var segments []ast.TypePathSegment

		// Парсим первый идентификатор
		identText, ok := p.parseIdent()
		if !ok {
			return ast.NoTypeID, false
		}
		identID := p.arenas.StringsInterner.Intern(identText)
		segments = append(segments, ast.TypePathSegment{
			Name:     identID,
			Generics: nil, // пока без generic аргументов
		})

		// Парсим дополнительные сегменты через точку
		for p.at(token.Dot) {
			p.advance() // съедаем '.'

			// После точки должен быть идентификатор
			if !p.at(token.Ident) {
				p.err(diag.SynExpectIdentifier, "expected identifier after '.'")
				return ast.NoTypeID, false
			}

			identText, ok := p.parseIdent()
			if !ok {
				return ast.NoTypeID, false
			}
			identID := p.arenas.StringsInterner.Intern(identText)
			segments = append(segments, ast.TypePathSegment{
				Name:     identID,
				Generics: nil, // пока без generic аргументов
			})
		}

		baseType := p.arenas.Types.NewPath(startSpan.Cover(p.lastSpan), segments)
		return p.parseTypeSuffix(baseType)

	case token.NothingLit:
		// nothing тип
		nothingTok := p.advance()
		nothingID := p.arenas.StringsInterner.Intern("nothing")
		segments := []ast.TypePathSegment{{
			Name:     nothingID,
			Generics: nil,
		}}
		baseType := p.arenas.Types.NewPath(nothingTok.Span, segments)
		return p.parseTypeSuffix(baseType)

	case token.LParen:
		// Кортеж (пока заглушка)
		p.err(diag.SynUnexpectedToken, "tuple types not yet implemented")
		return ast.NoTypeID, false

	case token.KwFn:
		// Функциональный тип (пока заглушка)
		p.err(diag.SynUnexpectedToken, "function types not yet implemented")
		return ast.NoTypeID, false

	default:
		p.err(diag.SynExpectType, "expected type")
		return ast.NoTypeID, false
	}
}

// parseTypeSuffix обрабатывает постфиксы: [], [Expr]
func (p *Parser) parseTypeSuffix(baseType ast.TypeID) (ast.TypeID, bool) {
	currentType := baseType

	// Обрабатываем массивы в цикле для поддержки вложенных массивов
	for p.at(token.LBracket) {
		p.advance()

		if p.at(token.RBracket) {
			// Динамический массив []Type
			p.advance()
			bracketEndSpan := p.lastSpan

			// Получаем span текущего типа и объединяем с span скобок
			currentTypeSpan := p.arenas.Types.Get(currentType).Span
			finalSpan := currentTypeSpan.Cover(bracketEndSpan)

			currentType = p.arenas.Types.NewArray(
				finalSpan,
				currentType,
				ast.ArraySlice,
				ast.NoExprID,
			)
		} else if p.at(token.IntLit) {
			// Массив фиксированного размера [N]Type
			// TODO: парсить число правильно
			p.advance()
			if !p.at(token.RBracket) {
				p.err(diag.SynExpectRightBracket, "expected ']' after array size")
				return ast.NoTypeID, false
			}
			p.advance()
			bracketEndSpan := p.lastSpan

			// Получаем span текущего типа и объединяем с span скобок
			currentTypeSpan := p.arenas.Types.Get(currentType).Span
			finalSpan := currentTypeSpan.Cover(bracketEndSpan)

			currentType = p.arenas.Types.NewArray(
				finalSpan,
				currentType,
				ast.ArraySized,
				ast.NoExprID, // TODO: парсить выражение размера
			)
		} else {
			p.err(diag.SynExpectRightBracket, "expected ']' or array size")
			return ast.NoTypeID, false
		}
	}

	return currentType, true
}
