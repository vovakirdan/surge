//go:build runtime_v2_pending

package vm_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Before an owned array leaves its shard, the crossing barrier walks its
// buffer: rt_array_unshare_walk hands every element slot to the step that makes
// that element's counted leaves private. The stand these rows drive asks the
// walk the questions the emitter never can, because the emitter only hands it
// an array it already believes is owned:
//
//   - an owned base is walked slot by slot, in layout order, and an empty base
//     or a missing array walks nothing;
//   - a VIEW refuses, because its slots are the base's slots and the base keeps
//     reading them on the origin shard; a BASE some view still reads refuses for
//     the same reason from the other side; a view of a view is a view;
//   - the refusal is about the view that is alive now: once it drops, the same
//     base walks again;
//   - the walk reads the array's length and not its capacity: every base the
//     stand builds has spare slots past its length, and a slot past the run
//     counts as misplaced in the census as well as as an extra call;
//   - the walk holds no lock while it steps: the recording step allocates and
//     releases a block, which re-enters the view registry the way the real
//     step does, and every row runs under a deadline, so a walk that kept the
//     registry lock is a red row by name and not a package timeout.
//
// A refusal is a panic the process does not survive, so the stand runs one row
// per process and a death row is read from the exit status and the report on
// stderr. The negative control cuts the refusal and leaves the walk, and the
// row that dies in the plain build is the row that survives under it.
func buildArrayUnshareWalkStand(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the array unshare-walk proof")
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), name)
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
	}
	args = append(args, extraFlags...)
	args = append(args, "-o", bin,
		filepath.Join(root, "internal", "vm", "testdata", "array_unshare_walk.c"))
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build array unshare-walk stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

const (
	arrayUnshareWalkViewRefusal = "panic VM1003: array view cannot cross a shard boundary: " +
		"its elements live in the base's buffer, which the origin shard keeps; " +
		"cross an owned array instead"
	arrayUnshareWalkLiveViewRefusal = "panic VM1003: array with a live view cannot cross a " +
		"shard boundary: a view on this shard still reads its buffer"
)

type arrayUnshareWalkRow struct {
	name string
	mode string
	// want is the census line of a row that survives; empty for a row that dies.
	want string
	// dies names the refusal a death row must report on stderr.
	dies string
}

func arrayUnshareWalkRows() []arrayUnshareWalkRow {
	return []arrayUnshareWalkRow{
		{
			// Row 1. One call per element, each at data + i * stride, in order.
			name: "owned-base-walks-every-slot",
			mode: "owned-base-walks-every-slot",
			want: "owned-base-walks-every-slot: calls=3 misplaced=0",
		},
		{
			// Row 2. An empty base, a slot holding no array, and no slot.
			name: "empty-and-null-walk-nothing",
			mode: "empty-and-null-walk-nothing",
			want: "empty-and-null-walk-nothing: calls=0",
		},
		{
			// Row 3. A view's slots are the base's slots.
			name: "view-refuses-to-cross",
			mode: "view-refuses-to-cross",
			dies: arrayUnshareWalkViewRefusal,
		},
		{
			// Row 4. A base some view still reads.
			name: "base-with-live-view-refuses-to-cross",
			mode: "base-with-live-view-refuses-to-cross",
			dies: arrayUnshareWalkLiveViewRefusal,
		},
		{
			// Row 5. The view dropped, the same base walks again.
			name: "base-walks-again-after-the-view-drops",
			mode: "base-walks-again-after-the-view-drops",
			want: "base-walks-again-after-the-view-drops: calls=3 misplaced=0",
		},
		{
			// Row 6. A view of a view is a view.
			name: "view-of-a-view-refuses-to-cross",
			mode: "view-of-a-view-refuses-to-cross",
			dies: arrayUnshareWalkViewRefusal,
		},
	}
}

// A row of this stand answers in milliseconds; a row that is still running
// after this long is one whose walk, or whose cleanup, is waiting on a lock
// the walk never gave back. The deadline turns that from a package timeout
// with a stray process into a red row with a name.
const arrayUnshareWalkRowTimeout = 30 * time.Second

func runArrayUnshareWalkStand(t *testing.T, bin, mode string, extraEnv []string) (string, string, int) {
	t.Helper()
	timeout := mtScaledTimeout(t, arrayUnshareWalkRowTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.Command(bin, mode)
	env := os.Environ()
	for _, value := range extraEnv {
		parts := strings.SplitN(value, "=", 2)
		env = overrideEnvVar(env, parts[0], parts[1])
	}
	cmd.Env = env
	result := runCommandWithCancellation(ctx, cmd, subprocessTerminationGrace)
	if result.contextErr != nil {
		t.Fatalf("array unshare-walk stand mode %q did not return within %s; the walk or its cleanup is blocked\nstdout:\n%s\nstderr:\n%s\ncancellation:\n%s",
			mode, timeout, result.stdout, result.stderr, formatCancellationDiagnostics(result))
	}
	if result.runErr != nil {
		t.Fatalf("run array unshare-walk stand mode %q: %v\nstderr:\n%s", mode, result.runErr, result.stderr)
	}
	return result.stdout, result.stderr, result.exitCode
}

func runArrayUnshareWalkRows(t *testing.T, bin string, extraEnv []string) {
	t.Helper()
	for _, row := range arrayUnshareWalkRows() {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runArrayUnshareWalkStand(t, bin, row.mode, extraEnv)
			if row.dies != "" {
				// A death row: the process must not come back, and what it
				// wrote on the way out must be the refusal, by name. A
				// sanitizer report on the way out would also be on stderr and
				// would also be non-zero, so its markers are refused too.
				if code == 0 {
					t.Fatalf("array unshare-walk stand mode %q survived; the walk did not refuse\nstdout:\n%s\nstderr:\n%s",
						row.mode, stdout, stderr)
				}
				if !strings.Contains(stderr, row.dies) {
					t.Fatalf("array unshare-walk stand mode %q died without the refusal (code=%d); want %q\nstdout:\n%s\nstderr:\n%s",
						row.mode, code, row.dies, stdout, stderr)
				}
				for _, marker := range []string{"Sanitizer", "runtime error:"} {
					if strings.Contains(stderr, marker) {
						t.Fatalf("array unshare-walk stand mode %q reported %q on the way out\nstdout:\n%s\nstderr:\n%s",
							row.mode, marker, stdout, stderr)
					}
				}
				if strings.Contains(stdout, "array-unshare-walk: failures=") {
					t.Fatalf("array unshare-walk stand mode %q printed a census after the refusal\nstdout:\n%s",
						row.mode, stdout)
				}
				return
			}
			if code != 0 {
				t.Fatalf("array unshare-walk stand mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
			// A sanitizer report is written to stderr, and a build configured
			// to keep going after one still exits 0. The exit status alone
			// would call that run green, so the silence is asserted too.
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("array unshare-walk stand mode %q wrote to stderr\nstdout:\n%s\nstderr:\n%s",
					row.mode, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("array unshare-walk stand mode %q reported an unexpected census; want %q\nstdout:\n%s",
					row.mode, row.want, stdout)
			}
			if !strings.Contains(stdout, "array-unshare-walk: failures=0") {
				t.Fatalf("array unshare-walk stand mode %q reported failures\nstdout:\n%s",
					row.mode, stdout)
			}
		})
	}
}

func TestRuntimeV2ArrayUnshareWalkWalksAnOwnedBufferAndRefusesAView(t *testing.T) {
	runArrayUnshareWalkRows(t, buildArrayUnshareWalkStand(t, "array_unshare_walk", nil), nil)
}

// The same rows under AddressSanitizer and UndefinedBehaviorSanitizer: the walk
// addresses slots it computed from a stride, and a slot past the run, or a
// header read through a released view, stops being a census number and becomes
// a report at the load.
//
// Leak detection is ON for the rows that survive: this stand starts no
// scheduler, so every block it allocates is one it is supposed to give back. A
// death row ends in the panic reporter's _exit, which runs no leak check, and
// what it must not report is a sanitizer finding on the way to the refusal.
func TestRuntimeV2ArrayUnshareWalkUnderAddressAndUndefinedSanitizers(t *testing.T) {
	bin := buildArrayUnshareWalkStand(t, "array_unshare_walk_asan", []string{
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	runArrayUnshareWalkRows(t, bin, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

// The negative control: RV2_ARRAY_UNSHARE_WALK_NEGATIVE_CONTROL throws the
// registry's answer away and walks anyway. The view row, which dies in the
// plain build, now survives and hands out the base's three slots through the
// view -- which is what the refusal is there to prevent. A build in which this
// row died would mean the plain build's death was not the check's doing.
func TestRuntimeV2ArrayUnshareWalkNegativeControlSkipsTheViewCheck(t *testing.T) {
	bin := buildArrayUnshareWalkStand(t, "array_unshare_walk_negative_control", []string{
		"-DRV2_ARRAY_UNSHARE_WALK_NEGATIVE_CONTROL",
	})
	stdout, stderr, code := runArrayUnshareWalkStand(t, bin, "view-refuses-to-cross", nil)
	if code != 0 {
		t.Fatalf("under the negative control the view row still died (code=%d); the control did not cut the check\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("under the negative control the view row wrote to stderr\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stdout, "view-refuses-to-cross: calls=3") {
		t.Fatalf("under the negative control the view row did not walk the base's three slots\nstdout:\n%s",
			stdout)
	}
	if !strings.Contains(stdout, "array-unshare-walk: failures=0") {
		t.Fatalf("under the negative control the view row reported failures\nstdout:\n%s", stdout)
	}
}
