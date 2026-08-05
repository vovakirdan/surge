package vm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/source"
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

func TestResolveCallTargetPrefersSyntheticFunctionBeforeIntrinsicFallback(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	intType := typesIn.Builtins().Int
	target := &mir.Func{ID: 2, Name: "__async_block$2", Result: intType}
	caller := &mir.Func{
		ID:     1,
		Name:   "caller",
		Locals: []mir.Local{{Type: intType}},
	}
	vm := &VM{M: &mir.Module{Funcs: map[mir.FuncID]*mir.Func{target.ID: target}}}
	frame := NewFrame(caller)
	call := &mir.CallInstr{
		HasDst: true,
		Dst:    mir.Place{Local: 0},
		Callee: mir.Callee{Kind: mir.CalleeValue, Name: target.Name},
	}

	if got := vm.resolveCallTarget(frame, call); got != target {
		t.Fatalf("synthetic call target = %+v, want exact MIR function", got)
	}
	call.Callee.Name = "__task_state"
	if got := vm.resolveCallTarget(frame, call); got != nil {
		t.Fatalf("missing intrinsic helper resolved as MIR function: %+v", got)
	}
}

func TestResolveCallTargetDoesNotShadowIntrinsicWithUnrelatedMagicMethod(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	intType := typesIn.Builtins().Int
	pointType := typesIn.RegisterStruct(typesIn.Strings.Intern("Point"), source.Span{})
	pointRef := typesIn.Intern(types.MakeReference(pointType, false))
	method := &mir.Func{
		ID:         2,
		Name:       "__add",
		ParamCount: 2,
		Result:     pointType,
		Locals:     []mir.Local{{Type: pointRef}, {Type: pointRef}},
	}
	caller := &mir.Func{
		ID:     1,
		Name:   "caller",
		Locals: []mir.Local{{Type: intType}},
	}
	vm := &VM{
		M:     &mir.Module{Funcs: map[mir.FuncID]*mir.Func{method.ID: method}},
		Types: typesIn,
	}
	call := &mir.CallInstr{
		HasDst: true,
		Dst:    mir.Place{Local: 0},
		Callee: mir.Callee{Kind: mir.CalleeSym, Name: "__add"},
		Args: []mir.Operand{
			{Kind: mir.OperandConst, Type: intType},
			{Kind: mir.OperandConst, Type: intType},
		},
	}

	if got := vm.resolveCallTarget(NewFrame(caller), call); got != nil {
		t.Fatalf("intrinsic int __add resolved to unrelated Point method: %+v", got)
	}
}
