package vm

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

// The call boundary's own tests. The corpus proves that calls produce the right
// answers; these prove that the interpreter reached those answers by asking the
// module's call-layout authority rather than by a rule of its own. A boundary
// that classified correctly but ignored the classification would pass the
// corpus and still be free to disagree with the native backend the moment
// either side changed.

type contractFixture struct {
	vm       *VM
	interner *types.Interner
	i64      types.TypeID
	pair     types.TypeID
	empty    types.TypeID
}

func newContractFixture(t *testing.T) *contractFixture {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()

	i64 := interner.Intern(types.Type{Kind: types.KindInt, Width: 64})

	pair := interner.RegisterStruct(interner.Strings.Intern("Pair"), source.Span{})
	interner.SetStructFields(pair, []types.StructField{
		{Name: interner.Strings.Intern("a"), Type: i64},
		{Name: interner.Strings.Intern("b"), Type: i64},
	})
	empty := interner.RegisterStruct(interner.Strings.Intern("Empty"), source.Span{})
	interner.SetStructFields(empty, nil)

	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{i64, pair, empty})
	if err != nil {
		t.Fatalf("freezing the fixture layouts must succeed: %v", err)
	}

	module := &mir.Module{Meta: &mir.ModuleMeta{
		Layouts:     registry,
		CallLayouts: mir.NewCallLayoutTable(interner, registry),
	}}
	return &contractFixture{
		vm:       New(module, nil, nil, interner, nil),
		interner: interner,
		i64:      i64,
		pair:     pair,
		empty:    empty,
	}
}

// fn builds a callee definition carrying a signature, which is the only thing
// the contract is keyed by.
func fn(name string, result types.TypeID, params ...types.TypeID) *mir.Func {
	locals := make([]mir.Local, 0, len(params))
	for i, param := range params {
		locals = append(locals, mir.Local{Name: string(rune('a' + i)), Type: param})
	}
	return &mir.Func{Name: name, Result: result, ParamCount: len(params), Locals: locals}
}

func TestCallContractClassifiesEveryPositionOfASignature(t *testing.T) {
	f := newContractFixture(t)

	contract, vmErr := f.vm.contractOf(fn("mixed", f.pair, f.i64, f.pair, f.empty))
	if vmErr != nil {
		t.Fatalf("a finalized module must classify its own signatures: %v", vmErr)
	}
	if !contract.classified {
		t.Fatal("a module carrying a call-layout table must produce a classification")
	}

	want := []mir.ParamClass{mir.ParamDirect, mir.ParamByval, mir.ParamElidedZST}
	if len(contract.abi.Params) != len(want) {
		t.Fatalf("a three-parameter signature must classify three positions, got %d", len(contract.abi.Params))
	}
	for i, class := range want {
		if got := contract.abi.Params[i].Class; got != class {
			t.Fatalf("argument %d travels as %s, want %s", i, got, class)
		}
	}
	if contract.abi.Params[1].Align == 0 {
		t.Fatal("a by-value argument travels by address and must state its alignment")
	}
	if contract.abi.Ret.Class != mir.RetSret {
		t.Fatalf("a composite result must travel through a hidden destination, got %s", contract.abi.Ret.Class)
	}
}

func TestCallContractIsRememberedPerCallee(t *testing.T) {
	f := newContractFixture(t)
	callee := fn("twice", f.i64, f.pair)

	first, vmErr := f.vm.contractOf(callee)
	if vmErr != nil {
		t.Fatalf("classifying must succeed: %v", vmErr)
	}
	second, vmErr := f.vm.contractOf(callee)
	if vmErr != nil {
		t.Fatalf("classifying the same callee again must succeed: %v", vmErr)
	}
	if first.abi.Ret != second.abi.Ret || len(first.abi.Params) != len(second.abi.Params) {
		t.Fatal("one callee must not be able to hold two contracts")
	}
	if len(f.vm.contracts) != 1 {
		t.Fatalf("the same callee must be remembered once, got %d entries", len(f.vm.contracts))
	}
}

// A scalar result is a value and a zero-sized result is nothing; neither may be
// mistaken for a hidden destination, because a caller that provided one would
// then be initialized twice.
func TestCallContractSendsOnlyCompositesThroughAHiddenDestination(t *testing.T) {
	f := newContractFixture(t)

	for _, tc := range []struct {
		name   string
		result types.TypeID
		want   mir.RetClass
	}{
		{"a scalar result", f.i64, mir.RetDirect},
		{"a zero-sized composite result", f.empty, mir.RetVoid},
		{"a sized composite result", f.pair, mir.RetSret},
		{"no result at all", types.NoTypeID, mir.RetVoid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract, vmErr := f.vm.contractOf(fn("returns"+tc.name, tc.result))
			if vmErr != nil {
				t.Fatalf("classifying must succeed: %v", vmErr)
			}
			caller := NewFrame(fn("caller", types.NoTypeID, f.pair))
			protocol := contract.resultProtocolFor(resultDest{Frame: caller, Has: true})
			if protocol.Class != tc.want {
				t.Fatalf("result travels as %s, want %s", protocol.Class, tc.want)
			}
			delivered, vmErr := f.vm.deliverResult(protocol, Value{})
			if vmErr != nil {
				t.Fatalf("delivery must not fail: %v", vmErr)
			}
			if delivered != (tc.want == mir.RetSret) {
				t.Fatalf("delivered=%v for %s; only a hidden destination is written here", delivered, protocol.Class)
			}
		})
	}
}

// A module that was never finalized has no classification authority, and the
// boundary must say so rather than read the zero value as "no hidden
// destination". RetGoverned is unusable by design, so an unclassified callee
// cannot be silently delivered through a destination it never asked for.
func TestCallContractOfAnUnfinalizedModuleIsUnusableRatherThanEmpty(t *testing.T) {
	machine := New(&mir.Module{}, nil, nil, types.NewInterner(), nil)
	contract, vmErr := machine.contractOf(fn("unclassified", types.NoTypeID))
	if vmErr != nil {
		t.Fatalf("an unfinalized module must not fail the call itself: %v", vmErr)
	}
	if contract.classified {
		t.Fatal("a module with no call-layout table cannot have produced a classification")
	}
	protocol := contract.resultProtocolFor(resultDest{Has: true})
	if protocol.Class != mir.RetGoverned {
		t.Fatalf("an unclassified result must be %s, got %s", mir.RetGoverned, protocol.Class)
	}
	delivered, vmErr := machine.deliverResult(protocol, Value{})
	if vmErr != nil {
		t.Fatalf("delivery must not fail: %v", vmErr)
	}
	if delivered {
		t.Fatal("an unclassified result must not be delivered through a hidden destination")
	}
}

func TestCallBoundaryRefusesArgumentsTheContractDoesNotDescribe(t *testing.T) {
	f := newContractFixture(t)
	callee := fn("takes_two", f.i64, f.i64, f.i64)
	contract, vmErr := f.vm.contractOf(callee)
	if vmErr != nil {
		t.Fatalf("classifying must succeed: %v", vmErr)
	}

	frame := NewFrame(callee)
	vmErr = f.vm.passArguments(frame, contract, []Value{MakeInt(1, f.i64)})
	if vmErr == nil {
		t.Fatal("a call passing fewer arguments than the contract classifies must be refused")
	}
	if !strings.Contains(vmErr.Message, "classified arguments") {
		t.Fatalf("the refusal must name the mismatch; %q does not", vmErr.Message)
	}
}

// ParamGoverned is the zero value and means some other authority classifies the
// argument. Reaching the generated boundary with one is a lost domain check,
// and letting it through would pass an argument under no contract at all.
func TestCallBoundaryRefusesAnArgumentNoAuthorityClassified(t *testing.T) {
	f := newContractFixture(t)
	callee := fn("takes_one", f.i64, f.i64)
	contract, vmErr := f.vm.contractOf(callee)
	if vmErr != nil {
		t.Fatalf("classifying must succeed: %v", vmErr)
	}
	contract.abi.Params[0].Class = mir.ParamGoverned

	frame := NewFrame(callee)
	vmErr = f.vm.passArguments(frame, contract, []Value{MakeInt(1, f.i64)})
	if vmErr == nil {
		t.Fatal("an argument no authority classified must be refused")
	}
	if !strings.Contains(vmErr.Message, "never classified") {
		t.Fatalf("the refusal must name what is missing; %q does not", vmErr.Message)
	}
}

func TestCallBoundaryPassesEveryClassifiedArgumentIntoItsOwnSlot(t *testing.T) {
	f := newContractFixture(t)
	callee := fn("takes_three", f.i64, f.i64, f.i64, f.i64)
	contract, vmErr := f.vm.contractOf(callee)
	if vmErr != nil {
		t.Fatalf("classifying must succeed: %v", vmErr)
	}

	frame := NewFrame(callee)
	if vmErr := f.vm.passArguments(frame, contract, []Value{MakeInt(7, f.i64), MakeInt(8, f.i64), MakeInt(9, f.i64)}); vmErr != nil {
		t.Fatalf("passing three classified arguments must succeed: %v", vmErr)
	}
	for i, want := range []int64{7, 8, 9} {
		slot := frame.Locals[i]
		if !slot.IsInit {
			t.Fatalf("parameter %d was left uninitialized", i)
		}
		if slot.V.Int != want {
			t.Fatalf("parameter %d holds %d, want %d", i, slot.V.Int, want)
		}
	}
}
