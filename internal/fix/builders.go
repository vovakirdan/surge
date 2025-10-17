package fix

import (
	"strings"

	"surge/internal/diag"
	"surge/internal/source"
)

// Option mutates fix during construction.
type Option func(*diag.Fix)

// WithApplicability overrides applicability metadata.
func WithApplicability(app diag.FixApplicability) Option {
	return func(f *diag.Fix) {
		f.Applicability = app
	}
}

// WithKind overrides fix classification.
func WithKind(kind diag.FixKind) Option {
	return func(f *diag.Fix) {
		f.Kind = kind
	}
}

// Preferred marks fix as preferred suggestion.
func Preferred() Option {
	return func(f *diag.Fix) {
		f.IsPreferred = true
	}
}

// WithRequiresAll marks fix as requiring all fixes to be applied.
func WithRequiresAll() Option {
	return func(f *diag.Fix) {
		f.RequiresAll = true
	}
}

// WithID sets stable identifier for fix.
func WithID(id string) Option {
	return func(f *diag.Fix) {
		f.ID = id
	}
}

// WithThunk attaches lazy builder to fix.
func WithThunk(thunk diag.FixThunk) Option {
	return func(f *diag.Fix) {
		f.Thunk = thunk
	}
}

func applyOptions(f diag.Fix, opts []Option) diag.Fix {
	for _, opt := range opts {
		if opt != nil {
			opt(&f)
		}
	}
	return f
}

// InsertText creates fix that inserts text at span (Span.Start == Span.End).
func InsertText(title string, at source.Span, text string, guard string, opts ...Option) diag.Fix {
	edit := diag.TextEdit{
		Span:    at,
		NewText: text,
		OldText: guard,
	}
	fix := diag.Fix{
		Title:         title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityAlwaysSafe,
		Edits:         []diag.TextEdit{edit},
	}
	return applyOptions(fix, opts)
}

// DeleteSpan removes text covered by span.
func DeleteSpan(title string, span source.Span, expect string, opts ...Option) diag.Fix {
	edit := diag.TextEdit{
		Span:    span,
		NewText: "",
		OldText: expect,
	}
	fix := diag.Fix{
		Title:         title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityAlwaysSafe,
		Edits:         []diag.TextEdit{edit},
	}
	return applyOptions(fix, opts)
}

// DeleteSpans removes text covered by spans.
func DeleteSpans(title string, spans []source.Span, opts ...Option) diag.Fix {
	if len(spans) == 0 {
		return diag.Fix{Title: title}
	}
	edits := make([]diag.TextEdit, len(spans))
	for i, span := range spans {
		edits[i] = diag.TextEdit{
			Span:    span,
			NewText: "",
		}
	}
	fix := diag.Fix{
		Title:         title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityAlwaysSafe,
		Edits:         edits,
	}
	return applyOptions(fix, opts)
}

// DeleteSpansWithGuards removes spans with per-span guard strings.
func DeleteSpansWithGuards(title string, spans []source.Span, expects []string, opts ...Option) diag.Fix {
	if len(spans) == 0 {
		return diag.Fix{Title: title}
	}
	if len(expects) != 0 && len(expects) != len(spans) {
		panic("DeleteSpansWithGuards expects len(expects)==0 or len(spans)")
	}
	edits := make([]diag.TextEdit, len(spans))
	for i, span := range spans {
		var guard string
		if len(expects) > 0 {
			guard = expects[i]
		}
		edits[i] = diag.TextEdit{
			Span:    span,
			NewText: "",
			OldText: guard,
		}
	}
	fix := diag.Fix{
		Title:         title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityAlwaysSafe,
		Edits:         edits,
	}
	return applyOptions(fix, opts)
}

// ReplaceSpan replaces text covered by span with newText.
func ReplaceSpan(title string, span source.Span, newText, expect string, opts ...Option) diag.Fix {
	edit := diag.TextEdit{
		Span:    span,
		NewText: newText,
		OldText: expect,
	}
	fix := diag.Fix{
		Title:         title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityAlwaysSafe,
		Edits:         []diag.TextEdit{edit},
	}
	return applyOptions(fix, opts)
}

// ReplaceSpans replaces multiple spans with provided text (guards optional).
func ReplaceSpans(title string, spans []source.Span, newTexts []string, expects []string, opts ...Option) diag.Fix {
	if len(spans) == 0 {
		return diag.Fix{Title: title}
	}
	if len(newTexts) != len(spans) {
		panic("ReplaceSpans requires len(newTexts) == len(spans)")
	}
	if len(expects) != 0 && len(expects) != len(spans) {
		panic("ReplaceSpans expects len(expects)==0 or len(spans)")
	}
	edits := make([]diag.TextEdit, len(spans))
	for i, span := range spans {
		var guard string
		if len(expects) > 0 {
			guard = expects[i]
		}
		edits[i] = diag.TextEdit{
			Span:    span,
			NewText: newTexts[i],
			OldText: guard,
		}
	}
	fix := diag.Fix{
		Title:         title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityAlwaysSafe,
		Edits:         edits,
	}
	return applyOptions(fix, opts)
}

// WrapWith surrounds span with prefix and suffix insertions.
func WrapWith(title string, span source.Span, prefix, suffix string, opts ...Option) diag.Fix {
	edits := []diag.TextEdit{
		{
			Span:    source.Span{File: span.File, Start: span.Start, End: span.Start},
			NewText: prefix,
		},
		{
			Span:    source.Span{File: span.File, Start: span.End, End: span.End},
			NewText: suffix,
		},
	}
	fix := diag.Fix{
		Title:         title,
		Kind:          diag.FixKindRefactorRewrite,
		Applicability: diag.FixApplicabilitySafeWithHeuristics,
		Edits:         edits,
	}
	return applyOptions(fix, opts)
}

// CommentLine replaces line contents with commented variant.
func CommentLine(title string, lineSpan source.Span, lineText string, opts ...Option) diag.Fix {
	lineNoNL := strings.TrimSuffix(lineText, "\n")
	if strings.HasPrefix(strings.TrimSpace(lineNoNL), "//") {
		return ReplaceSpan(title, lineSpan, lineText, lineText, opts...)
	}
	trimmedLeft := strings.TrimLeft(lineNoNL, " \t")
	leading := lineNoNL[:len(lineNoNL)-len(trimmedLeft)]
	commentBody := trimmedLeft
	if commentBody != "" && commentBody[0] == '/' {
		commentBody = " " + commentBody
	}
	comment := leading + "// " + strings.TrimLeft(commentBody, " ")
	if strings.HasSuffix(lineText, "\n") {
		comment += "\n"
	}
	return ReplaceSpan(title, lineSpan, comment, lineText, opts...)
}

// DeleteLine removes entire line (caller decides whether newline part of span).
func DeleteLine(title string, lineSpan source.Span, lineText string, opts ...Option) diag.Fix {
	newText := ""
	return ReplaceSpan(title, lineSpan, newText, lineText, opts...)
}
