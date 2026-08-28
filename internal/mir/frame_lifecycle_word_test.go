package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/sema"
)

// The lifecycle word on the two frame kinds that are NOT the async state
// machine's: a `spawn on` capture set and a `blocking` capture set.
//
// The async frame's four write sites are pinned next door, in
// ownership_async_state_test.go. These are the other four, and they were the
// unpinned half: an adversarial review took the crossing's two SPENT writes out
// of the tree and ran the whole of internal/mir and internal/crossinggate to
// EXIT 0, then wrote SPENT where the crossing construction site writes PACKED —
// a frame born claiming to be empty with the captures it just took sitting
// inside it — and got EXIT 0 again. The only guard was a field COUNT, and a
// count cannot see a wrong word.
//
// Nothing outside MIR reads the word yet, so no backend and no runtime test can
// notice one of these going missing. Until a reader lands, these rows are the
// entire reason a write site cannot quietly stop happening.

// structLitFrameWord returns the lifecycle word a struct literal is built with.
func structLitFrameWord(lit *mir.StructLit) (int64, bool) {
	for i := range lit.Fields {
		if lit.Fields[i].Name != mir.FrameStateField {
			continue
		}
		value := lit.Fields[i].Value
		if value.Kind != mir.OperandConst || value.Const.Kind != mir.ConstInt {
			return 0, false
		}
		return value.Const.IntValue, true
	}
	return 0, false
}

// requireSyntheticBody returns the one synthesized body whose name starts with
// prefix.
func requireSyntheticBody(t *testing.T, mod *mir.Module, prefix string) *mir.Func {
	t.Helper()
	var found *mir.Func
	for _, id := range mod.SortedFuncIDs() {
		f := mod.Funcs[id]
		if f == nil || !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		if found != nil {
			t.Fatalf("more than one body named %q*: %s and %s", prefix, found.Name, f.Name)
		}
		found = f
	}
	if found == nil {
		t.Fatalf("no synthetic body named %q* in module", prefix)
	}
	return found
}

// lastStateUnpackIndex returns the index of the last instruction in bb that
// reads a field out of the frame local — the read that empties it.
func lastStateUnpackIndex(f *mir.Func, bb *mir.Block) int {
	last := -1
	for ii := range bb.Instrs {
		ins := &bb.Instrs[ii]
		if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueField {
			continue
		}
		obj := ins.Assign.Src.Field.Object.Place
		if obj.Kind != mir.PlaceLocal || int(obj.Local) >= len(f.Locals) {
			continue
		}
		if f.Locals[obj.Local].Name == "__state" {
			last = ii
		}
	}
	return last
}

// requireSpentAfterTheUnpack asserts that the frame is marked SPENT by the
// instruction that immediately follows the read which emptied it.
//
// Adjacency is the whole of the argument for a plain store: between the read and
// the word there is no call, no yield and nothing else that could hand this
// frame to another reader, so no schedule exists in which the frame is empty
// while the word still says PACKED. A word written one instruction later than
// the code claims is a window, and a window is what this field exists to close.
func requireSpentAfterTheUnpack(t *testing.T, f *mir.Func, kind string) {
	t.Helper()
	entry := &f.Blocks[f.Entry]
	last := lastStateUnpackIndex(f, entry)
	if last < 0 {
		t.Fatalf("%s: %s unpacks nothing out of its frame; the probe stopped measuring what it claims to",
			kind, f.Name)
	}
	if last+1 >= len(entry.Instrs) {
		t.Fatalf("%s: %s empties the frame in its entry block's last instruction; the SPENT write has "+
			"nowhere adjacent to go, and a window opens where the frame lies", kind, f.Name)
	}
	word, ok := frameStateWord(&entry.Instrs[last+1])
	if !ok {
		t.Fatalf("%s: %s empties the frame at entry#%d and the next instruction (%s) is not a lifecycle "+
			"write; the frame goes on claiming to hold what the locals now hold",
			kind, f.Name, last, entry.Instrs[last+1].Kind)
	}
	if word != mir.FrameStateSpent {
		t.Fatalf("%s: %s marks the emptied frame %q; a frame whose captures are gone is SPENT",
			kind, f.Name, wordName(word))
	}
}

// A crossing's frame is born PACKED, and it is the VALUE that is asserted here.
//
// The count of fields cannot see the error worth catching. A frame born saying
// SPENT — with the captures the literal moved in one field below it — is the
// maximally wrong word: it tells any reclamation that gets there first to give
// the storage back and walk nothing, which strands every capture inside it. That
// reclamation is exactly the one that cannot see the site it came from, which is
// why the frame has to be the one carrying the answer.
func TestSpawnOnFrameIsBornPacked(t *testing.T) {
	compiled := compileCrossingMIR(t, spawnOnCaptureUnpackSource,
		map[sema.CrossingLoweringKind]bool{sema.CrossingLoweringSpawnOn: true})
	ins := requireMIRCrossing(t, compiled.mod, sema.CrossingLoweringSpawnOn)

	word, ok := structLitFrameWord(&ins.State)
	if !ok {
		t.Fatalf("the spawn_on state literal carries no constant %s field (%d fields); a frame whose word "+
			"was never written describes itself with whatever its storage happened to hold",
			mir.FrameStateField, len(ins.State.Fields))
	}
	if word != mir.FrameStatePacked {
		t.Fatalf("the spawn_on state literal is born %q while its captures sit in the fields below it; "+
			"a frame holding live captures is PACKED", wordName(word))
	}
	if ins.State.Fields[0].Name != mir.FrameStateField {
		t.Fatalf("the lifecycle word is not the literal's first field (%q is); the word is read at offset "+
			"zero by a reader that knows nothing else about the storage it holds", ins.State.Fields[0].Name)
	}
}

// The crossing poll empties its frame at entry and says so there.
func TestSpawnOnPollMarksTheFrameSpentAtEntry(t *testing.T) {
	compiled := compileCrossingMIR(t, spawnOnCaptureUnpackSource,
		map[sema.CrossingLoweringKind]bool{sema.CrossingLoweringSpawnOn: true})
	poll := requireSyntheticBody(t, compiled.mod, "__spawn_on_block$")
	requireSpentAfterTheUnpack(t, poll, "spawn on")
}

// Every return of a crossing body leaves the frame marked SPENT, and the word is
// the LAST instruction of the block that returns.
//
// Last is the assertion, not an accident of where it was appended. SPENT is
// truthful on this leg only because the drops of the owned captures run first:
// a Copy capture carrying storage arrived as this crossing's own copy and the
// local is its only holder, so a frame that claimed to hold nothing while that
// local was still alive would strand exactly that copy. Ordering the word after
// the drops is what makes the claim true, so anything appended after the word is
// a frame that lies for as long as it stands there.
func TestSpawnOnPollMarksTheFrameSpentAtEveryReturn(t *testing.T) {
	compiled := compileCrossingMIR(t, spawnOnCaptureUnpackSource,
		map[sema.CrossingLoweringKind]bool{sema.CrossingLoweringSpawnOn: true})
	poll := requireSyntheticBody(t, compiled.mod, "__spawn_on_block$")

	returns := 0
	for bi := range poll.Blocks {
		bb := &poll.Blocks[bi]
		if bb.Term.Kind != mir.TermAsyncReturn {
			continue
		}
		returns++
		if len(bb.Instrs) == 0 {
			t.Errorf("%s bb%d ends the activation with no instructions at all; nothing marks the frame "+
				"empty and whoever reclaims it walks what the body already took", poll.Name, bi)
			continue
		}
		word, ok := frameStateWord(&bb.Instrs[len(bb.Instrs)-1])
		if !ok {
			t.Errorf("%s bb%d ends the activation and its last instruction (%s) is not a lifecycle write; "+
				"the frame it hands back still claims to hold the body's locals",
				poll.Name, bi, bb.Instrs[len(bb.Instrs)-1].Kind)
			continue
		}
		if word != mir.FrameStateSpent {
			t.Errorf("%s bb%d ends the activation with the frame marked %q; a completed body left it empty",
				poll.Name, bi, wordName(word))
		}
	}
	if returns == 0 {
		t.Fatalf("%s has no returning block: the probe stopped measuring what it claims to", poll.Name)
	}
}

// A blocking job's frame is born PACKED and is SPENT from the body's entry.
//
// Both halves matter and they are two different windows. Before the body runs,
// the captures are in the frame and a job the runtime cancels there is reclaimed
// by destroying them through the frame's descriptor — a walk, which is PACKED.
// From the body's first instructions they are out, and the runtime has already
// spent its own record of the state so that a cancel landing mid-body releases
// the storage without walking it. A frame that stayed PACKED across that line
// would be a word that is wrong for the whole life of every blocking frame, and
// a reader trusting it would free the captures the body is holding a second
// time.
func TestBlockingFrameIsPackedUntilTheBodyTakesIt(t *testing.T) {
	compiled := compileCrossingMIR(t, blockingCaptureUnpackSource, nil)

	literals := 0
	for _, id := range compiled.mod.SortedFuncIDs() {
		f := compiled.mod.Funcs[id]
		if f == nil {
			continue
		}
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrBlocking {
					continue
				}
				literals++
				word, ok := structLitFrameWord(&ins.Blocking.State)
				if !ok {
					t.Fatalf("%s bb%d#%d submits a blocking job whose state carries no constant %s field "+
						"(%d fields)", f.Name, bi, ii, mir.FrameStateField, len(ins.Blocking.State.Fields))
				}
				if word != mir.FrameStatePacked {
					t.Fatalf("%s bb%d#%d submits a blocking job whose state is born %q while the captures "+
						"it consumed sit in the fields below it", f.Name, bi, ii, wordName(word))
				}
				if ins.Blocking.State.Fields[0].Name != mir.FrameStateField {
					t.Fatalf("%s bb%d#%d builds a blocking state whose first field is %q; the word is read "+
						"at offset zero", f.Name, bi, ii, ins.Blocking.State.Fields[0].Name)
				}
			}
		}
	}
	if literals == 0 {
		t.Fatal("no blocking submission in the module: the probe stopped measuring what it claims to")
	}

	body := requireSyntheticBody(t, compiled.mod, "__blocking_block$")
	requireSpentAfterTheUnpack(t, body, "blocking")
}
