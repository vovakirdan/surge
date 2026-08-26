package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A dropped map reclaims its entry storage and every key and value still in it.
//
// It reclaimed NOTHING before this row: `rt.h` exposed no `rt_map_free` at all,
// the backend's structural walk answered "owns no heap" for a `Map<K, V>`, so
// no drop glue was emitted and a map leaked unconditionally (RV2-DEBT-156). The
// `map-teardown-*` benchmark rows passed at zero structural allocations for the
// same reason: nothing happened at teardown, and a gate that does not run reads
// exactly like a gate that passes.
//
// The five configurations are the ones RV2-DEBT-156's close condition names,
// and each isolates a different thing the teardown owes:
//
//	empty          -- the header, and nothing else: storage was never allocated.
//	heap keys      -- the keys, destroyed through the key descriptor.
//	heap values    -- the values, destroyed through the value descriptor.
//	after removals -- exactly the LIVE entries: a removed value was destroyed by
//	                  the removal, and destroying it again at teardown would be
//	                  a double free rather than a leak.
//	after growth   -- the entries that were MOVED into a second block, plus the
//	                  first block, which growth gave back.
//
// The sixth row is the one that makes keys() an owner: an independent snapshot
// of the keys, walked while the map is emptied under it. That is the shape
// stdlib/json/stringify.sg takes, and with keys copied as BYTES it is where the
// teardown fix detonates -- the array and the map would hold one string each
// and free it twice.
const runtimeV2MapOwnedEntriesSource = `
type Owned = { label: string, extra: string };

fn owned(label: string) -> Owned {
    return Owned { label = label, extra = "payload" };
}

fn empty_map() -> int {
    let m: Map<string, int> = Map::<string, int>.new();
    return m.length() to int;
}

fn heap_keys() -> int {
    let mut m: Map<string, int> = Map::<string, int>.new();
    let _ = m.insert("alpha", 1);
    let _ = m.insert("beta", 2);
    let _ = m.insert("gamma", 3);
    return m.length() to int;
}

fn heap_values() -> int {
    let mut m: Map<int, Owned> = Map::<int, Owned>.new();
    let _ = m.insert(1, owned("one"));
    let _ = m.insert(2, owned("two"));
    return m.length() to int;
}

fn after_removals() -> int {
    let mut m: Map<string, Owned> = Map::<string, Owned>.new();
    let _ = m.insert("a", owned("a"));
    let _ = m.insert("b", owned("b"));
    let _ = m.insert("c", owned("c"));
    let gone = "b";
    let _ = m.remove(&gone);
    // A replacement displaces a value the caller never asks for, so the map
    // destroys it rather than abandoning it.
    let _ = m.insert("a", owned("a2"));
    return m.length() to int;
}

fn after_growth() -> int {
    let mut m: Map<string, Owned> = Map::<string, Owned>.new();
    let _ = m.insert("g0", owned("v0"));
    let _ = m.insert("g1", owned("v1"));
    let _ = m.insert("g2", owned("v2"));
    let _ = m.insert("g3", owned("v3"));
    let _ = m.insert("g4", owned("v4"));
    let _ = m.insert("g5", owned("v5"));
    let _ = m.insert("g6", owned("v6"));
    let _ = m.insert("g7", owned("v7"));
    let _ = m.insert("g8", owned("v8"));
    let _ = m.insert("g9", owned("v9"));
    return m.length() to int;
}

fn keys_then_remove() -> int {
    let mut m: Map<string, Owned> = Map::<string, Owned>.new();
    let _ = m.insert("k0", owned("w0"));
    let _ = m.insert("k1", owned("w1"));
    let _ = m.insert("k2", owned("w2"));
    let _ = m.insert("k3", owned("w3"));
    let ks: string[] = m.keys();
    let total: int = ks.__len() to int;
    let mut i: int = 0;
    let mut taken: int = 0;
    while i < total {
        let key = ks[i];
        let removed = m.remove(key);
        taken = taken + compare removed { Some(_) => 1; nothing => 0; };
        i = i + 1;
    }
    if m.length() to int != 0 { return 0 - 1; }
    return taken;
}

// The same ownership, reached from more than one worker at once: three tasks
// each build, mutate and destroy a map of their own. Under more than one shard
// they run on different threads, so the descriptor operations a map dispatches
// -- move, drop -- are exercised concurrently rather than only in sequence.
async fn map_worker(base: int) -> int {
    let mut m: Map<string, Owned> = Map::<string, Owned>.new();
    let _ = m.insert("w0", owned("a"));
    let _ = m.insert("w1", owned("b"));
    let _ = m.insert("w2", owned("c"));
    let gone = "w1";
    let _ = m.remove(&gone);
    return (m.length() to int) + base;
}

async fn concurrent_maps() -> int {
    let t0 = spawn map_worker(0);
    let t1 = spawn map_worker(10);
    let t2 = spawn map_worker(20);
    let a: int = compare t0.await() { Success(x) => x; Cancelled() => 0 - 1; };
    let b: int = compare t1.await() { Success(x) => x; Cancelled() => 0 - 1; };
    let c: int = compare t2.await() { Success(x) => x; Cancelled() => 0 - 1; };
    return a + b + c;
}

@entrypoint
fn main() -> int {
    if empty_map() != 0 { return 1; }
    if heap_keys() != 3 { return 2; }
    if heap_values() != 2 { return 3; }
    if after_removals() != 2 { return 4; }
    if after_growth() != 10 { return 5; }
    if keys_then_remove() != 4 { return 6; }
    let concurrent = spawn concurrent_maps();
    let total: int = compare concurrent.await() { Success(x) => x; Cancelled() => 0 - 1; };
    if total != 36 { return 7; }
    print("map-owned-entries-ok");
    return 0;
}
`

// Expected on the tree without this commit, measured on the same program by the
// lead: `rt_map_free` does not exist, so every one of the six maps leaks its
// header, its entry storage and every heap key and value still in it, and the
// leak summary is far from zero on both counts. The single-entry
// `Map<string, int>` of testdata/golden/vm_maps/map_get_mut.sg is the figure
// RV2-DEBT-156 recorded for the same absence: 32 bytes definitely lost in 1
// block plus 82 indirectly lost in 2 blocks.
//
// The keys() row is the one that fails the OTHER way once teardown works: with
// keys copied as bytes, the array and the map hold the same string, and the
// second free is an invalid free rather than a leak -- which is what
// hasValgrindMemcheckError reports here.
//
// NEGATIVE CONTROLS, to reproduce deliberately:
//   - delete `rt_map_free` from rt_map.c (or the map arm of `typeOwnsHeap` in
//     internal/backend/llvm/emit_drop_glue.go): every row leaks again;
//   - make `mapKeysDuplication` answer "null" unconditionally: the
//     keys_then_remove row double-frees each key.
func TestRuntimeV2MapOwnedEntriesValgrindZero(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2MapOwnedEntriesSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	// One shard proves the accounting; four put the concurrent phase's maps on
	// different worker threads, so a teardown that is only correct when nothing
	// else is running does not pass here.
	for _, shardCount := range []string{"1", "4"} {
		t.Run("shards_"+shardCount, func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", shardCount)
			env = overrideEnvVar(env, "SURGE_THREADS", shardCount)
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
			if hasValgrindMemcheckError(stderr) {
				t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			if exitCode != 0 {
				t.Fatalf("map owned-entries e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
			}
			if !strings.Contains(stdout, "map-owned-entries-ok") {
				t.Fatalf("map owned-entries e2e missing completion marker; stdout=%q", stdout)
			}
			definiteBytes, definiteBlocks := parseValgrindLeakMatch(valgrindDefiniteLeakRE, stderr)
			indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
			if definiteBytes != 0 || definiteBlocks != 0 || indirectBytes != 0 || indirectBlocks != 0 {
				t.Fatalf(
					"map teardown leaks at shards=%s: definitely_lost=%dB/%dblk indirectly_lost=%dB/%dblk, want strict zero on both\nstderr:\n%s",
					shardCount, definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, stderr,
				)
			}
		})
	}
}
