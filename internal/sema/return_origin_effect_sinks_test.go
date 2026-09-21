package sema

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
)

// A body-less selected operator can write a storage loan through a `&mut` formal whose
// referent reads reference-free, or hand one back in a part of its result. The reference
// operands of `cut` and `scale` reach selectedOperation; every other row reaches the
// borrow-free path, where SEMA borrows a by-value operand into the reference formal.
const loanSinkOperationText = `pragma no_std;
type Pair = { n: int };
type Boxed = { v: Array<int> };
extern<Pair> {
    fn __div(self: &Pair, other: &mut Boxed) -> int;
    fn __mul(self: &Pair, other: &mut int) -> int;
    fn __add(self: &Pair, other: &Pair) -> Boxed;
    fn __sub(self: &Pair, other: &Pair) -> Pair;
}
extern<Boxed> {
    fn __neg(self: &mut Boxed) -> int;
}
fn cut(a: &Pair, b: &mut Boxed) -> int { return a / b; }
fn scale(a: &Pair, n: &mut int) -> int { return a * n; }
fn cut_value(h: Pair, x: Boxed) -> int {
    let mut b: Boxed = x;
    return h / b;
}
fn scale_value(h: Pair, x: int) -> int {
    let mut n: int = x;
    return h * n;
}
fn negate_value(x: Boxed) -> int {
    let mut b: Boxed = x;
    return -b;
}
fn join_value(h: Pair, g: Pair) -> Boxed { return h + g; }
fn diff_value(h: Pair, g: Pair) -> Pair { return h - g; }
`

const loanSinkOperationDigest = "d7dab996d28a5d0e7180120a26c0a4758ef1a0e58633d32fa1acc1e7eca252b0"

// A generic formal names only its parameter, so its referent's shape is not proven;
// `Tin`'s body answers for its own writes and stays on the borrow-free path.
const loanSinkGenericText = `pragma no_std;
type Jar<T> = { v: T };
type Tin<T> = { v: T };
extern<Jar<T>> {
    fn __mod(self: &Jar<T>, other: &mut Jar<T>) -> int;
}
extern<Tin<T>> {
    fn __mod(self: &Tin<T>, other: &mut Tin<T>) -> int {
        return 0;
    }
}
fn cut_jar(h: Jar<Array<int>>, x: Jar<Array<int>>) -> int {
    let mut c: Jar<Array<int>> = x;
    return h % c;
}
fn cut_tin(h: Tin<Array<int>>, x: Tin<Array<int>>) -> int {
    let mut c: Tin<Array<int>> = x;
    return h % c;
}
`

const loanSinkGenericDigest = "993691df24092fd1828350a32b5ac5feb4f56e9445889575b34bcfedc82ba150"

type loanSinkLeaf struct {
	name       string
	kind       ast.ExprKind
	start, end int
	snippet    string
	reason     string
	refused    bool
	body       bool
}

type loanSinkSource struct {
	name, text, digest string
	leaves             []loanSinkLeaf
}

// loanSinkOperationSite finds the one expression of a kind at a frozen span of text.
func loanSinkOperationSite(t *testing.T, u ReturnOriginUnit, text string, leaf loanSinkLeaf) ast.ExprID {
	t.Helper()
	if text[leaf.start:leaf.end] != leaf.snippet {
		t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", leaf.start, leaf.end, leaf.snippet)
	}
	var found ast.ExprID
	for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := u.Builder.Exprs.Get(id)
		if node == nil || node.Kind != leaf.kind || int(node.Span.Start) != leaf.start || int(node.Span.End) != leaf.end {
			continue
		}
		if found.IsValid() {
			t.Fatalf("PRECONDITION: %q has more than one expression", leaf.snippet)
		}
		found = id
	}
	if !found.IsValid() {
		t.Fatalf("PRECONDITION: %q has no expression", leaf.snippet)
	}
	return found
}

// 1 parent + 2 sources + 9 leaves = 12 RUN.
func TestReturnOriginLoanSinkOperation(t *testing.T) {
	sources := []loanSinkSource{
		{name: "operation", text: loanSinkOperationText, digest: loanSinkOperationDigest, leaves: []loanSinkLeaf{
			{name: "contents_sink_refused", kind: ast.ExprBinary, start: 396, end: 401, snippet: "a / b", reason: returnOriginBinaryRefusal, refused: true, body: false},
			{name: "scalar_sink_control", kind: ast.ExprBinary, start: 453, end: 458, snippet: "a * n", reason: returnOriginBinaryRefusal, refused: false, body: false},
			{name: "by_value_sink_refused", kind: ast.ExprBinary, start: 540, end: 545, snippet: "h / b", reason: returnOriginBinaryRefusal, refused: true, body: false},
			{name: "by_value_scalar_control", kind: ast.ExprBinary, start: 625, end: 630, snippet: "h * n", reason: returnOriginBinaryRefusal, refused: false, body: false},
			{name: "unary_sink_refused", kind: ast.ExprUnary, start: 706, end: 708, snippet: "-b", reason: returnOriginUnaryRefusal, refused: true, body: false},
			{name: "result_part_refused", kind: ast.ExprBinary, start: 762, end: 767, snippet: "h + g", reason: returnOriginBinaryRefusal, refused: true, body: false},
			{name: "result_part_control", kind: ast.ExprBinary, start: 820, end: 825, snippet: "h - g", reason: returnOriginBinaryRefusal, refused: false, body: false},
		}},
		{name: "generic", text: loanSinkGenericText, digest: loanSinkGenericDigest, leaves: []loanSinkLeaf{
			{name: "generic_formal_refused", kind: ast.ExprBinary, start: 345, end: 350, snippet: "h % c", reason: returnOriginBinaryRefusal, refused: true, body: false},
			{name: "generic_body_control", kind: ast.ExprBinary, start: 461, end: 466, snippet: "h % c", reason: returnOriginBinaryRefusal, refused: false, body: true},
		}},
	}
	leaves := 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 2 || leaves != 9 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(src.text))); got != src.digest {
				t.Fatalf("PRECONDITION: frozen source changed: %s", got)
			}
			a := returnOriginConditionAnalyzer(t, src.text)
			u := a.units[0]
			if len(a.report.Diagnostics) != 0 {
				t.Fatalf("PRECONDITION: fixture has diagnostics: %+v", a.report.Diagnostics)
			}
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) {
					id := loanSinkOperationSite(t, u.ReturnOriginUnit, src.text, leaf)
					selections := u.Sema.MagicBinarySymbols
					if leaf.kind == ast.ExprUnary {
						selections = u.Sema.MagicUnarySymbols
					}
					selected, present := selections[id]
					fn, reason := a.selectedCallableFunction(u, selected)
					if !present || reason != "" || fn.candidate.HasBody != leaf.body || fn.item.Body.IsValid() != leaf.body {
						t.Fatalf("PRECONDITION: %q is not a certified selection with body=%v: present=%v reason=%q", leaf.snippet, leaf.body, present, reason)
					}
					site := ReturnOriginPending{SourceKey: u.SourceKey, Span: u.Builder.Exprs.Get(id).Span, Reason: leaf.reason}
					if got := slices.Contains(a.report.Pending, site); got != leaf.refused {
						t.Errorf("%q carries %q = %v, want %v; pending=%+v", leaf.snippet, leaf.reason, got, leaf.refused, a.report.Pending)
					}
				})
			}
		})
	}
}
