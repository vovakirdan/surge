package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/sema"
	"surge/internal/types"
)

// The relinquishing sites, seen from the emitted IR.
//
// The MIR side decided WHERE an un-share sits (lower_relinquish.go) and refuses
// a boundary it did not reach (validate_relinquish.go); these rows pin what
// the backend makes of the instruction: one `call void @unshare.typeN(ptr)` on
// the value's storage, placed before the runtime call that takes the value,
// and never on a retry re-entry, plus the body the module must define for it.
//
// The programs compile through the crossing harness (driver + sema + HIR +
// MIR, no buildpipeline gate), the way the MIR rows do. What these rows read
// is the IR the emitter makes of the instruction, so the surface question --
// may this shape cross at all -- is deliberately left to the crossing tables
// in `buildpipeline`, where a counted float now rides a capture, a channel
// element and a reply alike.
//
// Most rows get their un-share from the lowering. Two hand-build one
// (unshareOfLocalInMain), and say so where they stand: one pins the walk a
// dynamic array's local emits with no crossing around it, and one pins the
// emitter's last line of defence over a map, which no compilable program is
// meant to reach and which has no other way to be shown to work.

// unshareCallRe matches a call of any relinquishing walk body.
var unshareCallRe = regexp.MustCompile(`call void @unshare\.type\d+\(ptr %[^)]+\)`)

// unshareBodyRe matches the definition line of a relinquishing walk body.
var unshareBodyRe = regexp.MustCompile(`(?m)^define void @unshare\.type\d+\(ptr %val\) \{$`)

// A far-select SEND payload that may share a counted block is un-shared in the
// crossing's initial block, once, before the FIRST rt_far_channel_select --
// the one that ships the arm table -- and not on the retry re-entry, which
// ships `ptr null, ptr null` and no payload. The mutant is an un-share emitted
// where the retry can reach it: the private temp would be walked again on
// every poll of a pending select, after the runtime already consumed it.
func TestEmitFarSelectUnsharesTheSendPayloadOnce(t *testing.T) {
	sourceCode := `
async fn counted_far_select(ch: far Channel<float>, stop: far Channel<int>) -> float {
    let value: float = 1.5;
    let winner = select {
        ch.send(value) => 1;
        stop.recv() => 2;
    };
    return value;
}
`
	mod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringChannelSelect)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "counted_far_select$poll").ID))

	calls := unshareCallRe.FindAllStringIndex(body, -1)
	if len(calls) != 1 {
		t.Fatalf("the SEND payload is un-shared %d time(s), want exactly once in the poll body:\n%s", len(calls), body)
	}
	first := strings.Index(body, "@rt_far_channel_select(")
	if first < 0 {
		t.Fatalf("the poll body never calls rt_far_channel_select:\n%s", body)
	}
	if calls[0][0] > first {
		t.Fatalf("the un-share sits after the first rt_far_channel_select; the runtime took the payload first:\n%s", body)
	}
	retry := regexp.MustCompile(`call i32 @rt_far_channel_select\(ptr null, ptr null,`).FindStringIndex(body)
	if retry == nil {
		t.Fatalf("the poll body has no retry re-entry of rt_far_channel_select:\n%s", body)
	}
	if unshareCallRe.MatchString(body[retry[0]:]) {
		t.Fatalf("an un-share is reachable from the retry re-entry:\n%s", body)
	}
	assertUnshareBodiesDefinedOnce(t, ir, body)
}

// A spawn-on body's result is un-shared before rt_async_return moves it into
// the far task's slot: the body's own frame is on the destination shard, the
// asker on another, and the result travels back across that boundary.
func TestEmitSpawnOnUnsharesTheResultBeforeAsyncReturn(t *testing.T) {
	sourceCode := `
async fn run(n: int) -> float {
    let task: far Task<float> = spawn on shard(1:ShardId) {
        let x: float = 1.5;
        let y: float = x;
        let z: int = n;
        ret y;
    };
    return compare task.await() { Success(v) => v; Cancelled() => 0.0; };
}
`
	mod, result := lowerCrossingMIRFromSource(
		t, sourceCode, sema.CrossingLoweringSpawnOn, sema.CrossingLoweringFarTaskAwait)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	poll := findSpawnOnPollFunc(t, mod)
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(poll.ID))
	assertUnshareCallPrecedes(t, body, "@rt_async_return(")
	assertUnshareBodiesDefinedOnce(t, ir, body)
}

// A blocking body's `ret` moves a private value: the un-share precedes the
// body's own return, whose value the blocking dispatch stores into the
// runtime's result destination.
func TestEmitBlockingUnsharesTheResultBeforeItReturns(t *testing.T) {
	sourceCode := `
async fn runs_a_counted_blocking_body(seed: int) -> float {
    let job: Task<float> = blocking {
        let x: float = 1.5;
        let y: float = x;
        ret y;
    };
    return compare job.await() { Success(v) => v; Cancelled() => 0.0; };
}

@entrypoint
fn main() -> int { return 0; }
`
	mod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	blocking := findFuncByPrefix(t, mod, "__blocking_block$")
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(blocking.ID))
	assertUnshareCallPrecedes(t, body, "ret ptr ")
	dispatch := findLLVMFuncBody(t, ir, "__surge_blocking_call")
	if !strings.Contains(dispatch, "call ptr @fn."+itoaMIRFuncID(blocking.ID)+"(") ||
		!strings.Contains(dispatch, "store ptr %") || !strings.Contains(dispatch, ", ptr %out, align") {
		t.Fatalf("the blocking dispatch does not store the body's returned value into %%out:\n%s", dispatch)
	}
	assertUnshareBodiesDefinedOnce(t, ir, body)
}

// A capture is un-shared in the frame that gives it up, before the runtime
// call that publishes the state: the `spawn on` caller's poll body un-shares
// the moved `own P{ v: a }` -- whose field still shares `a`'s block with the
// caller's live `a` -- once, before rt_remote_spawn_publish_placement, and
// not on the retry re-entry.
func TestEmitSpawnOnUnsharesTheCaptureBeforePublish(t *testing.T) {
	sourceCode := `
@shard_movable
type P = { v: float };

fn use(p: own P) -> int { return 1; }

async fn run() -> int {
    let a: float = 1.5;
    let p: own P = own P{ v: a };
    let task: far Task<int> = spawn on shard(1:ShardId) { ret use(own p); };
    let b: float = a;
    return compare task.await() { Success(v) => v; Cancelled() => 0; };
}
`
	mod, result := lowerCrossingMIRFromSource(
		t, sourceCode, sema.CrossingLoweringSpawnOn, sema.CrossingLoweringFarTaskAwait)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "run$poll").ID))
	assertUnshareCallPrecedes(t, body, "@rt_remote_spawn_publish_placement(")
	assertUnshareBodiesDefinedOnce(t, ir, body)
}

// The immediate `on` form, with a Copy capture: the caller's binding stays
// live, so the capture is RETAINED into a transfer temp and that temp is
// un-shared -- a clone, since the block then has two holders -- before
// rt_immediate_on_execute takes the state.
func TestEmitImmediateOnUnsharesTheCopyCaptureBeforeExecute(t *testing.T) {
	sourceCode := `
async fn run() -> int {
    let f: float = 1.5;
    let r: TaskResult<int> = on shard(1:ShardId) { let g: float = f; ret 1; };
    let h: float = f;
    return compare r { Success(v) => v; Cancelled() => 0; };
}
`
	mod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringOnPlacement)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "run$poll").ID))
	assertUnshareCallPrecedes(t, body, "@rt_immediate_on_execute(")
	assertUnshareBodiesDefinedOnce(t, ir, body)
}

// Site 4, the anchored body's `ch.send(own f)`: the capture's own reference
// leaves for the ring and NOTHING in the body touches it first. The body's
// prefix replays on every wake after a park, over a block the ring or a
// receiver may already own, so the un-share of that capture is the CALLER's
// (site 1, in the caller's poll function, once) and the anchored body holds
// none at all — the validator's act for this sink is the local's provenance
// (unpacked from the state, untouched since), not an un-share.
func TestEmitAnchoredSendLeavesTheGivenCaptureUntouchedBeforeTheRing(t *testing.T) {
	sourceCode := `
async fn run() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(1:ShardId), 4);
    let a: float = 1.5;
    let f: float = a;
    let sent: TaskResult<nothing> = on ch { ch.send(own f); ret nothing; };
    let h: float = a;
    return 0;
}
`
	mod, result := lowerCrossingMIRFromSource(t, sourceCode,
		sema.CrossingLoweringOnFarHandle, sema.CrossingLoweringChannelCreate)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	anchored := findFuncByPrefix(t, mod, "__on_anchored_block$")
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(anchored.ID))
	if !strings.Contains(body, "@rt_anchored_channel_send(") {
		t.Fatalf("the anchored body never reaches the ring:\n%s", body)
	}
	if calls := unshareCallRe.FindAllStringIndex(body, -1); len(calls) != 0 {
		t.Fatalf("the anchored body un-shares %d time(s); its prefix replays and may not touch the value:\n%s",
			len(calls), body)
	}
	caller := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "run$poll").ID))
	assertUnshareCallPrecedes(t, caller, "@rt_immediate_on_execute_anchored(")
}

// A blocking capture is un-shared on the submitting thread before
// rt_blocking_submit hands the frame to the pool.
func TestEmitBlockingUnsharesTheCaptureBeforeSubmit(t *testing.T) {
	sourceCode := `
type P = { v: float };

fn sink(p: own P) -> int { return 1; }

async fn run() -> int {
    let a: float = 1.5;
    let p: P = P{ v: a };
    let job: Task<int> = blocking { ret sink(own p); };
    let b: float = a;
    return compare job.await() { Success(v) => v; Cancelled() => 0; };
}

@entrypoint
fn main() -> int { return 0; }
`
	mod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "run$poll").ID))
	assertUnshareCallPrecedes(t, body, "@rt_blocking_submit(")
	assertUnshareBodiesDefinedOnce(t, ir, body)
}

// A module that emits an un-share assembles: the body the call names is
// defined, with the signature the call uses, and the runtime leaf it CALLS is
// declared -- the counted scalar's un-share for a float result, the buffer
// walk for a float array captured into a blocking body. The leaf is pinned as
// a call, not as a symbol: every module declares both runtime symbols, so an
// empty walk body would otherwise assemble and read as green. The text rows
// above say WHAT is defined; the toolchain is the only judge of whether it
// links.
func TestEmittedModuleWithAnUnshareAssembles(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang unavailable")
	}
	cases := []struct{ name, src, leaf string }{
		{"a float result", `
async fn runs_a_counted_blocking_body(seed: int) -> float {
    let job: Task<float> = blocking {
        let x: float = 1.5;
        ret x;
    };
    return compare job.await() { Success(v) => v; Cancelled() => 0.0; };
}

@entrypoint
fn main() -> int { return 0; }
`, "call ptr @rt_bigfloat_unshare("},
		{"a float array capture", `
fn use(xs: own float[]) -> int { return 1; }

async fn run() -> int {
    let xs: float[] = [1.5];
    let job: Task<int> = blocking { ret use(own xs); };
    return compare job.await() { Success(v) => v; Cancelled() => 0; };
}

@entrypoint
fn main() -> int { return 0; }
`, "call void @rt_array_unshare_walk("},
		{"an array of optional floats built empty", `
fn use(xs: own Option<float>[]) -> int { return 1; }

async fn run() -> int {
    let xs: Option<float>[] = [];
    let job: Task<int> = blocking { ret use(own xs); };
    return compare job.await() { Success(v) => v; Cancelled() => 0; };
}

@entrypoint
fn main() -> int { return 0; }
`, "call void @rt_array_unshare_walk("},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod, result := lowerMIRFromSource(t, tc.src)
			ir, emitErr := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
			if emitErr != nil {
				t.Fatalf("emit LLVM IR: %v", emitErr)
			}
			if !unshareCallRe.MatchString(ir) || !strings.Contains(ir, tc.leaf) {
				t.Fatalf("the module emits no un-share reaching `%s` (a declaration alone is not a walk), so assembling it proves nothing:\n%s", tc.leaf, ir)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "module.ll")
			if writeErr := os.WriteFile(path, []byte(ir), 0o600); writeErr != nil {
				t.Fatal(writeErr)
			}
			out, assembleErr := exec.Command(clang, "-x", "ir", "-c", "-o", filepath.Join(dir, "module.o"), path).CombinedOutput()
			if assembleErr != nil {
				t.Fatalf("the emitted module does not assemble: %v\n%s", assembleErr, out)
			}
		})
	}
}

// A module with no relinquishing site defines no walk body: the drain writes
// only what a function body demanded, so a program that never crosses pays
// nothing for the walk.
func TestEmitDefinesNoUnshareBodyWithoutDemand(t *testing.T) {
	mod, result := lowerMIRFromSource(t, `
@entrypoint
fn main() -> int {
    let a: float = 1.5;
    let b: float = a;
    return 0;
}
`)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	if strings.Contains(ir, "@unshare.type") {
		t.Fatalf("a program with no relinquishing site names a walk:\n%s", ir)
	}
}

// The walk over a BARE counted scalar: the layout registry carries an entry
// for `float` itself, and the body reads the handle at offset zero of the slot
// it is handed, makes it private, and writes it back. This is the shape every
// relinquished float local takes, and the synthesis had it unverified.
func TestUnshareWalkOfABareCountedScalar(t *testing.T) {
	ir, ids, e := unshareProbe(t, "float")
	if !e.typeMayShareCountedBlock(ids["float"]) || !e.canUnshareValue(ids["float"]) {
		t.Fatal("a bare float must be reported as sharing and as one the walk can make private")
	}
	body := bodyOf(t, ir, unshareWalkName(ids["float"]))
	if n := strings.Count(body, "@rt_bigfloat_unshare("); n != 1 {
		t.Fatalf("a bare float is one counted leaf; the walk called rt_bigfloat_unshare %d times:\n%s", n, body)
	}
	if !strings.Contains(body, "getelementptr inbounds i8, ptr %val, i64 0\n") {
		t.Fatalf("the walk did not read the handle at offset zero of the slot:\n%s", body)
	}
	if !strings.Contains(body, "load ptr, ptr ") || !strings.Contains(body, "store ptr ") {
		t.Fatalf("the walk must load the handle and store the private one back:\n%s", body)
	}
}

// unshareOfLocalInMain hands the emitter a hand-built `unshare` of one local
// of `main`, so a row can pin the site guard and the walk for a shape without
// finding a program whose relinquishing site produces it.
func unshareOfLocalInMain(t *testing.T, src, local string) (string, string, error) {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, src)
	main := findMIRFunc(t, mirMod, "main")
	xs := findMIRLocal(t, main, local)
	main.Blocks[0].Instrs = append([]mir.Instr{{
		Kind:    mir.InstrUnshare,
		Unshare: mir.UnshareInstr{Place: mir.Place{Kind: mir.PlaceLocal, Local: xs}},
	}}, main.Blocks[0].Instrs...)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		return "", "", err
	}
	return ir, findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(main.ID)), nil
}

// An un-share of a dynamic array's local EMITS: the local's slot holds the
// handle word, which is the value the runtime's buffer walk takes, so the
// site guard passes it through, and the body the site calls hands the buffer
// to rt_array_unshare_walk with the float body. This is the shape a blocking
// capture of `own xs` produces, pinned here without a crossing around it.
func TestUnshareOfAFloatArrayCallsTheBufferWalk(t *testing.T) {
	ir, body, err := unshareOfLocalInMain(t, `
@entrypoint
fn main() -> int {
    let xs: float[] = [];
    return 0;
}
`, "xs")
	if err != nil {
		t.Fatalf("an un-share of float[] did not emit: %v", err)
	}
	if calls := unshareCallRe.FindAllString(body, -1); len(calls) != 1 {
		t.Fatalf("main un-shares the array %d time(s), want exactly once:\n%s", len(calls), body)
	}
	assertUnshareBodiesDefinedOnce(t, ir, body)
}

// An un-share of a shape the walk cannot make private is a build failure that
// names sema's predicate, never a silent no-op. Nothing compilable reaches it:
// sema refuses to cross a value that may share a counted block no walk
// reaches, and a map's table is one. The hand-built instruction is what makes
// the backstop a fact rather than an intention.
func TestUnshareOfAMapIsRefusedNamingSemasPredicate(t *testing.T) {
	_, _, err := unshareOfLocalInMain(t, `
@entrypoint
fn main() -> int {
    let m: Map<int, float> = Map::<int, float>.new();
    return 0;
}
`, "m")
	if err == nil {
		t.Fatal("an un-share of Map<int, float> emitted; nothing walks a map's table and the emitter must refuse")
	}
	if !strings.Contains(err.Error(), "MayShareCountedBlock") {
		t.Fatalf("the refusal must name sema's predicate, got: %v", err)
	}
}

// The emitter's two predicates and sema's, in lock step over one interner. The
// emitter decides whether a relinquishing site emits a call and whether the
// walk can serve it; sema decides whether the shape may cross at all. Two
// walks that disagreed would either ship a shared block (sema admits, the
// emitter sees nothing) or refuse with a backend error instead of a diagnostic
// (sema admits, the emitter cannot serve). The label table is the second
// belt: two predicates wrong the same way still do not read as green.
//
// The agreement is asserted over the labelled shapes, not the whole interner:
// on a union whose membership the module never published -- a stdlib
// instantiation the program never touches -- the emitter fails CLOSED on
// purpose, where sema reads the membership structurally. A relinquishing site
// CAN reach a type no expression builds: the element of an array built empty
// (`let xs: Option<float>[] = []`) is named by the array's type alone, and
// the lowering publishes it by looking through the handle to its payload.
// The `Array<Option<float>>` row is where that stops holding.
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

fn probe(f: float, c: C, p: own P, t: (float, int), u: U, xs: float[], w: WithArray,
         fx: float[4], s: string, ch: Channel<float>, ci: Channel<int>, r: &float,
         xss: float[][], chs: Channel<float>[], m: Map<int, float>, xo: Option<float>[]) -> int {
    return 0;
}

@entrypoint
fn main() -> int { return 0; }
`)
	in := result.Sema.TypeInterner
	e := &Emitter{mod: mirMod, types: in}

	rows := map[string]struct{ share, private bool }{
		"float":        {true, true},
		"C":            {true, true},
		"P":            {true, true},
		"own P":        {true, true},
		"(float, int)": {true, true},
		"U":            {true, true},
		// A dynamic array is served by the runtime's buffer walk, through
		// nesting; it answers for its element, so an array of channels is
		// refused for the ring no walk reaches, and a map for its table. The
		// optional-float element is named by the array's type and built
		// nowhere; its membership has to reach the emitter through the array.
		"Array<float>":                  {true, true},
		"WithArray":                     {true, true},
		"Array<Array<float>>":           {true, true},
		"Array<Channel<float>>":         {true, false},
		"Map<int, float>":               {true, false},
		"Array<Option<float>>":          {true, true},
		"ArrayFixed<float, const 4, 4>": {true, true},
		"string":                        {false, true},
		"Channel<float>":                {true, false},
		"Channel<int>":                  {false, true},
		// A borrow names storage it does not carry. The emitter's kind switch
		// answers for it only if the borrow is not stripped first; it was.
		"&float": {false, true},
	}
	// `float[4]` is in the table on purpose: the nominal ArrayFixed<T, N>
	// struct declares no fields, so a walker that reads only declared fields
	// sees four counted handles inline as sharing nothing. Both predicates
	// have an ArrayFixedInfo arm now; this row is where a walker that loses
	// it goes red.
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
	}
	for label := range rows {
		if !seen[label] {
			t.Errorf("%s: the program never produced this type, so its row pinned nothing", label)
		}
	}
}

// assertUnshareCallPrecedes pins exactly one un-share call in a body, placed
// before the first occurrence of the runtime call (or return) that takes the
// value.
func assertUnshareCallPrecedes(t *testing.T, body, sink string) {
	t.Helper()
	calls := unshareCallRe.FindAllStringIndex(body, -1)
	if len(calls) != 1 {
		t.Fatalf("the result is un-shared %d time(s), want exactly once:\n%s", len(calls), body)
	}
	at := strings.Index(body, sink)
	if at < 0 {
		t.Fatalf("the body never reaches %q:\n%s", sink, body)
	}
	if calls[0][0] > at {
		t.Fatalf("the un-share sits after %q; the value left first:\n%s", sink, body)
	}
}

// assertUnshareBodiesDefinedOnce pins the drain: every walk the site's body
// names, and every walk those bodies name in turn -- a nested composite's by
// a call, an array element's as the function pointer handed to the runtime --
// is defined exactly once in the module, and the module defines no walk
// nobody reaches. It also pins that each body CALLS a runtime leaf (the
// counted scalar's un-share or the buffer walk), so a walk emitted for the
// wrong layout -- one with nothing counted in it -- would not read as green.
func assertUnshareBodiesDefinedOnce(t *testing.T, ir, body string) {
	t.Helper()
	nameRe := regexp.MustCompile(`@(unshare\.type\d+)\b`)
	defined := map[string]int{}
	for _, line := range unshareBodyRe.FindAllString(ir, -1) {
		defined[nameRe.FindStringSubmatch(line)[1]]++
	}
	reached := map[string]bool{}
	pending := []string{}
	for _, m := range nameRe.FindAllStringSubmatch(body, -1) {
		pending = append(pending, m[1])
	}
	if len(pending) == 0 {
		t.Fatalf("the body calls no walk:\n%s", body)
	}
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]
		if reached[name] {
			continue
		}
		reached[name] = true
		if defined[name] != 1 {
			t.Fatalf("%s is reached and defined %d time(s), want exactly once:\n%s", name, defined[name], ir)
		}
		walk := bodyOf(t, ir, name)
		if !strings.Contains(walk, "call ptr @rt_bigfloat_unshare(") && !strings.Contains(walk, "call void @rt_array_unshare_walk(") {
			t.Fatalf("%s never calls a runtime leaf; it was emitted for a layout with nothing counted in it:\n%s", name, walk)
		}
		for _, m := range nameRe.FindAllStringSubmatch(walk, -1) {
			if m[1] != name {
				pending = append(pending, m[1])
			}
		}
	}
	for name := range defined {
		if !reached[name] {
			t.Fatalf("%s is defined but nothing reachable from the site's body names it:\n%s", name, ir)
		}
	}
}

func findFuncByPrefix(t *testing.T, mod *mir.Module, prefix string) *mir.Func {
	t.Helper()
	for _, fn := range mod.Funcs {
		if fn != nil && strings.HasPrefix(fn.Name, prefix) {
			return fn
		}
	}
	t.Fatalf("missing MIR function with prefix %q", prefix)
	return nil
}
