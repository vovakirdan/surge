package vm_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

type heapStatsSnapshot struct {
	allocs     uint64
	frees      uint64
	liveBlocks uint64
	liveBytes  uint64
}

func TestRuntimeV2HeapAccountingSequentialContracts(t *testing.T) {
	source := `fn print_stats(stats: HeapStats) -> nothing {
    print(stats.alloc_count to string);
    print(stats.free_count to string);
    print(stats.live_blocks to string);
    print(stats.live_bytes to string);
    return nothing;
}

@entrypoint
fn main() -> int {
    let zero: uint = 0:uint;
    let one: uint = 1:uint;
    let size4: uint = 4:uint;
    let size7: uint = 7:uint;
    let size8: uint = 8:uint;
    let size16: uint = 16:uint;
    let size32: uint = 32:uint;
    let size48: uint = 48:uint;
    let size64: uint = 64:uint;
    let size96: uint = 96:uint;
    let bad_align: uint = 24:uint;

    let seed = rt_alloc(one, one);
    rt_free(seed, one, one);

    let w1: HeapStats = rt_heap_stats();
    let w2: HeapStats = rt_heap_stats();

    let s0: HeapStats = rt_heap_stats();
    let p = rt_alloc(size32, one);
    let s1: HeapStats = rt_heap_stats();
    rt_free(p, size32, one);
    let s2: HeapStats = rt_heap_stats();

    let rp0 = rt_alloc(size8, one);
    let r1: HeapStats = rt_heap_stats();
    let rp1 = rt_realloc(rp0, size8, size16, one);
    let r2: HeapStats = rt_heap_stats();
    let rp2 = rt_realloc(rp1, size16, size4, one);
    let r3: HeapStats = rt_heap_stats();
    let nullp = rt_realloc(rp2, size4, zero, one);
    let r4: HeapStats = rt_heap_stats();
    let rp3 = rt_realloc(nullp, zero, size7, one);
    let r5: HeapStats = rt_heap_stats();
    rt_free(rp3, size7, one);
    let r6: HeapStats = rt_heap_stats();

    let a0: HeapStats = rt_heap_stats();
    let ap0 = rt_alloc(size48, size64);
    let a1: HeapStats = rt_heap_stats();
    let ap1 = rt_realloc(ap0, size48, size96, size64);
    let a2: HeapStats = rt_heap_stats();
    let ap2 = rt_realloc(ap1, size96, size32, size64);
    let a3: HeapStats = rt_heap_stats();
    rt_free(ap2, size32, size64);
    let a4: HeapStats = rt_heap_stats();

    let fp = rt_alloc(size8, one);
    let f1: HeapStats = rt_heap_stats();
    let failed = rt_realloc(fp, size8, size16, bad_align);
    let f2: HeapStats = rt_heap_stats();
    rt_free(failed, zero, one);
    rt_free(fp, size8, one);
    let f3: HeapStats = rt_heap_stats();

    print_stats(w1);
    print_stats(w2);
    print_stats(s0);
    print_stats(s1);
    print_stats(s2);
    print_stats(r1);
    print_stats(r2);
    print_stats(r3);
    print_stats(r4);
    print_stats(r5);
    print_stats(r6);
    print_stats(a0);
    print_stats(a1);
    print_stats(a2);
    print_stats(a3);
    print_stats(a4);
    print_stats(f1);
    print_stats(f2);
    print_stats(f3);
    return 0;
}
`

	stats := runRuntimeV2HeapAccountingSnapshots(t, source)
	if len(stats) != 19 {
		t.Fatalf("snapshot count: want 19, got %d (%v)", len(stats), stats)
	}
	// The counters have to be moving at all before any window below means
	// anything: every one of them is expressed as a difference from `overhead`,
	// so a report frozen at a constant would make each difference zero, and the
	// exact sizes they demand are what would catch it. Say it once here about
	// the run as a whole, where an absent mechanism cannot satisfy it.
	if stats[len(stats)-1].allocs <= stats[0].allocs {
		t.Fatalf("cumulative allocation counter never advanced: first=%+v last=%+v", stats[0], stats[len(stats)-1])
	}

	// Two snapshots with nothing between them measure what ASKING costs. The
	// question may allocate the answer, but it has to give those bytes back
	// before it answers, or every window's live figures describe the act of
	// measuring as much as the program. Both halves are checked because they
	// are separately reported numbers: live must not move, and the two
	// cumulative counters must agree with it.
	//
	// This once required the opposite. The answer was handed over as a heap box
	// the caller freed only at scope exit — after the window closed — so a
	// snapshot pair left one live block behind, and the assertion here demanded
	// that residue instead of forbidding it. Giving the composite its own
	// storage freed the box at the call, which is what took the residue to zero:
	// the allocation counts across this whole program are unchanged, and only
	// the free counts moved.
	overhead := heapStatsDelta(t, "stats overhead", stats[1], stats[0])
	if overhead.liveBlocks != 0 || overhead.liveBytes != 0 {
		t.Fatalf("taking a heap snapshot left live heap behind: %+v", overhead)
	}
	if overhead.allocs != overhead.frees {
		t.Fatalf("heap snapshot allocated and freed different numbers of blocks: %+v", overhead)
	}

	requireHeapDelta(t, "alloc", heapStatsDelta(t, "alloc", stats[3], stats[2]),
		heapDelta{allocs: overhead.allocs + 1, frees: overhead.frees, liveBlocks: overhead.liveBlocks + 1, liveBytes: overhead.liveBytes + 32})
	requireHeapDelta(t, "free counts", heapStatsDelta(t, "free counts", stats[4], stats[3]),
		heapDelta{allocs: overhead.allocs, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks - 1, liveBytes: overhead.liveBytes - 32})

	requireHeapDelta(t, "realloc grow", heapStatsDelta(t, "realloc grow", stats[6], stats[5]),
		heapDelta{allocs: overhead.allocs + 1, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks, liveBytes: overhead.liveBytes + 8})
	requireHeapDelta(t, "realloc shrink counts", heapStatsDelta(t, "realloc shrink counts", stats[7], stats[6]),
		heapDelta{allocs: overhead.allocs + 1, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks, liveBytes: overhead.liveBytes - 12})
	requireHeapDelta(t, "realloc zero-size counts", heapStatsDelta(t, "realloc zero-size counts", stats[8], stats[7]),
		heapDelta{allocs: overhead.allocs, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks - 1, liveBytes: overhead.liveBytes - 4})
	requireHeapDelta(t, "realloc null", heapStatsDelta(t, "realloc null", stats[9], stats[8]),
		heapDelta{allocs: overhead.allocs + 1, frees: overhead.frees, liveBlocks: overhead.liveBlocks + 1, liveBytes: overhead.liveBytes + 7})
	requireHeapDelta(t, "realloc final free", heapStatsDelta(t, "realloc final free", stats[10], stats[9]),
		heapDelta{allocs: overhead.allocs, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks - 1, liveBytes: overhead.liveBytes - 7})

	requireHeapDelta(t, "aligned alloc", heapStatsDelta(t, "aligned alloc", stats[12], stats[11]),
		heapDelta{allocs: overhead.allocs + 1, frees: overhead.frees, liveBlocks: overhead.liveBlocks + 1, liveBytes: overhead.liveBytes + 48})
	requireHeapDelta(t, "aligned realloc grow", heapStatsDelta(t, "aligned realloc grow", stats[13], stats[12]),
		heapDelta{allocs: overhead.allocs + 1, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks, liveBytes: overhead.liveBytes + 48})
	requireHeapDelta(t, "aligned realloc shrink", heapStatsDelta(t, "aligned realloc shrink", stats[14], stats[13]),
		heapDelta{allocs: overhead.allocs + 1, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks, liveBytes: overhead.liveBytes - 64})
	requireHeapDelta(t, "aligned final free", heapStatsDelta(t, "aligned final free", stats[15], stats[14]),
		heapDelta{allocs: overhead.allocs, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks - 1, liveBytes: overhead.liveBytes - 32})

	requireHeapDelta(t, "failed realloc unchanged", heapStatsDelta(t, "failed realloc unchanged", stats[17], stats[16]), overhead)
	requireHeapDelta(t, "failed realloc original free", heapStatsDelta(t, "failed realloc original free", stats[18], stats[17]),
		heapDelta{allocs: overhead.allocs, frees: overhead.frees + 1, liveBlocks: overhead.liveBlocks - 1, liveBytes: overhead.liveBytes - 8})
}

func TestRuntimeV2HeapAccountingConcurrentWorkersContract(t *testing.T) {
	source := `async fn allocation_worker(rounds: int, size: uint) -> int {
    let mut i: int = 0;
    while i < rounds {
        let p = rt_alloc(size, 1:uint);
        checkpoint().await();
        rt_free(p, size, 1:uint);
        checkpoint().await();
        i = i + 1;
    }
    return rounds;
}

async fn run() -> int {
    let workers: uint = rt_worker_count();
    if workers <= 1:uint {
        return 3;
    }
    let mut task_count_u: uint = workers;
    if task_count_u > 4:uint {
        task_count_u = 4:uint;
    }
    let task_count: int = task_count_u to int;
    let rounds: int = 16;
    let expected: uint = (task_count * rounds) to uint;
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(task_count_u);

    let s0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    while i < task_count {
        tasks[i] = spawn allocation_worker(rounds, 24:uint);
        i = i + 1;
    }

    let mut done: int = 0;
    for task in tasks {
        let result = task.await();
        compare result {
            Success(v) => { done = done + v; }
            Cancelled() => { return 4; }
        };
    }
    if done != task_count * rounds {
        return 5;
    }

    let s1: HeapStats = rt_heap_stats();
    let alloc_delta: uint = s1.alloc_count - s0.alloc_count;
    let free_delta: uint = s1.free_count - s0.free_count;
    if alloc_delta < expected {
        return 6;
    }
    if free_delta < expected {
        return 7;
    }
    if s1.free_count > s1.alloc_count {
        return 8;
    }
    if s1.live_blocks != s1.alloc_count - s1.free_count {
        return 9;
    }

    print("ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let result = run().await();
    return compare result {
        Success(v) => v;
        Cancelled() => 99;
    };
}
`

	runMTSource(t, source, 10*time.Second)
}

func runRuntimeV2HeapAccountingSnapshots(t *testing.T, source string) []heapStatsSnapshot {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, source)
	stdout, stderr, code := runBinary(t, outputPath)
	if code != 0 {
		t.Fatalf("run failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	fields := strings.Fields(stdout)
	if len(fields)%4 != 0 {
		t.Fatalf("heap stats output field count must be a multiple of 4, got %d:\n%s", len(fields), stdout)
	}
	stats := make([]heapStatsSnapshot, 0, len(fields)/4)
	for i := 0; i < len(fields); i += 4 {
		stats = append(stats, heapStatsSnapshot{
			allocs:     parseHeapStatField(t, fields[i]),
			frees:      parseHeapStatField(t, fields[i+1]),
			liveBlocks: parseHeapStatField(t, fields[i+2]),
			liveBytes:  parseHeapStatField(t, fields[i+3]),
		})
	}
	return stats
}

func parseHeapStatField(t *testing.T, field string) uint64 {
	t.Helper()
	value, err := strconv.ParseUint(field, 10, 64)
	if err != nil {
		t.Fatalf("parse heap stat %q: %v", field, err)
	}
	return value
}

// heapDelta is a signed difference between two snapshots. allocs and frees are
// cumulative counters and only ever grow, but liveBlocks and liveBytes move in
// both directions — a realloc shrink or free legitimately reduces them — so
// they must be signed. (They read as unsigned per-window only while every
// window is padded by incidental int/uint bignum allocations; once small ints
// became inline the shrink windows net negative, which is correct.)
type heapDelta struct {
	allocs     int64
	frees      int64
	liveBlocks int64
	liveBytes  int64
}

func heapStatsDelta(t *testing.T, label string, after, before heapStatsSnapshot) heapDelta {
	t.Helper()
	if after.allocs < before.allocs || after.frees < before.frees {
		t.Fatalf("%s cumulative counters decreased: before=%+v after=%+v", label, before, after)
	}
	return heapDelta{
		allocs:     int64(after.allocs) - int64(before.allocs),
		frees:      int64(after.frees) - int64(before.frees),
		liveBlocks: int64(after.liveBlocks) - int64(before.liveBlocks),
		liveBytes:  int64(after.liveBytes) - int64(before.liveBytes),
	}
}

func requireHeapDelta(t *testing.T, label string, got, want heapDelta) {
	t.Helper()
	if got != want {
		t.Fatalf("%s delta mismatch:\nwant=%+v\n got=%+v", label, want, got)
	}
}
