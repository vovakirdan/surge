package driver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/types"
)

const anonymousRecordPoint = `type Point = { x: int, y: int };

extern<Point> {
    fn __add(self: &Point, other: &Point) -> Point {
        return { x: self.x + other.x, y: self.y + other.y };
    }
}
`

const (
	anonymousRecordFieldReason     = "field of an untyped value that may carry a borrow needs a checked type"
	anonymousRecordBinaryReason    = "operator on an untyped value that may carry a borrow needs a checked type"
	anonymousRecordUncheckedReason = "untyped expression needs a checked type for its origins"
)

type anonymousRecordCase struct {
	name, src string
	literal   string // the literal the checker leaves untyped
	array     bool   // the literal is an empty array rather than an anonymous record
	operator  string // an operator over it that the checker selects nothing for
	site      string // where the verdict lands; empty for a clean row
	want      string // "" clean, "SEM3139", or the unfinished reason
}

func anonymousRecordCases() []anonymousRecordCase {
	return []anonymousRecordCase{
		{name: "let_bound_literal", src: `fn anonymous_record() -> int {
    let p = { x: 1, y: 2 };
    let p_x: int = p.x;
    let p_y: int = p.y;
    return p_x + p_y;
}
`, literal: "{ x: 1, y: 2 }"},
		{name: "add_operand_borrowed_self", src: anonymousRecordPoint + `fn main() {
    let p2: Point = { x: 3, y: 4 };
    let _ = { x: 1, y: 2 } + p2;
}
`, literal: "{ x: 1, y: 2 }", operator: "{ x: 1, y: 2 } + p2"},
		{name: "add_operand_value_overload", src: `type Point = { x: int, y: int };

extern<Point> {
    fn __add(self: &Point, other: &Point) -> Point {
        return { x: self.x + other.x, y: self.y + other.y };
    }

    @overload fn __add(self: Point, other: Point) -> Point {
        return { x: self.x + other.x, y: self.y + other.y };
    }
}

fn main() {
    let p2: Point = { x: 3, y: 4 };
    let _ = { x: 1, y: 2 } + p2;
    let _ = p2.x;
}
`, literal: "{ x: 1, y: 2 }", operator: "{ x: 1, y: 2 } + p2"},
		// A literal holds exactly its field values, so a field that borrows a
		// local dies with that local wherever the record goes.
		{name: "reference_field_block_exit", src: `fn probe() -> int {
    let kept = { let owned: int = 1; ret { r: &owned }; };
    return 1;
}
`, literal: "{ r: &owned }", site: "ret { r: &owned };", want: "SEM3139"},
		{name: "reference_field_bound_block_exit", src: `fn probe() -> int {
    let kept = { let owned: int = 1; let p = { r: &owned }; ret p; };
    return 1;
}
`, literal: "{ r: &owned }", site: "ret p;", want: "SEM3139"},
		{name: "reference_field_returned", src: `fn probe() -> int {
    let owned: int = 1;
    let p = { r: &owned };
    return p;
}
`, literal: "{ r: &owned }", site: "return p;", want: "SEM3139"},
		{name: "reference_field_through_callee", src: `fn probe(x: &int) -> int {
    let p = { r: x };
    return p;
}
fn caller() -> int {
    let kept = { let owned: int = 1; ret probe(&owned); };
    return kept;
}
`, literal: "{ r: x }", site: "ret probe(&owned);", want: "SEM3139"},
		// A field of, or a builtin operator over, a value that carries a borrow
		// would have to know whether that value is a reference: refused by name.
		{name: "borrowed_field_read_refused", src: `fn probe(x: &int) -> int {
    let p = { r: x };
    let q = p.r;
    return 1;
}
`, literal: "{ r: x }", site: "p.r", want: anonymousRecordFieldReason},
		{name: "borrowed_operand_refused", src: anonymousRecordPoint + `fn probe(x: &int) -> int {
    let p2: Point = { x: 3, y: 4 };
    let _ = { r: x } + p2;
    return 1;
}
`, literal: "{ r: x }", operator: "{ r: x } + p2", site: "{ r: x } + p2", want: anonymousRecordBinaryReason},
		{name: "untyped_assignment_refused", src: `fn probe(outside: &int) -> int {
    let mut p = { r: outside };
    {
        let owned: int = 1;
        p = { r: &owned };
    }
    return 1;
}
`, literal: "{ r: &owned }", site: "p = { r: &owned }", want: anonymousRecordUncheckedReason},
		// The same gap has one more root: an empty array literal with no expected
		// type. It holds nothing, but its binding and uses stay untyped; refused
		// by name rather than answered.
		{name: "empty_array_refused", src: `fn probe() -> int {
    let a = [];
    return 1;
}
`, literal: "[]", array: true, site: "[]", want: anonymousRecordUncheckedReason},
	}
}

// An anonymous record literal takes its type only from an expected struct
// type. Given none, the checker leaves it, and every expression that reads it,
// untyped with no diagnostic. Return-origin analysis aborted on it with
// "expression N is not typed", which failed three golden programs. These rows
// pin the transfer that answers it from the values alone: clean where nothing
// is borrowed, SEM3139 where a carried borrow outlives its owner, and a named
// refusal where a carried borrow would need the value's type.
func TestDiagnoseAnonymousRecordReturnOrigins(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Fatal("failed to locate stdlib root")
	}
	for _, tc := range anonymousRecordCases() {
		t.Run(tc.name, func(t *testing.T) {
			requireUntypedAnonymousRecord(t, tc)
			t.Setenv("SURGE_STDLIB", stdlibRoot)
			path := filepath.Join(t.TempDir(), "origin.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatal(err)
			}
			opts := DiagnoseOptions{Stage: DiagnoseStageAll, MaxDiagnostics: 64, IgnoreWarnings: true, KeepArtifacts: true}
			res, err := DiagnoseWithOptions(t.Context(), path, &opts)
			start := strings.Index(tc.src, tc.site)
			var unfinished *returnOriginUnfinishedError
			if errors.As(err, &unfinished) {
				t.Logf("ANONYMOUS_RECORD_PENDING case=%s pending=%+v", tc.name, unfinished.Pending)
				if tc.want == "" || tc.want == "SEM3139" || len(unfinished.Pending) != 1 {
					t.Fatalf("pending=%+v, want %q", unfinished.Pending, tc.want)
				}
				p := unfinished.Pending[0]
				if p.Reason != tc.want || int(p.Span.Start) != start || int(p.Span.End) != start+len(tc.site) {
					t.Fatalf("pending=%+v, want %q at %q", p, tc.want, tc.site)
				}
				return
			}
			if err != nil {
				t.Fatalf("diagnosis failed: %v", err)
			}
			items, marshalErr := json.Marshal(res.Bag.Items())
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			t.Logf("ANONYMOUS_RECORD_DIAGNOSTICS case=%s diagnostics=%s", tc.name, items)
			var errs []*diag.Diagnostic
			for _, d := range res.Bag.Items() {
				if d != nil && d.Severity >= diag.SevError {
					errs = append(errs, d)
				}
			}
			switch tc.want {
			case "":
				if len(errs) != 0 {
					t.Fatalf("errors=%s, want a clean diagnosis", items)
				}
			case "SEM3139":
				if len(errs) != 1 || errs[0].Code != diag.SemaBorrowEscapesReturn || errs[0].Primary.File != res.File.ID ||
					int(errs[0].Primary.Start) != start || int(errs[0].Primary.End) != start+len(tc.site) ||
					errs[0].Message != "borrow of 'owned' outlives its owner when this scope exits" {
					t.Fatalf("errors=%s, want exactly one SEM3139 for 'owned' at %q", items, tc.site)
				}
			default:
				t.Fatalf("errors=%s, want unfinished %q at %q", items, tc.want, tc.site)
			}
		})
	}
}

// requireUntypedAnonymousRecord checks the rows' premise on the checked source
// alone: the literal has no type, and its operator has none and selects nothing.
// If the checker starts typing them, this fails, so no row passes vacuously.
func requireUntypedAnonymousRecord(t *testing.T, tc anonymousRecordCase) {
	t.Helper()
	res := returnOriginTypedFixtureWithEscapeEvidence(t, tc.src, true)
	find := func(text string, kind ast.ExprKind) ast.ExprID {
		start := strings.Index(tc.src, text)
		for raw := uint32(1); raw <= res.Builder.Exprs.Arena.Len(); raw++ {
			node := res.Builder.Exprs.Get(ast.ExprID(raw))
			if node != nil && node.Kind == kind && int(node.Span.Start) == start && int(node.Span.End) == start+len(text) {
				return ast.ExprID(raw)
			}
		}
		return ast.NoExprID
	}
	kind := ast.ExprStruct
	if tc.array {
		kind = ast.ExprArray
	}
	literal := find(tc.literal, kind)
	if !literal.IsValid() || res.Sema.ExprTypes[literal] != types.NoTypeID {
		t.Fatalf("PRECONDITION: %q is not an untyped literal", tc.literal)
	}
	if tc.operator == "" {
		return
	}
	operator := find(tc.operator, ast.ExprBinary)
	if _, selected := res.Sema.MagicBinarySymbols[operator]; !operator.IsValid() || selected || res.Sema.ExprTypes[operator] != types.NoTypeID {
		t.Fatalf("PRECONDITION: %q is not an untyped operator with no selection", tc.operator)
	}
}
