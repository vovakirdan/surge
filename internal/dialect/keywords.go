package dialect

import (
	"strings"

	"surge/internal/source"
)

type keywordSignal struct {
	Dialect Kind
	Score   int
	Reason  string
}

var keywordSignals = map[string][]keywordSignal{
	// Rust-ish
	"impl":        {{Dialect: Rust, Score: 6, Reason: "rust keyword `impl`"}},
	"trait":       {{Dialect: Rust, Score: 6, Reason: "rust keyword `trait`"}},
	"macro_rules": {{Dialect: Rust, Score: 5, Reason: "rust macro_rules syntax"}},
	"crate":       {{Dialect: Rust, Score: 5, Reason: "rust keyword `crate`"}},
	"mod":         {{Dialect: Rust, Score: 4, Reason: "rust keyword `mod`"}},
	"match":       {{Dialect: Rust, Score: 4, Reason: "rust keyword `match`"}},
	"dyn":         {{Dialect: Rust, Score: 4, Reason: "rust keyword `dyn`"}},
	"ref":         {{Dialect: Rust, Score: 1, Reason: "rust keyword `ref`"}},
	"struct":      {{Dialect: Rust, Score: 2, Reason: "rust keyword `struct`"}},
	// "enum" is a Surge keyword too; treat it as low-signal.
	"enum": {{Dialect: Rust, Score: 1, Reason: "rust keyword `enum`"}},

	// Go-ish
	"defer":   {{Dialect: Go, Score: 5, Reason: "go keyword `defer`"}},
	"chan":    {{Dialect: Go, Score: 4, Reason: "go keyword `chan`"}},
	"package": {{Dialect: Go, Score: 4, Reason: "go keyword `package`"}},
	"func":    {{Dialect: Go, Score: 4, Reason: "go keyword `func`"}},
	"select":  {{Dialect: Go, Score: 3, Reason: "go keyword `select`"}},
	"range":   {{Dialect: Go, Score: 2, Reason: "go keyword `range`"}},
	"go":      {{Dialect: Go, Score: 2, Reason: "go keyword `go`"}},
	// `interface` is ambiguous (Go/TS); keep it low-signal for both.
	"interface": {
		{Dialect: Go, Score: 1, Reason: "go keyword `interface`"},
		{Dialect: TypeScript, Score: 1, Reason: "typescript keyword `interface`"},
	},

	// TypeScript-ish
	"implements": {{Dialect: TypeScript, Score: 4, Reason: "typescript keyword `implements`"}},
	"extends":    {{Dialect: TypeScript, Score: 4, Reason: "typescript keyword `extends`"}},
	"namespace":  {{Dialect: TypeScript, Score: 4, Reason: "typescript keyword `namespace`"}},
	"readonly":   {{Dialect: TypeScript, Score: 3, Reason: "typescript keyword `readonly`"}},
	"unknown":    {{Dialect: TypeScript, Score: 3, Reason: "typescript keyword `unknown`"}},
	"never":      {{Dialect: TypeScript, Score: 2, Reason: "typescript keyword `never`"}},

	// Python-ish
	"None": {{Dialect: Python, Score: 4, Reason: "python `None`"}},
	"def":  {{Dialect: Python, Score: 1, Reason: "python keyword `def`"}},
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
