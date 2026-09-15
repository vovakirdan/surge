package driver

import (
	"crypto/sha256"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
)

// Core operators and conversions on borrowed operands. Only the dependency
// module varies; every assertion is local to dep/main.sg.
const selectedOperatorSource = `pragma module::dep;
fn join_parts(a: &string, b: &string) -> string {
    return a + b;
}
fn show_number(n: &int) -> string {
    return (*n) to string;
}
fn same_item(xs: &Array<uint>, i: int, v: &uint) -> bool {
    return xs[i] == *v;
}
fn grow_text(s: string, t: &string) -> string {
    let mut out: string = s;
    out += t;
    return out;
}
fn widen_small(n: &int16) -> int64 {
    return (*n) to int32;
}
`

func TestAnalyzeSelectedOperatorOrigins(t *testing.T) {
	t.Logf("SELECTED_OPERATOR_SOURCE sha256=%x", sha256.Sum256([]byte(selectedOperatorSource)))
	f := originalGenericSignatureFixture(t, selectedOperatorSource, false, true)
	widen := strings.Index(selectedOperatorSource, "(*n) to int32")
	var cast ast.ExprID
	for raw := uint32(1); raw <= f.unit.Builder.Exprs.Arena.Len(); raw++ {
		node := f.unit.Builder.Exprs.Get(ast.ExprID(raw))
		if node != nil && node.Kind == ast.ExprCast && node.Span.File == f.owner.File.ID && int(node.Span.Start) == widen {
			cast = ast.ExprID(raw)
		}
	}
	if conv, ok := f.unit.Sema.ImplicitConversions[cast]; !cast.IsValid() || !ok || conv.Kind != sema.ImplicitConversionTo || !f.unit.Sema.ToSymbols[cast].IsValid() {
		t.Fatalf("PRECONDITION: the widened cast does not carry an implicit __to conversion on its own node")
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	var local []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == f.unit.SourceKey || pending.Span.File == f.owner.File.ID {
			local = append(local, pending)
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_operator", "pending": local, "diagnostics": analysis.Diagnostics})
	for _, d := range analysis.Diagnostics {
		if d.Primary.File == f.owner.File.ID {
			t.Errorf("unexpected dependency diagnostic: %+v", d)
		}
	}
	const binary = "binary callable needs an exact origin contract"
	const conversion = "conversion retains its actual expression for origin finalization"
	for _, tc := range []struct {
		function, site, reason string
		stays                  bool
	}{
		{"join_parts", "a + b", binary, false},
		{"show_number", "(*n) to string", conversion, false},
		{"same_item", "xs[i] == *v", binary, false},
		{"grow_text", "out += t", binary, true},
		{"widen_small", "(*n) to int32", conversion, true},
	} {
		t.Run(tc.function, func(t *testing.T) {
			start := strings.Index(selectedOperatorSource, tc.site)
			found := false
			for _, pending := range local {
				found = found || pending.SourceKey == f.unit.SourceKey && pending.Reason == tc.reason &&
					int(pending.Span.Start) == start && int(pending.Span.End) == start+len(tc.site)
			}
			if found != tc.stays {
				t.Errorf("%q refusal present=%v, want %v: %+v", tc.site, found, tc.stays, local)
			}
			if !tc.stays && tc.function != "same_item" {
				if s := requireReturnOriginSummary(t, analysis, tc.function); s.NoNormalReturn || s.Unknown || len(s.ParamSlots) != 0 {
					t.Errorf("%s summary = %+v, want a normal result with no sources", tc.function, s)
				}
			}
		})
	}
}
