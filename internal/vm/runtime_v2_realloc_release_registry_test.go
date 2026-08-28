//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A reallocation releases the old block, and this runtime has one release:
// rt_free, which is what tells the array view registry to forget an address.
//
// The stand these rows drive builds a base array and a slice of it — the pair
// the registry records — and then reallocates one of the two headers. The
// registry is the instrument because both sides of a recorded pair are raw
// ADDRESSES: a registration outlives the block it names unless the release
// says otherwise, so counting the registrations that still name a released
// address reads the question directly, with no sanitizer and no dependence on
// whether the allocator happened to move the block.
//
// Three questions become answerable:
//
//   - a release that did not go through rt_free leaves its registrations
//     standing, which the census sees the instant the call returns;
//   - a stale VIEW registration is written through when the base grows, since
//     syncing views stores the moved element run into every view it knows;
//   - a stale BASE registration is read through when the view is sliced again,
//     since resolving a view's base hands back the address the registry holds.
//
// A fourth row asks what a reallocation owes when the block it replaces was
// declared zero-sized: the block behind the pointer is real whatever the caller
// said, and the counters are where one nobody released shows up.
func buildReallocReleaseRegistryStand(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the reallocation-release proof")
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
		filepath.Join(root, "internal", "vm", "testdata", "realloc_release_registry.c"))
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build reallocation-release stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

type reallocReleaseRow struct {
	name string
	mode string
	want string
}

func reallocReleaseRows() []reallocReleaseRow {
	return []reallocReleaseRow{
		{
			// The census on its own. This row needs no sanitizer and does not
			// care whether the allocator moved anything: rt_free either ran on
			// the released address or it did not.
			name: "release-forgets-registration",
			mode: "release-forgets-registration",
			want: "release-forgets-registration: stale=0",
		},
		{
			// A stale view registration, written through.
			name: "released-view-then-base-grows",
			mode: "released-view-then-base-grows",
			want: "released-view-then-base-grows: stale=0 len=72",
		},
		{
			// A stale base registration, read through.
			name: "released-base-then-view-slices",
			mode: "released-base-then-view-slices",
			want: "released-base-then-view-slices: stale=0 nested_len=8",
		},
		{
			// The release a zero old size still owes.
			name: "zero-old-size-still-releases",
			mode: "zero-old-size-still-releases",
			want: "zero-old-size-still-releases: live_delta=0",
		},
	}
}

func runReallocReleaseRows(t *testing.T, bin string, extraEnv []string) {
	t.Helper()
	for _, row := range reallocReleaseRows() {
		t.Run(row.name, func(t *testing.T) {
			cmd := exec.Command(bin, row.mode)
			env := os.Environ()
			for _, value := range extraEnv {
				parts := strings.SplitN(value, "=", 2)
				env = overrideEnvVar(env, parts[0], parts[1])
			}
			cmd.Env = env
			stdout, stderr, code := runCommand(t, cmd, "")
			if code != 0 {
				t.Fatalf("reallocation-release stand mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
			// A sanitizer report is written to stderr, and a build configured
			// to keep going after one still exits 0. The exit status alone
			// would call that run green, so the silence is asserted too.
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("reallocation-release stand mode %q wrote to stderr\nstdout:\n%s\nstderr:\n%s",
					row.mode, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("reallocation-release stand mode %q reported an unexpected census; want %q\nstdout:\n%s",
					row.mode, row.want, stdout)
			}
			if !strings.Contains(stdout, "realloc-release-registry: failures=0") {
				t.Fatalf("reallocation-release stand mode %q reported failures\nstdout:\n%s",
					row.mode, stdout)
			}
		})
	}
}

func TestRuntimeV2ReallocReleaseIsForgottenByTheViewRegistry(t *testing.T) {
	runReallocReleaseRows(t, buildReallocReleaseRegistryStand(t, "realloc_release_registry", nil), nil)
}

// The same rows under AddressSanitizer and UndefinedBehaviorSanitizer, where a
// registration left standing over a released block stops being a number and
// becomes a use-after-free at the store or the load that follows it.
//
// Leak detection is ON here, unlike the stands that start the real executor:
// this one starts no scheduler, so every block it allocates is one it is
// supposed to give back, and a fix about RELEASE should be asked whether the
// releases happened. The rows that end in a sanitizer abort never reach their
// own teardown and would report leaks then too — but those rows have already
// failed on the abort, so the extra report costs a red row nothing.
func TestRuntimeV2ReallocReleaseUnderAddressAndUndefinedSanitizers(t *testing.T) {
	bin := buildReallocReleaseRegistryStand(t, "realloc_release_registry_asan", []string{
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	runReallocReleaseRows(t, bin, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}
