//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Freeing a task is two decisions and they are not independent.
//
// FIRST, a polled task must outlive the application of its own poll outcome.
// What keeps it addressable during its turn is its RUNNING status, not a
// reference: a release frees only a task that has completed, so nothing can
// reclaim a RUNNING one under its poller. apply_poll_outcome ENDS that
// protection in every arm -- it publishes READY, WAITING or DONE -- and then
// goes on using the pointer. The yielded arm is where it bites: it publishes
// TASK_READY and re-pushes, and ready_push_with_policy (rt_ready_queue.c)
// resolves the owner shard, BLOCKS on that shard's lock, and re-reads the
// task's status on the far side. A task that is READY and not yet enqueued can
// be woken by an awaiting poll in that window, popped by another worker, run to
// completion and freed by the last handle drop, all while this thread waits for
// the lock.
//
// SECOND, the release that frees must decide from ONE observation. "This drop
// emptied the count" and "the task has completed" used to be a fetch_sub and a
// separate load of task->status, and a poller can resurrect a task between
// them: it holds the raw pointer rather than a reference and takes one of its
// own, so a count that reached zero goes back to one, completes, and is freed
// by that reference's drop. The first thread's late status load then reads
// freed memory, sees TASK_DONE in the bytes and frees a second time -- and the
// second free reads task->id out of reused memory, which is how the task
// table's store came to panic with a heap pointer for an id.
//
// The two are one row because the first fix makes the second defect reachable:
// pinning across apply_poll_outcome adds exactly the resurrect-from-zero that
// the split decision cannot see. Landed apart, the pin makes the panic worse.
//
// The row is three arms and never a single green run. A green positive arm on
// its own would prove nothing about races that only sometimes run, so each
// negative control removes ONE half of the fix and must be caught by
// AddressSanitizer doing what that half prevents.
//
// Measured on this lane at SURGE_THREADS=4 with 24 concurrent processes
// (32 cores): trunk before either fix, 3 of 48 runs; no-pin control, 6 of 48;
// split-release control, 3 of 48; both fixes in place, 0 of 96 and 0 of 80.
const taskFreeObservationRuns = 96

// Where the freed read lands for each half. Asserting the SIGNATURE rather than
// "some sanitizer report" is deliberate: this row pins two defects, and a
// different use-after-free surfacing here should be read as its own finding
// rather than silently satisfying or failing this one. Any other report is
// logged in full.
//
// The pin's half has one frame. The release decision's half has several and is
// pinned by its FILE: the losing thread can be caught reading the freed refs
// word as it decides (task_drop_ref_owes_free), or one step later inside the
// second free itself (free_task reading task->id, which is the shape that
// reaches the task table and panics with a heap pointer for an id). Both are
// the same defect and both are in rt_task_lifetime.c; naming one function would
// make the row pass or fail on which of them the scheduler happened to reach.
var (
	pollOutcomePinSignature  = regexp.MustCompile(`#[0-9]+ 0x[0-9a-f]+ in ready_push_with_policy\b`)
	releaseDecisionSignature = regexp.MustCompile(`#[0-9]+ 0x[0-9a-f]+ in \w+ \S*rt_task_lifetime\.c:`)
)

// The same program as the timeout-cancel row: a target that awaits a long chain
// of checkpoints, a timeout that cancels it, and a second handle that awaits it
// afterwards. The checkpoint tasks are what yield, and the second handle is what
// supplies a last-reference drop concurrent with a completion.
func TestRuntimeV2LifecycleTaskFreeIsOneObservation(t *testing.T) {
	// One compile of the program, three instrumented links of the runtime it
	// was compiled against: the arms differ only in a -D, so the .sg build and
	// its out.o are shared.
	kept := keepTaskFreeObservationBuild(t)
	positive := buildTaskFreeObservationProgram(t, kept, "task_free_observation")
	noPin := buildTaskFreeObservationProgram(t, kept,
		"task_free_observation_no_pin", "-DRT_POLL_OUTCOME_PIN_NEGATIVE_CONTROL")
	splitRelease := buildTaskFreeObservationProgram(t, kept,
		"task_free_observation_split_release", "-DRT_TASK_RELEASE_SPLIT_NEGATIVE_CONTROL")

	// The negative controls run FIRST. If neither race runs on this machine the
	// row asked nothing, and saying so is more useful than a positive arm that
	// was green because nothing happened.
	noPinReports := runTaskFreeObservationStand(t, noPin)
	if len(reportsMatching(noPinReports, pollOutcomePinSignature)) == 0 {
		t.Fatalf("the control removed the poll-outcome pin and %d runs reported no "+
			"use-after-free in the re-push; the row proved nothing, because the race "+
			"it claims to pin did not run\nreports:\n%s",
			taskFreeObservationRuns, strings.Join(noPinReports, "\n---\n"))
	}

	splitReports := runTaskFreeObservationStand(t, splitRelease)
	if len(reportsMatching(splitReports, releaseDecisionSignature)) == 0 {
		t.Fatalf("the control restored the split release decision and %d runs reported "+
			"no use-after-free in the release path; the row proved nothing, because the "+
			"double free it claims to pin did not run\nreports:\n%s",
			taskFreeObservationRuns, strings.Join(splitReports, "\n---\n"))
	}

	positiveReports := runTaskFreeObservationStand(t, positive)
	for _, signature := range []*regexp.Regexp{pollOutcomePinSignature, releaseDecisionSignature} {
		matched := reportsMatching(positiveReports, signature)
		if len(matched) != 0 {
			t.Fatalf("both fixes are in place and %d of %d runs still read a freed task "+
				"(%s):\n%s",
				len(matched), taskFreeObservationRuns, signature,
				strings.Join(matched, "\n---\n"))
		}
	}
	for _, report := range positiveReports {
		t.Logf("a sanitizer report NOT pinned by this row (a separate, open defect -- "+
			"do not fold it into this one):\n%s", report)
	}
}

// The sources and the program object a --keep-tmp build left behind.
type taskFreeObservationBuild struct {
	sources    []string
	programObj string
}

// keepTaskFreeObservationBuild builds the program and collects what the
// instrumented links need. The driver has no hook for extra runtime cflags, and
// inventing one would put a sanitizer switch on the production build path;
// --keep-tmp already leaves the exact sources and the program's own object
// behind, so the instrumented builds are assembled from those instead.
func keepTaskFreeObservationBuild(t *testing.T) taskFreeObservationBuild {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping the task-free observation proof")
	}
	root := repoRoot(t)
	outputPath := buildLLVMProgramFromSource(t, runtimeV2TimeoutCancelSource)
	tmpDir := llvmTmpDir(root, outputPath)
	runtimeDir := filepath.Join(tmpDir, "native_runtime")

	sources, err := filepath.Glob(filepath.Join(runtimeDir, "*.c"))
	if err != nil {
		t.Fatalf("glob the kept runtime sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("no runtime sources under %s; --keep-tmp stopped keeping them", runtimeDir)
	}
	sort.Strings(sources)
	return taskFreeObservationBuild{sources: sources, programObj: filepath.Join(tmpDir, "out.o")}
}

// buildTaskFreeObservationProgram compiles the kept runtime sources with
// AddressSanitizer under the given -D flags and links them against the
// program's own object.
func buildTaskFreeObservationProgram(
	t *testing.T, kept taskFreeObservationBuild, name string, extraFlags ...string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the task-free observation proof")
	}
	root := repoRoot(t)
	sources := kept.sources

	objDir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(objDir, 0o700); err != nil {
		t.Fatalf("create object dir: %v", err)
	}

	objects := make([]string, 0, len(sources))
	var mu sync.Mutex
	var group sync.WaitGroup
	var failures []string
	gate := make(chan struct{}, runtime.NumCPU())
	for _, source := range sources {
		object := filepath.Join(objDir, filepath.Base(source)+".o")
		objects = append(objects, object)
		group.Add(1)
		go func(source, object string) {
			defer group.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			args := []string{
				"-c", "-std=c11", "-g", "-O1", "-fno-omit-frame-pointer",
				"-fsanitize=address", "-pthread",
			}
			args = append(args, extraFlags...)
			args = append(args, source, "-o", object)
			cmd := exec.Command(clang, args...)
			cmd.Dir = root
			// Not runCommand: it reports a spawn failure with t.Fatalf, which
			// may not be called from a goroutine.
			out, err := cmd.CombinedOutput()
			if err != nil {
				mu.Lock()
				failures = append(failures,
					fmt.Sprintf("%s: %v\noutput:\n%s", filepath.Base(source), err, out))
				mu.Unlock()
			}
		}(source, object)
	}
	group.Wait()
	if len(failures) != 0 {
		if strings.Contains(strings.Join(failures, "\n"), "unsupported option") {
			t.Skip("clang here cannot build with -fsanitize=address; skipping")
		}
		t.Fatalf("instrumented runtime build failed:\n%s", strings.Join(failures, "\n---\n"))
	}

	bin := filepath.Join(objDir, name)
	linkArgs := append([]string{"-fsanitize=address", "-g", kept.programObj}, objects...)
	linkArgs = append(linkArgs, "-o", bin, "-pthread")
	cmd := exec.Command(clang, linkArgs...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("link the instrumented program failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

// runTaskFreeObservationStand runs the binary taskFreeObservationRuns times and
// returns every sanitizer report it produced.
//
// The processes OVERSUBSCRIBE the machine on purpose. Both races are lost-lock
// and lost-window races, and at one process per core neither was seen in 40
// runs; at three worker threads per core trunk reported in 3 of 48.
func runTaskFreeObservationStand(t *testing.T, bin string) []string {
	t.Helper()
	root := repoRoot(t)
	env := overrideEnv(envWithStdlib(root), "4")
	env = append(env, "ASAN_OPTIONS=detect_leaks=0:halt_on_error=1:abort_on_error=0")

	concurrency := runtime.NumCPU() * 3 / 4
	if concurrency < 8 {
		concurrency = 8
	}
	if concurrency > 32 {
		concurrency = 32
	}
	jobs := make(chan int)
	var mu sync.Mutex
	var reports []string
	var group sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for range jobs {
				cmd := exec.Command(bin)
				cmd.Dir = root
				cmd.Env = env
				out, err := cmd.CombinedOutput()
				if err == nil {
					continue
				}
				text := string(out)
				if !strings.Contains(text, "AddressSanitizer") {
					continue
				}
				mu.Lock()
				reports = append(reports, text)
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < taskFreeObservationRuns; i++ {
		jobs <- i
	}
	close(jobs)
	group.Wait()
	return reports
}

var asanRegionLine = regexp.MustCompile(`(?m)^0x[0-9a-f]+ is located `)

// faultingStackOf keeps the part of a sanitizer report that names where the bad
// access happened, dropping the allocation and free stacks that follow it.
func faultingStackOf(report string) string {
	if loc := asanRegionLine.FindStringIndex(report); loc != nil {
		return report[:loc[0]]
	}
	return report
}

func reportsMatching(reports []string, signature *regexp.Regexp) []string {
	var out []string
	for _, report := range reports {
		// Only the FAULTING stack, which is everything before the "is located
		// ... freed by / previously allocated by" stacks. The free path appears
		// in the freeing stack of every report about a task, so matching the
		// whole text would let any of them satisfy this signature.
		if !signature.MatchString(faultingStackOf(report)) {
			continue
		}
		// Both shapes of the same defect: the second free reads the freed task
		// on its way in, and if the allocator got there first it is reported as
		// the double free itself.
		if strings.Contains(report, "heap-use-after-free") ||
			strings.Contains(report, "attempting double-free") {
			out = append(out, report)
		}
	}
	return out
}
