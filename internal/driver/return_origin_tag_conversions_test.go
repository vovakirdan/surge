package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/symbols"
)

// The finalized use of an implicit Some/Success wrap (a tag use with no
// call expression) is certified by the constructor the checker recorded
// (ImplicitConversion.Callee, mapped to its declaration) and by its one type argument
// being the wrapped value's type. The value transfer is the existing one: the wrapper
// holds the payload with every root and loan it carries. Every source is a ROOT
// program against the real core.

const (
	tagConvTypedCall   = "generic tag use disagrees with its original typed call"
	tagConvGeneric     = "generic tag conversion in a generic caller needs its exact type-dependent payload transfer"
	tagConvDeclaration = "generic tag conversion disagrees with its selected source declaration"
)

// tagConvAt names the one occurrence of snippet in text.
func tagConvAt(t *testing.T, text, snippet string) originSpan {
	t.Helper()
	start := strings.Index(text, snippet)
	if start < 0 || strings.Count(text, snippet) != 1 {
		t.Fatalf("PRECONDITION: %q is not unique in the fixture", snippet)
	}
	return originSpan{start, start + len(snippet), snippet}
}

// The shapes of hir/option_erring.sg, mono/option_implicit_wrap.sg,
// sema/valid/return_type_and_sugar.sg and recursive_handles.sg's `next = first;`.
var tagConvFinishRows = []struct{ name, text string }{
	{"option_let_wrap", `fn wrapped() -> int {
    let x: int? = 1;
    let y: Option<int> = 2;
    return compare x {
        Some(v) => v;
        nothing => 0;
    } + compare y {
        Some(v) => v;
        nothing => 0;
    };
}
`},
	{"return_sugar_some_and_success", `fn maybe(flag: bool) -> Option<int> {
    if flag {
        return 1;
    }
    return nothing;
}

fn fallible(flag: bool) -> Erring<int, Error> {
    if flag {
        return 2;
    }
    let e: Error = Error { message: "no", code: 1:uint };
    return e;
}
`},
	{"assigned_wrap", `type NodeId = uint;

fn chain() -> NodeId? {
    let mut next: NodeId? = nothing;
    let first: NodeId = 0:uint;
    next = first;
    return next;
}
`},
}

func TestReturnOriginTagConversionsFinish(t *testing.T) {
	for _, row := range tagConvFinishRows {
		t.Run(row.name, func(t *testing.T) {
			f, analysis := analyzeOriginRoot(t, "tag_conv_"+row.name, row.text, false, nil)
			for _, pending := range analysis.Pending {
				t.Errorf("unexpected pending %q at %s %v", pending.Reason, pending.SourceKey, pending.Span)
			}
			originNoEscape(t, analysis, f.owner.File.ID, originSpan{0, len(row.text), row.name})
		})
	}
}

// A wrapped reference keeps its roots: the summary names the parameter it borrows.
func TestReturnOriginTagConversionKeepsPayloadRoots(t *testing.T) {
	const text = `fn pass(x: &int) -> Option<&int> {
    return x;
}
`
	f, analysis := analyzeOriginRoot(t, "tag_conv_payload_roots", text, false, nil)
	for _, pending := range analysis.Pending {
		t.Errorf("unexpected pending %q at %s %v", pending.Reason, pending.SourceKey, pending.Span)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "pass", false, []uint32{0})
}

// A generic caller keeps a named refusal: its wrap's instance depends on T.
func TestReturnOriginTagConversionGenericCallerStaysRefused(t *testing.T) {
	const text = `fn wrap<T>(x: T) -> Option<T> {
    return x;
}

fn use_it() -> int {
    let o = wrap(3);
    return compare o { Some(v) => v; nothing => 0; };
}
`
	f, analysis := analyzeOriginRoot(t, "tag_conv_generic_caller", text, false, nil)
	originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(text), "generic caller"},
		[]originRefusal{{span: originSpan{43, 44, "x"}, reason: tagConvGeneric}})
}

// Identity, not name: the recorded constructor is swapped for a tag of another
// union and that tag is renamed `Some`. The certificate follows the declaration, so
// the use keeps a named refusal.
func TestReturnOriginTagConversionIsKeyedByDeclaration(t *testing.T) {
	const text = `tag Found<T>(T);
type Lookup<T> = Found(T) | nothing;

fn wrapped() -> Option<int> {
    return 7;
}
`
	f, analysis := analyzeOriginRoot(t, "tag_conv_forged_callee", text, false, func(f originalGenericFixture) {
		table := f.unit.Symbols.Table
		var found, some symbols.SymbolID
		for i, sym := range table.Symbols.Data() {
			name, _ := table.Strings.Lookup(sym.Name)
			if sym.Kind != symbols.SymbolTag {
				continue
			}
			id := symbols.SymbolID(i + 1)
			switch name {
			case "Found":
				if sym.Span.File == f.owner.File.ID {
					found = id
				}
			case "Some":
				some = id
			}
		}
		if !found.IsValid() || !some.IsValid() {
			t.Fatalf("PRECONDITION: tags Found=%v Some=%v", found, some)
		}
		forged := 0
		for expr, conversion := range f.unit.Sema.ImplicitConversions {
			if conversion.Kind == sema.ImplicitConversionSome && conversion.Callee == some {
				conversion.Callee = found
				f.unit.Sema.ImplicitConversions[expr] = conversion
				forged++
			}
		}
		if forged != 1 {
			t.Fatalf("PRECONDITION: %d Some conversions to forge", forged)
		}
		table.Symbols.Get(found).Name = table.Symbols.Get(some).Name
	})
	originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(text), "forged"},
		[]originRefusal{{span: tagConvAt(t, text, "7"), reason: tagConvDeclaration}})
}

// A wrapped reference to a dying local that escapes: the analysis follows the
// payload to `v` (SEM3139). At the base it stopped unfinished on the tag row.
func TestReturnOriginTagConversionEscapeIsRefused(t *testing.T) {
	const text = `fn leak() -> Option<&int> {
    let v: int = 1;
    let r: &int = &v;
    let o: Option<&int> = r;
    return o;
}
`
	stdlib := detectStdlibRootFrom(".")
	if stdlib == "" {
		t.Fatal("PRECONDITION: real stdlib unavailable")
	}
	t.Setenv("SURGE_STDLIB", stdlib)
	root := t.TempDir()
	path := filepath.Join(root, "main.sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
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
}

// The other "disagrees" forms are not tag conversions and keep their rows: an
// element place as a `&mut` receiver, and an explicit union member passed where the
// formal names its union.
func TestReturnOriginTagConversionOtherFormsKeepTheirRows(t *testing.T) {
	rows := []struct{ name, text, snippet, reason string }{
		{"element_receiver_argument", `fn grow(xs: &mut int[][]) -> nothing {
    xs[1].push(9);
    return nothing;
}
`, "xs[1].push(9)", "generic original call argument disagrees with its substituted source signature"},
		{"union_member_argument", `fn fill(opts: &mut Option<int64>[]) -> nothing {
    opts.push(Some(3:int64));
    return nothing;
}
`, "opts.push(Some(3:int64))", "generic original call argument disagrees with its substituted source signature"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f, analysis := analyzeOriginRoot(t, "tag_conv_other_"+row.name, row.text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(row.text), row.name},
				[]originRefusal{{span: tagConvAt(t, row.text, row.snippet), reason: row.reason}})
		})
	}
}
