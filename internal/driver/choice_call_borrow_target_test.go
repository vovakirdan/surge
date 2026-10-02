package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// These are typing controls. Whole owned choices can still be refused later
// by return-origin analysis, as they were before call-target propagation.
func TestChoiceCallBorrowTargetPreservesWholeResultBorrow(t *testing.T) {
	choiceJoinAccepted(t, []choiceJoinRow{
		{name: "minted_compare", text: `
fn peek(x: &string) -> nothing { return nothing; }
fn test(c: bool) { peek(compare c { true => "a" + "b"; false => "c" + "d"; }); }
`},
		{name: "mixed_ternary", text: `
fn peek(x: &string) -> nothing { return nothing; }
fn test(c: bool, x: string) { peek(c ? "a" + "b" : x); }
`},
		{name: "callable_parameter", text: `
fn test(c: bool, a: string, b: string, peek: fn(&string) -> nothing) {
    peek(compare c { true => &a; false => &b; });
}
`},
		{name: "reference_alias", text: `
type TextRef = &string;
fn peek(x: TextRef) -> nothing { return nothing; }
fn test(c: bool) { peek(c ? "a" + "b" : "c" + "d"); }
`},
		{name: "explicit_references", text: `
fn peek(x: &string) -> nothing { return nothing; }
fn test(c: bool, a: string, b: string) { peek(compare c { true => &a; false => &b; }); }
`},
		{name: "optional_reference", text: `
fn peek(x: Option<&int>) -> nothing { return nothing; }
fn test(c: bool, a: int) { peek(compare c { true => &a; false => nothing; }); }
`},
	})
}

func TestChoiceCallBorrowTargetPreservesReferenceRestrictions(t *testing.T) {
	for _, row := range []struct {
		name, source, code string
	}{
		{"mutable_temporary", `fn peek(x: &mut string) {}
fn test(c: bool) { peek(c ? "a" + "b" : "c" + "d"); }`, "SEM3023"},
		{"shared_to_mutable", `fn peek(x: &mut int) {}
fn test(c: bool, a: int, b: int) { peek(compare c { true => &a; false => &b; }); }`, "SEM3015"},
		{"absent_last", `fn peek(x: &string) {}
fn test(c: bool) { peek(compare c { true => "a" + "b"; false => nothing; }); }`, "SEM3015"},
		{"absent_first", `fn peek(x: &string) {}
fn test(c: bool) { peek(compare c { true => nothing; false => "a" + "b"; }); }`, "SEM3015"},
	} {
		t.Run(row.name, func(t *testing.T) {
			result, _ := bytesViewDiagnose(t, row.source)
			if result == nil || result.Bag == nil {
				t.Fatal("PRECONDITION: no diagnostics")
			}
			got := diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false)
			if !strings.Contains(got, row.code) {
				t.Fatalf("want %s for invalid reference argument; diagnostics:\n%s", row.code, got)
			}
		})
	}
}

func TestChoiceCallBorrowTargetDoesNotConvertPayload(t *testing.T) {
	for _, source := range []string{
		`fn peek(x: &string) {}
fn test(c: bool) { peek(compare c { true => 5; false => 6; }); }`,
		`fn peek(x: &int[]) {}
fn test(c: bool) { let a: int[2] = [1, 2]; let b: int[2] = [3, 4]; peek(compare c { true => a; false => b; }); }`,
	} {
		result, _ := bytesViewDiagnose(t, source)
		if result == nil || result.Bag == nil {
			t.Fatal("PRECONDITION: no diagnostics")
		}
		got := diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false)
		if !strings.Contains(got, "SEM3015") {
			t.Fatalf("borrowing must not convert the choice payload; diagnostics:\n%s", got)
		}
	}
}
