//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func numericEmitRows() []string {
	var rows []string
	for _, kind := range []string{"int", "uint"} {
		for _, operation := range []string{
			"inline-null-retain", "inline-null-drop", "inline-null-explicit-clone",
			"inline-null-glue-copy-drop", "inline-null-unshare", "inline-null-cross-clone",
			"heap-scalar-copy", "heap-glue-copy", "heap-release-last-owner",
			"heap-unshare-unique", "heap-unshare-sibling", "heap-cross-clone",
		} {
			rows = append(rows, kind+"/"+operation)
		}
	}
	return rows
}

func assertNumericEmitRow(t *testing.T, row, stdout, stderr string, code int) {
	t.Helper()
	want := "numeric-emit: row=" + row + " failures=0 outstanding_blocks=0"
	if code != 0 || strings.Contains(stdout, "FAIL ") || strings.Count(stdout, want) != 1 {
		t.Fatalf("emitted numeric row %q failed (code=%d), want one %q\nstdout:\n%s\nstderr:\n%s",
			row, code, want, stdout, stderr)
	}
	t.Logf("emitted numeric row %s:\n%s", row, stdout)
}

func runNumericEmitRows(t *testing.T, flags, env []string) {
	t.Helper()
	m := compileNumericEmitModule(t)
	bin := buildNumericEmitStand(t, m, m.ir, flags)
	for _, row := range numericEmitRows() {
		t.Run(row, func(t *testing.T) {
			stdout, stderr, code := runBignumRefcountStand(t, bin, row, env)
			assertNumericEmitRow(t, row, stdout, stderr, code)
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("emitted numeric row %s wrote to stderr:\n%s", row, stderr)
			}
		})
	}
}

func TestRuntimeV2NumericEmittedLifecycle(t *testing.T) {
	runNumericEmitRows(t, nil, nil)
}

func TestRuntimeV2NumericEmittedLifecycleUnderAddressAndUndefinedSanitizers(t *testing.T) {
	runNumericEmitRows(t, []string{
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer",
	}, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

func TestRuntimeV2NumericEmittedLifecycleValgrindZero(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	m := compileNumericEmitModule(t)
	bin := buildNumericEmitStand(t, m, m.ir, nil)
	for _, row := range numericEmitRows() {
		t.Run(row, func(t *testing.T) {
			env := overrideEnvVar(os.Environ(), "SURGE_NUMERIC_EMIT_ROW", row)
			stdout, stderr, code := runBinaryUnderValgrind(t, bin, env, bignumRefcountRowTimeout)
			assertNumericEmitRow(t, row, stdout, stderr, code)
			bytes, blocks := parseValgrindInUseAtExit(t, stderr)
			if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
				!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
				t.Fatalf("emitted numeric row %q failed strict physical Memcheck: in_use=%d bytes/%d blocks\nstderr:\n%s",
					row, bytes, blocks, stderr)
			}
			t.Log("physical Memcheck: in_use=0 bytes/0 blocks; zero errors")
		})
	}
}

// Each mutant cuts one generated body of one kind. The opposite kind is run
// first as an unchanged control. A linker/parser failure, signal or timeout is
// not the named lifecycle failure this test requires.
func TestRuntimeV2NumericEmittedLifecycleNegativeControls(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	m := compileNumericEmitModule(t)
	for _, kind := range []string{"int", "uint"} {
		for _, mutation := range []struct{ name, operation, failure string }{
			{"scratch", "inline-null-glue-copy-drop", "scratch changed"},
			{"release-guard", "inline-null-glue-copy-drop", "inline numeric lifecycle calls must be zero"},
			{"sibling", "heap-unshare-sibling", "private sibling not detached"},
			{"last-release", "heap-release-last-owner", "all heap owners released"},
		} {
			t.Run(kind+"/"+mutation.name, func(t *testing.T) {
				ir := numericEmitMutant(t, m, kind, mutation.name)
				bin := buildNumericEmitStand(t, m, ir, nil)
				other := "uint"
				if kind == "uint" {
					other = "int"
				}
				control := other + "/" + mutation.operation
				stdout, stderr, code := runBignumRefcountStand(t, bin, control, nil)
				assertNumericEmitRow(t, control, stdout, stderr, code)
				if strings.TrimSpace(stderr) != "" {
					t.Fatalf("unchanged control %s wrote to stderr:\n%s", control, stderr)
				}
				row := kind + "/" + mutation.operation
				stdout, stderr, code = runBignumRefcountStand(t, bin, row, nil)
				assertNumericEmitMutantFailure(t, row, mutation.failure, stdout, stderr, code)
				if strings.TrimSpace(stderr) != "" {
					t.Fatalf("mutant %s wrote to stderr instead of its named failure:\n%s", mutation.name, stderr)
				}
				if mutation.name == "last-release" {
					env := overrideEnvVar(os.Environ(), "SURGE_NUMERIC_EMIT_ROW", row)
					stdout, stderr, code = runBinaryUnderValgrind(t, bin, env, bignumRefcountRowTimeout)
					assertNumericEmitMutantFailure(t, row, mutation.failure, stdout, stderr, code)
					bytes, blocks := parseValgrindInUseAtExit(t, stderr)
					if bytes <= 0 || blocks != 1 || hasValgrindMemcheckError(stderr) {
						t.Fatalf("removed last release did not leave exactly one physical block: %d bytes/%d blocks\n%s", bytes, blocks, stderr)
					}
					t.Logf("last-release mutant physical Memcheck: in_use=%d bytes/%d blocks", bytes, blocks)
				}
			})
		}
	}
}

func assertNumericEmitMutantFailure(t *testing.T, row, failure, stdout, stderr string, code int) {
	t.Helper()
	want := fmt.Sprintf("FAIL %s: %s\n", row, failure)
	if code != 1 || !strings.Contains(stdout, want) ||
		!strings.Contains(stdout, "numeric-emit: row="+row+" failures=") {
		t.Fatalf("mutant failed for the wrong reason: code=%d, want %q\nstdout:\n%s\nstderr:\n%s",
			code, want, stdout, stderr)
	}
	t.Logf("named emitted lifecycle mutant failure:\n%s", stdout)
}
