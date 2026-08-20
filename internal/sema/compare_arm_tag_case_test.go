package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// The code number is kicked LITERALLY, not through the constant. A test that
// reads the constant agrees with whatever the constant says, including a value
// a parallel lane took first — which is how two rules once shared a number.
const armTagNotAUnionCaseCode = 3203

func TestArmTagNotAUnionCaseCodeNumber(t *testing.T) {
	if got := int(diag.SemaArmTagNotAUnionCase); got != armTagNotAUnionCaseCode {
		t.Fatalf("SemaArmTagNotAUnionCase is %d, want %d", got, armTagNotAUnionCaseCode)
	}
}

// nestedUnionSource builds the program the rule was written against. Only user
// tags and user types are used, so the stdlib-less snippet harness really does
// type this program — a harness that quietly typed nothing would agree with any
// claim made about it.
func nestedUnionSource(arms string) string {
	return `
tag T1<A>(A);
tag T2<B>(B);
tag Outer1<C>(C);
tag Stranger<Z>(Z);

type Inner<A, B> = T1(A) | T2(B);
type Outer<A, B, C> = Outer1(C) | Inner<A, B>;

fn main() -> int {
  let o: Outer<int, int, bool> = Outer1(true);
  compare o {
` + arms + `
  }
  return 0;
}
`
}

// TestArmTagNotAUnionCaseIsRefusedInEitherArmOrder is the rule's reason to
// exist. `T1` is a case of `Inner`, and `Inner` is a NESTED member of `Outer`,
// so `T1` is not a case of `Outer`.
//
// Both orders are kicked because before this rule the ORDER decided which
// consumer broke and how — the second arm narrowed onto the nested member and
// only LLVM refused (`missing finalized union case 2`), while the first arm left
// the payload binding with no type and MIR validation refused (`local L5 (v):
// unknown type`). One arm, two different failures, varying nothing but order.
func TestArmTagNotAUnionCaseIsRefusedInEitherArmOrder(t *testing.T) {
	cases := map[string]string{
		"hoisted tag first": `    T1(v) => { return 2; };
    Outer1(v) => { return 1; };
    finally => { return 9; };`,
		"hoisted tag second": `    Outer1(v) => { return 1; };
    T1(v) => { return 2; };
    finally => { return 9; };`,
	}
	for name, arms := range cases {
		t.Run(name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, nestedUnionSource(arms))
			if parseBag.Len() != 0 {
				t.Fatalf("snippet did not parse: %v", parseBag.Items())
			}
			if !bagContainsCode(semaBag, diag.SemaArmTagNotAUnionCase) {
				t.Fatalf("expected SEM%d for a hoisted nested-union tag, got %v",
					armTagNotAUnionCaseCode, semaBag.Items())
			}
			if msg := messageForCode(semaBag, diag.SemaArmTagNotAUnionCase); !strings.Contains(msg, "T1") ||
				!strings.Contains(msg, "Outer") {
				t.Fatalf("diagnostic must name the tag and the union it is not a case of, got %q", msg)
			}
		})
	}
}

// TestArmTagNotAUnionCaseRefusesAForeignTag is the same defect with the
// nesting removed: a tag belonging to no member of the union at all was also
// accepted, and also reached MIR validation with an untyped payload binding.
func TestArmTagNotAUnionCaseRefusesAForeignTag(t *testing.T) {
	arms := `    Outer1(v) => { return 1; };
    Stranger(v) => { return 2; };
    finally => { return 9; };`
	parseBag, semaBag := runSemaOnSnippet(t, nestedUnionSource(arms))
	if parseBag.Len() != 0 {
		t.Fatalf("snippet did not parse: %v", parseBag.Items())
	}
	if !bagContainsCode(semaBag, diag.SemaArmTagNotAUnionCase) {
		t.Fatalf("expected SEM%d for a tag of no member at all, got %v",
			armTagNotAUnionCaseCode, semaBag.Items())
	}
}

// TestArmTagOfTheUnionItselfStaysLegal is the half that decides the predicate
// is right rather than merely loud. `Outer1` IS a case of `Outer`, so the same
// program shape must type clean — a rule written as "any tag in a compare over
// a union with a nested member" fails here, which is what this is for.
//
// It also proves the harness is not passing vacuously: a compare that was never
// walked cannot produce the exhaustiveness answer the last assertion depends on.
func TestArmTagOfTheUnionItselfStaysLegal(t *testing.T) {
	arms := `    Outer1(v) => { return 1; };
    finally => { return 9; };`
	parseBag, semaBag := runSemaOnSnippet(t, nestedUnionSource(arms))
	if parseBag.Len() != 0 {
		t.Fatalf("snippet did not parse: %v", parseBag.Items())
	}
	if bagContainsCode(semaBag, diag.SemaArmTagNotAUnionCase) {
		t.Fatalf("a union's own tag must stay legal, got %v", semaBag.Items())
	}
	if semaBag.Len() != 0 {
		t.Fatalf("expected a clean program, got %v", semaBag.Items())
	}

	// Drop the `finally` and the SAME compare must now be non-exhaustive: that
	// answer can only come from a compare sema actually walked over a union it
	// actually resolved.
	bare := `    Outer1(v) => { return 1; };`
	_, bareBag := runSemaOnSnippet(t, nestedUnionSource(bare))
	if !bagContainsCode(bareBag, diag.SemaNonexhaustiveMatch) {
		t.Fatalf("harness never typed the compare: expected a non-exhaustive answer, got %v",
			bareBag.Items())
	}
}

// TestNamedBindingArmIsNotATagPattern keeps `Erring<T, E>`'s bare `E` member
// reachable. `err` is a binding, not a tag, and it is the only spelling that
// catches a union-typed member — refusing it would make that member unmatchable.
func TestNamedBindingArmIsNotATagPattern(t *testing.T) {
	arms := `    Outer1(v) => { return 1; };
    err => { return 2; };`
	parseBag, semaBag := runSemaOnSnippet(t, nestedUnionSource(arms))
	if parseBag.Len() != 0 {
		t.Fatalf("snippet did not parse: %v", parseBag.Items())
	}
	if bagContainsCode(semaBag, diag.SemaArmTagNotAUnionCase) {
		t.Fatalf("a named binding is not a tag pattern, got %v", semaBag.Items())
	}
}

func messageForCode(bag *diag.Bag, code diag.Code) string {
	for _, item := range bag.Items() {
		if item.Code == code {
			return item.Message
		}
	}
	return ""
}
