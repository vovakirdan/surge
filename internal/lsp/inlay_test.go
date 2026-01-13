package lsp

import (
	"strings"
	"testing"
)

func TestInlayHintsLetTypes(t *testing.T) {
	src := strings.Join([]string{
		"fn foo() -> int {",
		"    return 1;",
		"}",
		"",
		"fn main() -> int {",
		"    let n = foo();",
		"    let m: int = foo();",
		"    return n + m;",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	fullRange := lspRange{
		Start: position{Line: 0, Character: 0},
		End:   position{Line: 200, Character: 0},
	}
	hints := buildInlayHints(snapshot, uri, fullRange, defaultInlayHintConfig())
	if len(hints) != 1 {
		t.Fatalf("expected 1 inlay hint, got %d", len(hints))
	}
	if hints[0].Label != ": int" {
		t.Fatalf("unexpected hint label: %q", hints[0].Label)
	}
	letIdx := strings.Index(src, "let n = foo();")
	if letIdx < 0 {
		t.Fatal("missing let n")
	}
	nameOffset := letIdx + len("let n")
	expectPos := positionForOffsetUTF16(src, nameOffset)
	if hints[0].Position != expectPos {
		t.Fatalf("unexpected hint position: %+v", hints[0].Position)
	}
}

func TestInlayHintsOverlay(t *testing.T) {
	disk := strings.Join([]string{
		"fn foo() -> int {",
		"    return 1;",
		"}",
		"",
		"fn main() -> int {",
		"    let n = foo();",
		"    return 0;",
		"}",
		"",
	}, "\n")
	overlay := strings.Join([]string{
		"fn foo() -> int {",
		"    return 1;",
		"}",
		"",
		"fn main() -> int {",
		"    let n = \"hi\";",
		"    return 0;",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshotWithOverlay(t, disk, overlay)
	fullRange := lspRange{
		Start: position{Line: 0, Character: 0},
		End:   position{Line: 200, Character: 0},
	}
	hints := buildInlayHints(snapshot, uri, fullRange, defaultInlayHintConfig())
	if len(hints) != 1 {
		t.Fatalf("expected 1 inlay hint, got %d", len(hints))
	}
	if hints[0].Label != ": string" {
		t.Fatalf("unexpected overlay hint label: %q", hints[0].Label)
	}
}
