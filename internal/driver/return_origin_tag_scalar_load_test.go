package driver

import (
	"testing"

	"surge/internal/ast"
)

const tagScalarLoadSource = `fn safe_at(xs: &int[], idx: int) -> Option<int> {
    return Some(xs[idx]);
}
fn keep_ref(r: &int) -> Option<&int> {
    return Some::<&int>(r);
}
`

func TestAnalyzeTagCopyScalarReferenceLoad(t *testing.T) {
	safeCall := originSpan{61, 74, "Some(xs[idx])"}
	refCall := originSpan{128, 143, "Some::<&int>(r)"}
	checkOriginSource(t, tagScalarLoadSource, "9422df9e13b809f76061140bc7f18b4368102b55196778e08050127396d8f103", safeCall, refCall)
	f, analysis := analyzeOriginRoot(t, "tag_copy_scalar_reference_load", tagScalarLoadSource, false, func(f originalGenericFixture) {
		calls := map[originSpan]ast.ExprID{}
		for raw := uint32(1); raw <= f.unit.Builder.Exprs.Arena.Len(); raw++ {
			id := ast.ExprID(raw)
			node := f.unit.Builder.Exprs.Get(id)
			if node == nil || node.Kind != ast.ExprCall {
				continue
			}
			for _, span := range []originSpan{safeCall, refCall} {
				if int(node.Span.Start) == span.start && int(node.Span.End) == span.end {
					calls[span] = id
				}
			}
		}
		for _, span := range []originSpan{safeCall, refCall} {
			call, ok := f.unit.Builder.Exprs.Call(calls[span])
			if !ok || call == nil || len(call.Args) != 1 {
				t.Fatalf("PRECONDITION: %q is not the one-argument tag call", span.snippet)
			}
			_, referenceValue := f.unit.Sema.ReferenceValueArgs[call.Args[0].Value]
			if referenceValue != (span == refCall) {
				t.Fatalf("PRECONDITION: %q reference-as-value=%t", span.snippet, referenceValue)
			}
		}
	})
	if !analysis.Complete() || len(analysis.Diagnostics) != 0 {
		t.Fatalf("tag scalar load analysis stayed unfinished: pending=%+v diagnostics=%+v", analysis.Pending, analysis.Diagnostics)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "safe_at", false, nil)
	requireOriginSummary(t, analysis, f.owner.File.ID, "keep_ref", false, []uint32{0})
}
