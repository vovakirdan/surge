package llvm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmittedValueOpsDescriptorIsAdmittedByTheRuntime is RV2-DEBT-230's own bar:
// the runtime's slot preflight admitting a COMPILER-EMITTED descriptor rather
// than a hand-written harness one.
//
// Preflight is static, so the door is rt_slot_control_init, which reaches it
// through the storage preflight.
func TestEmittedValueOpsDescriptorIsAdmittedByTheRuntime(t *testing.T) {
	if status := admitEmittedDescriptor(t, defectNone); status != 0 {
		t.Fatalf("the runtime refused a correctly emitted descriptor with status %d", status)
	}
}

// TestADefectiveDescriptorIsRefusedByTheRuntime is the negative control the
// closing condition of RV2-DEBT-230 names: the same emission path, a planted
// fault, and the runtime saying no. Without it, admission proves only that
// something was accepted, not that acceptance means anything.
//
// Both arms must report RT_SLOT_CONTROL_INVALID_OPERATIONS (14): one breaks the
// unconditional plan_cross pair, the other the layout arithmetic, and preflight
// refuses both under that one status.
func TestADefectiveDescriptorIsRefusedByTheRuntime(t *testing.T) {
	const invalidOperations = 14
	for _, tc := range []struct {
		name   string
		defect descriptorDefect
	}{
		{"null-plan-cross", defectNullPlanCross},
		{"over-stride", defectOverStride},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status := admitEmittedDescriptor(t, tc.defect); status != invalidOperations {
				t.Fatalf("the runtime returned status %d, want %d for a defective descriptor",
					status, invalidOperations)
			}
		})
	}
}

// admitEmittedDescriptor emits a module with the given defect, links the
// descriptor slice against the real slot control, and returns the status the
// runtime reported: 0 for admitted.
func admitEmittedDescriptor(t *testing.T, defect descriptorDefect) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang unavailable")
	}
	root := llvmTestRepoRoot(t)

	mirMod, result := lowerMIRFromSource(t, valueOpsProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("no operation registry was published")
	}
	ir, err := emitModuleWithDescriptorDefect(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet, defect)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	emitted := parseEmittedDescriptors(t, ir)
	if len(emitted) == 0 {
		t.Fatal("nothing was emitted to admit")
	}
	var probeID uint64
	for id := range emitted {
		probeID = uint64(id)
		break
	}

	// Link only what is being proven: the descriptors the compiler wrote, the
	// bodies they point at, and the struct declarations they are typed by. The
	// whole module would drag the entire runtime in, and none of it is part of
	// the claim. Every line below is verbatim compiler output — this slices the
	// module, it does not rewrite it.
	temp := t.TempDir()
	irPath := filepath.Join(temp, "descriptors.ll")
	if err := os.WriteFile(irPath, []byte(sliceDescriptorIR(t, ir)), 0o600); err != nil {
		t.Fatal(err)
	}
	probeSrc := fmt.Sprintf(`#include "rt_slot_control.h"
#include <stdalign.h>
#include <stdint.h>
#include <stdio.h>

extern const rt_value_ops __surge_value_ops_type%d;

int main(void) {
    const rt_value_ops* ops = &__surge_value_ops_type%d;
    alignas(64) static unsigned char storage[256];
    rt_slot_control control;
    rt_slot_control_status status =
        rt_slot_control_init(&control, 1, ops, 1, (uintptr_t)storage,
                             ops->layout.size, ops->layout.align);
    if (status != RT_SLOT_CONTROL_OK) {
        fprintf(stderr, "status=%%d\n", (int)status);
        return 1;
    }
    return 0;
}
`, probeID, probeID)
	probePath := filepath.Join(temp, "probe.c")
	if err := os.WriteFile(probePath, []byte(probeSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	native := filepath.Join(root, "runtime", "native")
	units := []string{"rt_slot_control.c", "rt_slot_claim.c", "rt_slot_exclusive.c", "rt_value_ops.c"}
	args := make([]string, 0, 6+len(units))
	args = append(args, "-std=c11", "-I", native, "-o", filepath.Join(temp, "probe"), probePath, irPath)
	for _, unit := range units {
		args = append(args, filepath.Join(native, unit))
	}
	if out, err := exec.Command(clang, args...).CombinedOutput(); err != nil {
		t.Fatalf("link probe against the emitted descriptor: %v\n%s", err, out)
	}
	out, runErr := exec.Command(filepath.Join(temp, "probe")).CombinedOutput()
	if runErr == nil {
		return 0
	}
	text := strings.TrimSpace(string(out))
	var status int
	if _, scanErr := fmt.Sscanf(text, "status=%d", &status); scanErr != nil {
		t.Fatalf("probe failed without a status: %v\n%s", runErr, text)
	}
	return status
}

// sliceDescriptorIR keeps the emitted struct declarations, move bodies, the
// plan_cross stub and the descriptor constants, and adds only the declarations
// LLVM needs for the intrinsics those bodies call.
func sliceDescriptorIR(t *testing.T, ir string) string {
	t.Helper()
	var out strings.Builder
	lines := strings.Split(ir, "\n")
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		switch {
		case strings.HasPrefix(line, "%struct."),
			strings.HasPrefix(line, "@__surge_value_ops_type"):
			out.WriteString(line + "\n")
		case strings.HasPrefix(line, "define void @move_init.type"),
			strings.HasPrefix(line, "define internal zeroext i32 @__surge_value_plan_cross_unavailable"):
			for ; index < len(lines); index++ {
				out.WriteString(lines[index] + "\n")
				if lines[index] == "}" {
					break
				}
			}
		}
	}
	out.WriteString("declare void @llvm.memcpy.p0.p0.i64(ptr, ptr, i64, i1)\n")
	out.WriteString("declare void @llvm.trap()\n")
	out.WriteString("declare void @rt_value_copy_init_unbound_trap(ptr, ptr)\n")
	return out.String()
}
