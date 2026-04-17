package llvm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

func TestPickFuncCandidateRejectsMismatchedSingleCandidate(t *testing.T) {
	typeInfo := types.NewInterner()
	fn := &mir.Func{ID: 0, ParamCount: 2}
	emitter := &Emitter{
		mod:   &mir.Module{Funcs: map[mir.FuncID]*mir.Func{0: fn}},
		types: typeInfo,
		funcSigs: map[mir.FuncID]funcSig{
			0: {paramTypes: []types.TypeID{typeInfo.Builtins().Uint8, typeInfo.Builtins().Uint8}},
		},
	}

	if _, ok := emitter.pickFuncCandidate([]mir.FuncID{0}, []types.TypeID{typeInfo.Builtins().Uint8}, types.NoTypeID); ok {
		t.Fatal("expected no match for mismatched single candidate")
	}
}

func TestPickFuncCandidateRejectsAmbiguousFallback(t *testing.T) {
	emitter := &Emitter{
		mod: &mir.Module{
			Funcs: map[mir.FuncID]*mir.Func{
				0: {ID: 0, Name: "dup", ParamCount: 1},
				1: {ID: 1, Name: "dup", ParamCount: 1},
			},
		},
		types: types.NewInterner(),
		funcSigs: map[mir.FuncID]funcSig{
			0: {paramTypes: []types.TypeID{types.NoTypeID}},
			1: {paramTypes: []types.TypeID{types.NoTypeID}},
		},
	}

	if got, ok := emitter.pickFuncCandidate([]mir.FuncID{0, 1}, []types.TypeID{types.NoTypeID}, types.NoTypeID); ok {
		t.Fatalf("expected ambiguous candidates to resolve to no match, got %d", got)
	}
}

func TestPickFuncCandidateKeepsUniqueTypedMatch(t *testing.T) {
	typeInfo := types.NewInterner()
	u8 := typeInfo.Builtins().Uint8
	u16 := typeInfo.Builtins().Uint16
	fn1 := &mir.Func{ID: 0, Name: "pick", ParamCount: 1}
	fn2 := &mir.Func{ID: 1, Name: "pick", ParamCount: 1}
	emitter := &Emitter{
		mod: &mir.Module{
			Funcs: map[mir.FuncID]*mir.Func{
				0: fn1,
				1: fn2,
			},
		},
		types: typeInfo,
		funcSigs: map[mir.FuncID]funcSig{
			0: {paramTypes: []types.TypeID{u8}},
			1: {paramTypes: []types.TypeID{u16}},
		},
	}

	if got, ok := emitter.pickFuncCandidate([]mir.FuncID{0, 1}, []types.TypeID{u16}, types.NoTypeID); !ok || got != 1 {
		t.Fatalf("expected typed candidate match 1, got %d ok=%v", got, ok)
	}
}
