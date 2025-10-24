package parser

import (
	"slices"
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"surge/internal/token"
)

type Options struct {
	Trace         bool
	MaxErrors     uint
	CurrentErrors uint
	Reporter      diag.Reporter
}

// Enough - проверить, достигли ли мы максимального количества ошибок
func (o *Options) Enough() bool {
	if o.MaxErrors == 0 {
		return false
	}
	return o.CurrentErrors >= o.MaxErrors
}

type Result struct {
	File ast.FileID
	Bag  *diag.Bag
}

// Parser — состояние парсера на один файл
type Parser struct {
	lx       *lexer.Lexer    // поток токенов (Peek/Next/Expect)
	arenas   *ast.Builder    // построитель аренных узлов
	file     ast.FileID      // текущий FileID (в AST)
	fs       *source.FileSet // нужен только для спанов/путей при надобности
	opts     Options
	lastSpan source.Span // span последнего съеденного токена для лучшей диагностики
}

// ParseFile — входная точка для разбора одного файла.
// Требует уже созданный lexer (на основе source.File).
func ParseFile(
	fs *source.FileSet,
	lx *lexer.Lexer,
	arenas *ast.Builder,
	opts Options,
) Result {
	p := Parser{
		lx:       lx,
		arenas:   arenas,
		file:     arenas.Files.New(lx.EmptySpan()), // todo: проверить; по идее в lexer уже есть source.File
		fs:       fs,
		opts:     opts,
		lastSpan: lx.EmptySpan(), // инициализируем с пустым span
	}

	p.parseItems()
	var bag *diag.Bag
	if br, ok := opts.Reporter.(*diag.BagReporter); ok {
		bag = br.Bag
	}
	return Result{
		File: p.file,
		Bag:  bag,
	}
}

func (p *Parser) at(k token.Kind) bool {
	return p.lx.Peek().Kind == k
}

func (p *Parser) at_or(kinds ...token.Kind) bool {
	return slices.Contains(kinds, p.lx.Peek().Kind)
}

func (p *Parser) IsError() bool {
	return p.opts.CurrentErrors != 0
}

// parseItems — основной цикл верхнего уровня: пока не EOF — parseItem.
func (p *Parser) parseItems() {
	startSpan := p.lx.Peek().Span
	for !p.at(token.EOF) {
		itemID, ok := p.parseItem()
		if !ok {
			p.resyncTop()
		} else {
			p.arenas.PushItem(p.file, itemID)
		}
	}
	p.arenas.Files.Get(p.file).Span = startSpan.Cover(p.lx.Peek().Span) // зачем?
}

// parseItem выбирает по первому токену нужный распознаватель top-level конструкции.
// На этом шаге мы поддерживаем только `import`, `let` и `fn`.
func (p *Parser) parseItem() (ast.ItemID, bool) {
	// switch по ключевым словам: если import → parseImportItem().
	// Иначе — диагностика SynUnexpectedTopLevel и false.
	switch p.lx.Peek().Kind {
	case token.KwImport:
		return p.parseImportItem()
	case token.KwLet:
		return p.parseLetItem()
	case token.KwFn:
		return p.parseFnItem(fnModifiers{})
	case token.KwPub, token.KwAsync, token.Ident:
		mods, ok := p.parseFnModifiers()
		if ok && p.at(token.KwFn) {
			return p.parseFnItem(mods)
		}
		if mods.flags != 0 {
			span := mods.span
			if !mods.hasSpan {
				span = p.lx.Peek().Span
			}
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				span,
				"expected 'fn' after function modifiers",
				nil,
			)
		}
		return ast.NoItemID, false
	default:
		p.report(diag.SynUnexpectedTopLevel, diag.SevError, p.lx.Peek().Span, "unexpected top-level construct")
		return 0, false
	}
}

// resyncTop — восстановление после ошибки на верхнем уровне:
// прокручиваем до ';' ИЛИ до стартового токена следующего item ИЛИ EOF.
func (p *Parser) resyncTop() { // todo: использовать resyncUntill - надо явно знать до какого токена прокручивать
	// Список всех стартеров + semicolon
	stopTokens := []token.Kind{token.Semicolon, token.KwImport, token.KwLet, token.KwFn, token.KwPub, token.KwAsync}
	// TODO: добавить другие стартеры когда они будут реализованы: token.KwFn, token.KwType, etc.

	p.resyncUntil(stopTokens...)

	// Если нашли semicolon, съедаем его
	if p.at(token.Semicolon) {
		p.advance()
	}
}

// isTopLevelStarter — принадлежит ли токен стартерам item.
// isTopLevelStarter reports whether k is a token kind that begins a top-level declaration (import, let, fn, or fn-modifier).
func isTopLevelStarter(k token.Kind) bool {
	switch k {
	case token.KwImport, token.KwLet, token.KwFn, token.KwPub, token.KwAsync:
		return true
	default:
		return false
	}
}

// parseIdent — утилита: ожидает Ident и интернирует его, возвращает source.StringID.
// На ошибке — репорт SynExpectIdentifier.
func (p *Parser) parseIdent() (source.StringID, bool) {
	if p.at_or(token.Ident, token.Underscore) {
		tok := p.advance()
		id := p.arenas.StringsInterner.Intern(tok.Text)
		return id, true
	}
	p.err(diag.SynExpectIdentifier, "expected identifier, got \""+p.lx.Peek().Text+"\"")
	return source.NoStringID, false
}
