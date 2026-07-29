package llvm

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
)

// A drop of a PROJECTED place used to emit nothing at all. `placeBaseType`
// refused any projection, `emitInstrDrop` swallowed that error with a bare
// `return nil`, and the statement compiled into silence — which is why the
// language could not offer `@drop o.inner` and why a residual drop had no
// backend to lower to.
//
// There is no source syntax that produces one yet (Epic 24 step 8 lifts the
// gate), so the instruction is injected into lowered MIR. That is the point:
// the emitter has to be correct BEFORE anything is allowed to produce the
// shape, or the first program to try it silently leaks.
func TestEmitDropOfProjectedPlaceFreesTheField(t *testing.T) {
	const src = `
type Holder = { note: string, id: int }

fn build() -> int {
	let h: Holder = Holder{ note: "kept", id: 1 };
	return h.id;
}

@entrypoint
fn main() -> int { return build(); }
`
	mirMod, result := lowerMIRFromSource(t, src)

	fn := findFuncByName(t, mirMod, "build")
	local, ok := findLocalByName(fn, "h")
	if !ok {
		t.Fatalf("no local named `h` in the lowered function")
	}

	// The assertions look at the BODY of `build`, not at the module. The
	// composite glue calls `rt_string_free` internally, so a module-wide search
	// for it passes whatever the drop does — the first version of this test did
	// exactly that and survived reverting the fix it was written for.
	before, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit before rewrite: %v", err)
	}
	beforeBody := functionBody(t, before, fn.ID)
	if !strings.Contains(beforeBody, "call void @drop.type") {
		t.Fatalf("expected `build` to drop the whole composite before the rewrite:\n%s", beforeBody)
	}

	// Now make that drop act on `h.note` instead of on `h`.
	if !retargetDropToField(fn, local, "note") {
		t.Fatalf("no drop of `h` found to retarget")
	}

	after, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit after rewrite: %v", err)
	}
	afterBody := functionBody(t, after, fn.ID)

	// A string field is freed directly, not through the composite glue: the
	// glue would take `id` and the box with it, and not taking them is the
	// entire point of a residual drop.
	if strings.Contains(afterBody, "call void @drop.type") {
		t.Fatalf("a projected drop still called the whole-composite glue:\n%s", afterBody)
	}
	if !strings.Contains(afterBody, "call void @rt_string_free") {
		t.Fatalf("a projected drop of a string field freed nothing:\n%s", afterBody)
	}
}

// functionBody extracts one function's IR so an assertion cannot be satisfied
// by an unrelated part of the module — a helper, or the drop glue itself.
// Functions are emitted as `@fn.<id>`, not under their source name.
func functionBody(t *testing.T, ir string, id mir.FuncID) string {
	t.Helper()
	marker := fmt.Sprintf("define %s@fn.%d(", "", id)
	start := strings.Index(ir, marker)
	if start < 0 {
		// The return type sits between `define` and the name.
		needle := fmt.Sprintf("@fn.%d(", id)
		for idx := 0; ; {
			at := strings.Index(ir[idx:], needle)
			if at < 0 {
				break
			}
			at += idx
			lineStart := strings.LastIndex(ir[:at], "\n") + 1
			if strings.HasPrefix(ir[lineStart:], "define ") {
				start = lineStart
				break
			}
			idx = at + len(needle)
		}
	}
	if start < 0 {
		t.Fatalf("no function @fn.%d in the emitted IR", id)
	}
	rest := ir[start:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func findFuncByName(t *testing.T, mod *mir.Module, name string) *mir.Func {
	t.Helper()
	for _, fn := range mod.Funcs {
		if fn != nil && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("no function named %q in the lowered module", name)
	return nil
}

func findLocalByName(fn *mir.Func, name string) (mir.LocalID, bool) {
	for i := range fn.Locals {
		if fn.Locals[i].Name == name {
			return mir.LocalID(i), true
		}
	}
	return mir.NoLocalID, false
}

// retargetDropToField rewrites the first whole-local drop of local into a drop
// of one of its fields, which is the shape a residual drop lowers to.
func retargetDropToField(fn *mir.Func, local mir.LocalID, field string) bool {
	for b := range fn.Blocks {
		for i := range fn.Blocks[b].Instrs {
			ins := &fn.Blocks[b].Instrs[i]
			if ins.Kind != mir.InstrDrop {
				continue
			}
			if ins.Drop.Place.Local != local || len(ins.Drop.Place.Proj) != 0 {
				continue
			}
			ins.Drop.Place.Proj = []mir.PlaceProj{{
				Kind:      mir.PlaceProjField,
				FieldName: field,
				FieldIdx:  -1,
			}}
			return true
		}
	}
	return false
}

// A SHALLOW drop frees the container's own box and calls no glue: the fields
// still in it moved away, and the glue would free them along with everything
// else. This is the closing move of a residual drop, and the reason one can be
// expressed without writing a sentinel into the moved field.
func TestEmitShallowDropFreesTheBoxWithoutTheGlue(t *testing.T) {
	const src = `
type Holder = { note: string, id: int }

fn build() -> int {
	let h: Holder = Holder{ note: "kept", id: 1 };
	return h.id;
}

@entrypoint
fn main() -> int { return build(); }
`
	mirMod, result := lowerMIRFromSource(t, src)
	fn := findFuncByName(t, mirMod, "build")
	local, ok := findLocalByName(fn, "h")
	if !ok {
		t.Fatalf("no local named `h` in the lowered function")
	}

	if !markDropShallow(fn, local) {
		t.Fatalf("no drop of `h` found to mark shallow")
	}

	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	body := functionBody(t, ir, fn.ID)

	if strings.Contains(body, "call void @drop.type") {
		t.Fatalf("a shallow drop still called the recursive glue:\n%s", body)
	}
	if !strings.Contains(body, "call void @rt_free") {
		t.Fatalf("a shallow drop freed no box:\n%s", body)
	}
}

// markDropShallow flags the first whole-local drop of local as shallow.
func markDropShallow(fn *mir.Func, local mir.LocalID) bool {
	for b := range fn.Blocks {
		for i := range fn.Blocks[b].Instrs {
			ins := &fn.Blocks[b].Instrs[i]
			if ins.Kind != mir.InstrDrop || ins.Drop.Place.Local != local || len(ins.Drop.Place.Proj) != 0 {
				continue
			}
			ins.Drop.Shallow = true
			return true
		}
	}
	return false
}
