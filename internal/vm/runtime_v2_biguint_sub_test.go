//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func biguintSubRows() []bignumRefcountRow {
	return []bignumRefcountRow{
		{name: "equal-same-heap", want: "operation_allocs=0 operation_frees=0"},
		{name: "equal-distinct-heaps", want: "operation_allocs=0 operation_frees=0"},
		{name: "unequal-positive", want: "operation_allocs=2 operation_frees=2"},
		{name: "unequal-underflow", want: "operation_allocs=0 operation_frees=0"},
	}
}

func assertBiguintSubRow(t *testing.T, row bignumRefcountRow, stdout, stderr string, code int) {
	t.Helper()
	if code != 0 || !strings.Contains(stdout, row.name+": "+row.want) ||
		!strings.Contains(stdout, row.name+": outstanding_blocks=0") ||
		!strings.Contains(stdout, "biguint-sub: failures=0") {
		t.Fatalf("biguint subtraction row %q failed (code=%d), want %q and no outstanding blocks\nstdout:\n%s\nstderr:\n%s",
			row.name, code, row.want, stdout, stderr)
	}
}

func runBiguintSubRows(t *testing.T, name string, flags, env []string) {
	t.Helper()
	bin := buildBignumNativeStand(t, name, "biguint_sub.c", flags)
	for _, row := range biguintSubRows() {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runBignumRefcountStand(t, bin, row.name, env)
			assertBiguintSubRow(t, row, stdout, stderr, code)
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("biguint subtraction row %q wrote to stderr\n%s", row.name, stderr)
			}
		})
	}
}

// Both equal rows exercise the exported heap path and bu_sub's error output.
// Each input has two owners, checked after both calls; input setup and teardown
// are outside the operation census. The two unequal rows guard the comparison
// split, including its status-bearing underflow path, which must stay intact.
func TestRuntimeV2BiguintSubZeroAndOrder(t *testing.T) {
	runBiguintSubRows(t, "biguint_sub", nil, nil)
}

func TestRuntimeV2BiguintSubUnderAddressAndUndefinedSanitizers(t *testing.T) {
	runBiguintSubRows(t, "biguint_sub_asan", []string{
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all",
		"-fno-omit-frame-pointer", "-O1", "-g",
	}, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

// The old comparison allocates one unreachable two-limb result per equal
// subtraction. Each equal row calls the public API and the raw operation, so
// exactly two allocations leak. The order controls must remain green.
func TestRuntimeV2BiguintSubEqualNegativeControl(t *testing.T) {
	bin := buildBignumNativeStand(t, "biguint_sub_equal_negative", "biguint_sub.c",
		[]string{"-DRV2_BIGUINT_SUB_EQUAL_NEGATIVE_CONTROL"})
	for _, row := range biguintSubRows() {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runBignumRefcountStand(t, bin, row.name, nil)
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("biguint subtraction mutant row %q wrote to stderr\n%s", row.name, stderr)
			}
			if !strings.HasPrefix(row.name, "equal-") {
				assertBiguintSubRow(t, row, stdout, stderr, code)
				return
			}
			if code != 1 || !strings.Contains(stdout, row.name+": operation_allocs=2 operation_frees=0") ||
				!strings.Contains(stdout, row.name+": outstanding_blocks=2") ||
				!strings.Contains(stdout, "FAIL subtraction has the expected allocation count") ||
				!strings.Contains(stdout, "FAIL all input and result blocks are released") ||
				!strings.Contains(stdout, "biguint-sub: failures=2") {
				t.Fatalf("biguint equality mutant row %q failed for the wrong reason (code=%d); want exactly two leaked allocations\nstdout:\n%s\nstderr:\n%s",
					row.name, code, stdout, stderr)
			}
		})
	}
}

// Allocator byte accounting drifts on existing trimmed results. Physical
// Memcheck evidence must independently show zero bytes/blocks still allocated
// at exit, including reachable blocks, and a present zero-error summary.
func TestRuntimeV2BiguintSubValgrindZero(t *testing.T) {
	bin := buildBignumNativeStand(t, "biguint_sub_valgrind", "biguint_sub.c", []string{"-g"})
	requireOwnershipValgrind(t, exec.LookPath)
	for _, row := range biguintSubRows() {
		t.Run(row.name, func(t *testing.T) {
			env := overrideEnvVar(os.Environ(), "SURGE_BIGUINT_SUB_ROW", row.name)
			stdout, stderr, code := runBinaryUnderValgrind(t, bin, env, bignumRefcountRowTimeout)
			assertBiguintSubRow(t, row, stdout, stderr, code)
			bytes, blocks := parseValgrindInUseAtExit(t, stderr)
			if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
				!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
				t.Fatalf("biguint subtraction row %q failed strict physical Memcheck: in_use=%d bytes/%d blocks; want zero and no errors\nstderr:\n%s",
					row.name, bytes, blocks, stderr)
			}
		})
	}
}
