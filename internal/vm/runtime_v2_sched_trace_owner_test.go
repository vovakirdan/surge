//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Runtime V2 Section 1, "Traces and counters": every cell names its owner -- a
// carrier, a lane, or the runtime -- and a cell that names none is refused. A
// reported counter is admissible evidence only when its owner is named, and a
// row may not publish a number whose writers share neither a lock nor an owner.
//
// The scheduler's pop record is carrier-owned: which queue a carrier took its
// own next task from is a record of what THAT carrier did. Several carriers
// once folded it into one process-wide word each under its own shard's lock,
// which is a data race; but the fix that only removes the race -- making the
// word atomic -- leaves the rule broken, because one word shared by every
// carrier still reports a number belonging to nobody. So these tests are built
// to fail on BOTH: they demand that the number a carrier produced be reported
// as that carrier's, which a single word cannot do however it is synchronized.
//
// TestRuntimeV2SchedTraceReportsAnOwnerPerCell is the behavioural half: it runs
// a real multi-carrier workload and requires the pops to come back attributed,
// split across owners, adding up per owner, over an owner set the dump names.
// TestRuntimeV2SchedTraceCellsAreOwnedAndPaddedApart is the structural half: it
// pins the cell's publication order and the compile-time padding assert that
// keeps two owners off one cache line, neither of which the output can show.

const schedTraceOwnerCarriers = 4

// A workload every carrier participates in: enough short tasks, spawned from
// one place and awaited from another, that no single carrier can drain them
// alone. Faithful to TestMTWorkStealing, which is the shape the scheduler's
// steal path is already exercised by.
const schedTraceOwnerSource = `async fn worker(steps: int) -> int {
    let mut i: int = 0;
    while i < steps {
        checkpoint().await();
        i = i + 1;
    }
    return 0;
}

async fn spawn_many(count: int, steps: int) -> int {
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(count to uint);
    let mut i: int = 0;
    while i < count {
        tasks[i] = spawn worker(steps);
        i = i + 1;
    }
    let mut failed: bool = false;
    while tasks.__len() > 0:uint {
        let r = tasks.pop().safe().await();
        let ok = compare r {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ok {
            failed = true;
        }
    }
    if failed {
        return 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    if rt_worker_count() <= 1:uint {
        return 2;
    }
    let workers: int = rt_worker_count() to int;
    let count: int = workers * 24;
    let steps: int = 80;
    let res = spawn_many(count, steps).await();
    let ok = compare res {
        Success(v) => v == 0;
        Cancelled() => false;
    };
    if !ok {
        return 1;
    }
    print("ok");
    return 0;
}
`

func TestRuntimeV2SchedTraceReportsAnOwnerPerCell(t *testing.T) {
	requireLLVMBackend(t)
	ensureLLVMToolchain(t)

	outputPath := buildLLVMProgramFromSource(t, schedTraceOwnerSource)
	env := overrideEnv(envWithStdlib(repoRoot(t)), strconv.Itoa(schedTraceOwnerCarriers))
	env = overrideEnvVar(env, "SURGE_SCHED_TRACE", "1")
	dur, res := runBinaryWithTimeout(t, outputPath, env, mtScaledTimeout(t, 30*time.Second))
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	trace := parseSchedTrace(t, res.stderr)
	records := schedTraceRecords(res.stderr)
	t.Logf("owner records:\n%s", strings.Join(records, "\n"))

	// (1) No record may publish a pop count without saying whose it is. This is
	// the rule itself, and it is what fails the moment the five words go back to
	// being one shared cell -- atomic or not -- because the single row that cell
	// can produce carries `local=` under no owner at all.
	for _, line := range records {
		fields := schedTraceFields(line)
		if _, ok := fields["owner"]; !ok {
			t.Fatalf("SCHED_TRACE record names no owner:\n%s", line)
		}
		if _, ok := fields["local"]; !ok {
			continue
		}
		if fields["owner"] != "carrier" && fields["owner"] != "control" {
			t.Fatalf("pop counts are reported under %q, which owns no cell:\n%s",
				fields["owner"], line)
		}
		if fields["id"] == "" {
			t.Fatalf("pop counts are reported without identifying the owner:\n%s", line)
		}
	}

	// (2) The reader must be able to learn the owner set rather than guess it:
	// the runtime record declares how many owners the records above cover, and
	// exactly that many arrived. A dropped record would make every sum below
	// short, so it is a failure, not a footnote.
	if trace.declaredCarriers != schedTraceOwnerCarriers {
		t.Fatalf("runtime record declares carriers=%d, want %d:\n%s",
			trace.declaredCarriers, schedTraceOwnerCarriers, strings.Join(records, "\n"))
	}
	if got := uint64(len(trace.owners)); got != trace.declaredOwners {
		t.Fatalf("dump carries %d owner records but declares owners=%d:\n%s",
			got, trace.declaredOwners, strings.Join(records, "\n"))
	}
	if trace.droppedRecords != 0 || trace.unownedPops != 0 {
		t.Fatalf("dropped_records=%d unowned_pops=%d: the reported owner set is incomplete:\n%s",
			trace.droppedRecords, trace.unownedPops, strings.Join(records, "\n"))
	}

	// (3) Every carrier index appears exactly once, and the control lane once.
	// The owner set is the whole set, with nobody counted twice.
	seen := map[string]bool{}
	for _, owner := range trace.owners {
		key := fmt.Sprintf("%s/%d", owner.owner, owner.id)
		if seen[key] {
			t.Fatalf("owner %s reported twice:\n%s", key, strings.Join(records, "\n"))
		}
		seen[key] = true
		if owner.events != owner.local+owner.inject+owner.steal {
			t.Fatalf("owner %s: events=%d but local+inject+steal=%d",
				key, owner.events, owner.local+owner.inject+owner.steal)
		}
	}
	for i := 0; i < schedTraceOwnerCarriers; i++ {
		if !seen[fmt.Sprintf("carrier/%d", i)] {
			t.Fatalf("carrier %d published no cell:\n%s", i, strings.Join(records, "\n"))
		}
	}
	if !seen["control/0"] {
		t.Fatalf("the control lane published no cell:\n%s", strings.Join(records, "\n"))
	}

	// (4) The measured half, and the one an atomic-per-word fix cannot reach at
	// all: the pops this run actually made were split across owners, and each
	// owner that recorded any names the shard it was serving. One shared word
	// yields one number and no shard, so it can never satisfy this even when it
	// is perfectly synchronized.
	active := 0
	for _, owner := range trace.owners {
		if owner.events == 0 {
			continue
		}
		active++
		if owner.shard == "none" || owner.shard == "" {
			t.Fatalf("owner %s/%d recorded %d pops and names no shard:\n%s",
				owner.owner, owner.id, owner.events, strings.Join(records, "\n"))
		}
	}
	if active < 2 {
		t.Fatalf("only %d owner(s) recorded a pop over %d events across %d carriers; "+
			"the pop record is not attributed to whoever made it:\n%s",
			active, trace.events, schedTraceOwnerCarriers, strings.Join(records, "\n"))
	}
	if trace.events == 0 {
		t.Fatalf("no pops recorded at all:\n%s", strings.Join(records, "\n"))
	}
	t.Logf("owners=%d active=%d events=%d (local=%d inject=%d steal=%d)",
		len(trace.owners), active, trace.events, trace.local, trace.inject, trace.steal)
}

// TestRuntimeV2SchedTraceCellsAreOwnedAndPaddedApart pins what the dump cannot
// show. Section 1 requires cells of different owners to be padded apart and the
// separation asserted at compile time, and requires a carrier's cell to be
// published release and read acquire, since a carrier holds no lock and this
// section creates none.
func TestRuntimeV2SchedTraceCellsAreOwnedAndPaddedApart(t *testing.T) {
	// First, and deliberately: the five words must be GONE, not merely made
	// atomic. A file-scope pop counter is one cell every carrier writes, and
	// that is the violation whether or not the writes tear -- so this is the
	// check that has to name the reason when somebody "fixes" the race by
	// putting _Atomic in front of the same five words.
	legacy := readSchedTraceFile(t, "runtime/native/rt_async_trace.c")
	for _, name := range []string{
		"trace_sched_hash",
		"trace_sched_events",
		"trace_sched_local_pops",
		"trace_sched_inject_pops",
		"trace_sched_steal_pops",
	} {
		if strings.Contains(legacy, name) {
			t.Fatalf("rt_async_trace.c still holds %q: a scheduler pop counter at file "+
				"scope is written by every carrier and owned by none, atomic or not", name)
		}
	}

	header := readSchedTraceFile(t, "runtime/native/rt_sched_trace.h")
	body := readSchedTraceFile(t, "runtime/native/rt_sched_trace.c")
	for _, want := range []string{
		"_Static_assert(sizeof(rt_sched_trace_cell) == RT_SCHED_TRACE_CELL_BYTES",
		"_Static_assert(_Alignof(rt_sched_trace_cell) >= RT_SCHED_TRACE_CELL_BYTES",
		"_Alignas(RT_SCHED_TRACE_CELL_BYTES)",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("rt_sched_trace.h must keep the compile-time padding assert %q", want)
		}
	}
	if !strings.Contains(body, "memory_order_release") {
		t.Fatal("a carrier cell must be published release: the carrier holds no lock")
	}
	if !strings.Contains(body, "memory_order_acquire") {
		t.Fatal("a carrier cell must be read acquire: the reader holds no lock either")
	}
	for _, name := range []string{"local_pops", "inject_pops", "steal_pops", "pop_mix"} {
		decl := "static _Atomic uint64_t sched_trace_" + name
		if strings.Contains(body, decl) {
			t.Fatalf("rt_sched_trace.c declares %q at file scope: a pop counter every "+
				"owner writes names no owner, atomic or not", decl)
		}
	}
}

func readSchedTraceFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func schedTraceRecords(stderr string) []string {
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "SCHED_TRACE") {
			out = append(out, line)
		}
	}
	return out
}

// The stand's own constants, which the caller has to know in advance for the
// counts below to be a check rather than a transcript.
const (
	schedTraceStandCarriers  = 4
	schedTraceStandPops      = 30000
	schedTraceStandPerSource = schedTraceStandPops / 3
)

func buildSchedTraceStand(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the scheduler trace ownership stand")
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
		filepath.Join(root, "internal", "vm", "testdata", "sched_trace_owner.c"))
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build scheduler trace stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

// runSchedTraceStand drives the stand and returns its final dump: four carriers
// each recording an exact, known number of pops while a reader reads every cell
// through the public dump.
func runSchedTraceStand(t *testing.T, bin string, env []string) schedTrace {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), env...)
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("scheduler trace stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	final := stderr
	if idx := strings.LastIndex(stderr, "STAND_FINAL"); idx >= 0 {
		final = stderr[idx:]
	}
	if strings.Contains(stderr, "WARNING: ThreadSanitizer") {
		t.Fatalf("ThreadSanitizer reported a race over the scheduler trace cells:\n%s", stderr)
	}
	if strings.Contains(stderr, "STAND_FAIL") {
		t.Fatalf("stand refused to run:\n%s", stderr)
	}
	return parseSchedTrace(t, final)
}

// The counts are exact by construction, so this fails on an increment that gets
// lost under contention AND on a single shared counter that cannot report four
// separate exact numbers at all.
func assertSchedTraceStandCounts(t *testing.T, trace schedTrace) {
	t.Helper()
	if trace.declaredCarriers != schedTraceStandCarriers {
		t.Fatalf("stand declares carriers=%d, want %d", trace.declaredCarriers, schedTraceStandCarriers)
	}
	carriers := 0
	for _, owner := range trace.owners {
		if owner.owner != "carrier" {
			continue
		}
		carriers++
		if owner.events != schedTraceStandPops {
			t.Fatalf("carrier %d recorded %d pops, want exactly %d (a lost increment, "+
				"or somebody else's count): %+v", owner.id, owner.events, schedTraceStandPops, owner)
		}
		if owner.local != schedTraceStandPerSource ||
			owner.inject != schedTraceStandPerSource ||
			owner.steal != schedTraceStandPerSource {
			t.Fatalf("carrier %d source split is %d/%d/%d, want %d each: %+v",
				owner.id, owner.local, owner.inject, owner.steal, schedTraceStandPerSource, owner)
		}
	}
	if carriers != schedTraceStandCarriers {
		t.Fatalf("stand reported %d carrier records, want %d", carriers, schedTraceStandCarriers)
	}
	if trace.unownedPops != 0 || trace.droppedRecords != 0 {
		t.Fatalf("unowned_pops=%d dropped_records=%d", trace.unownedPops, trace.droppedRecords)
	}
}

func TestRuntimeV2SchedTraceStandCountsExactlyPerOwner(t *testing.T) {
	bin := buildSchedTraceStand(t, "sched_trace_owner", []string{"-O2"})
	assertSchedTraceStandCounts(t, runSchedTraceStand(t, bin, nil))
}

// The other half of the same question. The counts above say the numbers are
// attributed and whole; this says the writes and the reads that produced them
// are ordered by something, which is what the five plain words never were.
func TestRuntimeV2SchedTraceStandUnderThreadSanitizer(t *testing.T) {
	bin := buildSchedTraceStand(t, "sched_trace_owner_tsan", []string{
		"-fsanitize=thread",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	assertSchedTraceStandCounts(t, runSchedTraceStand(t, bin, []string{"TSAN_OPTIONS=halt_on_error=1"}))
}
