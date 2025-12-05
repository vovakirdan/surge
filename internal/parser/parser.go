package parser

import (
	"context"
	"fmt"
	"slices"
	"time"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/lexer"
	"surge/internal/source"
	"surge/internal/token"
	"surge/internal/trace"
)

type Options struct {
	Trace         bool
	MaxErrors     uint
	CurrentErrors uint
	Reporter      diag.Reporter
	DirectiveMode DirectiveMode
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
	// suspendColonCast > 0 disables treating ':' as a cast operator. Used for constructs
	// like struct literals where ':' has its own meaning.
	suspendColonCast int
	// allowFatArrow tracks the nesting depth of constructs where fat arrows are valid (compare arms, parallel expressions).
	allowFatArrow int
	pragmaParsed  bool
	tracer        trace.Tracer // трассировщик для отладки зависаний
	exprDepth     int          // глубина рекурсии для выражений
}

type DirectiveMode uint8

const (
	DirectiveModeOff DirectiveMode = iota
	DirectiveModeCollect
	DirectiveModeGen
	DirectiveModeRun
)

// ParseFile — входная точка для разбора одного файла.
// Требует уже созданный lexer (на основе source.File).
func ParseFile(
	ctx context.Context,
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
		tracer:   trace.FromContext(ctx),
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

func (p *Parser) atOr(kinds ...token.Kind) bool {
	return slices.Contains(kinds, p.lx.Peek().Kind)
}

func (p *Parser) IsError() bool {
	return p.opts.CurrentErrors != 0
}

// parseItems — основной цикл верхнего уровня: пока не EOF — parseItem.
func (p *Parser) parseItems() {
	var span *trace.Span
	if p.tracer != nil && p.tracer.Level() >= trace.LevelDebug {
		span = trace.Begin(p.tracer, trace.ScopeNode, "parse_items", 0)
		defer span.End("")
	}

	startSpan := p.lx.Peek().Span
	p.consumeModulePragma()

	itemCount := 0
	for !p.at(token.EOF) {
		// Emit progress point every 100 items
		if p.tracer != nil && p.tracer.Level() >= trace.LevelDebug && itemCount%100 == 0 && itemCount > 0 {
			p.tracer.Emit(&trace.Event{
				Time:   time.Now(),
				Kind:   trace.KindPoint,
				Scope:  trace.ScopeNode,
				Name:   "parse_items_progress",
				Detail: fmt.Sprintf("item=%d", itemCount),
			})
		}

		// Следим за прогрессом: если за итерацию не съели ни одного токена, нужно его форсированно
		// прокрутить, иначе можно зациклиться на повреждённом вводе.
		before := p.lx.Peek()

		itemID, ok := p.parseItem()
		if !ok {
			p.resyncTop()
		} else {
			p.arenas.PushItem(p.file, itemID)
			itemCount++
		}

		if !p.at(token.EOF) {
			after := p.lx.Peek()
			if after.Kind == before.Kind && after.Span == before.Span {
				p.advance()
			}
		}
	}
	p.arenas.Files.Get(p.file).Span = startSpan.Cover(p.lx.Peek().Span) // зачем?
}

// parseItem выбирает по первому токену нужный распознаватель top-level конструкции.
// На этом шаге мы поддерживаем только `import`, `let`, `fn` и связанные конструкции.
func (p *Parser) parseItem() (ast.ItemID, bool) {
	if p.lx.Peek().Kind == token.KwPragma {
		p.parsePragma(false)
		return ast.NoItemID, false
	}

	directiveBlocks := p.collectDirectiveBlocks()

	attrs, attrSpan, ok := p.parseAttributes()
	if !ok {
		p.resyncTop()
		return ast.NoItemID, false
	}

	switch p.lx.Peek().Kind {
	case token.KwImport:
		if len(attrs) > 0 && attrSpan.End > attrSpan.Start {
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				attrSpan,
				"attributes are not allowed on import declarations",
				nil,
			)
		}
		itemID, parsed := p.parseImportItem()
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwConst:
		itemID, parsed := p.parseConstItemWithVisibility(attrs, attrSpan, ast.VisPrivate, source.Span{}, false)
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwLet:
		itemID, parsed := p.parseLetItemWithVisibility(attrs, attrSpan, ast.VisPrivate, source.Span{}, false)
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwFn:
		itemID, parsed := p.parseFnItem(attrs, attrSpan, fnModifiers{})
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwType:
		itemID, parsed := p.parseTypeItem(attrs, attrSpan, ast.VisPrivate, source.Span{}, false)
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwContract:
		itemID, parsed := p.parseContractItem(attrs, attrSpan, ast.VisPrivate, source.Span{}, false)
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwTag:
		itemID, parsed := p.parseTagItem(attrs, attrSpan, ast.VisPrivate, source.Span{}, false)
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwExtern:
		itemID, parsed := p.parseExternItem(attrs, attrSpan)
		if parsed {
			p.attachDirectiveBlocks(itemID, directiveBlocks)
		}
		return itemID, parsed
	case token.KwPub, token.KwAsync, token.Ident:
		mods := p.parseFnModifiers()
		if p.at(token.KwFn) {
			itemID, parsed := p.parseFnItem(attrs, attrSpan, mods)
			if parsed {
				p.attachDirectiveBlocks(itemID, directiveBlocks)
			}
			return itemID, parsed
		}
		if p.at(token.KwLet) {
			visibility := ast.VisPrivate
			if mods.flags&ast.FnModifierPublic != 0 {
				visibility = ast.VisPublic
			}
			invalid := mods.flags &^ ast.FnModifierPublic
			if invalid != 0 {
				span := mods.span
				if !mods.hasSpan {
					span = p.lx.Peek().Span
				}
				p.emitDiagnostic(
					diag.SynUnexpectedModifier,
					diag.SevError,
					span,
					"unexpected modifiers before 'let'",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnexpectedModifier, span)
						suggestion := fix.DeleteSpan(
							"remove the invalid modifiers",
							span.ExtendRight(p.lx.Peek().Span),
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(span, "only 'pub' modifier is allowed before 'let'")
					},
				)
			}
			itemID, parsed := p.parseLetItemWithVisibility(attrs, attrSpan, visibility, mods.span, mods.hasSpan)
			if parsed {
				p.attachDirectiveBlocks(itemID, directiveBlocks)
			}
			return itemID, parsed
		}
		if p.at(token.KwConst) {
			visibility := ast.VisPrivate
			if mods.flags&ast.FnModifierPublic != 0 {
				visibility = ast.VisPublic
			}
			invalid := mods.flags &^ ast.FnModifierPublic
			if invalid != 0 {
				span := mods.span
				if !mods.hasSpan {
					span = p.lx.Peek().Span
				}
				p.emitDiagnostic(
					diag.SynUnexpectedModifier,
					diag.SevError,
					span,
					"unexpected modifiers before 'const'",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnexpectedModifier, span)
						suggestion := fix.DeleteSpan(
							"remove the invalid modifiers",
							span.ExtendRight(p.lx.Peek().Span),
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(span, "only 'pub' modifier is allowed before 'const'")
					},
				)
			}
			itemID, parsed := p.parseConstItemWithVisibility(attrs, attrSpan, visibility, mods.span, mods.hasSpan)
			if parsed {
				p.attachDirectiveBlocks(itemID, directiveBlocks)
			}
			return itemID, parsed
		}
		if p.at(token.KwType) {
			visibility := ast.VisPrivate
			if mods.flags&ast.FnModifierPublic != 0 {
				visibility = ast.VisPublic
			}
			invalid := mods.flags &^ ast.FnModifierPublic
			if invalid != 0 {
				span := mods.span
				if !mods.hasSpan {
					span = p.lx.Peek().Span
				}
				p.emitDiagnostic(
					diag.SynUnexpectedModifier,
					diag.SevError,
					span,
					"unexpected modifiers before 'type'",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnexpectedModifier, span)
						suggestion := fix.DeleteSpan(
							"remove the invalid modifiers",
							span.ExtendRight(p.lx.Peek().Span),
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(span, "only 'pub' modifier is allowed before 'type'")
					},
				)
			}
			itemID, parsed := p.parseTypeItem(attrs, attrSpan, visibility, mods.span, mods.hasSpan)
			if parsed {
				p.attachDirectiveBlocks(itemID, directiveBlocks)
			}
			return itemID, parsed
		}
		if p.at(token.KwContract) {
			visibility := ast.VisPrivate
			if mods.flags&ast.FnModifierPublic != 0 {
				visibility = ast.VisPublic
			}
			invalid := mods.flags &^ ast.FnModifierPublic
			if invalid != 0 {
				span := mods.span
				if !mods.hasSpan {
					span = p.lx.Peek().Span
				}
				p.emitDiagnostic(
					diag.SynUnexpectedModifier,
					diag.SevError,
					span,
					"unexpected modifiers before 'contract'",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnexpectedModifier, span)
						suggestion := fix.DeleteSpan(
							"remove the invalid modifiers",
							span.ExtendRight(p.lx.Peek().Span),
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(span, "only 'pub' modifier is allowed before 'contract'")
					},
				)
			}
			itemID, parsed := p.parseContractItem(attrs, attrSpan, visibility, mods.span, mods.hasSpan)
			if parsed {
				p.attachDirectiveBlocks(itemID, directiveBlocks)
			}
			return itemID, parsed
		}
		if p.at(token.KwTag) {
			visibility := ast.VisPrivate
			if mods.flags&ast.FnModifierPublic != 0 {
				visibility = ast.VisPublic
			}
			invalid := mods.flags &^ ast.FnModifierPublic
			if invalid != 0 {
				span := mods.span
				if !mods.hasSpan {
					span = p.lx.Peek().Span
				}
				p.emitDiagnostic(
					diag.SynUnexpectedModifier,
					diag.SevError,
					span,
					"unexpected modifiers before 'tag'",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnexpectedModifier, span)
						suggestion := fix.DeleteSpan(
							"remove the invalid modifiers",
							span.ExtendRight(p.lx.Peek().Span),
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(span, "only 'pub' modifier is allowed before 'tag'")
					},
				)
			}
			itemID, parsed := p.parseTagItem(attrs, attrSpan, visibility, mods.span, mods.hasSpan)
			if parsed {
				p.attachDirectiveBlocks(itemID, directiveBlocks)
			}
			return itemID, parsed
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
		if len(attrs) > 0 && attrSpan.End > attrSpan.Start {
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				attrSpan,
				"attributes must precede a function, let, or const declaration",
				nil,
			)
		}
		return ast.NoItemID, false
	default:
		if len(attrs) > 0 && attrSpan.End > attrSpan.Start {
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				attrSpan,
				"attributes are not allowed in this position",
				nil,
			)
		}
		p.report(diag.SynUnexpectedTopLevel, diag.SevError, p.lx.Peek().Span, "unexpected top-level construct")
		return 0, false
	}
}

// resyncTop — восстановление после ошибки на верхнем уровне:
// прокручиваем до ';' ИЛИ до стартового токена следующего item ИЛИ EOF.
func (p *Parser) resyncTop() { // todo: использовать resyncUntill - надо явно знать до какого токена прокручивать
	var span *trace.Span
	if p.tracer != nil && p.tracer.Level() >= trace.LevelDebug {
		span = trace.Begin(p.tracer, trace.ScopeNode, "resync_top", 0)
	}

	// Список всех стартеров + semicolon
	stopTokens := []token.Kind{
		token.Semicolon, token.KwImport, token.KwLet, token.KwConst,
		token.KwFn, token.KwPub, token.KwAsync,
		token.KwExtern,
		token.KwType,
		token.KwTag,
	}
	// TODO: добавить другие стартеры когда они будут реализованы: token.KwFn, token.KwType, etc.

	// Чтобы избежать зависания, запоминаем текущий токен и проверяем, сделал ли resync прогресс.
	// В противном случае мы просто стоим на том же токене (часто это проблемный starter),
	// и на следующей итерации цикла парсер снова попробует распознать его, попадая в бесконечный цикл.
	// После resync мы принудительно съедаем токен, если остались на месте.
	prev := p.lx.Peek()

	p.resyncUntil(stopTokens...)

	tokensSkipped := 0
	// Если resync не продвинулся (остались на том же токене) и это не EOF, съедаем токен,
	// чтобы гарантировать прогресс и избежать бесконечного цикла на повреждённом вводе.
	if !p.at(token.EOF) && p.lx.Peek().Span == prev.Span && p.lx.Peek().Kind == prev.Kind {
		p.advance()
		tokensSkipped++
	}

	// Если нашли semicolon, съедаем его
	if p.at(token.Semicolon) {
		p.advance()
		tokensSkipped++
	}

	if span != nil {
		span.End(fmt.Sprintf("tokens_skipped=%d", tokensSkipped))
	}
}

// isTopLevelStarter — принадлежит ли токен стартерам item.
// isTopLevelStarter reports whether k is a token kind that begins a top-level declaration (import, let, fn, or fn-modifier).
func isTopLevelStarter(k token.Kind) bool {
	switch k {
	case token.KwImport, token.KwLet, token.KwFn,
		token.KwPub, token.KwAsync, token.KwExtern, token.KwType, token.KwContract, token.KwTag, token.KwConst:
		return true
	default:
		return false
	}
}

// parseIdent — утилита: ожидает Ident и интернирует его, возвращает source.StringID.
// На ошибке — репорт SynExpectIdentifier.
func (p *Parser) parseIdent() (source.StringID, bool) {
	if p.atOr(token.Ident, token.Underscore) {
		tok := p.advance()
		id := p.arenas.StringsInterner.Intern(tok.Text)
		return id, true
	}
	p.err(diag.SynExpectIdentifier, "expected identifier, got \""+p.lx.Peek().Text+"\"")
	return source.NoStringID, false
}
