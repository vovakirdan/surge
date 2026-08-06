//go:build !golden
// +build !golden

package vm_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Backend parity over the ordinary-storage corpus.
//
// The two backends run the SAME sources, which is the whole point: the VM's
// heap is counted and the native heap is not, so an implementation that leans
// on one runtime's bookkeeping passes there and fails here. Parity is compared
// on all three observable channels — stdout, stderr and exit code — because the
// corpus reports failure through an exit code, and two backends that both
// return 1 for different reasons are not in agreement.
//
// This runs BEFORE either backend changes its representation, so the rows are
// green against what exists today. That ordering is deliberate: a parity
// harness written after a cutover cannot tell a divergence the cutover
// introduced from one that was always there.

// TestRuntimeV2OrdinaryStorageParity compares the VM and the native binary on
// every corpus row.
func TestRuntimeV2OrdinaryStorageParity(t *testing.T) {
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	env := envForParity(root)

	for _, fixture := range ordinaryStorageFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			rel := fixture.relPath()

			vmOut, vmErr, vmCode := runSurgeWithEnv(t, root, surge, env, "run", "--backend=vm", rel)

			buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, env, "build", rel)
			if buildCode != 0 {
				t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
			}

			binPath := filepath.Join(root, "target", "debug", fixture.name)
			llOut, llErr, llCode := runBinary(t, binPath)

			if llCode != vmCode {
				t.Fatalf("exit code mismatch: vm=%d llvm=%d\n--- vm stderr ---\n%s\n--- llvm stderr ---\n%s",
					vmCode, llCode, vmErr, llErr)
			}
			if llOut != vmOut {
				t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
			}
			if llErr != vmErr {
				t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
			}

			// Agreeing on the wrong answer is still a divergence from the
			// contract, so the expected result is checked here too rather than
			// left to the VM-only driver.
			if vmCode != 0 {
				t.Fatalf("both backends exited %d, want 0\nstdout:\n%s\nstderr:\n%s", vmCode, vmOut, vmErr)
			}
			if got := strings.TrimSpace(vmOut); got != fixture.want {
				t.Fatalf("both backends printed %q, want %q", got, fixture.want)
			}
		})
	}
}
