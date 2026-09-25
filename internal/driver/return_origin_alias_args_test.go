package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// N-ALIAS-ARG: a generic call argument whose type differs from the substituted
// template argument only by an alias chain (`T := byte` against `uint8`, `T := Nodes`
// against `Node[]`) matches its formal. The relation is declaration identity along
// one alias chain; sibling aliases of one target, a union member against its union,
// and an element place as a `&mut` receiver keep "generic original call argument
// disagrees with its substituted source signature". Every source is a ROOT program
// against the real core.

const (
	aliasArgDisagrees     = "generic original call argument disagrees with its substituted source signature"
	aliasArgLoanDiscarded = "storage loan would be discarded by a payload-free value"
)

// The alias shapes of recursive_handles.sg (`len(nodes)` over `Nodes = Node[]`) and of
// the vm_hash programs (`out.push(...)` of a `uint8` into a `byte[]`), without the rows
// other packets answer.
var aliasArgFinishRows = []struct{ name, text string }{
	{"alias_container_len", `type NodeId = uint;

type Node = {
    data: int,
}

type Nodes = Node[];

fn count() -> NodeId {
    let mut nodes: Nodes = [];
    nodes.push(Node { data: 1 });
    let first: NodeId = len(nodes);
    return first;
}
`},
	{"byte_push_both_directions", `fn push_raw(out: &mut byte[], v: uint8) -> nothing {
    out.push(v);
    return nothing;
}

fn push_octet(out: &mut uint8[], b: byte) -> nothing {
    out.push(b);
    return nothing;
}
`},
	{"byte_frame_push", `fn push_le(out: &mut byte[], value: uint64) -> nothing {
    let mut i: int = 0;
    while i < 8 {
        out.push(((value >> ((i * 8) to uint64)) & 255:uint64) to uint8);
        i = i + 1;
    }
    return nothing;
}
`},
	{"user_generic_alias_argument", `fn keep<T>(out: &mut T[], x: T) -> nothing {
    out.push(x);
    return nothing;
}

fn put(out: &mut byte[], v: uint8) -> nothing {
    keep::<byte>(out, v);
    return nothing;
}
`},
}

// aliasArgAt names the one occurrence of snippet in text.
func aliasArgAt(t *testing.T, text, snippet string) originSpan {
	t.Helper()
	start := strings.Index(text, snippet)
	if start < 0 || strings.Count(text, snippet) != 1 {
		t.Fatalf("PRECONDITION: %q is not unique in the fixture", snippet)
	}
	return originSpan{start, start + len(snippet), snippet}
}

func TestReturnOriginAliasArgumentsFinish(t *testing.T) {
	for _, row := range aliasArgFinishRows {
		t.Run(row.name, func(t *testing.T) {
			f, analysis := analyzeOriginRoot(t, "alias_arg_"+row.name, row.text, false, nil)
			for _, pending := range analysis.Pending {
				t.Errorf("unexpected pending %q at %s %v", pending.Reason, pending.SourceKey, pending.Span)
			}
			originNoEscape(t, analysis, f.owner.File.ID, originSpan{0, len(row.text), row.name})
		})
	}
}

// Canaries the analysis must keep refusing, each by its exact rows: the P-STASH
// shapes (RV2-DEBT-365: a window into a fixed array pushed into a `&mut` container
// parameter, written through `*out =`, sent into a channel), spelled with and
// without an alias, and the forms this packet does not answer.
func TestReturnOriginAliasArgumentCanariesKeepTheirRows(t *testing.T) {
	type want struct{ snippet, reason string }
	rows := []struct {
		name, text string
		want       []want
	}{
		{name: "p_stash_push_window", text: `fn stash(out: &mut int[][]) -> nothing {
    let xs: int[4] = [1, 2, 3, 4];
    out.push(xs[[1..3]]);
    return nothing;
}
`, want: []want{{"out.push(xs[[1..3]])", aliasArgLoanDiscarded}}},
		{
			// Held at base ONLY by this packet's row; after it, by G6.
			name: "p_stash_push_window_alias", text: `type Win = int[];

fn stash(out: &mut Win[]) -> nothing {
    let xs: int[4] = [1, 2, 3, 4];
    out.push(xs[[1..3]]);
    return nothing;
}
`, want: []want{{"out.push(xs[[1..3]])", aliasArgLoanDiscarded}},
		},
		{
			// Held at base ONLY by this packet's row; after it, by G6.
			name: "p_stash_generic_alias", text: `type Win = int[];

fn keep<T>(out: &mut T[], x: T) -> nothing {
    out.push(x);
    return nothing;
}

fn stash(out: &mut Win[]) -> nothing {
    let xs: int[4] = [1, 2, 3, 4];
    keep::<Win>(out, xs[[1..3]]);
    return nothing;
}
`, want: []want{{"keep::<Win>(out, xs[[1..3]])", aliasArgLoanDiscarded}},
		},
		{name: "p_stash_deref_write_alias", text: `type Win = int[];

fn stash(out: &mut Win) -> nothing {
    let xs: int[4] = [1, 2, 3, 4];
    *out = xs[[1..3]];
    return nothing;
}
`, want: []want{{"*out = xs[[1..3]]", aliasArgLoanDiscarded}}},
		{name: "p_stash_channel_alias", text: `type Win = int[];

fn stash(ch: Channel<Win>) -> nothing {
    let xs: int[4] = [1, 2, 3, 4];
    let w: Win = xs[[1..3]];
    ch.send(own w);
    return nothing;
}
`, want: []want{{"ch.send(own w)", aliasArgLoanDiscarded}}},
		{name: "union_member_form_keeps_row", text: `fn fill(opts: &mut Option<int64>[]) -> nothing {
    opts.push(Some(3:int64));
    return nothing;
}
`, want: []want{{"opts.push(Some(3:int64))", aliasArgDisagrees}}},
		{name: "union_member_window_keeps_both_rows", text: `fn stash_opt(opts: &mut Option<int[]>[]) -> nothing {
    let a: int[4] = [1, 2, 3, 4];
    opts.push(Some(a[[1..3]]));
    return nothing;
}
`, want: []want{{"opts.push(Some(a[[1..3]]))", aliasArgDisagrees}, {"Some(a[[1..3]])", aliasArgLoanDiscarded}}},
		{name: "index_receiver_form_keeps_row", text: `fn grow(xs: &mut int[][]) -> nothing {
    xs[1].push(9);
    return nothing;
}
`, want: []want{{"xs[1].push(9)", aliasArgDisagrees}}},
		{name: "index_receiver_window_keeps_row", text: `fn stash_elem(xs: &mut int[][][]) -> nothing {
    let a: int[4] = [1, 2, 3, 4];
    xs[0].push(a[[1..3]]);
    return nothing;
}
`, want: []want{{"xs[0].push(a[[1..3]])", aliasArgDisagrees}}},
		{
			// Two aliases of one target: neither chain reaches the other, so the
			// relation (declaration identity along ONE chain) does not hold.
			name: "sibling_aliases_keep_row", text: `type WinA = int[];
type WinB = int[];

fn keep<T>(out: &mut T[], x: T) -> nothing {
    out.push(x);
    return nothing;
}

fn stash(out: &mut WinA[], b: WinB) -> nothing {
    keep::<WinA>(out, b);
    return nothing;
}
`, want: []want{{"keep::<WinA>(out, b)", aliasArgDisagrees}},
		},
		{name: "sibling_byte_aliases_keep_row", text: `type Octet = uint8;

fn keep<T>(out: &mut T[], x: T) -> nothing {
    out.push(x);
    return nothing;
}

fn put(out: &mut byte[], o: Octet) -> nothing {
    keep::<byte>(out, o);
    return nothing;
}
`, want: []want{{"keep::<byte>(out, o)", aliasArgDisagrees}}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f, analysis := analyzeOriginRoot(t, "alias_arg_"+row.name, row.text, false, nil)
			var refusals []originRefusal
			for _, w := range row.want {
				refusals = append(refusals, originRefusal{span: aliasArgAt(t, row.text, w.snippet), reason: w.reason})
			}
			originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(row.text), row.name}, refusals)
		})
	}
}

// Escapes refused with SEM3139: P-VIEW2 (a slice of a window returned, RV2-DEBT-365)
// with and without an alias, and an alias to a reference passed through a generic
// call, which the analysis now follows to the local it borrows.
func TestReturnOriginAliasArgumentEscapesAreRefused(t *testing.T) {
	rows := []struct{ name, text string }{
		{"p_view2_slice_of_window", `fn view2() -> int[] {
    let a: int[4] = [1, 2, 3, 4];
    let v = a[[0..3]];
    return v[[0..1]];
}
`},
		{"p_view2_slice_of_window_alias", `type Win = int[];

fn view2() -> Win {
    let a: int[4] = [1, 2, 3, 4];
    let v: Win = a[[0..3]];
    return v[[0..1]];
}
`},
		{"reference_alias_through_generic", `type IntRef = &int;

fn id<T>(x: T) -> T {
    return x;
}

fn leak() -> IntRef {
    let v: int = 1;
    return id::<IntRef>(&v);
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdlib := detectStdlibRootFrom(".")
			if stdlib == "" {
				t.Fatal("PRECONDITION: real stdlib unavailable")
			}
			t.Setenv("SURGE_STDLIB", stdlib)
			root := t.TempDir()
			path := filepath.Join(root, "main.sg")
			if err := os.WriteFile(path, []byte(row.text), 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64})
			if err != nil || result == nil || result.Bag == nil {
				t.Fatalf("diagnose: result=%v err=%v", result != nil, err)
			}
			for _, d := range result.Bag.Items() {
				if d.Code == diag.SemaBorrowEscapesReturn && d.Severity >= diag.SevError {
					return
				}
			}
			t.Fatalf("expected %s, got:\n%s", diag.SemaBorrowEscapesReturn.ID(), diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
		})
	}
}
