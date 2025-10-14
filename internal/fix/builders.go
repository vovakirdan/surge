package fix

import (
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
