package mir_test

import (
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
// The rows are the answers the type gives, and they are the same ones an
// ordinary by-value argument position gives: a `string` and a struct holding
// one own heap and are not reference-counted, so the state took them; an `int`
// owns nothing at all and there is nothing to hand on.
//
// The third answer the predicate can give — owns heap AND is a
// reference-counted scalar, where the state retained a count of its own and
// the body only borrows — has no row in this table, and no longer because
// sema refuses it: a `float` captured into `blocking` compiles today, since
// the state literal's relinquishing operand makes the capture's block private
// before the job is submitted. That shape is pinned where it compiles end to
// end, in buildpipeline's crossing table; the clause stays in the predicate
// as the guard it is, and this table stays the by-value answers above.
const blockingCaptureUnpackSource = crossingMIRPrelude + `
@shard_movable
type Note = { text: string };

fn peek(s: &string) -> int { return 1; }
fn read(n: &Note) -> int { return peek(n.text); }

async fn runs_a_blocking_body(seed: int) -> int {
    let msg: string = "a capture wide enough to be a block";
    let count: int = seed;
    let note: Note = Note { text: "and a second one inside a struct" };
    let job: Task<int> = blocking {
        ret peek(&msg) + count + read(&note);
    };
    return compare job.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

fn main() -> int { return 0; }
`

// The control row: the crossing side already answers this question from sema's
// recorded capture MODE, and nothing in the tree pins it. If that answer ever
// stops separating an owned capture from a copied one, the blocking rows above
// would be pinning a convention that its own model no longer holds.
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

	cases := []struct {
		capture string
		why     string
		want    bool
	}{
		{"msg", "owns heap, not reference-counted: the state took it", true},
		{"note", "a struct holding a string: the state took that too", true},
		{"count", "owns no heap: there is nothing to hand on", false},
	}
	for _, tc := range cases {
		t.Run(tc.capture, func(t *testing.T) {
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
		{"tally", "a copy capture leaves the caller's binding standing", false},
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
