package llvm

import (
	"regexp"
	"strings"
	"testing"

	"surge/internal/sema"
)

// The relinquishing walk over an array whose ELEMENT has nothing to make
// private.
//
// The call is what the rows are about. An `int64[]` has no counted elements.
// The old counted-only gate emitted nothing -- no instruction in MIR, no body
// in the module, no call at the site -- and a VIEW of one crossed into a worker
// thread that then wrote through it into the buffer the origin shard kept
// reading. The array's header is a run-time fact no type can answer for, so the
// walk now hands every array's slot to the runtime and the runtime asks its
// view registry. When the element needs no walk of its own the callback is
// `ptr null`: rt_array_unshare_walk still performs both refusals and returns
// without iterating.
//
// What must NOT move is the `float[]` shape, which already worked: it still
// passes a real element body. The rows below are a matched set, so a change
// that armed the check by making every element walk -- paying a per-element
// cost on every crossing of a plain array -- would be red here rather than
// merely slow.

// arrayWalkProbeProgram declares the shapes these rows read. Nothing crosses in
// it: the site rows below use their own programs, and these pin the BODY a
// shape gets, in isolation.
const arrayWalkProbeProgram = `
type IntHolder = { n: int64, xs: int64[] };
type PlainPair = { a: int64, b: int64 };
type CountedIntHolder = { n: int64, xs: int[] };
type CountedPair = { a: int, b: uint };

fn probe(xs: int64[], xss: int64[][], ss: string[], h: IntHolder, p: PlainPair,
         t: (int64, int64), n: int64, fs: float[], fx: Array<int64>[4],
         ints: int[], uints: uint[], nested: int[][], fixed: Array<int>[4],
         counted: CountedIntHolder, pair: CountedPair, ct: (int, uint)) -> int64 {
    return n;
}

@entrypoint
fn main() -> int { return 0; }
`

// arrayWalkCallRe reads one buffer-walk call: the slot it is handed, the
// element stride, and the per-element callback.
var arrayWalkCallRe = regexp.MustCompile(
	`call void @rt_array_unshare_walk\(ptr (%\w+), i64 (\d+), ptr (null|@unshare\.type\d+)\)`)

// arrayWalkCalls returns the (slot, stride, callback) triples of one body.
func arrayWalkCalls(t *testing.T, body string) [][]string {
	t.Helper()
	return arrayWalkCallRe.FindAllStringSubmatch(body, -1)
}

// unshareCallTargetRe names the walk body a site calls, so a row can read that
// body rather than the whole module.
var unshareCallTargetRe = regexp.MustCompile(`call void @(unshare\.type\d+)\(ptr %[^)]+\)`)

func walkCalledAtTheSite(t *testing.T, ir, body string) string {
	t.Helper()
	m := unshareCallTargetRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the site calls no walk:\n%s", body)
	}
	return bodyOf(t, ir, m[1])
}

// The bodies, shape by shape. Each row names the callback it must pass, which
// is the whole decision this change makes.
func TestUnshareWalkPassesNullForAnElementWithNothingToMakePrivate(t *testing.T) {
	labels := []string{
		"Array<int64>", "Array<string>", "Array<float>", "Array<Array<int64>>",
		"IntHolder", "ArrayFixed<Array<int64>, const 4, 4>", "float",
	}
	ir, ids, e := unshareProbeFrom(t, arrayWalkProbeProgram, labels...)

	rows := []struct {
		label      string
		wantSlotAt string
		wantStride string
		// wantCallback is either "null" or the label whose body must be passed.
		wantCallback string
		wantCalls    int
		why          string
	}{
		{"Array<int64>", "i64 0", "8", "null", 1,
			"an int64 element has nothing to make private, so the runtime is asked only about the header"},
		{"Array<string>", "i64 0", "8", "null", 1,
			"a string's move carries its one reference; the buffer still has to be refused if it is a view"},
		{"Array<float>", "i64 0", "8", "float", 1,
			"a counted element keeps the real body it had before this change"},
		{"Array<Array<int64>>", "i64 0", "8", "Array<int64>", 1,
			"the inner array is what a holder route makes a view of, so the element's own body must run"},
		{"IntHolder", "i64 8", "8", "null", 1,
			"the array is the struct's second member, so the call sits at its byte offset"},
		{"ArrayFixed<Array<int64>, const 4, 4>", "i64 0", "8", "null", 4,
			"a fixed array of arrays walks every element; a needsFixup that lost its array arm would emit none"},
	}
	for _, row := range rows {
		body := bodyOf(t, ir, unshareWalkName(ids[row.label]))
		calls := arrayWalkCalls(t, body)
		if len(calls) != row.wantCalls {
			t.Errorf("%s: %d buffer-walk call(s), want %d (%s):\n%s", row.label, len(calls), row.wantCalls, row.why, body)
			continue
		}
		wantCallback := "null"
		if row.wantCallback != "null" {
			wantCallback = "@" + unshareWalkName(ids[row.wantCallback])
		}
		for _, call := range calls {
			if call[2] != row.wantStride {
				t.Errorf("%s: element stride i64 %s, want i64 %s:\n%s", row.label, call[2], row.wantStride, body)
			}
			if call[3] != wantCallback {
				t.Errorf("%s: callback %s, want %s (%s):\n%s", row.label, call[3], wantCallback, row.why, body)
			}
		}
		if !strings.Contains(body, "getelementptr inbounds i8, ptr %val, "+row.wantSlotAt+"\n") {
			t.Errorf("%s: no slot taken at %s; the walk was handed the wrong member:\n%s", row.label, row.wantSlotAt, body)
		}
		if !e.typeNeedsRelinquishWalk(ids[row.label]) {
			t.Errorf("%s: typeNeedsRelinquishWalk=false, yet a body was emitted for it", row.label)
		}
	}

	// The float element's own body is unchanged by this lane: it still reads the
	// handle and calls the counted scalar's un-share. Read here because the row
	// above only pins that the pointer to it is passed.
	floatElem := bodyOf(t, ir, unshareWalkName(ids["Array<float>"]))
	inner := arrayWalkCalls(t, floatElem)[0][3]
	if !strings.Contains(bodyOf(t, ir, strings.TrimPrefix(inner, "@")), "call ptr @rt_bigfloat_unshare(") {
		t.Errorf("the float element's body no longer reaches rt_bigfloat_unshare:\n%s", ir)
	}
}

// A value that can neither share a counted block nor reach an array is not
// asked anything: the emitter's gate answers false, and no walk is demanded for
// it anywhere. This is the row that keeps the widening from becoming "walk
// everything".
func TestUnshareWalkIsNotArmedForAValueWithNeitherAnArrayNorACount(t *testing.T) {
	_, ids, e := unshareProbeFrom(t, arrayWalkProbeProgram,
		"PlainPair", "(int64, int64)", "int64", "Array<int64>", "int", "uint", "CountedPair", "(int, uint)")
	for _, label := range []string{"PlainPair", "(int64, int64)", "int64"} {
		if e.typeNeedsRelinquishWalk(ids[label]) {
			t.Errorf("%s: typeNeedsRelinquishWalk=true; nothing in it shares a count or reaches a buffer", label)
		}
		if e.typeMayShareCountedBlock(ids[label]) {
			t.Errorf("%s: typeMayShareCountedBlock=true, which would make the row above vacuous", label)
		}
	}
	// The control: the same emitter answers true for the shape this lane arms.
	if !e.typeNeedsRelinquishWalk(ids["Array<int64>"]) {
		t.Error("Array<int64>: typeNeedsRelinquishWalk=false; the rows above would pass on a broken predicate")
	}
	for _, label := range []string{"int", "uint", "CountedPair", "(int, uint)"} {
		if !e.typeNeedsRelinquishWalk(ids[label]) || !e.typeMayShareCountedBlock(ids[label]) {
			t.Errorf("%s must keep its counted walk", label)
		}
	}
}

func TestUnshareWalkPassesCountedNumericElementBodies(t *testing.T) {
	labels := []string{"Array<int>", "Array<uint>", "Array<Array<int>>", "ArrayFixed<Array<int>, const 4, 4>", "CountedIntHolder", "int", "uint"}
	ir, ids, _ := unshareProbeFrom(t, arrayWalkProbeProgram, labels...)
	for _, tc := range []struct {
		label, callback, kind string
		calls                 int
	}{
		{"Array<int>", "int", "int", 1},
		{"Array<uint>", "uint", "uint", 1},
		{"Array<Array<int>>", "Array<int>", "int", 1},
		{"ArrayFixed<Array<int>, const 4, 4>", "int", "int", 4},
		{"CountedIntHolder", "int", "int", 1},
	} {
		t.Run(tc.label, func(t *testing.T) {
			body := bodyOf(t, ir, unshareWalkName(ids[tc.label]))
			calls := arrayWalkCalls(t, body)
			if len(calls) != tc.calls {
				t.Fatalf("%s: got %d buffer walks, want %d:\n%s", tc.label, len(calls), tc.calls, body)
			}
			for _, call := range calls {
				if call[2] != "8" || call[3] != "@"+unshareWalkName(ids[tc.callback]) {
					t.Fatalf("%s: wrong stride or callback: %v\n%s", tc.label, call, body)
				}
			}
			if tc.callback == "Array<int>" {
				inner := bodyOf(t, ir, unshareWalkName(ids[tc.callback]))
				if nested := arrayWalkCalls(t, inner); len(nested) != 1 || nested[0][3] != "@"+unshareWalkName(ids["int"]) {
					t.Fatalf("the nested array must reach the int callback: %v\n%s", nested, inner)
				}
			}
			assertTaggedUnshareLeaf(t, bodyOf(t, ir, unshareWalkName(ids[tc.kind])), tc.kind)
		})
	}
}

// The original int[] crossing now also walks potentially counted elements.
// It still makes exactly one buffer-walk call before submitting the frame;
// the fixed-width twin below retains the null-callback header-only control.
func TestEmitBlockingCaptureOfAnIntArrayCallsTheBufferWalk(t *testing.T) {
	mod, result := lowerMIRFromSource(t, `
fn use(xs: own int[]) -> int { return xs[0] to int; }

async fn run() -> int {
    let xs: int[] = [1, 2, 3, 4];
    let job: Task<int> = blocking { ret use(own xs); };
    return compare job.await() { Success(v) => v; Cancelled() => 0; };
}

@entrypoint
fn main() -> int { return 0; }
`)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "run$poll").ID))
	assertUnshareCallPrecedes(t, body, "@rt_blocking_submit(")
	assertUnshareBodiesDefinedOnce(t, ir, body)

	walk := walkCalledAtTheSite(t, ir, body)
	calls := arrayWalkCalls(t, walk)
	if len(calls) != 1 || calls[0][3] == "null" {
		t.Fatalf("the int[] capture needs one buffer walk with an element callback: %v\n%s", calls, walk)
	}
	assertTaggedUnshareLeaf(t, bodyOf(t, ir, strings.TrimPrefix(calls[0][3], "@")), "int")
}

func TestEmitBlockingCaptureOfAPlainInt64ArrayCallsOnlyBufferWalk(t *testing.T) {
	mod, result := lowerMIRFromSource(t, `
fn use(xs: own int64[]) -> int64 { return xs[0] to int64; }
async fn run() -> int64 {
    let xs: int64[] = [1, 2, 3, 4];
    let job: Task<int64> = blocking { ret use(own xs); };
    return compare job.await() { Success(v) => v; Cancelled() => 0:int64; };
}
@entrypoint
fn main() -> int { return 0; }
`)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "run$poll").ID))
	assertUnshareCallPrecedes(t, body, "@rt_blocking_submit(")
	assertUnshareBodiesDefinedOnce(t, ir, body)
	walk := walkCalledAtTheSite(t, ir, body)
	if calls := arrayWalkCalls(t, walk); len(calls) != 1 || calls[0][3] != "null" {
		t.Fatalf("the int64[] capture needs one header walk and no element callback: %v\n%s", calls, walk)
	}
}

// The float twin of the row above, byte for byte what it was before this lane:
// the element body is real and it reaches the counted scalar's un-share. A
// change that made every element ride a null callback would ship a shared block
// and would be green everywhere else.
func TestEmitBlockingCaptureOfAFloatArrayStillPassesAnElementBody(t *testing.T) {
	mod, result := lowerMIRFromSource(t, `
fn use(xs: own float[]) -> int { return 1; }

async fn run() -> int {
    let a: float = 1.5;
    let xs: float[] = [a, a];
    let job: Task<int> = blocking { ret use(own xs); };
    let b: float = a;
    return compare job.await() { Success(v) => v; Cancelled() => 0; };
}

@entrypoint
fn main() -> int { return 0; }
`)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	if !strings.Contains(ir, "call ptr @rt_bigfloat_unshare(") {
		t.Fatalf("a float[] crossing no longer reaches the counted scalar's un-share:\n%s", ir)
	}
	arrayCalls := arrayWalkCallRe.FindAllStringSubmatch(ir, -1)
	if len(arrayCalls) != 1 || arrayCalls[0][3] == "null" {
		t.Fatalf("a float[] crossing must pass a real element body, got %v:\n%s", arrayCalls, ir)
	}
}

// A crossing whose value neither shares a count nor carries an array emits
// nothing at all: no instruction reaches the emitter, and no walk body is
// defined. The silence is the row.
func TestEmitCrossingOfAPlainCompositeEmitsNoWalk(t *testing.T) {
	mod, result := lowerMIRFromSource(t, `
type PlainPair = { a: int64, b: int64 };

fn use(p: own PlainPair) -> int64 { return p.a; }

async fn run() -> int64 {
    let p: PlainPair = PlainPair{ a: 1, b: 2 };
    let job: Task<int64> = blocking { ret use(own p); };
    return compare job.await() { Success(v) => v; Cancelled() => 0:int64; };
}


@entrypoint
fn main() -> int { return 0; }
`)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	if strings.Contains(ir, "@unshare.type") {
		t.Fatalf("a crossing of a plain pair names a walk:\n%s", ir)
	}
	if arrayWalkCallRe.MatchString(ir) {
		t.Fatalf("a crossing of a plain pair calls the buffer walk:\n%s", ir)
	}
}

// Keep the original unbounded pair beside the no-walk fixed-width control.
// Both the captured pair and its int reply now legitimately demand walks.
func TestEmitCrossingOfACountedCompositeEmitsWalk(t *testing.T) {
	mod, result := lowerMIRFromSource(t, `
type PlainPair = { a: int, b: int };

fn use(p: own PlainPair) -> int { return p.a; }

async fn run() -> int {
    let p: PlainPair = PlainPair{ a: 1, b: 2 };
    let job: Task<int> = blocking { ret use(own p); };
    return compare job.await() { Success(v) => v; Cancelled() => 0; };
}

@entrypoint
fn main() -> int { return 0; }
`)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "run$poll").ID))
	assertUnshareCallPrecedes(t, body, "@rt_blocking_submit(")
	assertUnshareBodiesDefinedOnce(t, ir, body)
	walk := walkCalledAtTheSite(t, ir, body)
	if n := strings.Count(walk, "call ptr @rt_bigint_unshare("); n != 2 {
		t.Fatalf("the captured pair needs two counted leaves, got %d:\n%s", n, walk)
	}
}

// The far-select SEND arm of an `int[]`, which is the sink whose read mode had
// to widen with the gate: a borrowing read hands back a COPY operand for every
// type, and a COPY at a boundary is the aliasing bug the shape validator names.
// The row reads the IR rather than the MIR because the aliasing it prevents is
// what the emitted call would perform.
func TestEmitFarSelectSendOfAnIntArrayWalksThePayloadOnce(t *testing.T) {
	mod, result := lowerCrossingMIRFromSource(t, `
async fn feed(ch: far Channel<int[]>, stop: far Channel<int>) -> int {
    let xs: int[] = [1, 2, 3];
    let winner = select {
        ch.send(own xs) => 1;
        stop.recv() => 2;
    };
    return winner;
}
`, sema.CrossingLoweringChannelSelect)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "feed$poll").ID))
	if calls := unshareCallRe.FindAllString(body, -1); len(calls) != 1 {
		t.Fatalf("the SEND payload is walked %d time(s), want exactly once:\n%s", len(calls), body)
	}
	assertUnshareBodiesDefinedOnce(t, ir, body)
}
