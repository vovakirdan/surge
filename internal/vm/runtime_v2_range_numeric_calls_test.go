//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

var rangeNumericRows = []string{"int-fixnum", "uint-fixnum", "int-heap", "uint-heap", "float"}

func buildRangeNumericStand(t *testing.T, flags []string) string {
	t.Helper()
	if testing.Short() || os.Getenv("SURGE_SKIP_TIMEOUT_TESTS") != "0" {
		t.Fatal("range lifecycle proof requires -short=false and SURGE_SKIP_TIMEOUT_TESTS=0")
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Fatalf("range lifecycle proof requires clang: %v", err)
	}
	flags = append([]string{"-O0", "-g"}, flags...)
	for _, kind := range []string{"int", "uint", "float"} {
		for _, op := range []string{"retain", "release", "unshare"} {
			flags = append(flags, "-Wl,--wrap=rt_big"+kind+"_"+op)
		}
	}
	return buildBignumNativeStand(t, "range_numeric_calls", "range_numeric_calls.c", flags)
}

func assertRangeNumericRow(t *testing.T, row, stdout, stderr string, code int) {
	t.Helper()
	want := "range-numeric: row=" + row + " failures=0 outstanding_blocks=0"
	if code != 0 || strings.Contains(stdout, "FAIL ") || strings.Count(stdout, want) != 1 {
		t.Fatalf("range lifecycle row %s failed (code=%d), want one %q\nstdout:\n%s\nstderr:\n%s", row, code, want, stdout, stderr)
	}
	t.Log(stdout)
}

func runRangeNumericRows(t *testing.T, flags, env []string) {
	t.Helper()
	bin := buildRangeNumericStand(t, flags)
	for _, row := range rangeNumericRows {
		t.Run(row, func(t *testing.T) {
			stdout, stderr, code := runBignumRefcountStand(t, bin, row, env)
			assertRangeNumericRow(t, row, stdout, stderr, code)
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("range lifecycle row %s wrote to stderr:\n%s", row, stderr)
			}
		})
	}
}

func TestRuntimeV2RangeInlineBoundsAvoidNumericCalls(t *testing.T) {
	runRangeNumericRows(t, nil, nil)
}

func TestRuntimeV2RangeNumericCallsUnderAddressAndUndefinedSanitizers(t *testing.T) {
	runRangeNumericRows(t, []string{
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer",
	}, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

func TestRuntimeV2RangeNumericCallsValgrindZero(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	bin := buildRangeNumericStand(t, nil)
	for _, row := range rangeNumericRows {
		t.Run(row, func(t *testing.T) {
			env := overrideEnvVar(os.Environ(), "SURGE_RANGE_NUMERIC_ROW", row)
			stdout, stderr, code := runBinaryUnderValgrind(t, bin, env, bignumRefcountRowTimeout)
			assertRangeNumericRow(t, row, stdout, stderr, code)
			bytes, blocks := parseValgrindInUseAtExit(t, stderr)
			if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
				strings.Count(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") != 1 {
				t.Fatalf("range lifecycle row %s failed strict physical zero: bytes=%d blocks=%d\n%s", row, bytes, blocks, stderr)
			}
		})
	}
}
