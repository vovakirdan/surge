package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
)

type letTuplePatternCase struct {
	name, src string
	refused   []string // each pattern refused SEM3220, in source order
}

// LANGUAGE.md §2.10 and the grammar's `Let`: a `let` binds one name, and a tuple
// pattern is written only in a `compare` arm (owner ruling 2026-09-25). Each
// refused row carries exactly its SEM3220 errors, at the pattern, and nothing
// that reads a name of the pattern adds a second error.
func letTuplePatternCases() []letTuplePatternCase {
	return []letTuplePatternCase{
		{name: "literal", src: `fn f() -> int {
    let (a, b) = (1, 2);
    return a + b;
}`, refused: []string{"(a, b)"}},
		{name: "binding_with_an_owning_element", src: `fn f() -> string {
    let pair = (1, "hello");
    let (n, s) = pair;
    let t: string = s;
    return t;
}`, refused: []string{"(n, s)"}},
		{name: "call_result", src: `fn produce() -> (int, string, bool) { return (1, "hello", true); }
fn f() -> bool {
    let (a, b, c) = produce();
    let x: int = a;
    let y: string = b;
    return c;
}`, refused: []string{"(a, b, c)"}},
		{name: "nested", src: `fn f() -> int {
    let ((a, b), c) = ((1, 2), 3);
    return a + b + c;
}`, refused: []string{"((a, b), c)"}},
		{name: "wildcard", src: `fn f() -> int {
    let (_, b) = (1, 2);
    return b;
}`, refused: []string{"(_, b)"}},
		{name: "single", src: `fn f() -> int {
    let (a,) = (5,);
    return a;
}`, refused: []string{"(a,)"}},
		// Through a reference the names would take owning elements out of a borrow.
		{name: "through_a_reference", src: `fn f(r: &(int, string)) -> int {
    let (n, s) = r;
    return n;
}`, refused: []string{"(n, s)"}},
		{name: "owned_subject", src: `fn f(t: own (int, string)) -> int {
    let (n, s) = t;
    return n;
}`, refused: []string{"(n, s)"}},
		// The shape is never compared: the pattern already carries its one error.
		{name: "arity_mismatch", src: `fn f() -> int {
    let (x, y, z) = (1, 2);
    return 0;
}`, refused: []string{"(x, y, z)"}},
		{name: "non_identifier_element", src: `fn f() -> int {
    let (1, b) = (1, 2);
    return 0;
}`, refused: []string{"(1, b)"}},
		{name: "in_a_value_block", src: `fn f() -> int {
    let v: int = {
        let (a, b) = (1, 2);
        ret a + b;
    };
    return v;
}`, refused: []string{"(a, b)"}},
		{name: "two_in_a_loop", src: `fn f() -> int {
    let mut i: int = 0;
    let mut total: int = 0;
    while i < 2 {
        let (a, b) = (i, 1);
        let (c, d) = (a, b);
        total = total + c + d;
        i = i + 1;
    }
    return total;
}`, refused: []string{"(a, b)", "(c, d)"}},
		// Controls: an index, `let _`, and a tuple pattern in a `compare` arm are the language.
		{name: "index_is_the_way", src: `fn f() -> string {
    let t = (1, "hello");
    let n: int = t.0;
    return own t.1;
}`},
		{name: "discarded_let", src: `fn f() {
    let t = (1, 2);
    let _ = t;
}`},
		{name: "compare_arm_pattern", src: `fn f(t: (int, int)) -> int {
    return compare t {
        (a, b) => a + b;
    };
}`},
	}
}

func TestLetTuplePatternIsRefused(t *testing.T) {
	for _, tc := range letTuplePatternCases() {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src + "\n"
			bag := checkLetTuplePatternSource(t, src)
			var refused, others []*diag.Diagnostic
			for _, d := range bag.Items() {
				switch {
				case d.Severity < diag.SevError:
				case d.Code == diag.SemaLetTuplePattern:
					refused = append(refused, d)
				default:
					others = append(others, d)
				}
			}
			if len(refused) != len(tc.refused) || len(others) != 0 {
				t.Fatalf("got %d SEM3220 and %d other errors, want %d and none: %s", len(refused), len(others), len(tc.refused),
					diagnosticsSummary(bag))
			}
			from := 0
			for i, d := range refused {
				start := from + strings.Index(src[from:], tc.refused[i])
				if int(d.Primary.Start) != start || int(d.Primary.End) != start+len(tc.refused[i]) {
					t.Errorf("SEM3220 %d at %d-%d, want %q at %d", i, d.Primary.Start, d.Primary.End, tc.refused[i], start)
				}
				from = start + len(tc.refused[i])
			}
		})
	}
}

// The refusal says what a `let` is, where a tuple pattern is written, and what to write instead.
func TestLetTuplePatternMessage(t *testing.T) {
	bag := checkLetTuplePatternSource(t, "fn f() -> int {\n    let (a, b) = (1, 2);\n    return a;\n}\n")
	items := bag.Items()
	if len(items) != 1 || items[0].Code != diag.SemaLetTuplePattern {
		t.Fatalf("diagnostics: %s", diagnosticsSummary(bag))
	}
	d := items[0]
	if d.Code.ID() != "SEM3220" || d.Message != "a `let` binds one name; it cannot take a tuple apart" ||
		len(d.Notes) != 1 || d.Notes[0].Msg != "tuple patterns are written only in `compare` arms" ||
		len(d.Help) != 1 || d.Help[0].Msg != "bind the tuple and read its elements: `let t = value; let x = t.0;`" {
		t.Fatalf("diagnostic = %+v", *d)
	}
}

func checkLetTuplePatternSource(t *testing.T, src string) *diag.Bag {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(32)
	Check(context.Background(), builder, fileID, Options{Reporter: &diag.BagReporter{Bag: bag}, Symbols: syms})
	return bag
}
