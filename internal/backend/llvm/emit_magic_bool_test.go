package llvm

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

func TestEmitBoolComparisonsUseI1(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRootFromLLVMTest(t))
	source := `fn bool_equal(a: bool, b: bool) -> bool {
    return a == b;
}

fn bool_unequal(a: bool, b: bool) -> bool {
    return a != b;
}

@entrypoint
fn main() -> int {
    if bool_equal(true, false) {
        return 1;
    }
    if bool_unequal(true, false) {
        return 0;
    }
    return 2;
}
`
	module, result := lowerMIRFromSource(t, source)
	ir, err := EmitModule(module, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit bool comparisons: %v", err)
	}
	for _, tt := range []struct {
		name string
		op   string
	}{
		{name: "bool_equal", op: "eq"},
		{name: "bool_unequal", op: "ne"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := functionBody(t, ir, findMIRFunc(t, module, tt.name).ID)
			if got := strings.Count(body, " = icmp "+tt.op+" i1 "); got != 1 {
				t.Fatalf("expected one %s comparison of i1 parameters, got %d:\n%s", tt.op, got, body)
			}
		})
	}
}

func TestMagicBinaryBoolEligibility(t *testing.T) {
	in := types.NewInterner()
	boolType := in.Builtins().Bool
	intType := in.Builtins().Int64
	fe := &funcEmitter{emitter: &Emitter{types: in}}
	for _, tt := range []struct {
		name  string
		op    string
		left  types.TypeID
		right types.TypeID
		want  bool
	}{
		{name: "equal", op: "__eq", left: boolType, right: boolType, want: true},
		{name: "unequal", op: "__ne", left: boolType, right: boolType, want: true},
		{name: "no_arithmetic", op: "__add", left: boolType, right: boolType},
		{name: "no_ordering", op: "__lt", left: boolType, right: boolType},
		{name: "no_bool_integer", op: "__eq", left: boolType, right: intType},
		{name: "no_integer_bool", op: "__ne", left: intType, right: boolType},
		{name: "integer_control", op: "__add", left: intType, right: intType, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			call := &mir.CallInstr{Args: []mir.Operand{
				{Kind: mir.OperandCopy, Type: tt.left},
				{Kind: mir.OperandCopy, Type: tt.right},
			}}
			if got := fe.canEmitMagicBinary(call, tt.op); got != tt.want {
				t.Fatalf("%s eligibility for types %d/%d: want %t, got %t", tt.op, tt.left, tt.right, tt.want, got)
			}
		})
	}
}
