package llvm

// The DROPPABLE bit has two legs, and this pins them together.
//
// One leg is the capability classifier, published through the operation
// registry: it decides whether a carrier has a drop obligation. The other is
// this backend's `typeOwnsHeap`, which decides whether there is a leaf to
// reclaim and therefore whether a drop body would do anything. The manifest
// settles which question the bit answers — "drop_in_place is present and must
// run exactly once" — so a descriptor may carry the bit only where both legs
// say yes.
//
// Why this is worth a test of its own, when a sibling file already refuses to
// compare these two as booleans: that file compares CarrierDroppable against
// sema's `ownsHeap`, a pair designed to disagree. This one compares the bit
// that ships in the ABI against the backend that has to back it. Those must
// agree, and where they cannot yet, the disagreement is listed here by name so
// that a new one fails instead of joining a crowd.

import (
	"strings"
	"testing"

	"surge/internal/types"
	"surge/internal/valueops"
)

// knownDroppableDivergences are the carriers whose obligation the classifier
// states and whose leaf this backend has no reclamation for. They are named
// individually rather than by family, because a family is a category and a
// category quietly absorbs new members.
//
// A name leaves this list the day its reclamation is emitted, and the test then
// fails until the row is deleted — the intended amount of noise for a carrier
// family becoming real. The list is EMPTY now: its last two rows, `Channel<int>`
// and `own Channel<int>`, left when the channel became a reference-counted
// handle and its drop glue started calling `rt_channel_handle_drop`. The
// map stays, with the check that reads it, so that the
// next carrier this backend cannot back is recorded by name rather than
// joining a crowd — and the fixture below still reaches every carrier kind the
// comparison cares about.
//
// What a listed name would promise is narrow and worth stating exactly: the
// descriptor is still WRITTEN — a channel of channels needs its element's
// layout and move_init to exist at all — and what comes off is the DROPPABLE
// bit, leaving drop_in_place null. The ABI allows exactly that: capability
// slots are independent, and only move_init and plan_cross are mandatory.
const dropGluePrefix = "drop.type"

var knownDroppableDivergences = map[string]string{}

// The fixture reaches one carrier of every kind the comparison cares about: a
// composite owning a string (both legs say yes), a channel held by value and by
// an owning binding, and a task handle. Every row of the divergence list has to
// be produced by it, or the list is asserting about carriers nobody built.
const droppableAgreementSource = `
type Model = { label: string, count: int };

async fn tick() -> nothing {
    return nothing;
}

@entrypoint
fn main() -> int {
    let m: Model = { label = "kept", count = 1 };
    let ch: own Channel<int> = Channel::<int>::new(1:uint);
    ch.send(m.count);
    let t = spawn tick();
    let _ = t.await();
    print(own m.label);
    return 0;
}
`

func TestTheDroppableBitAgreesWithTheBackendThatHasToBackIt(t *testing.T) {
	withRepoStdlib(t)

	e, _ := prepareEmitterAndResultForTest(t, droppableAgreementSource)
	registry := e.mod.Meta.Operations
	if registry == nil {
		t.Fatal("the compilation published no operation registry, so there is only one side to compare")
	}

	seenDivergences := map[string]struct{}{}
	compared := 0
	for _, id := range registry.TypeIDs() {
		entry, err := registry.Value(id)
		if err != nil {
			t.Fatalf("registry entry for type#%d: %v", id, err)
		}
		compared++
		registrySays := entry.Flags&valueops.FlagDroppable != 0
		backendSays := e.typeOwnsHeap(id)
		if registrySays == backendSays {
			continue
		}
		name := typeNameForDivergence(e.types, id)
		if _, known := knownDroppableDivergences[name]; !known {
			t.Errorf(
				"type#%d (%s): registry says droppable=%v, backend says %v, and this divergence is not recorded.\n"+
					"Either the classifier is wrong, or this backend gained or lost a reclamation. "+
					"Do not add a row to make this pass without saying which.",
				id, name, registrySays, backendSays,
			)
			continue
		}
		seenDivergences[name] = struct{}{}
		// The descriptor is written, and the bit is what must not survive: a set
		// flag over a body that frees nothing passes the runtime's preflight and
		// then leaks in silence, which is the one failure nothing downstream can
		// catch.
		if registrySays && e.backedFlags(&entry)&valueops.FlagDroppable != 0 {
			t.Errorf(
				"type#%d (%s) diverges and yet its descriptor still claims DROPPABLE: "+
					"the bit would promise a drop this backend cannot perform",
				id, name,
			)
		}
	}

	if compared == 0 {
		t.Fatal("no registry entries were compared, so agreement was not measured")
	}
	// A recorded divergence the corpus no longer produces is a row nobody will
	// delete on their own, and a stale list is how a fixed defect keeps its
	// excuse. The fixture is small and deliberate, so every row must be met.
	for name, reason := range knownDroppableDivergences {
		if _, met := seenDivergences[name]; !met {
			t.Errorf(
				"the divergence recorded for %s (%s) did not occur: either the reclamation now exists "+
					"and the row should be deleted, or the fixture stopped reaching that carrier",
				name, reason,
			)
		}
	}
}

// typeNameForDivergence spells a type the way the divergence list does, so a
// failure names the carrier rather than a number that means nothing in a diff.
func typeNameForDivergence(typesIn *types.Interner, id types.TypeID) string {
	if typesIn == nil {
		return "?"
	}
	return types.Label(typesIn, id)
}

// TestAnEmittedDescriptorCarriesARealDropBody is the closing half: agreement
// alone is satisfied by a corpus where nothing is droppable at all, which is
// exactly the state this work was undoing. This asserts EXISTENCE with a count,
// and that the name in the slot is defined in the same module — a descriptor
// pointing at a body nobody wrote links, runs, and frees nothing.
func TestAnEmittedDescriptorCarriesARealDropBody(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, droppableAgreementSource)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	descriptors := 0
	withDrop := 0
	for _, line := range strings.Split(ir, "\n") {
		if !strings.HasPrefix(line, "@__surge_value_ops_type") {
			continue
		}
		descriptors++
		index := strings.Index(line, "ptr @"+dropGluePrefix)
		if index < 0 {
			continue
		}
		withDrop++
		name := line[index+len("ptr @"):]
		if cut := strings.IndexAny(name, ", "); cut >= 0 {
			name = name[:cut]
		}
		if !strings.Contains(ir, "define void @"+name+"(") {
			t.Errorf("descriptor points at @%s, which this module never defines", name)
		}
	}

	if descriptors == 0 {
		t.Fatal("no descriptor was emitted, so nothing was measured")
	}
	if withDrop == 0 {
		t.Fatalf(
			"%d descriptors were emitted and not one carries a drop body: the bit is staged again in all but name",
			descriptors,
		)
	}
}

// TestAChannelElementDescriptorCarriesTheHandleRelease is the acceptance the
// owner named for this rule, turned around now that a channel handle has a
// release to name.
//
// A channel of channels is legal and must stay legal: the typed constructor
// needs its element's layout and move_init, which this backend has always
// written. What it used to withhold was the DROP — the element was an opaque
// resource with no release, so the bit came off rather than promise a drop
// nobody performed. The channel is a reference-counted handle now, and an
// element that is a channel is destroyed by the outer channel's drain the way
// any other owning element is: its descriptor names a drop body, and that body
// gives the inner handle's reference back through `rt_channel_handle_drop`.
// A descriptor that dropped the bit again would let a `Channel<Channel<T>>`
// leak every inner channel still in its ring, silently, past every preflight.
func TestAChannelElementDescriptorCarriesTheHandleRelease(t *testing.T) {
	const source = `
@entrypoint
fn main() -> int {
    let inner = Channel::<int>::new(1:uint);
    let outer = Channel::<Channel<int>>::new(1:uint);
    outer.send(inner);
    return 0;
}
`
	mirMod, result := lowerMIRFromSource(t, source)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("a channel of channels must still compile: %v", err)
	}

	registry := mirMod.Meta.Operations
	if registry == nil {
		t.Fatal("no operation registry was published")
	}
	checked := 0
	for _, id := range registry.TypeIDs() {
		if !strings.HasPrefix(types.Label(result.Sema.TypeInterner, id), "Channel<") {
			continue
		}
		checked++
		line := descriptorLineFor(ir, id)
		if line == "" {
			t.Errorf("type#%d (%s) got no descriptor: the typed constructor would have nothing to pass",
				id, types.Label(result.Sema.TypeInterner, id))
			continue
		}
		if !strings.Contains(line, "@"+moveInitName(id)) {
			t.Errorf("type#%d carries no move_init, which is mandatory: %s", id, line)
		}
		dropName := dropGlueName(id)
		if !strings.Contains(line, "ptr @"+dropName) {
			t.Errorf("type#%d names no drop body: a channel element would be abandoned by the ring drain: %s", id, line)
			continue
		}
		body := findLLVMFuncBody(t, ir, dropName)
		if !strings.Contains(body, "call void @rt_channel_handle_drop(") {
			t.Errorf("type#%d's drop body does not give the handle's reference back:\n%s", id, body)
		}
	}
	if checked == 0 {
		t.Fatal("no channel type was reached, so the rule was not measured")
	}
}

func descriptorLineFor(ir string, id types.TypeID) string {
	prefix := "@" + valueOpsSymbol(id) + " = "
	for _, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
