package token

import (
	"testing"
)

func TestLookupKeyword_Positive(t *testing.T) {
	cases := map[string]Kind{
		"fn":       KwFn,
		"let":      KwLet,
		"return":   KwReturn,
		"parallel": KwParallel,
		"map":      KwMap,
		"reduce":   KwReduce,
		"with":     KwWith,
		"await":    KwAwait,
		"is":       KwIs,
		"heir":     KwHeir,
		"true":     KwTrue,
		"false":    KwFalse,
	}

	for lexeme, want := range cases {
		got, ok := LookupKeyword(lexeme)
		if !ok {
			t.Fatalf("LookupKeyword(%q) = !ok, want %v", lexeme, want)
		}
		if got != want {
			t.Fatalf("LookupKeyword(%q) = %v, want %v", lexeme, got, want)
		}
	}
}

func TestLookupKeyword_Negative(t *testing.T) {
	// Заведомо НЕ ключевые слова
	notKw := []string{
		"Fn", "LET", "Await", // регистр важен — понижение делает лексер
		"int", "int8", "uint32", "float64", // имена типов — Ident
		"identifier", "toString",
	}
	for _, s := range notKw {
		if _, ok := LookupKeyword(s); ok {
			t.Fatalf("LookupKeyword(%q) returned ok=true, want false", s)
		}
	}
}
