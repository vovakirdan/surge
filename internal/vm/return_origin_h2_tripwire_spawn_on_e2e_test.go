package vm_test

import (
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-365 tripwire, runtime rows for the `spawn on` runner (N-TASK-27S). The leaking programs are refused by the
// task check on either backend. Sound placed runners execute through the shared
// coroutine machinery on LLVM; VM retains its explicit transport diagnostics.

const spawnOnF8bBodyMadeChannelSource = `async fn run() -> int {
    let ft: far Task<int> = spawn on shard(1:ShardId) {
        let ch: far Channel<int64> = channel_on::<int64>(shard(0:ShardId), 2:uint);
        let won = select {
            ch.send(1:int64) => 1;
        };
        ret won;
    };
    return compare ft.await() {
        Success(v) => v;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnF8cLocalSelectSource = `async fn run() -> int {
    let ft: far Task<int> = spawn on shard(1:ShardId) {
        let ch = Channel::<int64>.new(2:uint);
        let won = select {
            ch.send(1:int64) => 1;
        };
        ret won;
    };
    return compare ft.await() {
        Success(v) => v;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnHangSonAwaitSource = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on shard(1:ShardId) {
        ret compare leak().await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
        Success(n) => n;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnLeakSonAwaitSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        ret compare leak().await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
        Success(n) => n;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnLeakSonRetSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

async fn run() -> int {
    let ft: far Task<Task<int>> = spawn on distributed {
        ret leak();
    };
    return compare ft.await() {
        Success(t) => compare t.await() { Success(n) => n + 40; Cancelled() => 100; };
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnLeakSonSpawnSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        let t = spawn leak();
        ret compare t.await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
        Success(n) => n;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnTwinSonAwaitSource = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        ret compare leak().await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
        Success(n) => n;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnTwinSonSpawnSource = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        let t = spawn leak();
        ret compare t.await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
        Success(n) => n;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const (
	spawnOnF8bBodyMadeChannelSourceDigest = "0887212f997daea398f5eebc567531f46aaeebe4409bc797cfcdcf1127f22511"
	spawnOnF8cLocalSelectSourceDigest     = "6dc3faf2c84183cb886a111c6af0f00bc154c572d8f4113dc12edebe925c8ac5"
	spawnOnHangSonAwaitSourceDigest       = "21377ab5c6446ee05aabeef8ff17830ed2fc35e4047bf0db8e8aabd20e547510"
	spawnOnLeakSonAwaitSourceDigest       = "4eab26811973c93b1276eea7b3559026ff367c7bbe37c7c2d95b0f01fca976e1"
	spawnOnLeakSonRetSourceDigest         = "bf0d226e9b3f4529c30ce8ca12fcd0ca2a5e8628cd8f9aef7d7b658c181484bd"
	spawnOnLeakSonSpawnSourceDigest       = "e20a45fe60918c2a8cee385c95f5396795124f74e1995d7b1494458d88321bf3"
	spawnOnTwinSonAwaitSourceDigest       = "081382c31d91f133613792ecee43e1a49d3edd5980f33117d40e3ccd52ba6ba1"
	spawnOnTwinSonSpawnSourceDigest       = "391d58f0a0de7c6b85dfa91ab1e5d1ce700d02ad30538196eb2a628e6d48b345"
)

func TestH2TripwireTaskCheckRefusesSpawnOnLeakedRuns(t *testing.T) {
	// son_ret hands the leaked task out as the far result, a task whose result is a task:
	// SEM3223 (owner ruling 2026-09-26) refuses its type and its body besides the task check.
	for _, row := range []struct {
		name, text, digest string
		beside             []string
	}{
		{"son_await", spawnOnLeakSonAwaitSource, spawnOnLeakSonAwaitSourceDigest, nil},
		{"son_spawn", spawnOnLeakSonSpawnSource, spawnOnLeakSonSpawnSourceDigest, nil},
		{"son_ret", spawnOnLeakSonRetSource, spawnOnLeakSonRetSourceDigest,
			[]string{"SEM3223@Task<Task<int>>", "SEM3223@spawn on distributed {\n        ret leak();\n    }"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.name, row.text, row.digest, "SEM3139", "t", row.beside...); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}

func TestH2TripwireSpawnOnSuspensionBackendContract(t *testing.T) {
	for _, row := range []struct {
		name, text, digest string
		exit               int
		singleShard        bool
	}{
		{"runner_await", spawnOnTwinSonAwaitSource, spawnOnTwinSonAwaitSourceDigest, 46, true},
		{"runner_spawn_then_await", spawnOnTwinSonSpawnSource, spawnOnTwinSonSpawnSourceDigest, 46, true},
		{"await_at_two_shards", spawnOnHangSonAwaitSource, spawnOnHangSonAwaitSourceDigest, 46, false},
		{"far_select", spawnOnF8bBodyMadeChannelSource, spawnOnF8bBodyMadeChannelSourceDigest, 1, false},
		{"local_select", spawnOnF8cLocalSelectSource, spawnOnF8cLocalSelectSourceDigest, 1, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			codes, _ := crossingFrameCompile(t, "spawn_on_suspend_"+row.name, row.text, row.digest)
			if testBackend(t) != backendLLVM {
				want := "FUT7015,FUT7016"
				if row.name == "far_select" {
					// This row additionally creates a remote channel and selects
					// over it, both unsupported by the VM transport capability.
					want = "FUT7015,FUT7016,FUT7018,FUT7022"
				}
				if codes != want {
					t.Fatalf("VM codes %q, want %s", codes, want)
				}
				return
			}
			if codes != "" {
				t.Fatalf("sound placed runner refused: %s", codes)
			}
			skipTimeoutTests(t)
			binary := buildLLVMProgramFromSource(t, row.text)
			shards := []string{"2", "8"}
			if row.singleShard {
				shards = append(shards, "1")
			}
			for _, count := range shards {
				env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", count)
				_, result := runBinaryWithTimeout(t, binary, env, 30*time.Second)
				if result.exitCode != row.exit || result.stdout != "" || result.stderr != "" {
					t.Fatalf("shards=%s exit=%d want=%d stdout=%q stderr=%q", count, result.exitCode, row.exit, result.stdout, result.stderr)
				}
			}
			env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2")
			stdout, stderr, code := runBinaryUnderValgrind(t, binary, env, 120*time.Second)
			if code != row.exit || stdout != "" || hasValgrindMemcheckError(stderr) || !strings.Contains(stderr, "All heap blocks were freed") {
				t.Fatalf("placed runner lifetime: exit=%d want=%d stdout=%q\n%s", code, row.exit, stdout, stderr)
			}
		})
	}
}
