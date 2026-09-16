package sema

import (
	"testing"

	"surge/internal/types"
)

// The four loan predicates, read on one core-free source. `Opt` and `Has` are
// `Option` and `Some` structurally -- a union whose tag member carries one
// argument -- and the walk reads only that, so the answers are the real ones.
const returnOriginLoanTypeSource = `pragma no_std;
tag Has<T>(T);
type Opt<T> = Has(T) | nothing;
tag Duo<A, B>(A, B);
type Both<A, B> = Duo(A, B) | nothing;
fn probe(a: Opt<uint64[]>, b: Opt<Opt<uint64[]>>, c: uint64[][], d: uint64, e: uint, f: Opt<uint64>, g: Both<Opt<uint64[]>, &int64>,
    h: Both<uint64[], &int64>, i: Both<int64, &int64>, j: Opt<uint64>[], k: Both<&uint64[], &int64>, n: &uint64[], r: &Opt<uint64[]>) -> nothing {
    return nothing;
}
fn fixed(m: Opt<uint64[]>[2], p: int64[512]) -> nothing {
    return nothing;
}
fn template<T>(x: Opt<T>, y: T[], z: Both<Opt<uint64[]>, T>, w: Both<uint64[], T>, v: Both<int64, T>) -> nothing {
    return nothing;
}
`

// returnOriginLoanTypeAnswer is one formal's expected reading. `parts` is the
// walk Part A uses, which answers for the concrete parts around a parameter.
type returnOriginLoanTypeAnswer struct {
	name                         string
	erased, holds, drops, erases bool
	parts                        bool
}

// checkReturnOriginLoanTypes reads every formal of one body, and its result when
// asked, through the predicates the guards share.
func checkReturnOriginLoanTypes(t *testing.T, body string, withResult bool, want []returnOriginLoanTypeAnswer) {
	t.Helper()
	a, fn := backingFactFunction(t, returnOriginLoanTypeSource, body)
	b := &returnOriginBody{analyzer: a, function: fn}
	ids := append([]types.TypeID{}, fn.info.Params...)
	if withResult {
		ids = append(ids, fn.info.Result)
	}
	if len(ids) != len(want) {
		t.Fatalf("PRECONDITION: %s has %d readings, want %d", body, len(ids), len(want))
	}
	for i, answer := range want {
		id := ids[i]
		got := [5]bool{b.erasedType(id), b.holdsLoan(id), b.dropsLoan(id), b.erasesLoans(id),
			b.loanWalk(id, true, b.analyzer.loanCarrier)}
		expect := [5]bool{answer.erased, answer.holds, answer.drops, answer.erases, answer.parts}
		if got != expect {
			t.Errorf("%s %s: erased/holds/drops/erases/parts = %v, want %v", body, answer.name, got, expect)
		}
	}
}

func TestReturnOriginLoanTypePredicates(t *testing.T) {
	t.Run("predicates", func(t *testing.T) {
		checkReturnOriginLoanTypes(t, "probe", true, []returnOriginLoanTypeAnswer{
			{name: "a Opt<uint64[]>", erased: true, holds: true, drops: true, erases: true, parts: true},
			{name: "b Opt<Opt<uint64[]>>", erased: true, holds: true, drops: true, erases: true, parts: true},
			{name: "c uint64[][]", holds: true, parts: true},
			{name: "d uint64", erased: true, erases: true},
			{name: "e uint", erased: true, erases: true},
			{name: "f Opt<uint64>", erased: true, erases: true},
			{name: "g Both<Opt<uint64[]>, &int64>", holds: true, drops: true, erases: true, parts: true},
			{name: "h Both<uint64[], &int64>", holds: true, parts: true},
			{name: "i Both<int64, &int64>"},
			{name: "j Opt<uint64>[]", holds: true, parts: true},
			{name: "k Both<&uint64[], &int64>"},
			{name: "n &uint64[]"},
			// CF-W2's witness: the only carrier sits strictly behind the reference
			// and then behind the wrapper's tag argument, a path
			// returnOriginContainer does not short-circuit, so the walk's reference
			// stop is the single conjunct holding every column false.
			{name: "r &Opt<uint64[]>"},
			{name: "result nothing", erased: true, erases: true},
		})
	})
	t.Run("fixed", func(t *testing.T) {
		checkReturnOriginLoanTypes(t, "fixed", false, []returnOriginLoanTypeAnswer{
			{name: "m Opt<uint64[]>[2]", holds: true, drops: true, erases: true, parts: true},
			{name: "p int64[512]", holds: true, parts: true},
		})
	})
	t.Run("template_parts", func(t *testing.T) {
		checkReturnOriginLoanTypes(t, "template", false, []returnOriginLoanTypeAnswer{
			{name: "x Opt<T>"},
			{name: "y T[]", parts: true},
			{name: "z Both<Opt<uint64[]>, T>", drops: true, erases: true, parts: true},
			{name: "w Both<uint64[], T>", parts: true},
			{name: "v Both<int64, T>"},
		})
	})
}
