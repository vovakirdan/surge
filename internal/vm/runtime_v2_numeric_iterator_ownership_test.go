package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func requireNumericIteratorHeapZero(t *testing.T, stderr string) {
	t.Helper()
	if hasValgrindMemcheckError(stderr) || !strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
		t.Fatalf("numeric iterator has a Memcheck error or no clean error summary:\n%s", stderr)
	}
	bytes, blocks := parseValgrindInUseAtExit(t, stderr)
	if bytes != 0 || blocks != 0 {
		t.Fatalf("numeric iterator retained %d bytes in %d blocks including reachable storage; want strict zero:\n%s", bytes, blocks, stderr)
	}
}

func runNumericIteratorValgrind(t *testing.T, source, witness string) {
	t.Helper()
	requireOwnershipValgrind(t, exec.LookPath)
	output := buildRuntimeV2CrossingSource(t, source, nil)
	stdout, stderr, code := runBinaryUnderValgrind(t, output, envWithStdlib(repoRoot(t)), 120*time.Second)
	if code != 0 || stdout != witness+"\n" {
		t.Fatalf("numeric iterator executable: exit=%d stdout=%q\nstderr:\n%s", code, stdout, stderr)
	}
	requireNumericIteratorHeapZero(t, stderr)
}

func TestRuntimeV2NumericIteratorFloatArraysValgrindZero(t *testing.T) {
	runNumericIteratorValgrind(t, numericIteratorArraySource, "numeric-iterator-arrays-witness")
}

func TestRuntimeV2NumericIteratorFloatBoundsValgrindZero(t *testing.T) {
	runNumericIteratorValgrind(t, numericIteratorBoundsSource, "numeric-iterator-bounds-witness")
}
