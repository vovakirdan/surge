package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// Kicked literally, not through the constant; see armTagNotAUnionCaseCode.
const nonexhaustiveGuardedMatchCode = 3219

func TestNonexhaustiveGuardedMatchCodeNumber(t *testing.T) {
	if got := int(diag.SemaNonexhaustiveGuardedMatch); got != nonexhaustiveGuardedMatchCode {
		t.Fatalf("SemaNonexhaustiveGuardedMatch is %d, want %d", got, nonexhaustiveGuardedMatchCode)
	}
	if !strings.Contains(diag.SemaNonexhaustiveGuardedMatch.Title(), "non-exhaustive") {
		t.Fatalf("SEM%d has no description of its own: %q", nonexhaustiveGuardedMatchCode,
			diag.SemaNonexhaustiveGuardedMatch.Title())
	}
}

// irrefutableCompareSource wraps arms in a compare over a user union, so the
// stdlib-less snippet harness types it. `Box` is a union with exactly one
// member, which makes `Box(b)` irrefutable for it; `Maybe<int>` has two, which
// makes any single tag of it refutable.
func irrefutableCompareSource(subjectType, init, arms string) string {
	return `
tag Just<T>(T);
tag Empty();
tag Box<T>(T);
type Maybe<T> = Just(T) | Empty;
type Boxed<T> = Box(T);

fn pick(v: ` + subjectType + `) -> int {
  return compare v {
` + arms + `
  };
}

fn main() -> int {
  let v: ` + subjectType + ` = ` + init + `;
  return pick(v);
}
`
}

type irrefutableRow struct {
	name        string
	subjectType string
	init        string
	arms        string
	want        []diag.Code // codes that must be present
	absent      []diag.Code // codes that must NOT be present
}

// TestCompareExhaustivenessCountsOnlyIrrefutableUnguardedArms is the owner
// ruling of 2026-09-25 (option B) as rows: an arm covers its variant only when
// it has no guard and every payload sub-pattern is irrefutable.
func TestCompareExhaustivenessCountsOnlyIrrefutableUnguardedArms(t *testing.T) {
	guarded := diag.SemaNonexhaustiveGuardedMatch
	unmentioned := diag.SemaNonexhaustiveMatch
	redundant := diag.SemaRedundantFinally
	rows := []irrefutableRow{
		{
			name: "guarded arm does not cover", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(x) if x > 0 => x;
    Empty => 0;`,
			want: []diag.Code{guarded}, absent: []diag.Code{unmentioned},
		},
		{
			name: "guarded arm then binding arm", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(x) if x > 0 => x;
    Just(y) => y + 1;
    Empty => 0;`,
			absent: []diag.Code{guarded, unmentioned},
		},
		{
			name: "literal payload does not cover", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(1) => 10;
    Empty => 0;`,
			want: []diag.Code{guarded}, absent: []diag.Code{unmentioned},
		},
		{
			name: "literal payload then wildcard payload", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(1) => 10;
    Just(_) => 11;
    Empty => 0;`,
			absent: []diag.Code{guarded, unmentioned},
		},
		{
			name: "literal payload then wildcard arm", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(1) => 10;
    _ => 11;`,
			absent: []diag.Code{guarded, unmentioned},
		},
		{
			name: "nested refutable tag does not cover", subjectType: "Maybe<Maybe<int>>", init: "Just(Empty())",
			arms: `    Just(Just(x)) => x;
    Empty => 0;`,
			want: []diag.Code{guarded}, absent: []diag.Code{unmentioned},
		},
		{
			name: "nested tag exhaustive for its type covers", subjectType: "Maybe<Boxed<int>>", init: "Just(Box(4))",
			arms: `    Just(Box(b)) => b;
    Empty => 0;`,
			absent: []diag.Code{guarded, unmentioned},
		},
		{
			name: "tuple payload with a literal does not cover", subjectType: "Maybe<(int, int)>", init: "Just((1, 2))",
			arms: `    Just((1, y)) => y;
    Empty => 0;`,
			want: []diag.Code{guarded}, absent: []diag.Code{unmentioned},
		},
		{
			name: "tuple payload of bindings covers", subjectType: "Maybe<(int, int)>", init: "Just((1, 2))",
			arms: `    Just((x, y)) => x + y;
    Empty => 0;`,
			absent: []diag.Code{guarded, unmentioned},
		},
		{
			name: "finally closes refutable arms and is not redundant", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(1) => 10;
    Just(x) if x > 5 => x;
    Empty => 0;
    finally => 3;`,
			absent: []diag.Code{guarded, unmentioned, redundant},
		},
		{
			name: "finally after irrefutable coverage stays redundant", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(x) => x;
    Empty => 0;
    finally => 3;`,
			want: []diag.Code{redundant}, absent: []diag.Code{guarded, unmentioned},
		},
		{
			name: "unmentioned variant keeps the old code alone", subjectType: "Maybe<int>", init: "Just(3)",
			arms:   `    Just(x) => x;`,
			want:   []diag.Code{unmentioned},
			absent: []diag.Code{guarded},
		},
		{
			name: "guarded zero-payload variant does not cover", subjectType: "Maybe<int>", init: "Just(3)",
			arms: `    Just(x) => x;
    Empty if true => 0;`,
			want: []diag.Code{guarded}, absent: []diag.Code{unmentioned},
		},
		{
			name: "non-union subject is not checked, before or after", subjectType: "int", init: "3",
			arms: `    1 => 10;
    2 => 20;`,
			absent: []diag.Code{guarded, unmentioned},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, irrefutableCompareSource(row.subjectType, row.init, row.arms))
			if parseBag.Len() != 0 {
				t.Fatalf("snippet did not parse: %v", parseBag.Items())
			}
			for _, code := range row.want {
				if !bagContainsCode(semaBag, code) {
					t.Fatalf("expected %s, got %v", code.ID(), semaBag.Items())
				}
			}
			for _, code := range row.absent {
				if bagContainsCode(semaBag, code) {
					t.Fatalf("did not expect %s, got %v", code.ID(), semaBag.Items())
				}
			}
			if len(row.want) == 0 {
				for _, item := range semaBag.Items() {
					if item.Severity == diag.SevError {
						t.Fatalf("expected no errors, got %v", semaBag.Items())
					}
				}
			}
		})
	}
}

// TestNonexhaustiveGuardedMatchNamesTheVariantAndTheWayOut pins the wording:
// the headline names the variant, the help gives the irrefutable arm.
func TestNonexhaustiveGuardedMatchNamesTheVariantAndTheWayOut(t *testing.T) {
	_, semaBag := runSemaOnSnippet(t, irrefutableCompareSource("Maybe<int>", "Just(3)", `    Just(1) => 10;
    Empty => 0;`))
	var found *diag.Diagnostic
	for _, item := range semaBag.Items() {
		if item.Code == diag.SemaNonexhaustiveGuardedMatch {
			found = item
			break
		}
	}
	if found == nil {
		t.Fatalf("expected SEM%d, got %v", nonexhaustiveGuardedMatchCode, semaBag.Items())
	}
	if !strings.Contains(found.Message, "`Just`") {
		t.Fatalf("headline must name the variant, got %q", found.Message)
	}
	if len(found.Help) != 1 || !strings.Contains(found.Help[0].Msg, "`Just(_) => ...`") ||
		!strings.Contains(found.Help[0].Msg, "`finally`") {
		t.Fatalf("help must offer `Just(_) => ...` or `finally`, got %+v", found.Help)
	}
}
