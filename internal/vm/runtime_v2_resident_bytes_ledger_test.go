package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The resident-byte ledger on its own: a balance per kind with a high-water
// mark and a running total, a process-wide balance with its own peak, a
// release that outruns its acquire clamped and counted rather than wrapped,
// and the one TRACE_RESIDENT line that reports all of it.
const residentBytesLedgerHarness = `
#include "rt_resident_bytes.h"

int main(void) {
    rt_resident_bytes_acquire(RT_RESIDENT_PAYLOAD, 10);
    rt_resident_bytes_acquire(RT_RESIDENT_PAYLOAD, 5);
    rt_resident_bytes_acquire(RT_RESIDENT_ENVELOPE, 36);
    rt_resident_bytes_acquire(RT_RESIDENT_SIDECAR, 0);
    rt_resident_bytes_release(RT_RESIDENT_PAYLOAD, 15);
    rt_resident_bytes_release(RT_RESIDENT_ENVELOPE, 40);
    rt_resident_bytes_record_crossing_clone(64);
    rt_resident_bytes_record_crossing_clone(0);
    struct rt_resident_bytes_snapshot s = rt_resident_bytes_snapshot();
    if (s.live[RT_RESIDENT_PAYLOAD] != 0 || s.peak[RT_RESIDENT_PAYLOAD] != 15 ||
        s.acquired[RT_RESIDENT_PAYLOAD] != 15) {
        return 11;
    }
    if (s.live[RT_RESIDENT_ENVELOPE] != 0 || s.peak[RT_RESIDENT_ENVELOPE] != 36 ||
        s.acquired[RT_RESIDENT_ENVELOPE] != 36 || s.underflows != 1) {
        return 12;
    }
    if (s.acquired[RT_RESIDENT_SIDECAR] != 0 || s.peak[RT_RESIDENT_SIDECAR] != 0) {
        return 13;
    }
    if (s.live_total != 0 || s.peak_total != 51) {
        return 14;
    }
    if (s.crossing_clone_bytes != 64 || s.crossing_clones != 1) {
        return 15;
    }
    rt_resident_bytes_dump("ledger");
    return 0;
}
`

func TestRuntimeV2ResidentBytesLedger(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatal("clang is required for the resident-byte ledger check")
	}
	root := repoRoot(t)
	temporary := t.TempDir()
	harness := filepath.Join(temporary, "resident_bytes_ledger.c")
	binary := filepath.Join(temporary, "resident_bytes_ledger")
	if writeErr := os.WriteFile(harness, []byte(residentBytesLedgerHarness), 0o600); writeErr != nil {
		t.Fatalf("write ledger harness: %v", writeErr)
	}
	compile := exec.Command(clang,
		"-std=c11", "-O2", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I", filepath.Join(root, "runtime", "native"),
		filepath.Join(root, "runtime", "native", "rt_resident_bytes.c"),
		harness, "-o", binary,
	)
	if compileOutput, compileErr := compile.CombinedOutput(); compileErr != nil {
		t.Fatalf("compile ledger harness: %v\n%s", compileErr, compileOutput)
	}
	output, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("ledger harness failed: %v\n%s", err, output)
	}
	line := strings.TrimSpace(string(output))
	if !strings.HasPrefix(line, "TRACE_RESIDENT reason=ledger ") || strings.Count(line, "\n") != 0 {
		t.Fatalf("dump must be exactly one TRACE_RESIDENT line: %q", output)
	}
	for _, want := range []string{
		" payload_live=0 ", " payload_peak=15 ", " payload_acquired=15 ",
		" envelope_peak=36 ", " live_total=0 ", " peak_total=51 ",
		" crossing_clone_bytes=64 ", " crossing_clones=1 ", " underflows=1",
	} {
		if !strings.Contains(line+" ", want+" ") && !strings.Contains(line, want) {
			t.Fatalf("dump lacks %q:\n%s", strings.TrimSpace(want), line)
		}
	}
}
