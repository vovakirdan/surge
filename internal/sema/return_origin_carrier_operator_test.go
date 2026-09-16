package sema

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
)

// The borrow-free fast path needs ALL-RefFree operands, which by-value operands give
// and reference parameters do not. These three bodies supply them, so they are where
// the carrier-result hole is visible: a body-less OPAQUE declaration reaches the same
// arm as a body, and neither can be proven to keep its operand's storage out of its
// result. `__sub` returns a scalar and must stay on the fast path.
const carrierOperatorSource = `pragma no_std;
type Arr = { items: Array<uint64> };
extern<Arr> {
    fn __add(self: &Arr, other: &Arr) -> Array<uint64>;
    fn __neg(self: &Arr) -> Array<uint64>;
    fn __sub(self: &Arr, other: &Arr) -> int;
}
fn opaque_carrier(a: Arr, b: Arr) -> Array<uint64> {
    return a + b;
}
fn opaque_unary(a: Arr) -> Array<uint64> {
    return -a;
}
fn erased_control(a: Arr, b: Arr) -> int {
    return a - b;
}
`

const carrierOperatorDigest = "e269b7fae061c7e5d697ca2b2f5a05288e8fd9939b9730d1e2ca61a3b1959e14"

const returnOriginUnaryRefusal = "unary callable needs an exact origin contract"

// carrierOperatorSite finds the one expression of a kind whose source text is the
// frozen snippet, and checks it sits at the frozen span.
func carrierOperatorSite(t *testing.T, u ReturnOriginUnit, kind ast.ExprKind, start, end int, snippet string) ast.ExprID {
	t.Helper()
	if carrierOperatorSource[start:end] != snippet {
		t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", start, end, snippet)
	}
	var found ast.ExprID
	for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := u.Builder.Exprs.Get(id)
		if node == nil || node.Kind != kind || int(node.Span.Start) != start || int(node.Span.End) != end {
			continue
		}
		if found.IsValid() {
			t.Fatalf("PRECONDITION: %q has more than one expression", snippet)
		}
		found = id
	}
	if !found.IsValid() {
		t.Fatalf("PRECONDITION: %q has no expression", snippet)
	}
	return found
}

func TestReturnOriginCarrierOperatorContract(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(carrierOperatorSource))); got != carrierOperatorDigest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	a := returnOriginConditionAnalyzer(t, carrierOperatorSource)
	u := a.units[0]
	if len(a.report.Diagnostics) != 0 {
		t.Fatalf("PRECONDITION: fixture has diagnostics: %+v", a.report.Diagnostics)
	}
	for _, leaf := range []struct {
		name       string
		kind       ast.ExprKind
		start, end int
		snippet    string
		reason     string
		refused    bool
	}{
		{name: "opaque_carrier", kind: ast.ExprBinary, start: 277, end: 282, snippet: "a + b", reason: returnOriginBinaryRefusal, refused: true},
		{name: "opaque_unary", kind: ast.ExprUnary, start: 340, end: 342, snippet: "-a", reason: returnOriginUnaryRefusal, refused: true},
		{name: "erased_control", kind: ast.ExprBinary, start: 400, end: 405, snippet: "a - b", reason: returnOriginBinaryRefusal, refused: false},
	} {
		t.Run(leaf.name, func(t *testing.T) {
			id := carrierOperatorSite(t, u.ReturnOriginUnit, leaf.kind, leaf.start, leaf.end, leaf.snippet)
			site := ReturnOriginPending{SourceKey: u.SourceKey, Span: u.Builder.Exprs.Get(id).Span, Reason: leaf.reason}
			if got := slices.Contains(a.report.Pending, site); got != leaf.refused {
				t.Errorf("%q carries %q = %v, want %v; pending=%+v", leaf.snippet, leaf.reason, got, leaf.refused, a.report.Pending)
			}
		})
	}
}
