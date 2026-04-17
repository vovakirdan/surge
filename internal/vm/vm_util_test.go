package vm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

func TestPickFunctionCandidateRejectsMismatchedSingleCandidate(t *testing.T) {
	typeInfo := types.NewInterner()
	vm := &VM{Types: typeInfo}

	fn := &mir.Func{ParamCount: 2}
	argTypes := []types.TypeID{typeInfo.Builtins().Uint8}
	if got := vm.pickFunctionCandidate([]*mir.Func{fn}, argTypes, types.NoTypeID); got != nil {
		t.Fatalf("expected no match for mismatched single candidate, got %+v", got)
	}
}

func TestPickFunctionCandidateRejectsAmbiguousFallback(t *testing.T) {
	vm := &VM{Types: types.NewInterner()}

	fn1 := &mir.Func{ID: 1, Name: "dup", ParamCount: 1}
	fn2 := &mir.Func{ID: 2, Name: "dup", ParamCount: 1}
	argTypes := []types.TypeID{types.NoTypeID}
	if got := vm.pickFunctionCandidate([]*mir.Func{fn1, fn2}, argTypes, types.NoTypeID); got != nil {
		t.Fatalf("expected ambiguous candidates to resolve to nil, got %+v", got)
	}
}

func TestPickFunctionCandidateKeepsUniqueTypedMatch(t *testing.T) {
	typeInfo := types.NewInterner()
	vm := &VM{Types: typeInfo}

	u8 := typeInfo.Builtins().Uint8
	u16 := typeInfo.Builtins().Uint16
	fn1 := &mir.Func{
		ID:         1,
		Name:       "pick",
		ParamCount: 1,
		Locals:     []mir.Local{{Type: u8}},
	}
	fn2 := &mir.Func{
		ID:         2,
		Name:       "pick",
		ParamCount: 1,
		Locals:     []mir.Local{{Type: u16}},
	}
	argTypes := []types.TypeID{u16}
	if got := vm.pickFunctionCandidate([]*mir.Func{fn1, fn2}, argTypes, types.NoTypeID); got != fn2 {
		t.Fatalf("expected typed candidate match %v, got %v", fn2, got)
	}
}
