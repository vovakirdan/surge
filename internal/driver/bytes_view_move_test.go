package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// A live BytesView borrows the string it views (owner ruling 2026-09-15): the checker
// refuses a move of that string (SEM3020) and a reassignment (SEM3019) while the view's
// borrow lives, exactly as it does for an array window over its base -- and, as for a
// window, the borrow lasts until the end of the block that made the view. Each refusal
// row has a window twin that the same rule refuses today.

const bytesViewSink = `fn sink(s: own string) -> nothing {
    return nothing;
}

`

// bytesViewDiagnose runs the full pipeline over one root program with the real core.
func bytesViewDiagnose(t *testing.T, text string) (*DiagnoseResult, error) {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	root := t.TempDir()
	path := filepath.Join(root, "main.sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64})
}

// bytesViewRefusal requires exactly one error with code at the one occurrence of snippet.
func bytesViewRefusal(t *testing.T, text string, code diag.Code, snippet string) {
	t.Helper()
	result, err := bytesViewDiagnose(t, text)
	var unfinished *returnOriginUnfinishedError
	if errors.As(err, &unfinished) {
		t.Fatalf("the checker accepted it; only return-origin analysis stopped it: %v", err)
	}
	if result == nil || result.Bag == nil {
		t.Fatalf("PRECONDITION: the checker produced no diagnostics bag: %v", err)
	}
	start := strings.Index(text, snippet)
	if start < 0 || strings.Count(text, snippet) != 1 {
		t.Fatalf("PRECONDITION: %q is not unique in the fixture", snippet)
	}
	found := 0
	for _, d := range result.Bag.Items() {
		if d.Code == code && d.Severity >= diag.SevError {
			if int(d.Primary.Start) != start {
				t.Errorf("%s at byte %d, want %d (%q)", code.ID(), d.Primary.Start, start, snippet)
			}
			found++
		}
	}
	if found != 1 {
		t.Fatalf("want one %s at %q, got:\n%s", code.ID(), snippet, diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
	}
}

// bytesViewAccepted requires the checker to accept: the pipeline either finishes or
// stops in return-origin analysis, which runs only after a clean checker.
func bytesViewAccepted(t *testing.T, text string) {
	t.Helper()
	result, err := bytesViewDiagnose(t, text)
	var unfinished *returnOriginUnfinishedError
	if err != nil && !errors.As(err, &unfinished) {
		t.Fatalf("diagnose: %v", err)
	}
	if result != nil && result.Bag != nil {
		for _, d := range result.Bag.Items() {
			if d.Severity >= diag.SevError {
				t.Fatalf("the checker refused an accepted control:\n%s", diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
			}
		}
	}
}

func TestBytesViewBlocksMovingItsString(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"move_while_view_lives", bytesViewSink + `fn a(s: string) -> uint8 {
    let v = s.bytes();
    sink(own s);
    return v[0];
}
`, "own s);", diag.SemaBorrowMove},
		{"view_passed_on_after_move", bytesViewSink + `fn read(v: BytesView) -> uint8 {
    return v[0];
}

fn c(s: string) -> uint8 {
    let v = s.bytes();
    sink(own s);
    return read(v);
}
`, "own s);", diag.SemaBorrowMove},
		{"reassign_while_view_lives", `fn b(t: string) -> uint8 {
    let mut s: string = t;
    let v = s.bytes();
    s = "c" + "d";
    return v[0];
}
`, `s = "c" + "d"`, diag.SemaBorrowMutation},
		{"copied_view_still_blocks", bytesViewSink + `fn a(s: string) -> uint8 {
    let v = s.bytes();
    let v2 = v;
    sink(own s);
    return v2[0];
}
`, "own s);", diag.SemaBorrowMove},
		// As for a window (`let w = xs[[0..2]]; let f = w[0]; sink(own xs);` is SEM3020 today),
		// the borrow lasts to the end of the block, not to the view's last use.
		{"dead_view_in_same_block", bytesViewSink + `fn a(s: string) -> uint8 {
    let v = s.bytes();
    let first: uint8 = v[0];
    sink(own s);
    return first;
}
`, "own s);", diag.SemaBorrowMove},
		// The window twins, refused by the same rule before this change.
		{"window_twin_move", `fn sinkw(xs: own int[]) -> nothing {
    return nothing;
}

fn a(xs: int[]) -> int {
    let w = xs[[0..2]];
    sinkw(own xs);
    return w[0];
}
`, "own xs", diag.SemaBorrowMove},
		{"window_twin_reassign", `fn b(t: int[]) -> int {
    let mut xs: int[] = t;
    let w = xs[[0..2]];
    xs = [7, 8, 9];
    return w[0];
}
`, "xs = [7, 8, 9]", diag.SemaBorrowMutation},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, row.code, row.snippet)
		})
	}
}

func TestBytesViewControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"view_block_ends_before_move", bytesViewSink + `fn a(s: string) -> uint8 {
    let mut first: uint8 = 0:uint8;
    {
        let v = s.bytes();
        first = v[0];
    }
    sink(own s);
    return first;
}
`},
		{"view_rederived_after_reassignment", `fn b(t: string) -> uint8 {
    let mut s: string = t;
    let mut first: uint8 = 0:uint8;
    {
        let v = s.bytes();
        first = v[0];
    }
    s = "c" + "d";
    {
        let v2 = s.bytes();
        first = first + v2[0];
    }
    return first;
}
`},
		{"bytes_copied_into_array_before_move", bytesViewSink + `fn a(s: string) -> uint8 {
    let mut copy: uint8[] = [];
    {
        let v = s.bytes();
        copy.push(v[0]);
    }
    sink(own s);
    return copy[0];
}
`},
		{"no_view_no_borrow", bytesViewSink + `fn a(s: string) -> uint {
    let n: uint = len(&s);
    sink(own s);
    return n;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, row.text)
		})
	}
}
