package llvm

import (
	"strings"
	"testing"

	"surge/internal/types"
)

// A channel's cell holds one machine word: a scalar, or a pointer to a box for
// anything wider. Its element's descriptor describes the element UNBOXED. So
// the two drop bodies a type can have are not interchangeable, and this pins
// the difference at the point where it is visible -- in the emitted IR --
// because the place it would do damage is a runtime reclaim that frees the
// wrong address without saying anything.
//
// drop.typeN takes the address of an unboxed value and reaches into its fields.
// drop_result.typeN takes the word and releases the box itself.
//
// Handing drop.typeN a pointer to a channel cell therefore makes it read the
// box pointer AS the element's first field and free that. For a composite whose
// first field is a string, that is rt_string_free on a box -- a plausible
// pointer, freed from the wrong allocation.
//
// This test exists because that substitution was made, shipped, and reverted.
// Nothing else caught it: `make check` runs with SURGE_SKIP_TIMEOUT_TESTS=1,
// where no native row executes a channel, and no gate corpus reclaims a
// composite element out of one. The measurement that preceded it sampled
// Channel<string>, Channel<int> and Channel<Channel<int>> -- every one of them
// pointer-shaped, and pointer-shaped is exactly where the two bodies agree.
//
// It is here to keep the asymmetry stated where the next attempt will look. The
// flip is only safe once a cell holds what the descriptor describes; on the day
// that is true, these bodies stop being distinguishable in the way asserted
// here, and this test is deleted as part of that change rather than before it.
const channelCompositeSource = `
type Model = { label: string, count: int };

@entrypoint
fn main() -> int {
    let ch = Channel::<Model>::new(2:uint);
    let m: Model = { label = "kept", count = 1 };
    ch.send(own m);
    return 0;
}
`

func TestABoxedElementsTwoDropBodiesAreNotInterchangeable(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, channelCompositeSource)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	elem := types.NoTypeID
	for _, line := range strings.Split(ir, "\n") {
		index := strings.Index(line, "@rt_channel_new(i64 ")
		if index < 0 {
			continue
		}
		rest := line[index:]
		comma := strings.LastIndex(rest, ", i64 ")
		if comma < 0 {
			continue
		}
		id := strings.TrimSuffix(strings.TrimSpace(rest[comma+len(", i64 "):]), ")")
		var parsed uint64
		for _, r := range id {
			if r < '0' || r > '9' {
				parsed = 0
				break
			}
			parsed = parsed*10 + uint64(r-'0')
		}
		if parsed != 0 {
			elem = types.TypeID(parsed)
			break
		}
	}
	if elem == types.NoTypeID {
		t.Fatal("the fixture built no channel, so nothing was measured")
	}

	descriptorBody := emittedDefinition(ir, dropGlueName(elem))
	resultBody := emittedDefinition(ir, dropResultGlueName(elem))
	if descriptorBody == "" || resultBody == "" {
		t.Fatalf("a composite channel element must have both drop bodies; got descriptor=%v result=%v",
			descriptorBody != "", resultBody != "")
	}
	if descriptorBody == resultBody {
		t.Fatal("the two drop bodies became identical; if that is intended, the storage flip landed and this guard should go with it")
	}
	// The concrete asymmetry: the descriptor's body reaches into the value it is
	// pointed at, and the result body releases what it is handed. A cell holds
	// the latter, so only the latter may be dispatched from a reclaim.
	if !strings.Contains(descriptorBody, "getelementptr") {
		t.Errorf("expected the descriptor's drop to address fields of an unboxed value:\n%s", descriptorBody)
	}
	if !strings.Contains(resultBody, "@"+runtimeOwnedReleaseName(elem)) {
		t.Errorf("expected the result drop to release the box itself:\n%s", resultBody)
	}
}
