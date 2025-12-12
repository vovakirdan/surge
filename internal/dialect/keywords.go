package dialect

import (
	"strings"

	"surge/internal/source"
)

type keywordSignal struct {
	Dialect DialectKind
	Score   int
	Reason  string
}

var keywordSignals = map[string][]keywordSignal{
	// Rust-ish
	"impl":        {{Dialect: DialectRust, Score: 6, Reason: "rust keyword `impl`"}},
	"trait":       {{Dialect: DialectRust, Score: 6, Reason: "rust keyword `trait`"}},
	"macro_rules": {{Dialect: DialectRust, Score: 5, Reason: "rust macro_rules syntax"}},
	"crate":       {{Dialect: DialectRust, Score: 5, Reason: "rust keyword `crate`"}},
	"mod":         {{Dialect: DialectRust, Score: 4, Reason: "rust keyword `mod`"}},
	"match":       {{Dialect: DialectRust, Score: 4, Reason: "rust keyword `match`"}},
	"dyn":         {{Dialect: DialectRust, Score: 4, Reason: "rust keyword `dyn`"}},
	"ref":         {{Dialect: DialectRust, Score: 1, Reason: "rust keyword `ref`"}},
	"struct":      {{Dialect: DialectRust, Score: 2, Reason: "rust keyword `struct`"}},
	// "enum" is a Surge keyword too; treat it as low-signal.
	"enum": {{Dialect: DialectRust, Score: 1, Reason: "rust keyword `enum`"}},

	// Go-ish
	"defer":   {{Dialect: DialectGo, Score: 5, Reason: "go keyword `defer`"}},
	"chan":    {{Dialect: DialectGo, Score: 4, Reason: "go keyword `chan`"}},
	"package": {{Dialect: DialectGo, Score: 4, Reason: "go keyword `package`"}},
	"func":    {{Dialect: DialectGo, Score: 4, Reason: "go keyword `func`"}},
	"select":  {{Dialect: DialectGo, Score: 3, Reason: "go keyword `select`"}},
	"range":   {{Dialect: DialectGo, Score: 2, Reason: "go keyword `range`"}},
	"go":      {{Dialect: DialectGo, Score: 2, Reason: "go keyword `go`"}},
	// `interface` is ambiguous (Go/TS); keep it low-signal for both.
	"interface": {
		{Dialect: DialectGo, Score: 1, Reason: "go keyword `interface`"},
		{Dialect: DialectTypeScript, Score: 1, Reason: "typescript keyword `interface`"},
	},

	// TypeScript-ish
	"implements": {{Dialect: DialectTypeScript, Score: 4, Reason: "typescript keyword `implements`"}},
	"extends":    {{Dialect: DialectTypeScript, Score: 4, Reason: "typescript keyword `extends`"}},
	"namespace":  {{Dialect: DialectTypeScript, Score: 4, Reason: "typescript keyword `namespace`"}},
	"readonly":   {{Dialect: DialectTypeScript, Score: 3, Reason: "typescript keyword `readonly`"}},
	"unknown":    {{Dialect: DialectTypeScript, Score: 3, Reason: "typescript keyword `unknown`"}},
	"never":      {{Dialect: DialectTypeScript, Score: 2, Reason: "typescript keyword `never`"}},

	// Python-ish
	"None": {{Dialect: DialectPython, Score: 4, Reason: "python `None`"}},
	"def":  {{Dialect: DialectPython, Score: 1, Reason: "python keyword `def`"}},
}

// RecordIdent collects keyword evidence for an identifier token. It tries an exact
// match, and also a lowercased match for keyword-like spellings (e.g., "Impl").
func RecordIdent(e *Evidence, ident string, span source.Span) {
	if e == nil || ident == "" {
		return
	}
	recordIdentKey(e, ident, span)
	if lower := strings.ToLower(ident); lower != ident {
		recordIdentKey(e, lower, span)
	}
}

func recordIdentKey(e *Evidence, ident string, span source.Span) {
	signals := keywordSignals[ident]
	for _, sig := range signals {
		e.Add(Hint{
			Dialect: sig.Dialect,
			Score:   sig.Score,
			Reason:  sig.Reason,
			Span:    span,
		})
	}
}
