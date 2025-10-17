package parser

import (
	"surge/internal/ast"
	"surge/internal/token"
	"surge/internal/diag"
	"surge/internal/source"
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
//		let [mut] name: Type = Expr;
//		let [mut] name: Type;
//		let [mut] name = Expr;
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
//		own Type, &Type, &mut Type, *Type (префиксы)
//		Type[] (постфиксы)
//		Type[Expr] (постфиксы)
//		простые типы
func (p *Parser) parseTypeExpr() (ast.TypeID, bool) {
	// Если нет двоеточия, тип не указан
	if !p.at(token.Colon) {
		return ast.NoTypeID, true
	}
	p.advance()

	return p.parseTypePrefix()
}

// parseTypePrefix обрабатывает префиксы: own, &, &mut, *
func (p *Parser) parseTypePrefix() (ast.TypeID, bool) {
	startSpan := p.lx.Peek().Span

	// Проверяем наличие модификаторов владения/ссылки
	var op ast.TypeUnaryOp
	var hasPrefix bool

	switch p.lx.Peek().Kind {
	case token.KwOwn:
		op = ast.TypeUnaryOwn
		hasPrefix = true
		p.advance()
	case token.Amp:
		op = ast.TypeUnaryRef
		hasPrefix = true
		p.advance()
		// Проверяем &mut
		if p.at(token.KwMut) {
			op = ast.TypeUnaryRefMut
			p.advance()
		}
	case token.Star:
		op = ast.TypeUnaryPointer
		hasPrefix = true
		p.advance()
	}

	// Парсим базовый тип
	baseType, ok := p.parseTypePrimary()
	if !ok {
		return ast.NoTypeID, false
	}

	// Если есть префикс, оборачиваем в унарный тип
	if hasPrefix {
		endSpan := p.lastSpan
		return p.arenas.Types.NewUnary(startSpan.Cover(endSpan), op, baseType), true
	}

	return baseType, true
}

// parseTypePrimary обрабатывает базовые формы типов:
//		идентификатор/путь
//		( tuple )
//		fn ( сигнатура ) -> ...
func (p *Parser) parseTypePrimary() (ast.TypeID, bool) {
	startSpan := p.lx.Peek().Span

	switch p.lx.Peek().Kind {
	case token.Ident:
		// Простой путь к типу (пока без generic аргументов)
		identText, ok := p.parseIdent()
		if !ok {
			return ast.NoTypeID, false
		}

		// Интернируем строку для получения StringID
		identID := p.arenas.StringsInterner.Intern(identText)

		// Создаем простой путь из одного сегмента
		segments := []ast.TypePathSegment{{
			Name:     identID,
			Generics: nil, // пока без generic аргументов
		}}

		baseType := p.arenas.Types.NewPath(startSpan.Cover(p.lastSpan), segments)
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
		startSpan := p.lx.Peek().Span
		p.advance()

		if p.at(token.RBracket) {
			// Динамический массив []Type
			p.advance()
			currentType = p.arenas.Types.NewArray(
				startSpan.Cover(p.lastSpan),
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
			currentType = p.arenas.Types.NewArray(
				startSpan.Cover(p.lastSpan),
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
