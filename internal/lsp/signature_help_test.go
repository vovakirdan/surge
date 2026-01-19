package lsp

import (
	"strings"
	"testing"
)

func TestSignatureHelpActiveParam(t *testing.T) {
	src := strings.Join([]string{
		"fn foo(a: int, b: int, c: int) -> int {",
		"    return a + b + c;",
		"}",
		"",
		"fn main() {",
		"    let x = foo(1, 2, 3);",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	callIdx := strings.Index(src, "foo(1, 2, 3)")
	if callIdx < 0 {
		t.Fatal("missing call")
	}
	firstArgOffset := callIdx + len("foo(")
	helper := buildSignatureHelp(snapshot, uri, positionForOffsetUTF16(src, firstArgOffset))
	if helper == nil || len(helper.Signatures) == 0 {
		t.Fatal("expected signature help")
	}
	if helper.ActiveParameter != 0 {
		t.Fatalf("expected active parameter 0, got %d", helper.ActiveParameter)
	}

	secondArgOffset := callIdx + len("foo(1,")
	helper = buildSignatureHelp(snapshot, uri, positionForOffsetUTF16(src, secondArgOffset))
	if helper == nil || len(helper.Signatures) == 0 {
		t.Fatal("expected signature help for second arg")
	}
	if helper.ActiveParameter != 1 {
		t.Fatalf("expected active parameter 1, got %d", helper.ActiveParameter)
	}
}

func TestSignatureHelpOverloads(t *testing.T) {
	src := strings.Join([]string{
		"@overload fn foo(a: int) -> int { return a; }",
		"@overload fn foo(a: string) -> int { return 0; }",
		"",
		"fn main() {",
		"    let x = foo(1);",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	callIdx := strings.Index(src, "foo(1)")
	if callIdx < 0 {
		t.Fatal("missing call")
	}
	helper := buildSignatureHelp(snapshot, uri, positionForOffsetUTF16(src, callIdx+len("foo(")))
	if helper == nil {
		t.Fatal("expected signature help")
	}
	if len(helper.Signatures) != 2 {
		t.Fatalf("expected 2 overloads, got %d", len(helper.Signatures))
	}
	if helper.ActiveSignature < 0 || helper.ActiveSignature >= len(helper.Signatures) {
		t.Fatalf("unexpected active signature index %d", helper.ActiveSignature)
	}
	activeLabel := helper.Signatures[helper.ActiveSignature].Label
	if !strings.Contains(activeLabel, "a: int") {
		t.Fatalf("expected active signature for int overload, got %q", activeLabel)
	}
}
