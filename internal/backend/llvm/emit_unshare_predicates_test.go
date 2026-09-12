package llvm

import (
	"testing"

	"surge/internal/types"
)

// The emitter's predicates and sema's, in lock step over one interner. The
// emitter decides whether a relinquishing site emits a call and whether the
// walk can serve it; sema decides whether the shape may cross at all. Two walks
// that disagreed would either ship a shared block (sema admits, the emitter
// sees nothing) or refuse with a backend error instead of a diagnostic (sema
// admits, the emitter cannot serve). The label table is the second belt: two
// predicates wrong the same way still do not read as green.
//
// FOUR columns, because a value reaches the walk for two different reasons and
// can be turned away for a third. `share` is the counted-block question, which
// each side answers with its OWN walk -- the backend has no sema.Result, and
// its union arm fails closed where sema reads membership structurally -- so it
// is the column that can still drift. `private` is whether the walk can serve
// the shape at all. `walk` is what a relinquishing site actually asks: share OR
// the value carries a dynamic array. Its array half is ONE function both sides
// import (types.Interner.ContainsDynamicArray), so agreement there is exact by
// construction and the rows below only have to say what the answer is.
//
// `refused` is sema's alone and has no emitter twin on purpose: it names the
// shapes that never reach a relinquishing site because the crossing gate turns
// them away -- a map's table, a channel's ring, a task's result slot, each
// holding an array no walk can hand the runtime. It sits in this table so the
// two halves of one decision are read on one line: what is not armed here and
// carries an array is refused there, and a future container that answers false
// in both columns is the silent admission this table exists to catch.
//
// The agreement is asserted over the labelled shapes, not the whole interner:
// on a union whose membership the module never published -- a stdlib
// instantiation the program never touches -- the emitter fails CLOSED on
// purpose, where sema reads the membership structurally. A relinquishing site
// CAN reach a type no expression builds: the element of an array built empty
// (`let xs: Option<float>[] = []`) is named by the array's type alone, and the
// lowering publishes it by looking through the handle to its payload. The
// `Array<Option<float>>` row is where that stops holding.
func TestUnsharePredicatesAgreeWithSema(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, `
@copy
type C = { v: float };

@shard_movable
type P = { v: float };

tag Held(P);
tag Empty();
type U = Held(P) | Empty();

type WithArray = { xs: float[] };

type IntHolder = { n: int, xs: int[] };

fn probe(f: float, c: C, p: own P, t: (float, int), u: U, xs: float[], w: WithArray,
         fx: float[4], s: string, ch: Channel<float>, ci: Channel<int>, r: &float,
         xss: float[][], chs: Channel<float>[], m: Map<int, float>, xo: Option<float>[],
         ns: int[], nss: int[][], ss: string[], h: IntHolder, nt: (int[], int),
         nfx: Array<int>[4], nch: Channel<int>[], mi: Map<int, int[]>,
         cai: Channel<int[]>, tai: Task<int[]>, rf: Range<float>, ri: Range<int>,
         ru: Range<uint>, rs: Range<string>, n: int) -> int {
    return n;
}

@entrypoint
fn main() -> int { return 0; }
`)
	in := result.Sema.TypeInterner
	e := &Emitter{mod: mirMod, types: in}

	rows := map[string]struct{ share, private, walk, refused bool }{
		"float":        {true, true, true, false},
		"C":            {true, true, true, false},
		"P":            {true, true, true, false},
		"own P":        {true, true, true, false},
		"(float, int)": {true, true, true, false},
		"U":            {true, true, true, false},
		// A dynamic array is served by the runtime's buffer walk, through
		// nesting; it answers for its element, so an array of channels is
		// refused for the ring no walk reaches, and a map for its table. The
		// optional-float element is named by the array's type and built
		// nowhere; its membership has to reach the emitter through the array.
		"Array<float>":                  {true, true, true, false},
		"WithArray":                     {true, true, true, false},
		"Array<Array<float>>":           {true, true, true, false},
		"Array<Channel<float>>":         {true, false, true, false},
		"Map<int, float>":               {true, false, true, false},
		"Array<Option<float>>":          {true, true, true, false},
		"ArrayFixed<float, const 4, 4>": {true, true, true, false},
		"string":                        {false, true, false, false},
		"Channel<float>":                {true, false, true, false},
		"Channel<int>":                  {true, false, true, false},
		// A borrow names storage it does not carry. The emitter's kind switch
		// answers for it only if the borrow is not stripped first; it was.
		"&float": {false, true, false, false},

		// Integer leaves may now share blocks. Array<string> remains the
		// uncounted control that still needs the runtime's array-view check.
		"Array<int>":                         {true, true, true, false},
		"Array<Array<int>>":                  {true, true, true, false},
		"Array<string>":                      {false, true, true, false},
		"IntHolder":                          {true, true, true, false},
		"(Array<int>, int)":                  {true, true, true, false},
		"ArrayFixed<Array<int>, const 4, 4>": {true, true, true, false},
		"Array<Channel<int>>":                {true, false, true, false},
		"int":                                {true, true, true, false},
		// These handles hide both counted leaves and an array-view check from
		// the walk, so both independent crossing refusals apply.
		"Map<int, Array<int>>": {true, false, true, true},
		"Channel<Array<int>>":  {true, false, true, true},
		"Task<Array<int>>":     {true, false, true, true},

		// A range is the one handle whose payload the walk CAN reach: its two
		// bound words sit at fixed offsets inside one object, the object's own
		// bound byte says which of the three lifecycles they belong to, and
		// rt_range_unshare steps both. So `Range<float>` shares a counted
		// block, can be made private, and crosses -- where `Channel<float>`,
		// three rows up, shares one and cannot.
		//
		// Integer ranges share counted leaves too; the private column must
		// keep using the RANGE arm, since the generic handle arm would refuse.
		//
		// `Range<string>` is the shape no constructor builds and the type graph
		// can still spell. Its bound is not one of the three the byte can name,
		// so nothing here admits it: the range arm declines and the handle arm
		// answers as it does for any other handle. A predicate written as "is
		// this a Range" rather than "can the byte name its bounds" reads true
		// on this row.
		"Range<float>":  {true, true, true, false},
		"Range<int>":    {true, true, true, false},
		"Range<uint>":   {true, true, true, false},
		"Range<string>": {false, true, false, false},
	}
	// `float[4]` is in the table on purpose: the nominal ArrayFixed<T, N>
	// struct declares no fields, so a walker that reads only declared fields
	// sees four counted handles inline as sharing nothing. Both predicates
	// have an ArrayFixedInfo arm now; this row is where a walker that loses
	// it goes red. `Array<int>[4]` is its twin for the array half.
	seen := make(map[string]bool, len(rows))
	for id := types.TypeID(1); ; id++ {
		if _, ok := in.Lookup(id); !ok {
			break
		}
		label := types.Label(in, id)
		want, ok := rows[label]
		if !ok {
			continue
		}
		seen[label] = true
		share := e.typeMayShareCountedBlock(id)
		semaShare := result.Sema.MayShareCountedBlock(id)
		if share != semaShare {
			t.Errorf("%s (type#%d): emitter typeMayShareCountedBlock=%v, sema MayShareCountedBlock=%v", label, id, share, semaShare)
		}
		if share != want.share {
			t.Errorf("%s: typeMayShareCountedBlock=%v, want %v", label, share, want.share)
		}
		private := e.canUnshareValue(id)
		if private != want.private {
			t.Errorf("%s: canUnshareValue=%v, want %v", label, private, want.private)
		}
		// The second predicate in lock step: sema's crossing gate admits a
		// shape when this answers true, so a disagreement here is a program
		// that sema lets through and the emitter refuses with a build error.
		if semaPrivate := result.Sema.CountedBlockCanBeMadePrivate(id); semaPrivate != private {
			t.Errorf("%s (type#%d): emitter canUnshareValue=%v, sema CountedBlockCanBeMadePrivate=%v",
				label, id, private, semaPrivate)
		}
		// The third: what a relinquishing site asks. The lowering emits the
		// instruction on sema's answer and the emitter decides what to make of
		// it on its own; a disagreement is an instruction that lowers to
		// nothing, which is the silent no-op this whole family exists to avoid.
		walk := e.typeNeedsRelinquishWalk(id)
		if semaWalk := result.Sema.NeedsRelinquishWalk(id); walk != semaWalk {
			t.Errorf("%s (type#%d): emitter typeNeedsRelinquishWalk=%v, sema NeedsRelinquishWalk=%v",
				label, id, walk, semaWalk)
		}
		if walk != want.walk {
			t.Errorf("%s: typeNeedsRelinquishWalk=%v, want %v", label, walk, want.walk)
		}
		if walk != (share || in.ContainsDynamicArray(id)) {
			t.Errorf("%s: the walk question is not the OR of its two halves", label)
		}
		// The fourth column is sema's alone -- the emitter never asks it,
		// because a refused shape does not reach a relinquishing site. It is
		// pinned here so the two answers sit on one line: what the walk does
		// not arm and carries an array is what sema turns away.
		if refused := result.Sema.DynamicArrayStaysUnchecked(id); refused != want.refused {
			t.Errorf("%s: sema DynamicArrayStaysUnchecked=%v, want %v", label, refused, want.refused)
		}
	}
	for label := range rows {
		if !seen[label] {
			t.Errorf("%s: the program never produced this type, so its row pinned nothing", label)
		}
	}
}
