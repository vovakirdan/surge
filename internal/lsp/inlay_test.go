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
	letIdx := strings.Index(overlay, "let n")
	if letIdx < 0 {
		t.Fatal("missing let n")
	}
	nameOffset := letIdx + len("let n")
	expectPos := positionForOffsetUTF16(overlay, nameOffset)
	smallRange := lspRange{
		Start: position{Line: expectPos.Line, Character: 0},
		End:   position{Line: expectPos.Line, Character: expectPos.Character + 1},
	}
	repeat := buildInlayHints(snapshot, uri, smallRange, defaultInlayHintConfig())
	if len(repeat) != 1 {
		t.Fatalf("expected 1 inlay hint for small range, got %d", len(repeat))
	}
	if repeat[0].Label != ": string" {
		t.Fatalf("unexpected small-range hint label: %q", repeat[0].Label)
	}
}

func TestInlayHintsDefaultInit(t *testing.T) {
	src := strings.Join([]string{
		"fn foo() -> int {",
		"    return 1;",
		"}",
		"",
		"fn main() {",
		"    let a: int;",
		"    let b = foo();",
		"    let c: int = foo();",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	fullRange := lspRange{
		Start: position{Line: 0, Character: 0},
		End:   position{Line: 200, Character: 0},
	}
	hints := buildInlayHints(snapshot, uri, fullRange, defaultInlayHintConfig())
	if len(hints) != 2 {
		t.Fatalf("expected 2 inlay hints, got %d", len(hints))
	}
	foundType := false
	foundDefault := false
	semicolonIdx := strings.Index(src, "let a: int;")
	if semicolonIdx < 0 {
		t.Fatal("missing let a")
	}
	semicolonOffset := semicolonIdx + len("let a: int")
	expectDefaultPos := positionForOffsetUTF16(src, semicolonOffset)
	for _, hint := range hints {
		switch hint.Label {
		case ": int":
			foundType = true
		case " = default::<int>();":
			foundDefault = true
			if hint.Position != expectDefaultPos {
				t.Fatalf("unexpected default-init position: %+v", hint.Position)
			}
		}
	}
	if !foundType {
		t.Fatal("expected type hint for let b")
	}
	if !foundDefault {
		t.Fatal("expected default-init hint for let a")
	}
}

func TestInlayHintsEnumImplicitValues(t *testing.T) {
	src := strings.Join([]string{
		"enum Color = {",
		"    Red,",
		"    Green = 4,",
		"    Blue,",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	fullRange := lspRange{
		Start: position{Line: 0, Character: 0},
		End:   position{Line: 200, Character: 0},
	}
	hints := buildInlayHints(snapshot, uri, fullRange, defaultInlayHintConfig())
	if len(hints) != 2 {
		t.Fatalf("expected 2 enum inlay hints, got %d", len(hints))
	}
	redOffset := strings.Index(src, "Red") + len("Red")
	blueOffset := strings.Index(src, "Blue") + len("Blue")
	expectRed := positionForOffsetUTF16(src, redOffset)
	expectBlue := positionForOffsetUTF16(src, blueOffset)
	foundRed := false
	foundBlue := false
	for _, hint := range hints {
		switch hint.Label {
		case " = 0":
			foundRed = true
			if hint.Position != expectRed {
				t.Fatalf("unexpected Red hint position: %+v", hint.Position)
			}
		case " = 5":
			foundBlue = true
			if hint.Position != expectBlue {
				t.Fatalf("unexpected Blue hint position: %+v", hint.Position)
			}
		}
	}
	if !foundRed || !foundBlue {
		t.Fatalf("missing enum hints: Red=%v Blue=%v", foundRed, foundBlue)
	}
}
