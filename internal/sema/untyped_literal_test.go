package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
)

const untypedLiteralPoint = `type Point = { x: int, y: int };
extern<Point> {
    fn __add(self: &Point, other: &Point) -> Point {
        return Point { x: self.x + other.x, y: self.y + other.y };
    }
}
type Bag = { n: int };
extern<Bag> {
    fn put(self: &mut Bag, p: Point) -> nothing { return nothing; }
}
fn take(p: Point) -> int { return p.x; }
fn take_all(xs: int[]) -> int { return 1; }
fn id<T>(p: T) -> T { return p; }
`

type untypedLiteralCase struct {
	name, src string
	refused   []string // each literal refused SEM3219, in source order
	help      string   // a fragment every refusal's help carries
	other     string   // the one other error code the row keeps, if any
}

// LANGUAGE.md §2.5: a struct literal without a type name, and an empty array
// literal, take their type only from an expected type (owner ruling
// 2026-09-25). Refused rows carry exactly their SEM3219 errors, at the
// literal, and nothing that reads the literal adds a second error.
func untypedLiteralCases() []untypedLiteralCase {
	return []untypedLiteralCase{
		{name: "let_without_annotation", src: `fn f() -> int {
    let p = { x: 1, y: 2 };
    let a: int = p.x;
    return a + p.y;
}`, refused: []string{"{ x: 1, y: 2 }"}, help: "let p: Point = { ... };"},
		{name: "empty_array_let", src: `fn f() -> int {
    let mut a = [];
    a.put(Point { x: 1, y: 2 });
    return take_all(a);
}`, refused: []string{"[]"}, help: "let items: int[] = [];"},
		{name: "operand_of_user_add", src: `fn f() {
    let p2: Point = { x: 3, y: 4 };
    let _ = { x: 1, y: 2 } + p2;
}`, refused: []string{"{ x: 1, y: 2 }"}},
		{name: "right_operand_of_user_add", src: `fn f() {
    let p2: Point = { x: 3, y: 4 };
    let _ = p2 + { x: 1, y: 2 };
}`, refused: []string{"{ x: 1, y: 2 }"}},
		{name: "argument", src: `fn f() -> int { return take({ x: 1, y: 2 }); }`,
			refused: []string{"{ x: 1, y: 2 }"}, help: "name the struct: `Point { ... }`"},
		{name: "empty_array_argument", src: `fn f() -> int { return take_all([]); }`,
			refused: []string{"[]"}, help: "let items: int[] = [];"},
		{name: "argument_through_binding", src: `fn f() -> int {
    let p = { x: 1, y: 2 };
    return take(p) + take(p);
}`, refused: []string{"{ x: 1, y: 2 }"}},
		{name: "method_argument", src: `fn f(b: &mut Bag) { b.put({ x: 1, y: 2 }); }`,
			refused: []string{"{ x: 1, y: 2 }"}},
		{name: "method_receiver", src: `fn f() { ({ n: 1 }).put(Point { x: 1, y: 2 }); }`,
			refused: []string{"{ n: 1 }"}, help: "let p: Point = { ... };"},
		{name: "generic_argument", src: `fn f() { let _ = id({ x: 1, y: 2 }); }`,
			refused: []string{"{ x: 1, y: 2 }"}},
		{name: "array_elements", src: `fn f() { let a = [{ x: 1, y: 2 }]; }`,
			refused: []string{"{ x: 1, y: 2 }"}},
		{name: "tuple_element", src: `fn f() { let t = ({ x: 1, y: 2 }, 2); }`,
			refused: []string{"{ x: 1, y: 2 }"}},
		{name: "nested_reports_outermost", src: `fn f() { let o = { i: { v: 1 } }; }`,
			refused: []string{"{ i: { v: 1 } }"}},
		{name: "expression_statement", src: `fn f() { ({ x: 1, y: 2 }); }`,
			refused: []string{"{ x: 1, y: 2 }"}},
		{name: "positional_and_empty", src: `fn f() { let a = { 1, 2 }; let b = {}; }`,
			refused: []string{"{ 1, 2 }", "{}"}},
		{name: "assignment_to_untyped_binding", src: `fn f() {
    let mut p = { x: 1, y: 2 };
    p = { x: 3, y: 4 };
}`, refused: []string{"{ x: 1, y: 2 }", "{ x: 3, y: 4 }"}},
		// An expected type exists: the literal takes it, and nothing is reported.
		{name: "typed_let_annotation", src: `fn f() -> int { let p: Point = { x: 1, y: 2 }; let a: int[] = []; return p.x; }`},
		{name: "typed_return", src: `fn f() -> Point { return { x: 1, y: 2 }; }`},
		{name: "typed_return_in_compare_arm", src: `fn f(n: int) -> int[] {
    compare n {
        1 => {
            return [];
            0:int;
        }
        _ => {
            return [1];
            0:int;
        }
    };
    return [];
}`},
		{name: "typed_array_elements", src: `fn f() { let a: Point[] = [{ x: 1, y: 2 }]; }`},
		{name: "typed_tuple_elements", src: `fn f() -> int { let t: (Point, int) = ({ x: 1, y: 2 }, 2); return t.1; }`},
		{name: "typed_assignment", src: `fn f() { let mut p: Point = Point { x: 1, y: 2 }; p = { x: 3, y: 4 }; }`},
		{name: "typed_field", src: `type Line = { a: Point, b: Point };
fn f() { let l = Line { a: { x: 1, y: 2 }, b: { x: 3, y: 4 } }; }`},
		// An error already covers the literal; it gets no second one.
		{name: "annotation_not_a_struct", src: `fn f() { let p: int = { x: 1, y: 2 }; }`, other: "SEM3015"},
		{name: "return_not_a_struct", src: `fn f() -> int { return { x: 1, y: 2 }; }`, other: "SEM3015"},
		{name: "tuple_element_mismatch", src: `fn f() { let t: (Point, int) = ({ x: 1, y: 2 }, true); }`, other: "SEM3015"},
		{name: "error_inside_literal", src: `fn f() { let p = { x: 1 + true }; }`, other: "SEM3016"},
	}
}

func TestUntypedLiteralNeedsExpectedType(t *testing.T) {
	for _, tc := range untypedLiteralCases() {
		t.Run(tc.name, func(t *testing.T) {
			src := untypedLiteralPoint + tc.src + "\n"
			bag := checkUntypedLiteralSource(t, src)
			var refused, others []*diag.Diagnostic
			for _, d := range bag.Items() {
				switch {
				case d.Severity < diag.SevError:
				case d.Code == diag.SemaLiteralNeedsType:
					refused = append(refused, d)
				default:
					others = append(others, d)
				}
			}
			if len(refused) != len(tc.refused) {
				t.Fatalf("got %d SEM3219, want %d: %s", len(refused), len(tc.refused), diagnosticsSummary(bag))
			}
			from := len(untypedLiteralPoint)
			for i, d := range refused {
				start := from + strings.Index(src[from:], tc.refused[i])
				if int(d.Primary.Start) != start || int(d.Primary.End) != start+len(tc.refused[i]) {
					t.Errorf("SEM3219 %d at %d-%d, want %q at %d", i, d.Primary.Start, d.Primary.End, tc.refused[i], start)
				}
				if len(d.Help) != 1 || !strings.Contains(d.Help[0].Msg, tc.help) {
					t.Errorf("SEM3219 %d help = %+v, want one containing %q", i, d.Help, tc.help)
				}
				from = start + len(tc.refused[i])
			}
			if tc.other == "" && len(others) != 0 || tc.other != "" && (len(others) != 1 || others[0].Code.ID() != tc.other) {
				t.Fatalf("other errors: %s, want %q", diagnosticsSummary(bag), tc.other)
			}
		})
	}
}

// The argument's refusal names the parameter's struct and says why an
// argument does not type the literal.
func TestUntypedLiteralArgumentMessage(t *testing.T) {
	src := untypedLiteralPoint + "fn f() -> int { return take({ x: 1, y: 2 }); }\n"
	bag := checkUntypedLiteralSource(t, src)
	items := bag.Items()
	if len(items) != 1 || items[0].Code != diag.SemaLiteralNeedsType {
		t.Fatalf("diagnostics: %s", diagnosticsSummary(bag))
	}
	d := items[0]
	if d.Message != "struct literal without a type name gets no type here" ||
		len(d.Notes) != 1 || d.Notes[0].Msg != "a parameter's type does not give a struct literal its type" ||
		len(d.Help) != 1 || d.Help[0].Msg != "name the struct: `Point { ... }`" {
		t.Fatalf("diagnostic = %+v", *d)
	}
}

func checkUntypedLiteralSource(t *testing.T, src string) *diag.Bag {
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
