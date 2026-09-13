package mir_test

import (
	"fmt"
	"testing"

	"surge/internal/mir"
)

func TestMIRCompareCopyPayloadOwnership(t *testing.T) {
	for _, tt := range []struct {
		name, declaration, payload string
		composite                  bool
		bindingRead                mir.OperandKind
		bindingDrops, tempDrops    int
	}{
		{"plain", "@copy type Packet = { value: int };", "Packet", true, mir.OperandMove, 1, 0},
		{"alias_chain", "@copy type Packet = { value: int }; type Alias1 = Packet; type Alias2 = Alias1;", "Alias2", true, mir.OperandMove, 1, 0},
		{"generic_instance", "@copy type Pair<T> = (T, T);", "Pair<int>", true, mir.OperandMove, 1, 0},
		{"move_only_control", "type Packet = { value: string };", "Packet", false, mir.OperandMove, 0, 0},
		{"counted_control", "", "int", false, mir.OperandRetain, 1, 1},
		{"reference_control", "", "&int64", false, mir.OperandCopy, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := fmt.Sprintf(compareCopyPayloadMIRSource, tt.declaration, tt.payload, tt.payload, tt.payload)
			compiled := compileCrossingMIR(t, src, nil)
			var fn *mir.Func
			for _, id := range compiled.mod.SortedFuncIDs() {
				candidate := compiled.mod.Funcs[id]
				if candidate != nil && candidate.Name == "take" {
					if fn != nil {
						t.Fatal("multiple take functions reached MIR")
					}
					fn = candidate
				}
			}
			if fn == nil {
				t.Fatal("take did not reach MIR")
			}
			binding, payload := mir.NoLocalID, mir.NoLocalID
			for i, local := range fn.Locals {
				if local.Name == "value" {
					if binding != mir.NoLocalID {
						t.Fatal("multiple value bindings reached MIR")
					}
					binding = mir.LocalID(i)
				}
			}
			for _, block := range fn.Blocks {
				for _, ins := range block.Instrs {
					if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueTagPayload || ins.Assign.Src.TagPayload.TagName != "Payload" {
						continue
					}
					if payload != mir.NoLocalID || !ins.Assign.Src.TagPayload.MoveOut {
						t.Fatal("expected exactly one owned Payload extraction")
					}
					payload = ins.Assign.Dst.Local
				}
			}
			if binding == mir.NoLocalID || payload == mir.NoLocalID {
				t.Fatal("payload binding or extraction did not reach MIR")
			}
			if tt.composite && (!compiled.sema.IsCopyType(fn.Locals[binding].Type) || !compiled.types.IsValueComposite(fn.Locals[binding].Type) || !compiled.sema.OwnsHeap(fn.Locals[binding].Type)) {
				t.Fatal("expected a typed heap-owning Copy composite binding")
			}

			transfer := func(t *testing.T) {
				t.Helper()
				reads := 0
				for _, block := range fn.Blocks {
					for _, ins := range block.Instrs {
						if ins.Kind != mir.InstrAssign || ins.Assign.Dst.Local != binding || ins.Assign.Src.Kind != mir.RValueUse {
							continue
						}
						op := ins.Assign.Src.Use
						if op.Place.Local != payload || len(op.Place.Proj) != 0 || op.Kind != tt.bindingRead {
							t.Fatalf("payload-to-binding read = %+v, want kind %v from payload L%d", op, tt.bindingRead, payload)
						}
						reads++
					}
				}
				if reads != 1 {
					t.Fatalf("payload-to-binding assignments = %d, want 1", reads)
				}
			}
			drop := func(t *testing.T) {
				t.Helper()
				bindingDrops, tempDrops, resultCopies, orderedDrops := 0, 0, 0, 0
				for _, block := range fn.Blocks {
					copied := false
					for _, ins := range block.Instrs {
						if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueUse {
							op := ins.Assign.Src.Use
							if op.Kind == mir.OperandCopyValue && op.Place.Local == binding && len(op.Place.Proj) == 0 {
								copied = true
								resultCopies++
							}
						}
						if ins.Kind != mir.InstrDrop || len(ins.Drop.Place.Proj) != 0 {
							continue
						}
						switch ins.Drop.Place.Local {
						case binding:
							bindingDrops++
							if copied {
								orderedDrops++
							}
						case payload:
							tempDrops++
						}
					}
				}
				if bindingDrops != tt.bindingDrops || tempDrops != tt.tempDrops {
					t.Fatalf("payload binding/temp drops = %d/%d, want %d/%d", bindingDrops, tempDrops, tt.bindingDrops, tt.tempDrops)
				}
				if tt.composite && (resultCopies != 1 || orderedDrops != 1) {
					t.Fatalf("copied arm result/drop after preservation = %d/%d, want 1/1", resultCopies, orderedDrops)
				}
			}
			if tt.composite {
				t.Run("payload_transfer", transfer)
				t.Run("binding_drop", drop)
			} else {
				transfer(t)
				drop(t)
			}
		})
	}
}

const compareCopyPayloadMIRSource = `
tag Payload<T>(T);
tag Empty();
type Outcome<T> = Payload(T) | Empty();
%s

fn take(input: Outcome<%s>, fallback: %s) -> %s {
    return compare input {
        Payload(value) => value;
        Empty() => fallback;
    };
}
`
