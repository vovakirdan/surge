//go:build runtime_v2_pending

package vm_test

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This observes the native binary64 promotion API directly. The VM uses its
// own numeric representation and cannot witness this C conversion defect.
func bigfloatFromF64ExactRows() []string {
	return []string{
		"positive_2p62", "negative_2p62", "positive_2p63", "negative_2p63",
		"positive_2p63_toward_zero", "negative_2p63_toward_zero",
		"positive_2p63_away_zero", "negative_2p63_away_zero",
		"positive_fraction", "negative_fraction", "binary_point_one", "next_above_one",
		"minimum_subnormal", "minimum_normal", "maximum_finite",
		"positive_zero", "negative_zero", "nan", "positive_infinity", "negative_infinity",
	}
}

func buildBigfloatFromF64ExactStand(t *testing.T, flags []string) string {
	t.Helper()
	requireNumericProofRun(t)
	if _, err := exec.LookPath("clang"); err != nil {
		t.Fatalf("required exact bigfloat tool clang unavailable: %v", err)
	}
	const source = "bigfloat_from_f64_exact.c"
	bin := buildBignumNativeStand(t, "bigfloat_from_f64_exact", source, flags)
	for _, path := range []string{filepath.Join(repoRoot(t), "internal", "vm", "testdata", source), bin} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("exact bigfloat artifact=%s SHA256=%x", path, sha256.Sum256(data))
	}
	return bin
}

func assertBigfloatFromF64Exact(t *testing.T, row, stdout, stderr string, code int) {
	t.Helper()
	want := "bigfloat-from-f64-exact: row=" + row + " exact=1 released=1\n"
	if code != 0 || stdout != want {
		t.Fatalf("exact bigfloat row %s: exit=%d stdout=%q\nstderr:\n%s", row, code, stdout, stderr)
	}
	t.Log(strings.TrimSpace(stdout))
}

func runBigfloatFromF64ExactRows(t *testing.T, flags, env []string) {
	t.Helper()
	bin := buildBigfloatFromF64ExactStand(t, flags)
	for _, row := range bigfloatFromF64ExactRows() {
		t.Run(row, func(t *testing.T) {
			stdout, stderr, code := runBignumRefcountStand(t, bin, row, env)
			if stderr != "" {
				t.Fatalf("exact bigfloat row %s wrote stderr: exit=%d stdout=%q\n%s", row, code, stdout, stderr)
			}
			assertBigfloatFromF64Exact(t, row, stdout, stderr, code)
		})
	}
}

func TestRuntimeV2BigfloatFromF64Exact(t *testing.T) {
	runBigfloatFromF64ExactRows(t, nil, nil)
}

func TestRuntimeV2BigfloatFromF64ExactUnderAddressAndUndefinedSanitizers(t *testing.T) {
	runBigfloatFromF64ExactRows(t, []string{
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-O1", "-g",
	}, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

func TestRuntimeV2BigfloatFromF64ExactValgrindZero(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	bin := buildBigfloatFromF64ExactStand(t, nil)
	for _, row := range bigfloatFromF64ExactRows() {
		t.Run(row, func(t *testing.T) {
			env := overrideEnvVar(os.Environ(), "SURGE_BIGFLOAT_EXACT_ROW", row)
			stdout, stderr, code := runBinaryUnderValgrind(t, bin, env, bignumRefcountRowTimeout)
			// Inspect the physical census even when conversion returns a wrong
			// value: cleanup must remain observable in the pre-fix snapshot.
			bytes, blocks := parseValgrindInUseAtExit(t, stderr)
			if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
				!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
				t.Fatalf("exact bigfloat row %s physical Memcheck: exit=%d in_use=%d bytes/%d blocks; want strict zero\nstdout:\n%s\nstderr:\n%s",
					row, code, bytes, blocks, stdout, stderr)
			}
			t.Log("physical Memcheck: in_use=0 bytes/0 blocks; zero errors")
			assertBigfloatFromF64Exact(t, row, stdout, stderr, code)
		})
	}
}
