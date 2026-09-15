package sema

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/symbols"
)

// Operators and conversions resolved to body-less extern methods. Positive rows
// drop operand origins; each control keeps its refusal for exactly one reason.
const returnOriginSelectedOperationSource = `pragma no_std;
type Pair = { n: int };
@sealed type Tagged = { n: int64 };
extern<Pair> {
    fn __add(self: &Pair, other: &Pair) -> int;
    fn __sub(self: &Pair, other: &Pair) -> Tagged;
    fn __eq(self: &Pair, other: &mut &string) -> bool;
    fn __to(self: &Pair, _: int) -> int;
    fn __mul(self: &Pair, other: &Pair) -> int { return 0; }
    fn __div(self: &Pair, other: &mut Array<int>) -> int;
}
fn plus(a: &Pair, b: &Pair) -> int { return a + b; }
fn minus(a: &Pair, b: &Pair) -> Tagged { return a - b; }
fn touches(a: &Pair, dst: &mut &string) -> bool { return a == dst; }
fn times(a: &Pair, b: &Pair) -> int { return a * b; }
fn convert(p: Pair) -> int { return p to int; }
fn cut(a: &Pair, xs: &mut Array<int>) -> int { return a / xs; }
`

const (
	returnOriginBinaryRefusal = "binary callable needs an exact origin contract"
	returnOriginCastRefusal   = "conversion retains its actual expression for origin finalization"
)

func returnOriginOperationExprs(t *testing.T, u ReturnOriginUnit) map[string]ast.ExprID {
	t.Helper()
	src := returnOriginSelectedOperationSource
	out := make(map[string]ast.ExprID)
	for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := u.Builder.Exprs.Get(id)
		if node != nil && (node.Kind == ast.ExprBinary || node.Kind == ast.ExprCast) && int(node.Span.End) <= len(src) {
			out[src[node.Span.Start:node.Span.End]] = id
		}
	}
	for _, text := range []string{"a + b", "a - b", "a == dst", "a * b", "p to int", "a / xs"} {
		if !out[text].IsValid() {
			t.Fatalf("PRECONDITION: operation %q has no typed expression", text)
		}
	}
	return out
}

func TestReturnOriginSelectedOperationPreconditions(t *testing.T) {
	a := returnOriginConditionAnalyzer(t, returnOriginSelectedOperationSource)
	u := a.units[0]
	ops := returnOriginOperationExprs(t, u.ReturnOriginUnit)
	if len(a.report.Diagnostics) != 0 {
		t.Fatalf("PRECONDITION: fixture has diagnostics: %+v", a.report.Diagnostics)
	}
	for text, body := range map[string]bool{"a + b": false, "a - b": false, "a == dst": false, "a * b": true, "a / xs": false, "p to int": false} {
		selections := u.Sema.MagicBinarySymbols
		if text == "p to int" {
			selections = u.Sema.ToSymbols
		}
		fn, reason := a.selectedCallableFunction(u, selections[ops[text]])
		if reason != "" || fn.candidate.HasBody != body || fn.item.Body.IsValid() != body {
			t.Errorf("selection for %q: reason=%q body=%v, want certified with body=%v", text, reason, fn != nil && fn.candidate.HasBody, body)
		}
	}
}

func TestReturnOriginSelectedOperationContract(t *testing.T) {
	for _, tc := range []struct {
		name, function, site, reason string
		stays                        bool
		mutate                       func(r *Result, u ReturnOriginUnit, ops map[string]ast.ExprID)
	}{
		{name: "plus", function: "plus", site: "a + b", reason: returnOriginBinaryRefusal},
		{name: "convert", function: "convert", site: "p to int", reason: returnOriginCastRefusal},
		{name: "touches", function: "touches", site: "a == dst", reason: returnOriginBinaryRefusal, stays: true},
		{name: "attributed_result_control", function: "minus", site: "a - b", reason: returnOriginBinaryRefusal, stays: true},
		{name: "body_control", function: "times", site: "a * b", reason: returnOriginBinaryRefusal, stays: true},
		{name: "container_formal_control", function: "cut", site: "a / xs", reason: returnOriginBinaryRefusal, stays: true},
		{name: "wrong_name_control", function: "plus", site: "a + b", reason: returnOriginBinaryRefusal, stays: true,
			mutate: func(r *Result, _ ReturnOriginUnit, ops map[string]ast.ExprID) {
				r.MagicBinarySymbols[ops["a + b"]] = r.ToSymbols[ops["p to int"]]
			}},
		{name: "absent_binary_selection_control", function: "plus", site: "a + b", reason: returnOriginBinaryRefusal, stays: true,
			mutate: func(r *Result, _ ReturnOriginUnit, ops map[string]ast.ExprID) {
				delete(r.MagicBinarySymbols, ops["a + b"])
			}},
		{name: "invalid_present_cast_control", function: "convert", site: "p to int", reason: returnOriginCastRefusal, stays: true,
			mutate: func(r *Result, _ ReturnOriginUnit, ops map[string]ast.ExprID) {
				r.ToSymbols[ops["p to int"]] = symbols.NoSymbolID
			}},
		// Synthetic: typing never records a conversion on an operator operand.
		{name: "implicit_conversion_operand_control", function: "plus", site: "a + b", reason: returnOriginBinaryRefusal, stays: true,
			mutate: func(r *Result, u ReturnOriginUnit, ops map[string]ast.ExprID) {
				data, _ := u.Builder.Exprs.Binary(ops["a + b"])
				r.ImplicitConversions[data.Left] = ImplicitConversion{Kind: ImplicitConversionTo}
			}},
		// Synthetic here; the driver's widened cast is the source form.
		{name: "cast_node_conversion_control", function: "convert", site: "p to int", reason: returnOriginCastRefusal, stays: true,
			mutate: func(r *Result, _ ReturnOriginUnit, ops map[string]ast.ExprID) {
				r.ImplicitConversions[ops["p to int"]] = ImplicitConversion{Kind: ImplicitConversionTo}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prepare []func(*Result, ReturnOriginUnit)
			if tc.mutate != nil {
				prepare = append(prepare, func(r *Result, u ReturnOriginUnit) {
					if r.ImplicitConversions == nil {
						r.ImplicitConversions = make(map[ast.ExprID]ImplicitConversion)
					}
					tc.mutate(r, u, returnOriginOperationExprs(t, u))
				})
			}
			a := returnOriginConditionAnalyzer(t, returnOriginSelectedOperationSource, prepare...)
			u := a.units[0]
			if len(a.report.Diagnostics) != 0 {
				t.Fatalf("PRECONDITION: fixture has diagnostics: %+v", a.report.Diagnostics)
			}
			var fn *returnOriginFunction
			for _, candidate := range a.functions {
				if candidate.name == tc.function {
					fn = candidate
				}
			}
			if fn == nil {
				t.Fatalf("PRECONDITION: body %s is missing", tc.function)
			}
			site := ReturnOriginPending{SourceKey: u.SourceKey, Span: u.Builder.Exprs.Get(returnOriginOperationExprs(t, u.ReturnOriginUnit)[tc.site]).Span, Reason: tc.reason}
			if got := slices.Contains(a.report.Pending, site); got != tc.stays {
				t.Errorf("%q refusal present=%v, want %v; pending=%+v", tc.site, got, tc.stays, a.report.Pending)
			}
			if tc.stays {
				return
			}
			for _, p := range a.report.Pending {
				if p.Span.File == fn.item.Span.File && p.Span.Start >= fn.item.Span.Start && p.Span.End <= fn.item.Span.End {
					t.Errorf("%s left unfinished: %s at %d:%d", tc.function, p.Reason, p.Span.Start, p.Span.End)
				}
			}
			for _, summary := range a.report.Summaries {
				if summary.BodyKey == fn.key && (summary.NoNormalReturn || summary.Unknown || len(summary.ParamSlots) != 0) {
					t.Errorf("%s summary = %+v, want a normal result with no sources", tc.function, summary)
				}
			}
		})
	}
}
