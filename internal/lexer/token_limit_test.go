package lexer

import (
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

func TestTokenTooLongTriggersDiagnosticAndStops(t *testing.T) {
	content := strings.Repeat("a", maxTokenLength+1)
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("long.sg", []byte(content))
	file := fs.Get(fileID)

	bag := diag.NewBag(4)
	lx := New(file, Options{Reporter: &diag.BagReporter{Bag: bag}})

	tok := lx.Next()
	if tok.Kind != token.Invalid {
		t.Fatalf("expected invalid token, got %v", tok.Kind)
	}
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics for long token")
	}
	items := bag.Items()
	if items[0].Code != diag.LexTokenTooLong {
		t.Fatalf("expected LexTokenTooLong, got %v", items[0].Code)
	}

	// Lexer should fast-forward to EOF after the error.
	if next := lx.Next(); next.Kind != token.EOF {
		t.Fatalf("expected EOF after long token, got %v", next.Kind)
	}
}

func TestTokenAtLimitAllowed(t *testing.T) {
	content := strings.Repeat("b", maxTokenLength)
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("limit.sg", []byte(content))
	file := fs.Get(fileID)

	bag := diag.NewBag(1)
	lx := New(file, Options{Reporter: &diag.BagReporter{Bag: bag}})

	tok := lx.Next()
	if tok.Kind != token.Ident {
		t.Fatalf("expected ident token, got %v", tok.Kind)
	}
	if bag.HasErrors() {
		t.Fatalf("did not expect diagnostics, got %v", bag.Items())
	}
}
