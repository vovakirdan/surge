package vm_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const asyncAllocationCensusMarker = "ASYNC_ALLOCATION_CENSUS: tasks=0 scopes=0 ready=0 running=0 publishing=0 waiters=0 sleeps=0 inbound=0 reserved=0 segments=1/1"

// Reuse the normal source compiler and its retained native objects. Relinking
// substitutes a test-only main; it does not rebuild or modify runtime code.
func buildAsyncAllocationProgram(t *testing.T, source string) string {
	t.Helper()
	if testing.Short() || strings.TrimSpace(os.Getenv("SURGE_SKIP_TIMEOUT_TESTS")) != "0" {
		t.Fatal("async allocation proof requires non-short mode and explicit SURGE_SKIP_TIMEOUT_TESTS=0")
	}
	for _, tool := range []string{"clang", "ar", "valgrind"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("async allocation proof requires %s on PATH: %v", tool, err)
		}
	}
	ordinary := buildRuntimeV2CrossingSource(t, source, nil)
	tmp := filepath.Join(filepath.Dir(ordinary), ".tmp", filepath.Base(ordinary))
	runtimeDir := filepath.Join(tmp, "native_runtime")
	object := filepath.Join(tmp, "out.o")
	archive := filepath.Join(runtimeDir, "libruntime_native.a")
	for _, path := range []string{object, archive} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing retained source-build object %s: %v", path, err)
		}
	}
	bin := filepath.Join(tmp, "async-allocation-program")
	args := []string{"-std=c11", "-g", "-O0", "-fno-omit-frame-pointer", "-Wall", "-Wextra", "-Werror", "-pthread", "-I" + runtimeDir,
		filepath.Join(repoRoot(t), "internal", "vm", "testdata", "async_allocation_baseline.c"), object, archive, "-o", bin}
	cmd := exec.Command("clang", args...)
	cmd.Dir = repoRoot(t)
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("relink async allocation observer (exit=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	return bin
}

func asyncAllocationEnvironment(t *testing.T, workers string) []string {
	t.Helper()
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "SURGE_") && !strings.HasPrefix(entry, "VALGRIND_OPTS=") {
			env = append(env, entry)
		}
	}
	return append(env, "SURGE_STDLIB="+repoRoot(t), "SURGE_SHARDS=1", "SURGE_THREADS="+workers,
		"SURGE_BLOCKING_THREADS="+workers)
}

func runAsyncAllocationXML(t *testing.T, bin string, env []string, mode, rounds, marker string) []asyncAllocationRecord {
	t.Helper()
	xmlPath := filepath.Join(filepath.Dir(bin), sanitizeTestName(t.Name())+"-"+mode+"-"+rounds+".xml")
	// Keep every workload frame through main. Unwinding below process entry on
	// this toolchain reads argc/argv as callers after _start; those have no symbols.
	// The parser still rejects any missing frame inside the measured call chain.
	args := []string{"--leak-check=full", "--show-leak-kinds=all", "--leak-resolution=high",
		"--num-callers=" + strconv.Itoa(asyncAllocationStackLimit), "--show-below-main=no",
		"--default-suppressions=no", "--error-limit=no", "--errors-for-leak-kinds=definite,indirect",
		"--error-exitcode=97", "--xml=yes", "--xml-file=" + xmlPath, bin, mode, rounds}
	cmd := exec.Command("valgrind", args...)
	cmd.Dir, cmd.Env = repoRoot(t), env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start allocation Valgrind: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(mtScaledTimeout(t, 120*time.Second))
	defer timer.Stop()
	var runErr error
	select {
	case runErr = <-done:
	case <-timer.C:
		wedge := valgrindWedgeReport(cmd.Process.Pid, 120*time.Second, env)
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("allocation Valgrind timed out: %s\nXML: %s\nstdout:\n%s\nstderr:\n%s", wedge, xmlPath, stdout.String(), stderr.String())
	}
	want := asyncAllocationCensusMarker
	if mode != "control" {
		// core/base.sg print calls rt_write_stdout, which writes directly to
		// STDOUT_FILENO. No FILE buffer is left for exit to flush after atexit.
		want = marker + " rounds=" + rounds + "\n" + want
	}
	if runErr != nil || strings.TrimSpace(stdout.String()) != want || strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("allocation subject/control failed: %v\nXML: %s\nwant stdout: %q\nstdout:\n%s\nstderr:\n%s", runErr, xmlPath, want, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatalf("read allocation XML: %v", err)
	}
	records, err := parseAsyncAllocationXML(data)
	if err != nil {
		t.Fatalf("allocation XML rejected: %v (artifact: %s)", err, xmlPath)
	}
	t.Logf("allocation XML: %s", xmlPath)
	return records
}

func runAsyncAllocationBaseline(t *testing.T, bin, marker string, env []string) {
	t.Helper()
	baseline := runAsyncAllocationXML(t, bin, env, "control", "1", marker)
	if err := validateAsyncAllocationBaseline(baseline); err != nil {
		t.Fatalf("unapproved allocation baseline: %v", err)
	}
	var bytes, blocks, possibleBytes, possibleBlocks uint64
	for _, record := range baseline {
		bytes += record.What.Bytes
		blocks += record.What.Blocks
		if record.Kind == "Leak_PossiblyLost" {
			possibleBytes += record.What.Bytes
			possibleBlocks += record.What.Blocks
		}
		origin, _ := asyncAllocationOrigin(record)
		t.Logf("bootstrap origin: %s kind=%s bytes=%d blocks=%d", origin, record.Kind, record.What.Bytes, record.What.Blocks)
	}
	t.Logf("process bootstrap retained: records=%d bytes=%d blocks=%d; matched glibc TLS PossiblyLost bytes=%d blocks=%d (not zero total Memcheck errors)",
		len(baseline), bytes, blocks, possibleBytes, possibleBlocks)
	for _, rounds := range []string{"1", "16"} {
		t.Run("rounds-"+rounds, func(t *testing.T) {
			subject := runAsyncAllocationXML(t, bin, env, "subject", rounds, marker)
			if err := compareAsyncAllocationRecords(baseline, subject); err != nil {
				t.Fatal(err)
			}
		})
	}
}

const asyncAllocationNegativeSource = `
async fn run(rounds: uint) -> int {
    let mut index: uint = 0:uint;
    while index < rounds {
        checkpoint().await();
        index = index + 1:uint;
    }
    print("async-allocation-negative-witness rounds=" + (index to string));
    return 0;
}
@entrypoint("argv")
fn main(rounds: uint) -> int {
    let task = spawn run(rounds);
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

func TestRuntimeV2AsyncAllocationBaselineRejectsRetainedChannel(t *testing.T) {
	bin := buildAsyncAllocationProgram(t, asyncAllocationNegativeSource)
	for _, workers := range []string{"1", "8"} {
		t.Run("workers-"+workers, func(t *testing.T) {
			env := asyncAllocationEnvironment(t, workers)
			const marker = "async-allocation-negative-witness"
			baseline := runAsyncAllocationXML(t, bin, env, "control", "1", marker)
			if err := validateAsyncAllocationBaseline(baseline); err != nil {
				t.Fatal(err)
			}
			positive := runAsyncAllocationXML(t, bin, env, "subject", "1", marker)
			if err := compareAsyncAllocationRecords(baseline, positive); err != nil {
				t.Fatalf("negative's unchanged subject must first pass: %v", err)
			}
			negative := runAsyncAllocationXML(t, bin, env, "retained-channel", "1", marker)
			err := compareAsyncAllocationRecords(baseline, negative)
			if err == nil || !strings.Contains(err.Error(), "extra subject=") || !strings.Contains(err.Error(), "rt_channel_new") {
				t.Fatalf("retained real channel escaped the allocation oracle: %v", err)
			}
			t.Logf("real retained-channel negative rejected after successful logical census: %v", err)
		})
	}
}
