package llvm

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/types"
)

// The lookup exists so a channel can be handed its element's descriptor when
// all it has is the element's type id -- which is the only thing a far channel
// ever has, because the id crosses the boundary and a process-static address
// does not.
//
// So the property that matters is not "the function was emitted". It is that
// asking it about the ELEMENT of any channel the program builds yields a real
// descriptor. These tests measure that on a corpus that reaches the shapes the
// answer could differ for: a Copy element, an element that owns heap, and an
// element that is itself an opaque runtime resource.
const valueOpsLookupSource = `
@entrypoint
fn main() -> int {
    let numbers = Channel::<int>::new(1:uint);
    let texts = Channel::<string>::new(1:uint);
    let inner = Channel::<int>::new(1:uint);
    let nested = Channel::<Channel<int>>::new(1:uint);
    numbers.send(1);
    texts.send(own "kept");
    nested.send(inner);
    return 0;
}
`

func TestEveryChannelElementHasADescriptorReachableByItsTypeID(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, valueOpsLookupSource)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	registry := mirMod.Meta.Operations
	if registry == nil {
		t.Fatal("no operation registry was published")
	}
	typesIn := result.Sema.TypeInterner

	lookup := emittedDefinition(ir, "__surge_value_ops_for")
	if lookup == "" {
		t.Fatal("__surge_value_ops_for was not emitted, so a channel built from an id has no way back to a descriptor")
	}

	checked := 0
	for _, id := range registry.TypeIDs() {
		label := types.Label(typesIn, id)
		if !strings.HasPrefix(label, "Channel<") {
			continue
		}
		elem, ok := typesIn.ArrayInfo(id)
		if !ok {
			payloads, handle := typesIn.RuntimeHandlePayloads(id)
			if !handle || len(payloads) != 1 {
				continue
			}
			elem = payloads[0]
		}
		checked++
		// The element must be routable: a case for its id, returning its
		// descriptor. Anything less and the constructor would be handed a null
		// for a type the program actually stores.
		arm := "  ret ptr @" + valueOpsSymbol(elem)
		if !strings.Contains(lookup, arm) {
			t.Errorf(
				"the element of %s is type#%d (%s), and the lookup does not return its descriptor: "+
					"a channel of it could not be told what it holds",
				label, elem, types.Label(typesIn, elem),
			)
			continue
		}
		if !strings.Contains(lookup, fmt.Sprintf("i64 %d, label", elem)) {
			t.Errorf("type#%d (%s) has no case in the lookup switch", elem, types.Label(typesIn, elem))
		}
	}
	// The corpus deliberately builds a Copy element, an element that owns heap
	// and an element that is an opaque resource, because those are the three
	// shapes the answer could differ for. Fewer than three means the fixture
	// stopped reaching one of them and the coverage is narrower than it reads.
	if checked < 3 {
		t.Fatalf("only %d channel elements were measured; the corpus is meant to reach three distinct shapes", checked)
	}
}

// An id nobody emitted a descriptor for answers null. That is a real answer --
// the registry deliberately skips a type whose slots cannot be filled honestly
// -- and a caller checks. Panicking here would turn a legitimate question into
// a crash, which is what the sibling drop dispatch does BECAUSE its ids are only
// ever minted for types that have a wrapper.
func TestAnUnknownTypeIDAnswersNullRatherThanPanicking(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, valueOpsLookupSource)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	lookup := emittedDefinition(ir, "__surge_value_ops_for")
	if lookup == "" {
		t.Fatal("__surge_value_ops_for was not emitted")
	}
	if !strings.Contains(lookup, "label %value_ops_absent") {
		t.Error("the lookup has no default arm, so an unknown id has nowhere to land")
	}
	if !strings.Contains(lookup, "value_ops_absent:\n  ret ptr null") {
		t.Errorf("the default arm does not answer null:\n%s", lookup)
	}
	if strings.Contains(lookup, "@rt_panic") || strings.Contains(lookup, "unreachable") {
		t.Errorf("asking about a type with no descriptor must not be fatal:\n%s", lookup)
	}
}

// emittedDefinition returns the text of one emitted definition, so a test can
// assert about that function without matching lines from its neighbours.
func emittedDefinition(ir string, name string) string {
	marker := "@" + name + "("
	start := strings.Index(ir, marker)
	if start < 0 {
		return ""
	}
	for start > 0 && ir[start-1] != '\n' {
		start--
	}
	end := strings.Index(ir[start:], "\n}\n")
	if end < 0 {
		return ir[start:]
	}
	return ir[start : start+end+3]
}

// TestAChannelIsToldItsElementTypeEvenWhenNothingHasToBeDestroyed pins the half
// of the storage flip that is visible from here.
//
// The constructor used to take a DROP-FN id, which was zero whenever the
// element had nothing to reclaim -- so a channel of int was told nothing at all
// about what it holds. The runtime now needs the element's layout whether or
// not that element has to be destroyed, so the argument is the element's TYPE
// id, and it is unconditional. A zero would mean "no descriptor", which for a
// type the program actually stores is a lie the runtime cannot detect.
func TestAChannelIsToldItsElementTypeEvenWhenNothingHasToBeDestroyed(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, valueOpsLookupSource)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	constructions := 0
	for _, line := range strings.Split(ir, "\n") {
		index := strings.Index(line, "@rt_channel_new(i64 ")
		if index < 0 {
			continue
		}
		constructions++
		rest := line[index:]
		comma := strings.LastIndex(rest, ", i64 ")
		if comma < 0 {
			t.Fatalf("unreadable construction: %s", line)
		}
		argument := strings.TrimSuffix(strings.TrimSpace(rest[comma+len(", i64 "):]), ")")
		if argument == "0" {
			t.Errorf(
				"a channel was constructed with element type id 0, so the runtime cannot ask what it holds: %s",
				strings.TrimSpace(line),
			)
		}
	}
	// The corpus builds four channels; a fixture that stopped building them
	// would leave this asserting about nothing.
	if constructions < 4 {
		t.Fatalf("only %d channel constructions were seen, expected the corpus's four", constructions)
	}
}
