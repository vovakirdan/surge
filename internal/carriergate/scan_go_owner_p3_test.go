package carriergate

import "testing"

func TestStructuralOwnerCensusKeepsDeclarationEnvironmentsLexical(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/declaration_scope.go": []byte(`package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint32 }
type SlotP = generalSlot
type slotLeaf struct { value SlotP }
type slotWrap[SlotP any] struct { nested slotLeaf }
type CarrierP = Value
type carrierLeaf struct { value CarrierP }
type carrierWrap[CarrierP any] struct { nested carrierLeaf }
type VM struct { pool map[uint64]slotWrap[int] }
type token struct { root carrierWrap[int] }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan lexical declaration environments: %v", err)
	}
	for _, want := range []string{
		"VM.pool->general-slot(generalSlot)",
		"token.root->carrierWrap.nested->carrierLeaf.value->Value",
	} {
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("caller binding captured package declaration token %q", want)
		}
	}
}

func TestStructuralOwnerCensusCanonicalizesAnonymousActualsInEnvironment(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/anonymous_actuals.go": []byte(`package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint32 }
type SlotDirect[P, Q any] struct {
	next *SlotDirect[struct{ x Q }, generalSlot]
	value P
}
type SlotRoot[Q any] struct {
	pool map[uint64]SlotDirect[struct{ x Q }, generalSlot]
}
type VM = SlotRoot[int]
type CarrierDirect[P, Q any] struct {
	next *CarrierDirect[struct{ x Q }, Value]
	value P
}
type CarrierRoot[Q any] struct {
	value CarrierDirect[struct{ x Q }, Value]
}
type token struct { root CarrierRoot[int] }
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan environment-bound anonymous actuals: %v", err)
	}
	for _, want := range []string{
		"VM.pool->general-slot(generalSlot)",
		"token.root->CarrierRoot.value->CarrierDirect.next->CarrierDirect.value->x->Value",
	} {
		if !hasFinding(findings, categoryVMUniversalOwner, want) {
			t.Errorf("anonymous actual collapsed recursion token %q: %+v", want, findings)
		}
	}
}

func TestStructuralOwnerCensusTerminatesTransformedGenericAliasCycle(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/transformed_alias_cycle.go": []byte(`package vm
type Link[P any] = *P
type VM = Link[VM]
`),
	}, false)
	if _, err := Scan(root); err != nil {
		t.Fatalf("scan transformed generic alias cycle: %v", err)
	}
}

func TestStructuralOwnerCensusDistinguishesFiniteNestedAliasActuals(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/nested_alias_actuals.go": []byte(`package vm
type slotState uint8
type generalSlot struct { state slotState; generation uint32 }
type Wrap[P any] = struct { x P }
type Direct[P, Q any] struct {
	next *Direct[Wrap[Wrap[Q]], generalSlot]
	value P
}
type Root[Q any] struct {
	pool map[uint64]Direct[Wrap[Wrap[Q]], generalSlot]
}
type VM = Root[int]
`),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan finite nested generic aliases: %v", err)
	}
	const want = "VM.pool->general-slot(generalSlot)"
	if !hasFinding(findings, categoryVMUniversalOwner, want) {
		t.Errorf("finite nested aliases collapsed token %q: %+v", want, findings)
	}
}
