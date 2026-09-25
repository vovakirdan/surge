package vm_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// A NULL `Range` that is USED is a runtime error on both backends, in the VM's
// words (owner ruling 2026-09-25, docs/RUNTIME_V2.md "The Default Of A Runtime
// Handle"). Natively the words come from rt_range_require
// (runtime/native/rt_range.c), which the two slice bounds readers call and the
// emitter calls before a `for` or a `.next()` loads a range's kind byte. Before
// the ruling a null range was the WHOLE range to a slice (range_bounds read
// NULL as "no bounds") and a load through NULL to a `for`.
//
// The D2 return-origin gate refuses every program that reaches a Range default
// at this base, so no end-to-end row can make a null range; this stand hands
// NULL to each native entry point instead, one row per process, and reads a
// death row from its exit status and the report on stderr. The VM half is
// runtime_handle_null_range_internal_test.go, with the same words; the emitted
// half is internal/backend/llvm's TestEmitRangeIterationRequiresALiveRange.

const nullRangeRefusal = "panic VM1203: null range handle"

type nullRangeStandRow struct {
	mode string
	// want is the line a surviving row prints; empty for a death row.
	want string
}

func nullRangeStandRows() []nullRangeStandRow {
	return []nullRangeStandRow{
		// The four entry points a null range reaches: each must refuse it.
		{mode: "array-slice-null"},
		{mode: "array-slice-fixed-null"},
		{mode: "string-slice-null"},
		{mode: "require-null"},
		// Controls: a range that is not null -- `..`, the real whole range --
		// is read, and a null on a lifetime path is nothing.
		{mode: "array-slice-whole", want: "array-slice-whole: len=3"},
		{mode: "string-slice-whole", want: "string-slice-whole: len=6"},
		{mode: "require-live", want: "require-live: returned"},
		{mode: "null-lifetime-is-nothing", want: "null-lifetime-is-nothing: ok"},
	}
}

func buildNullRangeStand(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the null range stand")
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "range_null_handle")
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o", bin, filepath.Join(root, "internal", "vm", "testdata", "range_null_handle.c"),
	}
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build null range stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	return bin
}

func TestRuntimeV2NullRangeIsRefusedByEveryNativeEntryPoint(t *testing.T) {
	bin := buildNullRangeStand(t)
	for _, row := range nullRangeStandRows() {
		t.Run(row.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result := runCommandWithCancellation(ctx, exec.Command(bin, row.mode), subprocessTerminationGrace)
			if result.contextErr != nil {
				t.Fatalf("stand row %q did not return\nstdout:\n%s\nstderr:\n%s", row.mode, result.stdout, result.stderr)
			}
			if result.runErr != nil {
				t.Fatalf("run stand row %q: %v\nstderr:\n%s", row.mode, result.runErr, result.stderr)
			}
			if row.want == "" {
				// A death row: exit 1 through the panic reporter, whose first
				// line is the refusal, and nothing printed after the call.
				first, _, _ := strings.Cut(result.stderr, "\n")
				if result.exitCode != 1 || first != nullRangeRefusal || strings.Contains(result.stdout, ": len=") ||
					strings.Contains(result.stdout, "returned") {
					t.Fatalf("stand row %q: exit=%d, want 1 with %q as the first stderr line and nothing after the call\nstdout:\n%s\nstderr:\n%s",
						row.mode, result.exitCode, nullRangeRefusal, result.stdout, result.stderr)
				}
				return
			}
			if result.exitCode != 0 || strings.TrimSpace(result.stderr) != "" ||
				!strings.Contains(result.stdout, row.want) || !strings.Contains(result.stdout, "range-null-handle: failures=0") {
				t.Fatalf("stand row %q: exit=%d, want 0, no stderr and %q\nstdout:\n%s\nstderr:\n%s",
					row.mode, result.exitCode, row.want, result.stdout, result.stderr)
			}
		})
	}
}
