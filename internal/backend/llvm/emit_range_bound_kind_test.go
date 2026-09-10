package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// The bound-kind byte, read back out of the emitted module.
//
// The byte is the whole of what a later reader holding only a `void*` knows
// about a range's two bound words: rt_range_free picks a release with it,
// rt_range_bounds_retain picks a retain, rt_range_unshare picks an unshare. A
// wrong byte is not a compile error and not a wrong answer either -- it is a
// reference count taken on a word four or eight bytes from where the count
// actually lives -- so the earliest place it can be caught is here, against the
// type the range was built from.
//
// The `Range<float>` row is the one that punishes a guess, and it is the shape
// that carried the defect: before this lane every constructor wrote the same
// object with no kind in it at all, and the integer step read a float bound as
// a SurgeBigInt.
//
// The tail of the call is what the rows match, because the two pointer operands
// are temp names that move with any unrelated emission. The inclusive flag is
// matched beside the byte on purpose: they are the last two operands of one
// call, and a row that pinned only the byte would not notice them swap.
func TestARangeConstructorNamesItsBoundKind(t *testing.T) {
	const source = `@entrypoint
fn main() -> int {
    let i = 1..4;
    let u = 1:uint..3:uint;
    let f = 1.5..2.5;
    let inc = 2.5..=4.5;
    let mut n: int = 0;
    for a: int in i { n = n + 1; }
    for b: uint in u { n = n + 1; }
    for c: float in f { n = n + 1; }
    for d: float in inc { n = n + 1; }
    return n;
}
`
	ir := emitAllocGuardProgram(t, source)
	for _, c := range []struct{ kind, tail string }{
		{"SURGE_RANGE_BOUND_INT, exclusive", ", i1 0, i8 0)"},
		{"SURGE_RANGE_BOUND_UINT, exclusive", ", i1 0, i8 1)"},
		{"SURGE_RANGE_BOUND_FLOAT, exclusive", ", i1 0, i8 2)"},
		{"SURGE_RANGE_BOUND_FLOAT, inclusive", ", i1 1, i8 2)"},
	} {
		if !strings.Contains(ir, "call ptr @rt_range_bounds_new(ptr %t") {
			t.Fatalf("the module never builds a bounded range at all:\n%s", ir)
		}
		if !strings.Contains(ir, c.tail) {
			t.Fatalf("no range constructor ends in %q (%s), so a bound kind is wrong:\n%s", c.tail, c.kind, ir)
		}
	}
	// Exactly four, so a fifth constructor smuggled in with a defaulted byte
	// shows up as a count rather than as nothing.
	if got := strings.Count(ir, "call ptr @rt_range_bounds_new("); got != 4 {
		t.Fatalf("the module builds %d bounded ranges, want 4:\n%s", got, ir)
	}
}

// The cursor takes its own references, and takes them AFTER the copy.
//
// A for-loop duplicates the range it walks by copying its bytes, which names
// the same two bound blocks twice. The retain is what makes that a second
// ownership rather than a second free at the end of the loop. Order is the
// claim as much as presence: retaining before the copy would bump the counts of
// whatever the fresh allocation happened to contain.
func TestARangeCursorRetainsTheBoundsItCopied(t *testing.T) {
	const source = `@entrypoint
fn main() -> int {
    let r = 1.5..2.5;
    let mut n: int = 0;
    for x: float in r { n = n + 1; }
    return n;
}
`
	ir := emitAllocGuardProgram(t, source)
	pair := regexp.MustCompile(
		`call void @llvm\.memcpy\.p0\.p0\.i64\(ptr align 8 (%t\d+), ptr align 8 %t\d+, i64 %t\d+, i1 false\)\n` +
			`\s*call void @rt_range_bounds_retain\(ptr (%t\d+)\)`)
	m := pair.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("the cursor's byte copy is not immediately followed by a retain of what it copied:\n%s", ir)
	}
	if m[1] != m[2] {
		t.Fatalf("the retain names %s and the copy wrote %s: the cursor retains an object it did not copy", m[2], m[1])
	}
}

// The step's arithmetic roster, per bound kind.
//
// RV2-DEBT-357 was this and nothing else: `Range<float>` compiled, fell to the
// integer roster by default, and handed rt_bigint_cmp a SurgeBigFloat, which
// read the float's words as a length and dereferenced them. The rosters are
// picked from the same three predicates that pick the byte above, so this row
// and that one go red together if a kind is lost.
//
// The COMPARISON is what the rows count, and it is the only member of the
// roster that can be counted: `add` and `from_i64` are what ordinary integer
// arithmetic in the loop body reaches too, so a float program names
// rt_bigint_add honestly. Nothing but a range step compares two bignums here.
func TestARangeStepWalksItsBoundsWithTheirOwnArithmetic(t *testing.T) {
	comparisons := []string{"rt_bigint_cmp", "rt_biguint_cmp", "rt_bigfloat_cmp"}
	cases := []struct {
		name   string
		source string
		cmp    string
		wants  []string
	}{
		{
			name:   "float",
			source: "let r = 1.5..2.5;\n    for x: float in r { n = n + 1; }",
			cmp:    "rt_bigfloat_cmp",
			wants:  []string{"@rt_bigfloat_add(", "@rt_bigfloat_from_i64(", "@rt_bigfloat_release("},
		},
		{
			name:   "uint",
			source: "let r = 1:uint..3:uint;\n    for x: uint in r { n = n + 1; }",
			cmp:    "rt_biguint_cmp",
			wants:  []string{"@rt_biguint_add(", "@rt_biguint_from_u64(", "@rt_biguint_release("},
		},
		{
			name:   "int",
			source: "let r = 1..4;\n    for x: int in r { n = n + 1; }",
			cmp:    "rt_bigint_cmp",
			wants:  []string{"@rt_bigint_add(", "@rt_bigint_from_i64(", "@rt_bigint_release("},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir := emitAllocGuardProgram(t, "@entrypoint\nfn main() -> int {\n    let mut n: int = 0;\n    "+
				tc.source+"\n    return n;\n}\n")
			for _, want := range tc.wants {
				if !strings.Contains(ir, want) {
					t.Fatalf("the step never calls %s, so this bound kind is walked with somebody "+
						"else's arithmetic:\n%s", want, ir)
				}
			}
			for _, cmp := range comparisons {
				call := "= call i32 @" + cmp + "("
				got := strings.Count(ir, call)
				want := 0
				if cmp == tc.cmp {
					want = 1
				}
				if got != want {
					t.Fatalf("the module compares with %s %d times, want %d: the step is dispatching "+
						"on somebody else's bound kind:\n%s", cmp, got, want, ir)
				}
			}
		})
	}
}

// The language's range-literal spelling keeps its own four names and its own
// arity.
//
// `[a..b]` is lowered to an ordinary call to a symbol `core/intrinsics.sg`
// declares, so its signature is part of the language's surface and widening it
// would have rippled through sema, the HIR lowering and the VM for a byte that
// is a constant on that path: the type checker holds those bounds to `int`. The
// four names say `int` and now mean it -- they reach the general constructor
// from the C side with SURGE_RANGE_BOUND_INT. This row is what stops the two
// spellings being quietly merged into one.
func TestTheRangeLiteralSpellingKeepsTheIntConstructors(t *testing.T) {
	const source = `@entrypoint
fn main() -> int {
    let a: int[] = [1, 2, 3];
    let head: int[] = a[[..2]];
    let tail: int[] = a[[1..]];
    let whole: int[] = a[[..]];
    let mid: int[] = a[[1..3]];
    return head[0] + tail[0] + whole[0] + mid[0];
}
`
	ir := emitAllocGuardProgram(t, source)
	for _, want := range []string{
		"call ptr @rt_range_int_new(",
		"call ptr @rt_range_int_from_start(",
		"call ptr @rt_range_int_to_end(",
		"call ptr @rt_range_int_full(",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the literal spelling no longer reaches %s:\n%s", want, ir)
		}
	}
	if !strings.Contains(ir, "declare ptr @rt_range_int_new(ptr, ptr, i1)") {
		t.Fatalf("the literal constructor's declared arity changed; the language's own roster in "+
			"core/intrinsics.sg would have to change with it:\n%s", ir)
	}
}
