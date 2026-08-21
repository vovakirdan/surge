package llvm

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanCrossUnavailableStubIsTerminalWhenCalled is the executing half of the
// owner's RV2-DEBT-232 ruling: plan_cross is required unconditionally, and a
// type with no real crossing implementation binds an INVARIANT TRAP rather than
// a refusal. A trap that returns a status is not a trap — the caller would
// carry on with a plan it never got.
//
// The sibling test reads the stub's BODY and asserts it contains llvm.trap, no
// ret, and is kept alive in llvm.used. That proves what was emitted. This one
// dispatches through the descriptor's own slot and proves what HAPPENS, which
// is a different claim: the emitted body is what actually runs.
//
// TWO THINGS THIS DELIBERATELY DOES NOT ASSERT.
//
// It does not pin the signal. llvm.trap lowers to ud2 (SIGILL) on x86-64 and
// to brk (SIGTRAP) on AArch64, so a signal number is a fact about the LOWERING,
// not about the invariant. Pinning one would make this gate host-specific, the
// exact defect the copy-trap gate's comment warns against.
//
// It does not accept "the child died" as proof. A child can die from a
// mis-sliced module, a wrong symbol in the slot, or a fault inside the probe
// itself, and every one of those would pass a bare "it crashed" assertion. So
// the probe prints and FLUSHES a marker immediately before the dispatch and
// would print a second line after it: the test requires the first present and
// the second absent. Reaching the stub and not coming back is the claim.
func TestPlanCrossUnavailableStubIsTerminalWhenCalled(t *testing.T) {
	text, signalled, runErr := dispatchPlanCrossStub(t, defectNone)
	if runErr == nil {
		t.Fatalf("the probe returned normally; the stub is not terminal\n%s", text)
	}
	if !strings.Contains(text, "about to dispatch") {
		t.Fatalf("the probe died BEFORE the dispatch, so this proves nothing about the stub: %v\n%s",
			runErr, text)
	}
	if strings.Contains(text, "plan_cross returned status=") {
		t.Fatalf("the stub RETURNED instead of trapping; a trap that returns a status is not a trap\n%s", text)
	}
	if strings.Contains(text, "plan_cross is null") {
		t.Fatalf("plan_cross was null, but preflight requires it unconditionally\n%s", text)
	}
	// Signalled rather than a chosen signal number: which signal llvm.trap
	// lowers to is a property of the target, and this gate must not be.
	if !signalled {
		t.Fatalf("the stub ended the process without a fault (%v); an invariant trap must not exit cleanly\n%s",
			runErr, text)
	}
}

// TestAReturningPlanCrossStubIsCaught is this gate's own negative control. With
// the stub emitting `ret i32 0` through the same writer, the probe comes back
// with a status — and if this test cannot tell that apart from a trap, it was
// never proving terminality in the first place.
func TestAReturningPlanCrossStubIsCaught(t *testing.T) {
	text, _, runErr := dispatchPlanCrossStub(t, defectReturningPlanCrossStub)
	if runErr != nil {
		t.Fatalf("a returning stub should let the probe finish, but it failed: %v\n%s", runErr, text)
	}
	if !strings.Contains(text, "plan_cross returned status=") {
		t.Fatalf("the returning stub did not report a status, so the control proves nothing\n%s", text)
	}
}

// dispatchPlanCrossStub emits a module with the given defect, links the
// descriptor slice against the real slot control, dispatches through the
// descriptor's plan_cross slot, and reports what the child did.
func dispatchPlanCrossStub(t *testing.T, defect descriptorDefect) (string, bool, error) {
	t.Helper()
	clang, lookErr := exec.LookPath("clang")
	if lookErr != nil {
		t.Skip("clang unavailable")
	}
	root := llvmTestRepoRoot(t)

	mirMod, result := lowerMIRFromSource(t, valueOpsProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("no operation registry was published")
	}
	ir, emitErr := emitModuleWithDescriptorDefect(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet, defect)
	if emitErr != nil {
		t.Fatalf("emit: %v", emitErr)
	}
	emitted := parseEmittedDescriptors(t, ir)
	if len(emitted) == 0 {
		t.Fatal("nothing was emitted to dispatch through")
	}
	var probeID uint64
	for id := range emitted {
		probeID = uint64(id)
		break
	}

	temp := t.TempDir()
	irPath := filepath.Join(temp, "descriptors.ll")
	if err := os.WriteFile(irPath, []byte(sliceDescriptorIR(t, ir)), 0o600); err != nil {
		t.Fatal(err)
	}

	// The dispatch goes through the descriptor's own slot rather than calling
	// the symbol by name: what is being proven is that the SLOT a real
	// descriptor carries is terminal, not that some function somewhere traps.
	// Same reason as the admit probe: a sliced drop body names its reclamation,
	// which this claim never dispatches.
	var stubs strings.Builder
	for _, symbol := range dropBodyRuntimeCallees(ir) {
		fmt.Fprintf(&stubs, "void %s(void* unused) { (void)unused; }\n", symbol)
	}
	probeSrc := fmt.Sprintf(`#include "rt_slot_control.h"
#include <stdalign.h>
#include <stdint.h>
#include <stdio.h>

extern const rt_value_ops __surge_value_ops_type%d;
%s
int main(void) {
    const rt_value_ops* ops = &__surge_value_ops_type%d;
    alignas(64) static unsigned char storage[256];
    rt_cross_plan plan;

    if (ops->plan_cross == NULL) {
        fprintf(stderr, "plan_cross is null, so nothing was dispatched\n");
        fflush(stderr);
        return 2;
    }
    fprintf(stderr, "about to dispatch\n");
    fflush(stderr);
    rt_carrier_status status = ops->plan_cross((const void*)storage, (rt_cross_mode)0, &plan);
    fprintf(stderr, "plan_cross returned status=%%u\n", (unsigned)status);
    fflush(stderr);
    return 0;
}
`, probeID, stubs.String(), probeID)
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

	out, err := exec.Command(filepath.Join(temp, "probe")).CombinedOutput()
	// Whether the child was SIGNALLED is the property this gate needs, and it
	// is read here so callers never touch the platform-specific shape. Which
	// signal llvm.trap lowers to differs by target, so the signal number is
	// deliberately not reported.
	signalled := false
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if sys, ok := exitErr.Sys().(interface{ Signaled() bool }); ok {
			signalled = sys.Signaled()
		}
	}
	return string(out), signalled, err
}
