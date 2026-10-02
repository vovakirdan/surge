package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

func TestChoiceCallTargetDoesNotEnableImplicitTo(t *testing.T) {
	for _, signature := range []string{"fn consume(x: string) {}\nfn test(c: bool)", "fn test(c: bool, consume: fn(string) -> nothing)"} {
		src := signature + " { consume(compare c { true => 5; false => 6; }); }"
		result, _ := bytesViewDiagnose(t, src)
		if result == nil || result.Bag == nil {
			t.Fatal("PRECONDITION: no diagnostics")
		}
		if !strings.Contains(diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false), "SEM3015") {
			t.Fatal("call accepted int branches as string without @allow_to")
		}
	}
}

func TestChoiceCallTargetRejectsBindingOnlyConversions(t *testing.T) {
	rows := []struct{ name, text string }{
		{"nested_compare", `fn consume(x: string) {}
fn test(c: bool) { consume(compare c { true => compare c { true => 5; false => 6; }; false => 7; }); }`},
		{"block_result", `fn consume(x: string) {}
fn test(c: bool) { consume(compare c { true => { ret 5; } false => { ret 6; } }); }`},
		{"fixed_to_dynamic", `fn consume(x: int[]) {}
fn test(c: bool) { let a: int[2] = [1, 2]; let b: int[2] = [3, 4]; consume(compare c { true => a; false => b; }); }`},
		{"fixed_to_dynamic_option", `fn consume(x: int[]?) {}
fn test(c: bool) { let a: int[2] = [1, 2]; let b: int[2] = [3, 4]; consume(compare c { true => a; false => b; }); }`},
		{"function_parameter_widening", `fn consume(x: fn(int16) -> nothing) {}
fn callback(x: int8) {}
fn test(c: bool) { consume(compare c { true => callback; false => callback; }); }`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			result, _ := bytesViewDiagnose(t, row.text)
			if result == nil || result.Bag == nil {
				t.Fatal("PRECONDITION: no diagnostics")
			}
			got := diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false)
			if !strings.Contains(got, "SEM3015") {
				t.Fatalf("call must reject a binding-only conversion; diagnostics:\n%s", got)
			}
		})
	}
}

// The callable's known parameter is the choice's target. Reject the absent
// branch itself, rather than first inferring Option<int> for the whole argument.
func TestChoiceCallTargetRejectsAbsentBranch(t *testing.T) {
	choiceJoinRefused(t, diag.SemaTypeMismatch, []choiceJoinRow{
		{name: "compare_value_first", snippet: "nothing;", text: `
fn consume(x: int) {}
fn test(c: bool) { consume(compare c { true => 5; false => nothing; }); }
`},
		{name: "compare_nothing_first", snippet: "nothing;", text: `
fn consume(x: int) {}
fn test(c: bool) { consume(compare c { true => nothing; false => 5; }); }
`},
		{name: "nested_parentheses", snippet: "nothing;", text: `
fn consume(x: int) {}
fn test(c: bool) { consume(((compare c { true => 5; false => nothing; }))); }
`},
		{name: "compare_empty_block", snippet: "{}", text: `
fn consume(x: int) -> nothing { return nothing; }
fn test(c: bool) { consume(compare c { true => { ret 5; } false => {} }); }
`},
		{name: "ternary_value_first", snippet: "nothing)", text: `
fn consume(x: int) {}
fn test(c: bool) { consume(c ? 5 : nothing); }
`},
		{name: "ternary_nothing_first", snippet: "nothing :", text: `
fn consume(x: int) {}
fn test(c: bool) { consume(c ? nothing : 5); }
`},
		{name: "named_reordered", snippet: "nothing;", text: `
fn consume(label: string, x: int) {}
fn test(c: bool) { consume(x: compare c { true => 5; false => nothing; }, label: "ok"); }
`},
		{name: "default_parameter", snippet: "nothing;", text: `
fn consume(x: int, extra: int = 9) {}
fn test(c: bool) { consume(compare c { true => 5; false => nothing; }); }
`},
		{name: "variadic_tail", snippet: "nothing;", text: `
fn consume(prefix: string, ...xs: int) {}
fn test(c: bool) { consume("ok", 1, compare c { true => 5; false => nothing; }); }
`},
		{name: "callable_parameter", snippet: "nothing;", text: `
fn test(c: bool, consume: fn(int) -> nothing) {
    consume(compare c { true => 5; false => nothing; });
}
`},
		{name: "callable_shadows_function", snippet: "nothing;", text: `
fn consume(x: string) {}
fn test(c: bool, consume: fn(int) -> nothing) {
    consume(compare c { true => 5; false => nothing; });
}
`},
	})
}

func TestChoiceCallTargetPreservesInference(t *testing.T) {
	choiceJoinAccepted(t, []choiceJoinRow{
		{name: "allow_to_function", text: `
@allow_to fn consume(x: string) {}
fn test(c: bool) { consume(compare c { true => 5; false => 6; }); }
`},
		{name: "allow_to_parameter", text: `
fn consume(@allow_to x: string) {}
fn test(c: bool) { consume(compare c { true => 5; false => 6; }); }
`},
		{name: "plain_target", text: `
fn consume(x: int) {}
fn test(c: bool) { consume(compare c { true => 5; false => 6; }); }
`},
		{name: "option_target", text: `
fn consume(x: int?) {}
fn test(c: bool) { consume(compare c { true => 5; false => nothing; }); }
`},
		{name: "union_target", text: `
type Maybe = int | nothing;
fn consume(x: Maybe) {}
fn test(c: bool) { consume(compare c { true => 5; false => nothing; }); }
`},
		{name: "overload_plain_first", text: `
fn consume(x: int) {}
@overload fn consume(x: int?) {}
fn test(c: bool) { consume(compare c { true => 5; false => nothing; }); }
`},
		{name: "overload_option_first", text: `
fn consume(x: int?) {}
@overload fn consume(x: int) {}
fn test(c: bool) { consume(compare c { true => 5; false => nothing; }); }
`},
		{name: "generic_inferred", text: `
fn identity<T>(x: T) -> T { return x; }
fn test(c: bool) -> int? { return identity(compare c { true => 5; false => nothing; }); }
`},
		{name: "generic_explicit", text: `
fn identity<T>(x: T) -> T { return x; }
fn test(c: bool) -> int? { return identity::<int?>(compare c { true => 5; false => nothing; }); }
`},
		{name: "unannotated_choice", text: `
fn test(c: bool) -> int? {
    let result = compare c { true => 5; false => nothing; };
    return result;
}
`},
	})
}
