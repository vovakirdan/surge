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

// The heap halves of `int` and `uint` carry a reference count, and nothing in a
// compiled program can read it: no emitter calls the entry points yet and the
// count is not reachable from Surge source at all. The stand these rows drive
// asks the runtime directly, in C, what only C can ask:
//
//   - a value small enough to ride inline in the word owns no block, so retain,
//     release, free, clone and unshare each have to answer it from the tag
//     before any load. There is one row per entry point per kind, because a row
//     dies at its FIRST dereference and a row calling all five would only ever
//     be measuring the first one's guard;
//   - a fresh block starts at one, counts up and down with its references, and
//     goes back to the allocator on the release that reaches zero -- once;
//   - unshare, the crossing barrier's leaf, hands back the same block when the
//     caller holds the only reference and a duplicate when a sibling still
//     holds it, which is what leaves each side of a crossing with a block of
//     its own;
//   - and the biguint view of a bigint's tail aliases the int's own count word,
//     which is the price the suffix layout pays and the reason a view may never
//     be retained, released or freed.
//
// "Gave the block back" is read from the allocator's own alloc/free counters
// and, in the sanitizer build, from ASan's leak and double-free reports. It is
// never read from live_bytes: 16 sites shrink a bignum's `len` after allocating
// it and size the free off the shrunken field, so live_bytes drifts upward for
// a reason that predates this lane.
func buildBignumRefcountStand(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the bignum refcount proof")
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
		filepath.Join(root, "internal", "vm", "testdata", "bignum_refcount.c"))
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build bignum refcount stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

type bignumRefcountRow struct {
	name string
	// want is the census line the row prints when it survives.
	want string
}

// The rows, in the order the stand lists them. Each runs in its own process.
func bignumRefcountRows() []bignumRefcountRow {
	rows := []bignumRefcountRow{}
	// Ten fixnum rows: one entry point each, for each kind. Their census is
	// the same sentence -- an inline word and NULL cost no allocation and no
	// free -- because that IS the claim; what differs is which entry point was
	// asked, and that is the row's name.
	for _, kind := range []string{"int", "uint"} {
		for _, entry := range []string{"retain", "release", "free", "clone", "unshare"} {
			name := kind + "-fixnum-" + entry + "-is-uncounted"
			rows = append(rows, bignumRefcountRow{name: name, want: name + ": allocs=0 frees=0"})
		}
	}
	return append(rows,
		bignumRefcountRow{
			// A block off the allocator carries the one reference returned.
			name: "fresh-heap-value-starts-at-one",
			want: "fresh-heap-value-starts-at-one: rc=1",
		},
		bignumRefcountRow{
			// Retains and releases move the count and nothing else; the
			// release that reaches zero gives the block back exactly once.
			name: "heap-value-counts-and-frees-at-zero",
			want: "heap-value-counts-and-frees-at-zero: allocs=0 frees=1",
		},
		bignumRefcountRow{
			// A sole reference travels with its block: no duplicate.
			name: "unshare-at-one-hands-back-the-same-block",
			want: "unshare-at-one-hands-back-the-same-block: same=1 allocs=0 clones=0",
		},
		bignumRefcountRow{
			// A sibling holder forces the duplicate, and exactly one.
			name: "unshare-with-a-sibling-clones-once",
			want: "unshare-with-a-sibling-clones-once: different=1 allocs=1 clones=1",
		},
		bignumRefcountRow{
			// The suffix layout's alias, field by field.
			name: "the-uint-view-of-an-int-aliases-its-count",
			want: "the-uint-view-of-an-int-aliases-its-count: len=1 rc=1 limbs=1",
		},
		bignumRefcountRow{
			name: "biguint-counts-and-frees-at-zero",
			want: "biguint-counts-and-frees-at-zero: allocs=0 frees=1",
		},
		bignumRefcountRow{
			name: "biguint-unshare-at-one-hands-back-the-same-block",
			want: "biguint-unshare-at-one-hands-back-the-same-block: same=1 allocs=0",
		},
		bignumRefcountRow{
			name: "biguint-unshare-with-a-sibling-clones-once",
			want: "biguint-unshare-with-a-sibling-clones-once: different=1 allocs=1 clones=1",
		},
		bignumRefcountRow{
			// A clone is a block of its own at count one, not a demotion back
			// to an inline word and not a second reference to the source.
			name: "clone-is-an-independent-block",
			want: "clone-is-an-independent-block: different=1 allocs=1",
		},
	)
}

// A row of this stand answers in milliseconds: it starts no scheduler, takes no
// lock and allocates a handful of blocks. A row still running after this long
// is one whose call did not return, and the deadline turns that from a package
// timeout with a stray process into a red row with a name.
const bignumRefcountRowTimeout = 30 * time.Second

func runBignumRefcountStand(t *testing.T, bin, mode string, extraEnv []string) (string, string, int) {
	t.Helper()
	timeout := mtScaledTimeout(t, bignumRefcountRowTimeout)
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
		t.Fatalf("bignum refcount stand row %q did not return within %s\nstdout:\n%s\nstderr:\n%s\ncancellation:\n%s",
			mode, timeout, result.stdout, result.stderr, formatCancellationDiagnostics(result))
	}
	if result.runErr != nil {
		t.Fatalf("run bignum refcount stand row %q: %v\nstderr:\n%s", mode, result.runErr, result.stderr)
	}
	return result.stdout, result.stderr, result.exitCode
}

// A surviving row: exit 0, the census it promised, no failed assertion, and
// silence on stderr. The silence matters because a sanitizer writes its report
// there and a build configured to keep going after one still exits 0 -- the
// exit status alone would call such a run green.
func assertBignumRefcountRowSurvives(t *testing.T, row bignumRefcountRow, stdout, stderr string, code int) {
	t.Helper()
	if code != 0 {
		t.Fatalf("bignum refcount row %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			row.name, code, stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("bignum refcount row %q wrote to stderr\nstdout:\n%s\nstderr:\n%s",
			row.name, stdout, stderr)
	}
	if !strings.Contains(stdout, row.want) {
		t.Fatalf("bignum refcount row %q reported an unexpected census; want %q\nstdout:\n%s",
			row.name, row.want, stdout)
	}
	if !strings.Contains(stdout, "bignum-refcount: failures=0") {
		t.Fatalf("bignum refcount row %q reported failures\nstdout:\n%s", row.name, stdout)
	}
}

func runBignumRefcountRows(t *testing.T, bin string, extraEnv []string) {
	t.Helper()
	for _, row := range bignumRefcountRows() {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runBignumRefcountStand(t, bin, row.name, extraEnv)
			assertBignumRefcountRowSurvives(t, row, stdout, stderr, code)
		})
	}
}

// An unknown row name is the stand's own failure with its own exit status, so a
// typo in the list above cannot read as a row that passed. Asserting it here is
// what makes the row list load-bearing rather than decorative.
func TestRuntimeV2BignumRefcountRejectsAnUnknownRow(t *testing.T) {
	bin := buildBignumRefcountStand(t, "bignum_refcount_unknown_row", nil)
	stdout, stderr, code := runBignumRefcountStand(t, bin, "no-such-row", nil)
	if code != 2 {
		t.Fatalf("an unknown row did not report the stand's own failure (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stderr, `bignum-refcount: unknown row "no-such-row"`) {
		t.Fatalf("an unknown row did not name itself on stderr\nstderr:\n%s", stderr)
	}
}

func TestRuntimeV2BignumRefcountCountsHeapValuesAndPassesFixnumsThrough(t *testing.T) {
	runBignumRefcountRows(t, buildBignumRefcountStand(t, "bignum_refcount", nil), nil)
}

// The same rows under AddressSanitizer and UndefinedBehaviorSanitizer, and this
// is where "freed exactly once" is actually decided. Leak detection is ON: the
// stand starts no scheduler, so every block it allocates is one it must give
// back, and a count that reaches zero without freeing is reported as a leak
// while a block freed twice is reported as a double free. Heap accounting
// cannot answer that question -- its live_bytes drifts on every trimmed bignum
// -- which is why the stand asserts counts there and leaves the byte figure to
// ASan.
func TestRuntimeV2BignumRefcountUnderAddressAndUndefinedSanitizers(t *testing.T) {
	bin := buildBignumRefcountStand(t, "bignum_refcount_asan", []string{
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	runBignumRefcountRows(t, bin, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

// What each control cuts, and the rows that must move when it does. A control
// that moved MORE rows than it names would be cutting something else as well;
// a control that moved fewer would mean the row it was supposed to cover is
// green for some other reason. Both are failures here, which is why every row
// is run under every control rather than only the ones expected to go red.
type bignumRefcountControl struct {
	// macro is the compile-time definition the buildpipeline turns a
	// SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL entry into.
	macro string
	// why the control exists, for the failure message.
	cuts string
	// dies are rows the cut kills outright: the process does not come back.
	dies []string
	// reds are rows that survive but report a failed assertion.
	reds []string
}

func bignumRefcountControls() []bignumRefcountControl {
	var fixnumRows []string
	for _, kind := range []string{"int", "uint"} {
		for _, entry := range []string{"retain", "release", "free", "clone", "unshare"} {
			fixnumRows = append(fixnumRows, kind+"-fixnum-"+entry+"-is-uncounted")
		}
	}
	return []bignumRefcountControl{
		{
			macro: "RV2_BIGNUM_TAG_GUARD_NEGATIVE_CONTROL",
			cuts: "the fixnum tag test in all ten entry points, leaving the NULL-only " +
				"guard a bigfloat can afford",
			// Every fixnum row, and only those: an inline word is loaded
			// through as an address and the process dies at that load. A
			// fixnum row still green under this control would mean the tag
			// test is not what was keeping it green.
			dies: fixnumRows,
		},
		{
			macro: "RV2_BIGNUM_RELEASE_FREE_NEGATIVE_CONTROL",
			cuts:  "the free-at-zero tail of both releases",
			// The count still reaches zero; the block is simply not given
			// back, so every row that asserts a free is red and no row that
			// only counts is.
			reds: []string{
				"heap-value-counts-and-frees-at-zero",
				"unshare-with-a-sibling-clones-once",
				"biguint-counts-and-frees-at-zero",
				"biguint-unshare-with-a-sibling-clones-once",
			},
		},
		{
			macro: "RV2_BIGINT_UNSHARE_NEGATIVE_CONTROL",
			cuts:  "the clone branch of the bigint crossing barrier",
			// The shared block travels shared: the caller gets the same
			// pointer back, no block is allocated, and nothing is recorded.
			// Only the bigint sibling row moves -- the uint half has its own
			// control, so a single #ifdef covering both would be caught here.
			reds: []string{"unshare-with-a-sibling-clones-once"},
		},
		{
			macro: "RV2_BIGUINT_UNSHARE_NEGATIVE_CONTROL",
			cuts:  "the clone branch of the biguint crossing barrier",
			reds:  []string{"biguint-unshare-with-a-sibling-clones-once"},
		},
	}
}

func TestRuntimeV2BignumRefcountNegativeControlsCutEachBranch(t *testing.T) {
	for _, control := range bignumRefcountControls() {
		t.Run(control.macro, func(t *testing.T) {
			dies := make(map[string]bool, len(control.dies))
			for _, name := range control.dies {
				dies[name] = true
			}
			reds := make(map[string]bool, len(control.reds))
			for _, name := range control.reds {
				reds[name] = true
			}
			bin := buildBignumRefcountStand(t,
				"bignum_refcount_"+strings.ToLower(control.macro),
				[]string{"-D" + control.macro})
			for _, row := range bignumRefcountRows() {
				t.Run(row.name, func(t *testing.T) {
					stdout, stderr, code := runBignumRefcountStand(t, bin, row.name, nil)
					switch {
					case dies[row.name]:
						if code == 0 {
							t.Fatalf("with %s cut, row %q survived; the cut branch is not what keeps it green\nstdout:\n%s\nstderr:\n%s",
								control.cuts, row.name, stdout, stderr)
						}
						if strings.Contains(stdout, "bignum-refcount: failures=") {
							t.Fatalf("with %s cut, row %q printed a census after it should have died\nstdout:\n%s",
								control.cuts, row.name, stdout)
						}
					case reds[row.name]:
						if code == 0 {
							t.Fatalf("with %s cut, row %q still passed; the row is not measuring that branch\nstdout:\n%s\nstderr:\n%s",
								control.cuts, row.name, stdout, stderr)
						}
						if !strings.Contains(stdout, "FAIL ") {
							t.Fatalf("with %s cut, row %q failed without naming an assertion\nstdout:\n%s\nstderr:\n%s",
								control.cuts, row.name, stdout, stderr)
						}
					default:
						// The control's blast radius: a row outside the cut
						// must be untouched, or the #ifdef is removing more
						// than it claims.
						assertBignumRefcountRowSurvives(t, row, stdout, stderr, code)
					}
				})
			}
		})
	}
}
