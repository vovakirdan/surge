package mir

import (
	"testing"

	"surge/internal/source"
)

func TestOwnershipFindingSpanFallsBackToFunctionForSyntheticLocal(t *testing.T) {
	functionSpan := source.Span{File: 7, Start: 20, End: 40}
	verifier := ownershipFuncVerifier{f: &Func{
		Name:   "worker$poll",
		Span:   functionSpan,
		Locals: []Local{{Name: "__state"}},
	}}

	finding := verifier.finding(0, ownershipPoint{}, OwnershipSinkCallArg, "arg[0]", "use")
	if finding.Span != functionSpan {
		t.Fatalf("synthetic local finding span = %s, want function span %s", finding.Span, functionSpan)
	}
}

func TestOwnershipFindingSpanPrefersSourceLocal(t *testing.T) {
	functionSpan := source.Span{File: 7, Start: 20, End: 40}
	localSpan := source.Span{File: 7, Start: 28, End: 33}
	verifier := ownershipFuncVerifier{f: &Func{
		Name: "worker",
		Span: functionSpan,
		Locals: []Local{{
			Name: "value",
			Span: localSpan,
		}},
	}}

	finding := verifier.finding(0, ownershipPoint{}, OwnershipSinkReturn, "value", "use")
	if finding.Span != localSpan {
		t.Fatalf("source local finding span = %s, want local span %s", finding.Span, localSpan)
	}
}
