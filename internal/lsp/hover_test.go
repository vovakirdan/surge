package lsp

import (
	"strings"
	"testing"
)

func TestHoverTargets(t *testing.T) {
	src := strings.Join([]string{
		"fn foo() -> int {",
		"    return 1;",
		"}",
		"",
		"fn main() -> int {",
		"    let n = foo();",
		"    return n;",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)

	fnIdx := strings.Index(src, "fn foo")
	if fnIdx < 0 {
		t.Fatal("missing fn foo")
	}
	fnPos := positionForOffsetUTF16(src, fnIdx+3)
	fnHover := buildHover(snapshot, uri, fnPos)
	if fnHover == nil {
		t.Fatal("expected hover for function")
	}
	if !strings.Contains(fnHover.Contents.Value, "fn foo") {
		t.Fatalf("expected function signature, got %q", fnHover.Contents.Value)
	}
	if !strings.Contains(fnHover.Contents.Value, "int") {
		t.Fatalf("expected return type in hover, got %q", fnHover.Contents.Value)
	}

	callIdx := strings.Index(src, "foo();")
	if callIdx < 0 {
		t.Fatal("missing call")
	}
	callPos := positionForOffsetUTF16(src, callIdx)
	callHover := buildHover(snapshot, uri, callPos)
	if callHover == nil {
		t.Fatal("expected hover for call")
	}
	if !strings.Contains(callHover.Contents.Value, "Type:") {
		t.Fatalf("expected call type hover, got %q", callHover.Contents.Value)
	}
	if !strings.Contains(callHover.Contents.Value, "int") {
		t.Fatalf("expected call type int, got %q", callHover.Contents.Value)
	}

	returnIdx := strings.LastIndex(src, "return n;")
	if returnIdx < 0 {
		t.Fatal("missing return n")
	}
	varPos := positionForOffsetUTF16(src, returnIdx+len("return "))
	varHover := buildHover(snapshot, uri, varPos)
	if varHover == nil {
		t.Fatal("expected hover for variable")
	}
	if !strings.Contains(varHover.Contents.Value, "let n") {
		t.Fatalf("expected variable hover, got %q", varHover.Contents.Value)
	}
	if !strings.Contains(varHover.Contents.Value, "int") {
		t.Fatalf("expected variable type, got %q", varHover.Contents.Value)
	}
	if varHover.Range == nil {
		t.Fatal("expected hover range for variable")
	}
	expectStart := positionForOffsetUTF16(src, returnIdx+len("return "))
	expectEnd := positionForOffsetUTF16(src, returnIdx+len("return n"))
	if varHover.Range.Start != expectStart || varHover.Range.End != expectEnd {
		t.Fatalf("unexpected hover range: %+v", *varHover.Range)
	}
}
