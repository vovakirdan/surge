package vm_test

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/types"
)

func numericInitializerSource(kind, form string) string {
	lower, upper := "11:"+kind, "14:"+kind
	if kind == "int" || kind == "uint" {
		lower = "(9223372036854775811:uint64):" + kind
		upper = "(9223372036854775814:uint64):" + kind
	}
	declarations, bounds := "", "lower()..upper()"
	if form == "named_bounds" {
		declarations = "let seed: " + kind + " = lower(); let limit: " + kind + " = upper();"
		bounds = "seed..limit"
	} else if form == "cast_end" {
		declarations = "let seed: " + kind + " = lower();"
		bounds = "seed..(" + upper + ")"
	}
	return fmt.Sprintf(`
fn lower() -> %[1]s { return %[2]s; }
fn upper() -> %[1]s { return %[3]s; }
fn probe() -> int {
    %[4]s
    let mut seen: int64 = 0:int64;
    for x in %[5]s { seen = seen + 1:int64; }
    return seen to int;
}
@entrypoint fn main() -> int { return probe() - 3; }
`, kind, lower, upper, declarations, bounds)
}

func TestNumericRangeInitializerOwnsTemporaryBoundsSource(t *testing.T) {
	for _, form := range []string{"named_bounds", "call_bounds", "cast_end"} {
		for _, kind := range []string{"int", "uint"} {
			t.Run(kind+"_"+form, func(t *testing.T) {
				checkNumericInitializerSource(t, kind, form, true)
			})
		}
	}
}

func TestNumericRangeInitializerFixedWidthSource(t *testing.T) {
	for _, kind := range []string{"int64", "uint64"} {
		t.Run(kind+"_call_bounds", func(t *testing.T) {
			checkNumericInitializerSource(t, kind, "call_bounds", false)
		})
	}
}

// These stores initialize the very locals the first Range comparison reads.
// A retain elsewhere cannot make a borrowing initializer an owning one.
func checkNumericInitializerSource(t *testing.T, kind, form string, counted bool) {
	t.Helper()
	mod, _, typeInfo := compileToMIRFromSource(t, numericInitializerSource(kind, form))
	var probe *mir.Func
	for _, f := range mod.Funcs {
		if f.Name == "probe" {
			if probe != nil {
				t.Fatal("multiple probe functions")
			}
			probe = f
		}
	}
	if probe == nil {
		t.Fatal("probe function is absent")
	}
	owners := map[string]mir.LocalID{}
	for id, local := range probe.Locals {
		role := ""
		if local.Name == "x" {
			role = "current"
		} else if strings.HasPrefix(local.Name, "__end") {
			role = "end"
		}
		if role == "" {
			continue
		}
		if _, exists := owners[role]; exists {
			t.Fatalf("duplicate %s bound local", role)
		}
		owners[role] = mir.LocalID(id)
		tt, ok := typeInfo.Lookup(local.Type)
		wantKind := types.KindInt
		if strings.HasPrefix(kind, "uint") {
			wantKind = types.KindUint
		}
		if !ok || tt.Kind != wantKind || (tt.Width == types.WidthAny) != counted || (local.Flags&mir.LocalFlagOwnsHeap != 0) != counted {
			t.Fatalf("%s local has wrong numeric type/ownership: %+v", role, local)
		}
	}
	if len(owners) != 2 {
		t.Fatalf("bound locals = %v, want current and end", owners)
	}
	headers := 0
	for _, block := range probe.Blocks {
		for _, ins := range block.Instrs {
			if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueBinaryOp {
				continue
			}
			bin := ins.Assign.Src.Binary
			if bin.Op == ast.ExprBinaryLess && bin.Left.Kind == mir.OperandCopy && bin.Right.Kind == mir.OperandCopy && bin.Left.Place.Local == owners["current"] && bin.Right.Place.Local == owners["end"] {
				headers++
			}
		}
	}
	if headers != 1 {
		t.Fatalf("found %d Range comparisons of these bounds, want 1", headers)
	}
	for role, local := range owners {
		stores := 0
		for index, ins := range probe.Blocks[probe.Entry].Instrs {
			if ins.Kind != mir.InstrAssign || ins.Assign.Dst.Local != local || len(ins.Assign.Dst.Proj) != 0 {
				continue
			}
			stores++
			if ins.Assign.Src.Kind != mir.RValueUse {
				t.Fatalf("%s initializer is not a direct value use", role)
			}
			op := ins.Assign.Src.Use
			if counted && op.Kind != mir.OperandRetain {
				t.Errorf("%s initializer borrows %s; its owning loop local must retain before temporary cleanup", role, op.Kind)
			}
			if !counted && op.Kind == mir.OperandRetain {
				t.Errorf("fixed %s initializer unexpectedly retains", role)
			}
			if counted {
				drops := 0
				for _, after := range probe.Blocks[probe.Entry].Instrs[index+1:] {
					if after.Kind == mir.InstrDrop && after.Drop.Place.Local == op.Place.Local && len(after.Drop.Place.Proj) == 0 {
						drops++
					}
				}
				wantDrops := 1
				if form == "named_bounds" || (form == "cast_end" && role == "current") {
					wantDrops = 0
				}
				if drops != wantDrops {
					t.Errorf("%s initializer input has %d subsequent statement drops, want %d", role, drops, wantDrops)
				}
			}
		}
		if stores != 1 {
			t.Fatalf("%s has %d entry initializers, want 1", role, stores)
		}
		if !counted {
			for _, block := range probe.Blocks {
				for _, ins := range block.Instrs {
					if ins.Kind == mir.InstrDrop && ins.Drop.Place.Local == local {
						t.Errorf("fixed %s has a counted loop drop", role)
					}
				}
			}
		}
	}
}
