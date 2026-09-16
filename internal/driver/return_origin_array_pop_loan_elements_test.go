package driver

import (
	"slices"
	"testing"
)

// Reasons this packet does not emit, carried here because the leaves below are
// asserted as BEFORE-equality against them.
const arrayPopTemporaryOwner = "borrowed temporary has no proven storage owner"

const arrayPopGenericIndex = "generic index lacks its finalized concrete use"

const arrayPopContainerLoanBase = "container loans lack a proven base"

// A payload-free element that is itself an array or a cursor still keeps the
// storage loans its container's value records. E reads such elements through a
// certified pop, through a checked call's substitution and through its posts;
// F reads them out of a container FORMAL, where the loan is a parameter root that
// only each caller can judge, so the refusal is raised at the call rather than
// inside the callee that read it.

const arrayPopSourceE = `pragma module::dep;
fn wrap<T>(x: T) -> Array<T> {
    let mut out: Array<T> = [];
    out.push(x);
    return out;
}
fn some<T>(x: T) -> Option<T> {
    return Some::<T>(x);
}
fn move_opt<T>(dst: &mut Array<Option<T>>, src: &mut Array<T>) -> nothing {
    dst.push(src.pop());
    return nothing;
}
fn element_read() -> uint64 {
    let mut values: uint64[][] = [[6:uint64, 7:uint64]];
    let bare: uint64 = compare values.pop() { Some(inner) => inner[0]; _ => 0:uint64; };
    let mut more: uint64[][] = [[6:uint64, 7:uint64]];
    let summed: uint64 = compare more.pop() { Some(inner) => inner[0] + inner[1]; _ => 0:uint64; };
    let mut nested: uint64[][][] = [[[14:uint64]]];
    let deep: uint64 = compare nested.pop() { Some(inner) => inner[0][0]; _ => 0:uint64; };
    return bare + summed + deep;
}
fn teardown(values: &mut uint64[][]) -> uint64 {
    let mut checksum: uint64 = 0:uint64;
    checksum = checksum + compare values.pop() {
        Some(inner) => {
            let mut owned: uint64[] = inner;
            compare owned.pop() {
                Some(value) => value;
                nothing => 0:uint64;
            };
        }
        nothing => 0:uint64;
    };
    return checksum;
}
fn pop_option_view() -> Option<Option<uint64[]>> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: Array<Option<uint64[]>> = wrap::<Option<uint64[]>>(some::<uint64[]>(xs[[1..3]]));
    return views.pop();
}
fn move_literal() -> uint {
    let mut src: uint64[][] = [[1:uint64]];
    let mut dst: Array<Option<uint64[]>> = [];
    move_opt::<uint64[]>(&mut dst, &mut src);
    return dst.__len();
}
fn move_views() -> uint {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut src: Array<uint64[]> = wrap::<uint64[]>(xs[[1..3]]);
    let mut dst: Array<Option<uint64[]>> = [];
    move_opt::<uint64[]>(&mut dst, &mut src);
    return dst.__len();
}
fn pop_view_let() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: Array<uint64[]> = wrap::<uint64[]>(xs[[1..3]]);
    let v = views.pop();
    return v;
}
`

const arrayPopSourceEDigest = "221e48e7f450b2fb22a3e7b955561e1b0ad4c68d6d0b39acf20d85b65eaae88b"

const arrayPopSourceF = `pragma module::dep;
fn wrap<T>(x: T) -> Array<T> {
    let mut out: Array<T> = [];
    out.push(x);
    return out;
}
fn pop_formal(views: &mut uint64[][]) -> Option<uint64[]> {
    let v = views.pop();
    return v;
}
fn leak_formal() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    return pop_formal(&mut views);
}
fn take(views: &mut uint64[][]) -> Option<uint64[]> {
    return views.pop();
}
fn leak_take() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    let o = take(&mut views);
    return o;
}
fn take_rt(views: &mut uint64[][]) -> Option<uint64[]> {
    return rt_array_pop(views);
}
fn leak_take_rt() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    let o = take_rt(&mut views);
    return o;
}
fn leak_formal_inner() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut outer: Array<Array<uint64[]>> = wrap::<Array<uint64[]>>(wrap::<uint64[]>(xs[[1..3]]));
    let o = pop_formal(&mut outer[0]);
    return o;
}
fn pop_inner_rt() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut outer: Array<Array<uint64[]>> = wrap::<Array<uint64[]>>(wrap::<uint64[]>(xs[[1..3]]));
    let v = rt_array_pop(&mut outer[0]);
    return v;
}
fn count_views(views: &mut uint64[][]) -> uint {
    return views.__len();
}
fn count_local() -> uint {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    return count_views(&mut views);
}
fn none_for(xs: &mut uint64[]) -> Option<uint64[]> {
    return nothing;
}
fn view_none() -> Option<uint64[]> {
    let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut ys: uint64[] = fixed[[0..2]];
    return none_for(&mut ys);
}
fn take_inner(views: &mut uint64[][]) -> uint64[] {
    let o = views.pop();
    return compare o { Some(v) => v; nothing => [0:uint64]; };
}
fn leak_inner() -> uint64[] {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    return take_inner(&mut views);
}
fn shuffle(src: &mut uint64[][], dst: &mut uint64[][]) -> nothing {
    let o = src.pop();
    compare o { Some(v) => { dst.push(v); } nothing => {} };
    return nothing;
}
fn leak_shuffle() -> uint64[][] {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    let mut dst: uint64[][] = [];
    shuffle(&mut views, &mut dst);
    return dst;
}
fn cell_pop(mark: &mut &string, views: &mut uint64[][]) -> Option<uint64[]> {
    let o = views.pop();
    return o;
}
fn leak_cell(mark: &mut &string) -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    let o = cell_pop(mark, &mut views);
    return o;
}
fn put(views: &mut uint64[][], out: Option<&mut uint64[]>) -> nothing {
    let o = views.pop();
    compare out { Some(r) => { compare o { Some(v) => { *r = v; } nothing => {} }; } nothing => {} };
    return nothing;
}
fn leak_put() -> nothing {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: uint64[][] = wrap::<uint64[]>(xs[[1..3]]);
    let mut a: uint64[] = [0:uint64];
    put(&mut views, Some::<&mut uint64[]>(&mut a));
    return nothing;
}
`

const arrayPopSourceFDigest = "f28ca3c6011634e7661653ac3c0e7313d2df2dec37bf300e045b4d8eeeba4478"

// arrayPopLeakingRows is the exact-set form for a body that keeps several rows
// and still names the owner it would otherwise have handed out.
func arrayPopLeakingRows(function string, rows []backingPending, at backingEscape) backingCheck {
	return backingCheck{function: function, only: true, pending: rows, escapes: []backingEscape{at}}
}

func arrayPopLoanElementSource() arrayPopSource {
	return arrayPopSource{name: "e_loan_element_reads", text: arrayPopSourceE, digest: arrayPopSourceEDigest,
		spans: []originSpan{{418, 430, "values.pop()"}, {564, 574, "more.pop()"}, {714, 726, "nested.pop()"},
			{934, 946, "values.pop()"}, {1374, 1428, "wrap::<Option<uint64[]>>(some::<uint64[]>(xs[[1..3]]))"},
			{1399, 1427, "some::<uint64[]>(xs[[1..3]])"}, {1441, 1452, "views.pop()"},
			{1434, 1453, "return views.pop();"},
			{1267, 1328, "let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];"},
			{1579, 1619, "move_opt::<uint64[]>(&mut dst, &mut src)"},
			{1855, 1895, "move_opt::<uint64[]>(&mut dst, &mut src)"}, {2108, 2119, "views.pop()"}},
		leaves: []backingLeaf{
			arrayPopLeaf("wrap", arrayPopClean("wrap", 0)),
			arrayPopLeaf("some", arrayPopClean("some", 0)),
			arrayPopLeaf("move_opt", arrayPopClean("move_opt")),
			arrayPopLeaf("teardown", arrayPopClean("teardown")),
			// The rows at 1374:1428 and 1399:1427 belong to the landed generic call
			// guard; the row at 1441:1452 is this packet's, and the roots it keeps
			// are why the return still names the owner.
			arrayPopLeaf("pop_option_view", arrayPopLeakingRows("pop_option_view",
				[]backingPending{{1374, 1428, backingLoanDiscard}, {1399, 1427, backingLoanDiscard},
					{1441, 1452, backingLoanDiscard}},
				backingEscape{1434, 1453, "xs", 1267, 1328})),
			arrayPopLeaf("move_literal", arrayPopClean("move_literal")),
			arrayPopLeaf("move_views", arrayPopOnly("move_views", backingPending{1855, 1895, backingLoanDiscard})),
			arrayPopLeaf("pop_view_let", arrayPopOnly("pop_view_let", backingPending{2108, 2119, backingLoanDiscard})),
		}}
}

// 1 parent + 1 source + 8 table leaves + element_read = 11 RUN.
func TestAnalyzeArrayPopLoanElements(t *testing.T) {
	src := arrayPopLoanElementSource()
	if len(src.leaves) != 8 {
		t.Fatalf("PRECONDITION: frozen roster changed: leaves=%d", len(src.leaves))
	}
	t.Run(src.name, func(t *testing.T) {
		f, g := analyzeArrayPopSource(t, "array_pop_loan_elements", src, nil)
		// element_read carries the A1.2 generic-index family, which this packet
		// neither causes nor can remove (return_origin_index_primitive.go:104/:117).
		// §7.9's pre-authorized S-G1 narrowing applies: the three golden-mirror pops
		// must stay clean and none of the four texts this packet can introduce may
		// appear in the body; the A1.2 rows are logged and carried to the DEBT row.
		t.Run("element_read", func(t *testing.T) {
			within := originPendingWithin(g.analysis, f.unit.SourceKey, 300, 809)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "array_pop_element_read", "pending": within})
			for _, pop := range []originSpan{{418, 430, "values.pop()"}, {564, 574, "more.pop()"},
				{714, 726, "nested.pop()"}} {
				if originPendingAt(g.analysis, f.unit.SourceKey, pop, "") {
					t.Errorf("golden-mirror pop %q gained a refusal", pop.snippet)
				}
			}
			for _, p := range within {
				for _, reason := range arrayPopIntroducedReasons() {
					if p.Reason == reason {
						t.Errorf("element_read gained %q at %d:%d", reason, p.Span.Start, p.Span.End)
					}
				}
			}
		})
		for _, leaf := range src.leaves {
			t.Run(leaf.name, func(t *testing.T) {
				for _, check := range leaf.checks {
					checkBackingFunction(t, g, check)
				}
			})
		}
	})
}

func arrayPopLoanFormalSource() arrayPopSource {
	return arrayPopSource{name: "f_loan_formal_results", text: arrayPopSourceF, digest: arrayPopSourceFDigest,
		spans: []originSpan{{190, 201, "views.pop()"}, {397, 419, "pop_formal(&mut views)"}, {488, 499, "views.pop()"},
			{680, 696, "take(&mut views)"}, {782, 801, "rt_array_pop(views)"}, {985, 1004, "take_rt(&mut views)"},
			{1244, 1269, "pop_formal(&mut outer[0])"}, {1504, 1531, "rt_array_pop(&mut outer[0])"},
			{1792, 1815, "count_views(&mut views)"}, {2053, 2070, "none_for(&mut ys)"},
			{2385, 2407, "take_inner(&mut views)"}, {2785, 2814, "shuffle(&mut views, &mut dst)"},
			{3148, 3174, "cell_pop(mark, &mut views)"}, {1255, 1268, "&mut outer[0]"},
			{1260, 1268, "outer[0]"}, {3345, 3347, "*r"},
			{3610, 3656, "put(&mut views, Some::<&mut uint64[]>(&mut a))"}},
		leaves: []backingLeaf{
			arrayPopLeaf("wrap", arrayPopClean("wrap", 0)),
			arrayPopLeaf("pop_formal", arrayPopClean("pop_formal")),
			arrayPopLeaf("leak_formal", arrayPopOnly("leak_formal", backingPending{397, 419, backingLoanDiscard})),
			arrayPopLeaf("take", arrayPopQuiet("take")),
			arrayPopLeaf("leak_take", arrayPopOnly("leak_take", backingPending{680, 696, backingLoanDiscard})),
			arrayPopLeaf("take_rt", arrayPopQuiet("take_rt")),
			arrayPopLeaf("leak_take_rt", arrayPopOnly("leak_take_rt", backingPending{985, 1004, backingLoanDiscard})),
			// BEFORE-equality: the temporary-owner row (return_origin_expr.go:168) and
			// the generic-index row (index_primitive.go:104/:117, the A1.2 gap) are both
			// pre-existing and emitted by files this packet does not ship. Only 1244:1269
			// is P1n's. Keeping the leaf rather than dropping it preserves CF-T5/CF-B1.
			arrayPopLeaf("leak_formal_inner", arrayPopOnly("leak_formal_inner",
				backingPending{1255, 1268, arrayPopTemporaryOwner},
				backingPending{1260, 1268, arrayPopGenericIndex},
				backingPending{1244, 1269, arrayPopLoanElement})),
			// The twin of leak_formal_inner: same pre-existing pair, same reasoning.
			arrayPopLeaf("pop_inner_rt", arrayPopOnly("pop_inner_rt",
				backingPending{1517, 1530, arrayPopTemporaryOwner},
				backingPending{1522, 1530, arrayPopGenericIndex},
				backingPending{1504, 1531, arrayPopLoanElement})),
			arrayPopLeaf("count_views", arrayPopClean("count_views")),
			arrayPopLeaf("count_local", arrayPopClean("count_local")),
			arrayPopLeaf("none_for", arrayPopClean("none_for")),
			arrayPopLeaf("view_none", arrayPopClean("view_none")),
			arrayPopLeaf("leak_inner", arrayPopOnly("leak_inner", backingPending{2385, 2407, backingLoanDiscard})),
			arrayPopLeaf("leak_shuffle", arrayPopOnly("leak_shuffle", backingPending{2785, 2814, backingLoanDiscard})),
			arrayPopLeaf("leak_cell", arrayPopOnly("leak_cell", backingPending{3148, 3174, arrayPopLoanElement})),
			// leak_put no longer returns `a` (that was a move under a live exclusive
			// borrow). What pins the leaf is unchanged and is the whole point of it:
			// the loan reaches a SECOND argument, so guardLoanResult must raise G6 at
			// the call itself. CF-T8 and CF-T9 still score here.
			arrayPopLeaf("leak_put", arrayPopOnly("leak_put", backingPending{3610, 3656, backingLoanDiscard})),
		}}
}

// arrayPopCallee is one callee body that must gain no obligation its CALLER
// owns, plus any row measured as already present there before this packet.
type arrayPopCallee struct {
	span    originSpan
	allowed []backingPending
}

// The four callees: the loan each reads out of a formal is judged at its
// callers, never inside it. `put` carries one pre-existing containerLoans row
// (return_origin_backing.go:243), measured present BEFORE this packet.
func arrayPopFormalCallees() []arrayPopCallee {
	return []arrayPopCallee{
		{span: originSpan{2074, 2215, "take_inner"}},
		{span: originSpan{2411, 2584, "shuffle"}},
		{span: originSpan{2834, 2952, "cell_pop"}},
		{span: originSpan{3192, 3412, "put"},
			allowed: []backingPending{{3345, 3347, arrayPopContainerLoanBase}}},
	}
}

// 1 parent + 1 source + 17 leaves = 19 RUN.
func TestAnalyzeArrayPopLoanFormals(t *testing.T) {
	src := arrayPopLoanFormalSource()
	if len(src.leaves) != 17 {
		t.Fatalf("PRECONDITION: frozen roster changed: leaves=%d", len(src.leaves))
	}
	t.Run(src.name, func(t *testing.T) {
		f, g := analyzeArrayPopSource(t, "array_pop_loan_formals", src, nil)
		for _, callee := range arrayPopFormalCallees() {
			for _, p := range originPendingWithin(g.analysis, f.unit.SourceKey, callee.span.start, callee.span.end) {
				if slices.ContainsFunc(callee.allowed, func(a backingPending) bool { return backingPendingMatches(p, a) }) {
					continue
				}
				t.Errorf("callee %s gained an obligation its caller owns: %s at %d:%d",
					callee.span.snippet, p.Reason, p.Span.Start, p.Span.End)
			}
		}
		for _, leaf := range src.leaves {
			t.Run(leaf.name, func(t *testing.T) {
				for _, check := range leaf.checks {
					checkBackingFunction(t, g, check)
				}
			})
		}
	})
}
