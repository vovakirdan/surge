package mir_test

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/sema"
	"surge/internal/types"
)

// A blocking body reads its captures out of a state struct the submission
// literal built by CONSUMING the caller's bindings. Whether that read is a
// move or a look is the whole question of who frees the capture: the state is
// destroyed once the job is released, and a state whose field still looks
// initialized will free a string the body has already handed on.
//
// Strings and value structs transfer into the body. The int64 control owns
// nothing. Counted int/uint captures also move out: the submitting state took
// a private reference and the body owes its release. This differs from an
// ordinary counted by-value parameter, which only borrows its caller's value.
const blockingCaptureUnpackSource = crossingMIRPrelude + `
@shard_movable
type Note = { text: string };

fn peek(s: &string) -> int { return 1; }
fn read(n: &Note) -> int { return peek(n.text); }

async fn runs_a_blocking_body(seed: int) -> int {
    let msg: string = "a capture wide enough to be a block";
    let count: int64 = seed:int64;
    let count_int: int = seed;
    let count_uint: uint = seed:uint;
    let note: Note = Note { text: "and a second one inside a struct" };
    let job: Task<int> = blocking {
        ret peek(&msg) + (count:int) + count_int + (count_uint:int) + read(&note);
    };
    return compare job.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

fn main() -> int { return 0; }
`

// The caller keeps a Copy binding, but the crossing acquired its own holder
// before publication. Emptying that frame transfers the counted holder into
// the body just as it transfers the owned Movable capture.
const spawnOnCaptureUnpackSource = crossingMIRPrelude + `
fn use(m: own Movable) -> int {
    return m.id;
}

fn run(dst: Placement, m: own Movable, tally: int) -> far Task<int> {
    return spawn on dst {
        ret use(own m) + tally;
    };
}

fn main() -> int { return 0; }
`

// stateUnpacksIn collects, for every synthetic body whose name starts with
// prefix, the MoveOut flag of each read out of that body's `__state` local,
// keyed by the name of the local it initializes — which is the capture's own
// name, because the unpack reuses the enclosing binding's symbol.
func stateUnpacksIn(t *testing.T, mod *mir.Module, prefix string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	found := false
	for _, id := range mod.SortedFuncIDs() {
		f := mod.Funcs[id]
		if f == nil || !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		found = true
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueField {
					continue
				}
				src := ins.Assign.Src.Field
				obj := src.Object.Place
				if obj.Kind != mir.PlaceLocal || int(obj.Local) >= len(f.Locals) {
					continue
				}
				if f.Locals[obj.Local].Name != "__state" {
					continue
				}
				dst := ins.Assign.Dst
				if dst.Kind != mir.PlaceLocal || int(dst.Local) >= len(f.Locals) {
					continue
				}
				out[f.Locals[dst.Local].Name] = src.MoveOut
			}
		}
	}
	if !found {
		t.Fatalf("no synthetic body named %q* in module", prefix)
	}
	return out
}

func TestBlockingCaptureUnpackDeclaresTheTransfer(t *testing.T) {
	compiled := compileCrossingMIR(t, blockingCaptureUnpackSource, nil)
	unpacks := stateUnpacksIn(t, compiled.mod, "__blocking_block$")
	body := requireSyntheticBody(t, compiled.mod, "__blocking_block$")
	numericTypes := map[string]string{"count": "int64", "count_int": "int", "count_uint": "uint"}

	cases := []struct {
		capture string
		why     string
		want    bool
	}{
		{"msg", "owns heap, not reference-counted: the state took it", true},
		{"note", "a struct holding a string: the state took that too", true},
		{"count", "owns no heap: there is nothing to hand on", false},
		{"count_int", "the state retained a private int reference for the body", true},
		{"count_uint", "the state retained a private uint reference for the body", true},
	}
	for _, tc := range cases {
		t.Run(tc.capture, func(t *testing.T) {
			if wantType, numeric := numericTypes[tc.capture]; numeric {
				typ := body.Locals[namedLocal(t, body, tc.capture)].Type
				if got := types.Label(compiled.types, typ); got != wantType {
					t.Fatalf("capture %s type=%s, want %s", tc.capture, got, wantType)
				}
				if got := compiled.types.IsRefCountedScalar(typ); got != tc.want {
					t.Fatalf("capture %s counted=%v, want %v", tc.capture, got, tc.want)
				}
			}
			got, ok := unpacks[tc.capture]
			if !ok {
				t.Fatalf("capture %q was never unpacked from the blocking state (unpacks: %v)",
					tc.capture, unpacks)
			}
			if got != tc.want {
				t.Errorf("capture %q: MoveOut = %v, want %v (%s)",
					tc.capture, got, tc.want, tc.why)
			}
		})
	}
}

// The membership a relinquishing walk reads is published from the capture's
// TYPE. An array built empty names its element union nowhere else in the
// function, and a module that published memberships only for the unions an
// operand touched left the walk without one: sema admitted `own xs`, the
// emitter refused it as a build error naming sema's predicate. The lowering
// looks through a handle to its payload, so the element union is on the
// module with every member and the index the layout stamps on it.
func TestBlockingCaptureOfAnArrayBuiltEmptyPublishesItsElementUnion(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
fn use(xs: own Option<float>[]) -> int { return 1; }

async fn runs_a_blocking_body(seed: int) -> int {
    let xs: Option<float>[] = [];
    let job: Task<int> = blocking { ret use(own xs); };
    return compare job.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

fn main() -> int { return 0; }
`, nil)
	elem := types.NoTypeID
	for id := types.TypeID(1); ; id++ {
		tt, ok := compiled.types.Lookup(id)
		if !ok {
			break
		}
		if tt.Kind == types.KindUnion && types.Label(compiled.types, id) == "Option<float>" {
			elem = id
			break
		}
	}
	if elem == types.NoTypeID {
		t.Fatal("the program never produced Option<float>, so this row pins nothing")
	}
	if compiled.mod.Meta == nil {
		t.Fatal("the lowering published no module metadata")
	}
	cases, ok := compiled.mod.Meta.UnionCases[elem]
	if !ok {
		t.Fatalf("the module publishes no membership for Option<float> (type#%d), the element of an array the function builds empty; the walk that un-shares the capture cannot switch on its tag (published: %d unions)",
			elem, len(compiled.mod.Meta.UnionCases))
	}
	if len(cases) != 2 {
		t.Fatalf("Option<float> has two members, Some(float) and None; the module published %d", len(cases))
	}
}

func TestSpawnOnCaptureUnpackDeclaresTheTransfer(t *testing.T) {
	compiled := compileCrossingMIR(t, spawnOnCaptureUnpackSource,
		map[sema.CrossingLoweringKind]bool{sema.CrossingLoweringSpawnOn: true})
	unpacks := stateUnpacksIn(t, compiled.mod, "__spawn_on_block$")

	cases := []struct {
		capture string
		why     string
		want    bool
	}{
		{"m", "an owned shard-movable is MOVED into the state", true},
		{"tally", "the frame gives its private counted holder to the body", true},
	}
	for _, tc := range cases {
		t.Run(tc.capture, func(t *testing.T) {
			got, ok := unpacks[tc.capture]
			if !ok {
				t.Fatalf("capture %q was never unpacked from the spawn_on state (unpacks: %v)",
					tc.capture, unpacks)
			}
			if got != tc.want {
				t.Errorf("capture %q: MoveOut = %v, want %v (%s)",
					tc.capture, got, tc.want, tc.why)
			}
		})
	}
}

// requireSpawnOnTallyConstruction follows the one tally capture in the fixed
// run fixture. Its frame field must receive the exact holder retained from the
// caller's parameter and unshared before publication, not a nearby retain of
// another value. The int64 row needs no holder and stays a direct Copy.
func requireSpawnOnTallyConstruction(t *testing.T, compiled crossingMIRCompileResult, poll *mir.Func, counted bool) {
	t.Helper()
	var caller *mir.Func
	for _, f := range compiled.mod.Funcs {
		if f != nil && f.Name == "run" {
			if caller != nil {
				t.Fatal("more than one run constructor")
			}
			caller = f
		}
	}
	if caller == nil {
		t.Fatal("missing run constructor")
	}
	source := namedLocal(t, caller, "tally")
	pollTally := namedLocal(t, poll, "tally")
	if int(source) >= caller.ParamCount || caller.Locals[source].Sym == 0 ||
		caller.Locals[source].Sym != poll.Locals[pollTally].Sym || caller.Locals[source].Type != poll.Locals[pollTally].Type {
		t.Fatal("poll tally is not the constructor's typed tally parameter")
	}
	var block *mir.Block
	crossingAt := -1
	for bi := range caller.Blocks {
		for ii, ins := range caller.Blocks[bi].Instrs {
			if ins.Kind == mir.InstrCrossing && ins.Crossing.BodyFuncID == poll.ID {
				if block != nil {
					t.Fatal("more than one publication of this poll")
				}
				block, crossingAt = &caller.Blocks[bi], ii
			}
		}
	}
	if block == nil {
		t.Fatal("missing publication of this poll")
	}
	crossing := &block.Instrs[crossingAt].Crossing
	var capture *mir.CrossingCapture
	captureIndex := -1
	for i := range crossing.Captures {
		if crossing.Captures[i].Symbol == caller.Locals[source].Sym {
			if capture != nil {
				t.Fatal("tally appears twice in the capture list")
			}
			capture, captureIndex = &crossing.Captures[i], i
		}
	}
	if capture == nil || capture.Mode != sema.CrossingCaptureCopy || capture.Type != caller.Locals[source].Type {
		t.Fatal("missing typed Copy capture of tally")
	}
	var field *mir.Operand
	for i := range crossing.State.Fields {
		if crossing.State.Fields[i].Name == fmt.Sprintf("__cap%d", captureIndex) {
			if field != nil {
				t.Fatal("tally appears twice in the state literal")
			}
			field = &crossing.State.Fields[i].Value
		}
	}
	if field == nil || field.Kind != capture.Value.Kind || field.Type != capture.Type ||
		capture.Value.Type != capture.Type || !sameBareLocal(field.Place, capture.Value.Place) {
		t.Fatal("capture record and state field do not publish the same typed local")
	}
	if !counted {
		if field.Kind != mir.OperandCopy || !sameBareLocal(field.Place, mir.Place{Local: source}) {
			t.Fatal("int64 capture must copy the original non-owning parameter")
		}
		return
	}
	private := field.Place.Local
	if field.Kind != mir.OperandMove || private < 0 || int(private) >= len(caller.Locals) || private == source ||
		caller.Locals[private].Type != capture.Type || caller.Locals[private].Flags&mir.LocalFlagOwnsHeap == 0 {
		t.Fatal("counted capture must move a distinct owning temp into the frame")
	}
	assignAt, unshareAt, assignments, unshares := -1, -1, 0, 0
	for ii := 0; ii < crossingAt; ii++ {
		ins := &block.Instrs[ii]
		if ins.Kind == mir.InstrAssign && sameBareLocal(ins.Assign.Dst, field.Place) {
			assignAt, assignments = ii, assignments+1
			use := ins.Assign.Src.Use
			if ins.Assign.Src.Kind != mir.RValueUse || use.Kind != mir.OperandRetain ||
				use.Type != capture.Type || !sameBareLocal(use.Place, mir.Place{Local: source}) {
				t.Fatal("private holder was not retained from this exact tally parameter")
			}
		}
		if ins.Kind == mir.InstrUnshare && sameBareLocal(ins.Unshare.Place, field.Place) {
			unshareAt, unshares = ii, unshares+1
		}
		if (ins.Kind == mir.InstrDrop && sameBareLocal(ins.Drop.Place, field.Place)) ||
			(ins.Kind == mir.InstrCall && ins.Call.HasDst && sameBareLocal(ins.Call.Dst, field.Place)) {
			t.Fatal("private holder was dropped or overwritten before publication")
		}
	}
	if assignments != 1 || unshares != 1 || assignAt >= unshareAt {
		t.Fatalf("private holder requires exactly Retain -> Unshare -> publication; assigns=%d at=%d unshares=%d at=%d crossing=%d",
			assignments, assignAt, unshares, unshareAt, crossingAt)
	}
}
