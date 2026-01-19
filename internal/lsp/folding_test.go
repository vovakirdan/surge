package lsp

import (
	"strings"
	"testing"
)

func TestFoldingRangesBraces(t *testing.T) {
	src := strings.Join([]string{
		"fn main() {",
		"    let s = \"\\u03c0\";",
		"    if true {",
		"        print(s);",
		"    }",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	ranges := buildFoldingRanges(snapshot, uri)
	if len(ranges) < 2 {
		t.Fatalf("expected at least 2 folding ranges, got %d", len(ranges))
	}
	if !hasFoldingRange(ranges, 0, 5) {
		t.Fatalf("missing folding range for fn block: %+v", ranges)
	}
	if !hasFoldingRange(ranges, 2, 4) {
		t.Fatalf("missing folding range for if block: %+v", ranges)
	}
}

func hasFoldingRange(ranges []foldingRange, start, end int) bool {
	for _, rng := range ranges {
		if rng.StartLine == start && rng.EndLine == end {
			return true
		}
	}
	return false
}
