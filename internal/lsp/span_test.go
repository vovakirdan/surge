package lsp

import (
	"strings"
	"testing"
)

func TestUTF16SpanMapping(t *testing.T) {
	src := strings.Join([]string{
		"fn foo() -> int {",
		"    return 1;",
		"}",
		"",
		"fn main() -> int {",
		"    let s: string = \"e\u0301🙂\"; let n = foo();",
		"    return n;",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)

	occurrence := strings.Index(src, "foo();")
	if occurrence < 0 {
		t.Fatal("missing call")
	}
	startOff := occurrence
	endOff := occurrence + len("foo")
	expectStart := positionForOffsetUTF16(src, startOff)
	expectEnd := positionForOffsetUTF16(src, endOff)

	hover := buildHover(snapshot, uri, expectStart)
	if hover == nil || hover.Range == nil {
		t.Fatal("expected hover range")
	}
	if hover.Range.Start != expectStart || hover.Range.End != expectEnd {
		t.Fatalf("unexpected hover range: %+v", *hover.Range)
	}

	nameOccurrence := strings.Index(src, "let n =")
	if nameOccurrence < 0 {
		t.Fatal("missing let binding")
	}
	nameStart := nameOccurrence + len("let ")
	nameEnd := nameStart + len("n")
	expectInlay := positionForOffsetUTF16(src, nameEnd)

	fullRange := lspRange{
		Start: position{Line: 0, Character: 0},
		End:   position{Line: 200, Character: 0},
	}
	hints := buildInlayHints(snapshot, uri, fullRange, defaultInlayHintConfig())
	found := false
	for _, hint := range hints {
		if hint.Label == ": int" {
			found = true
			if hint.Position != expectInlay {
				t.Fatalf("unexpected inlay position: %+v", hint.Position)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected int inlay hint")
	}
}
