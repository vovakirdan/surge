package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// LANGUAGE.md §2.1: "Dynamic → fixed **never implicit**. Explicit cast
// required; may trap if out of range." The rule covers every numeric family
// (int, uint, float). Overload resolution priced a dynamic numeric argument
// for a fixed-width formal as a widening (the dynamic width is
// zero, so it compared as the narrower side) and passed it through with no
// conversion: `f(b)` with `f(x: int64)` and `b: int` printed 7 on the VM and
// 15 natively, which read the dynamic integer's representation as an int64.
func TestDynamicNumericNeverReachesAFixedWidthFormalImplicitly(t *testing.T) {
	refused := map[string]string{
		"by_value": `fn f(x: int64) -> int64 { return x; }
fn probe(b: int) -> int64 { return f(b); }
`,
		"by_reference": `fn g(x: &int64) -> int64 { return *x; }
fn probe(b: int) -> int64 { return g(&b); }
`,
		"unsigned_by_value": `fn f(x: uint32) -> uint32 { return x; }
fn probe(b: uint) -> uint32 { return f(b); }
`,
		"float_by_value": `fn f(x: float32) -> float32 { return x; }
fn probe(b: float) -> float32 { return f(b); }
`,
	}
	for name, src := range refused {
		t.Run(name, func(t *testing.T) {
			bag := runOverloadSource(t, src)
			if !bagHasCode(bag, diag.SemaTypeMismatch) && !bagHasCode(bag, diag.SemaNoOverload) {
				t.Fatalf("dynamic integer reached a fixed-width formal: %s", diagnosticsSummary(bag))
			}
		})
	}
	accepted := map[string]string{
		// Fixed → dynamic of the same family stays implicit.
		"fixed_to_dynamic": `fn w(x: int) -> int { return x; }
fn probe(b: int64) -> int { return w(b); }
`,
		// A literal still fits a fixed-width formal.
		"literal": `fn f(x: int64) -> int64 { return x; }
fn probe() -> int64 { return f(5); }
`,
		// The explicit cast the rule asks for.
		"explicit_cast": `fn f(x: int64) -> int64 { return x; }
fn probe(b: int) -> int64 { return f(b to int64); }
`,
		"exact": `fn f(x: int64) -> int64 { return x; }
fn probe(b: int64) -> int64 { return f(b); }
`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			bag := runOverloadSource(t, src)
			if bag.HasErrors() {
				t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
			}
		})
	}
}

func bagHasCode(bag *diag.Bag, code diag.Code) bool {
	for _, d := range bag.Items() {
		if d.Code == code {
			return true
		}
	}
	return false
}

// The refusal names the conversion to write, and a failed argument is not
// reported again as "got unknown".
func TestDynamicNumericRefusalNamesTheConversion(t *testing.T) {
	bag := runOverloadSource(t, `fn f(x: int64) -> int64 { return x; }
fn probe(b: int) -> int64 { return f(b); }
`)
	items := bag.Items()
	if len(items) != 1 || items[0].Code != diag.SemaTypeMismatch || len(items[0].Help) != 1 ||
		!strings.Contains(items[0].Help[0].Msg, "to int64`") || len(items[0].Fixes) != 1 {
		t.Fatalf("want one refusal with a `to int64` help and fix: %s", diagnosticsSummary(bag))
	}
}
