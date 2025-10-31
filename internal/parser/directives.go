package parser

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) collectDirectiveBlocks() []ast.DirectiveBlock {
	if p.opts.DirectiveMode == DirectiveModeOff {
		return nil
	}
	tok := p.lx.Peek()
	if len(tok.Leading) == 0 {
		return nil
	}

	var (
		blocks  []ast.DirectiveBlock
		current *directiveBuild
	)

	finish := func() {
		if current == nil {
			return
		}
		current.finish(p, &blocks)
		current = nil
	}

	for _, tr := range tok.Leading {
		if tr.Kind != token.TriviaDocLine {
			continue
		}
		content, contentSpan := docLineContent(tr)
		trimmed, trimmedSpan := trimDirectiveLine(content, contentSpan)

		if trimmed == "" {
			finish()
			continue
		}

		if strings.HasSuffix(trimmed, ":") {
			ns := strings.TrimSpace(trimmed[:len(trimmed)-1])
			if isValidDirectiveNamespace(ns) {
				finish()
				current = &directiveBuild{
					namespace: ns,
					spanStart: tr.Span,
					spanEnd:   tr.Span,
				}
				continue
			}
		}

		if current != nil && trimmed != "" {
			current.addLine(trimmed, trimmedSpan)
			continue
		}
	}

	finish()
	return blocks
}

func (p *Parser) attachDirectiveBlocks(owner ast.ItemID, blocks []ast.DirectiveBlock) {
	if len(blocks) == 0 {
		return
	}
	file := p.arenas.Files.Get(p.file)
	if file == nil {
		return
	}
	for _, block := range blocks {
		block.Owner = owner
		file.Directives = append(file.Directives, block)
	}
}

type directiveLineDraft struct {
	text string
	span source.Span
}

type directiveBuild struct {
	namespace string
	spanStart source.Span
	spanEnd   source.Span
	lines     []directiveLineDraft
}

func (b *directiveBuild) addLine(text string, span source.Span) {
	if text == "" {
		return
	}
	b.lines = append(b.lines, directiveLineDraft{
		text: text,
		span: span,
	})
	if b.spanEnd == (source.Span{}) {
		b.spanEnd = span
	} else {
		b.spanEnd = b.spanEnd.Cover(span)
	}
}

func (b *directiveBuild) finish(p *Parser, out *[]ast.DirectiveBlock) {
	if b == nil || len(b.lines) == 0 {
		return
	}
	firstLine := b.lines[0].text
	if !strings.HasPrefix(firstLine, b.namespace+".") && !strings.HasPrefix(firstLine, b.namespace+"::") {
		return
	}
	blockSpan := b.spanStart
	if b.spanEnd != (source.Span{}) {
		blockSpan = blockSpan.Cover(b.spanEnd)
	}
	lines := make([]ast.DirectiveLine, len(b.lines))
	for i, ln := range b.lines {
		lines[i] = ast.DirectiveLine{
			Text: p.arenas.StringsInterner.Intern(ln.text),
			Span: ln.span,
		}
	}
	block := ast.DirectiveBlock{
		Namespace: p.arenas.StringsInterner.Intern(b.namespace),
		Lines:     lines,
		Span:      blockSpan,
		Owner:     ast.NoItemID,
	}
	*out = append(*out, block)
}

func docLineContent(tr token.Trivia) (string, source.Span) {
	text := tr.Text
	offset := 0
	if strings.HasPrefix(text, "///") {
		offset = 3
	}
	for offset < len(text) {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if !unicode.IsSpace(r) {
			break
		}
		offset += size
	}
	span := tr.Span
	if delta, err := safecast.Conv[uint32](offset); err == nil {
		span.Start += delta
	}
	return text[offset:], span
}

func trimDirectiveLine(content string, span source.Span) (string, source.Span) {
	if content == "" {
		return "", span
	}
	start := 0
	for start < len(content) {
		r, size := utf8.DecodeRuneInString(content[start:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if !unicode.IsSpace(r) {
			break
		}
		start += size
	}
	end := len(content)
	for end > start {
		r, size := utf8.DecodeLastRuneInString(content[:end])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	trimmed := content[start:end]
	trimmedSpan := span
	if delta, err := safecast.Conv[uint32](start); err == nil {
		trimmedSpan.Start += delta
	}
	if delta, err := safecast.Conv[uint32](end); err == nil {
		trimmedSpan.End = span.Start + delta
	}
	return trimmed, trimmedSpan
}

func isValidDirectiveNamespace(ns string) bool {
	if ns == "" {
		return false
	}
	for i, r := range ns {
		if unicode.IsLetter(r) || r == '_' {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}
