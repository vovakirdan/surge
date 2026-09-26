package driver

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diag"
)

// The byte read of a BytesView is certified by the selected declaration, not by
// the result type: a user indexer that returns a byte keeps its named refusal.
func TestReturnOriginSelectedContainerIndexIsKeyedByDeclaration(t *testing.T) {
	const text = `type ByteBox = { values: uint8[] };
extern<ByteBox> {
    fn __index(self: &ByteBox, index: int) -> uint8 {
        return self.values[index];
    }
}

fn user_index_byte(box: &ByteBox) -> uint8 {
    return box[0];
}
`
	f, analysis := analyzeOriginRoot(t, "selected_container_index_user_byte", text, false, nil)
	start := len(text) - len("fn user_index_byte(box: &ByteBox) -> uint8 {\n    return box[0];\n}\n")
	at := start + len("fn user_index_byte(box: &ByteBox) -> uint8 {\n    return ")
	if !originPendingAt(analysis, f.unit.SourceKey, originSpan{at, at + len("box[0]"), "box[0]"}, "index requires its selected container transfer") {
		t.Errorf("a user byte indexer lost its named refusal: %+v", originPendingWithin(analysis, f.unit.SourceKey, start, len(text)))
	}
}

// A view borrows the bytes of its string, which a move or a reassignment of that
// string frees. The checker refuses that for a view as it does for a reference
// and an array window (SEM3020 for the move, SEM3019 for the reassignment), and
// each of these programs must stay refused somewhere: built clean, it reads freed
// bytes (VM3301 on the VM, a wrong byte natively).
var indexViewStringGoneRows = []struct{ name, text string }{
	{"view_read_after_string_moved", `fn sink(s: string) -> nothing { return nothing; }

fn f() -> uint8 {
    let s: string = "a" + "b";
    let v = s.bytes();
    sink(s);
    return v[0];
}
`},
	{"view_read_after_string_reassigned", `fn f() -> uint8 {
    let mut s: string = "a" + "b";
    let v = s.bytes();
    s = "c" + "d";
    return v[0];
}
`},
	{"view_passed_after_string_moved", `fn sink(s: string) -> nothing { return nothing; }

fn first(v: BytesView) -> uint8 {
    return v[0];
}

fn f() -> uint8 {
    let s: string = "a" + "b";
    let v = s.bytes();
    sink(s);
    return first(v);
}
`},
}

func TestReturnOriginIndexViewReadNeedsItsString(t *testing.T) {
	for _, row := range indexViewStringGoneRows {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
			dir := t.TempDir()
			path := filepath.Join(dir, "main.sg")
			if err := os.WriteFile(path, []byte(row.text), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := DiagnoseWithOptions(t.Context(), path, &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: dir, MaxDiagnostics: 64})
			var unfinished *returnOriginUnfinishedError
			if errors.As(err, &unfinished) {
				return
			}
			if err != nil || res == nil || res.Bag == nil {
				t.Fatalf("PRECONDITION: diagnose: result=%v err=%v", res != nil, err)
			}
			for _, d := range res.Bag.Items() {
				if d.Severity >= diag.SevError {
					return
				}
			}
			t.Fatalf("builds clean: the view reads the bytes of a string that is gone")
		})
	}
}
