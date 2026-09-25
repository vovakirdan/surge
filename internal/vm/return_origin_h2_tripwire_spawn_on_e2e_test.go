package vm_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"surge/internal/buildpipeline"
)

// RV2-DEBT-365 tripwire, runtime rows for the `spawn on` runner (N-TASK-27S). The leaking programs are refused by the
// task check on either backend. Their twins borrow nothing: natively, at one shard (`distributed` resolves to the only
// shard), the far body joins the task and the program exits 46; the VM has no cross-shard transport and refuses the
// build (FUT7015 at the `spawn on`, FUT7016 at the far await). A body that suspends hangs when it lands on another
// shard (RV2-DEBT-344 (a)), and a select in a body fails in the lowering (344 (b)): TestH2TripwireSpawnOnBodySuspension-
// Hangs pins both as incidental barriers, so a fix turns a leaf red and must re-run the runner matrix at two shards.

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
	for _, row := range []struct{ name, text, digest string }{
		{"son_await", spawnOnLeakSonAwaitSource, spawnOnLeakSonAwaitSourceDigest},
		{"son_spawn", spawnOnLeakSonSpawnSource, spawnOnLeakSonSpawnSourceDigest},
		{"son_ret", spawnOnLeakSonRetSource, spawnOnLeakSonRetSourceDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.name, row.text, row.digest, "SEM3139", "t"); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}

func TestH2TripwireSpawnOnRunnerRuns(t *testing.T) {
	for _, row := range []struct{ name, text, digest string }{
		{"son_await", spawnOnTwinSonAwaitSource, spawnOnTwinSonAwaitSourceDigest},
		{"son_spawn", spawnOnTwinSonSpawnSource, spawnOnTwinSonSpawnSourceDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			codes, _ := crossingFrameCompile(t, "spawn_on_runner_"+row.name, row.text, row.digest)
			if testBackend(t) != backendLLVM {
				if codes != "FUT7015,FUT7016" {
					t.Fatalf("codes %q, want exactly FUT7015,FUT7016: the VM has no cross-shard transport", codes)
				}
				return
			}
			if codes != "" {
				t.Fatalf("codes %q, want none: the runner twin is a sound program", codes)
			}
			skipTimeoutTests(t)
			outputPath := buildRuntimeV2CrossingSource(t, row.text, nil)
			env := overrideEnvVar(overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "1"), "SURGE_THREADS", "1")
			_, res := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
			if res.exitCode != 46 {
				t.Fatalf("exit %d stdout %q stderr %q, want 46: the far body did not run the task it joined", res.exitCode, res.stdout, res.stderr)
			}
		})
	}
}

// spawnOnBuild builds text natively and answers the executable, or the build's error.
func spawnOnBuild(t *testing.T, name, text, digest string) (string, error) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen probe changed: %s", got)
	}
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	dir := t.TempDir()
	path := filepath.Join(dir, name+".sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := buildpipeline.Build(t.Context(), &buildpipeline.BuildRequest{
		CompileRequest: buildpipeline.CompileRequest{TargetPath: path, BaseDir: dir, MaxDiagnostics: 64},
		OutputName:     name,
		OutputRoot:     dir,
		Profile:        "debug",
		Backend:        buildpipeline.BackendLLVM,
	})
	if err != nil {
		return "", err
	}
	return res.OutputPath, nil
}

// spawnOnGroupSurvivors answers the pids still in process group pgid, read from /proc (field 5 of stat is the pgrp).
func spawnOnGroupSurvivors(pgid int) []string {
	var out []string
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		s := string(raw)
		if i := strings.LastIndexByte(s, ')'); i >= 0 {
			if f := strings.Fields(s[i+1:]); len(f) > 3 && f[2] == strconv.Itoa(pgid) {
				out = append(out, e.Name())
			}
		}
	}
	return out
}

// Every leaf logs `SPAWN_ON_344 <leaf> verdict=<hang|completed|build-failed|built|vm-refused>` before it asserts, so the
// measurement tells a hang from a build failure by the row's own words.
func TestH2TripwireSpawnOnBodySuspensionHangs(t *testing.T) {
	for _, row := range []struct{ name, text, digest, want string }{
		{"await_in_body_at_two_shards", spawnOnHangSonAwaitSource, spawnOnHangSonAwaitSourceDigest, "hang"},
		{"far_select_in_body_fails_emission", spawnOnF8bBodyMadeChannelSource, spawnOnF8bBodyMadeChannelSourceDigest, "build:must be lowered inside an async suspend context"},
		{"local_select_in_body_fails_validation", spawnOnF8cLocalSelectSource, spawnOnF8cLocalSelectSourceDigest, "build:select ready target"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if testBackend(t) != backendLLVM {
				codes, _ := crossingFrameCompile(t, "spawn_on_344_"+row.name, row.text, row.digest)
				t.Logf("SPAWN_ON_344 %s verdict=vm-refused codes=%s", row.name, codes)
				if !strings.HasPrefix(codes, "FUT7015,") {
					t.Fatalf("codes %q, want FUT7015 first: the VM has no cross-shard transport", codes)
				}
				return
			}
			skipTimeoutTests(t)
			exe, err := spawnOnBuild(t, "spawn_on_344_"+row.name, row.text, row.digest)
			if msg, ok := strings.CutPrefix(row.want, "build:"); ok {
				if err == nil {
					t.Logf("SPAWN_ON_344 %s verdict=built", row.name)
					t.Fatalf("the program built: RV2-DEBT-344 (b) seems fixed -- flip this leaf into a runner row and re-run the matrix")
				}
				t.Logf("SPAWN_ON_344 %s verdict=build-failed %v", row.name, err)
				if !strings.Contains(err.Error(), msg) {
					t.Fatalf("build error %q, want it to name %q (RV2-DEBT-344 (b))", err, msg)
				}
				return
			}
			if err != nil {
				t.Logf("SPAWN_ON_344 %s verdict=build-failed %v", row.name, err)
				t.Fatalf("PRECONDITION: the hang program does not build: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.Command(exe)
			cmd.Env = overrideEnvVar(overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2"), "SURGE_THREADS", "2")
			res := runCommandWithCancellation(ctx, cmd, subprocessTerminationGrace)
			pgid := 0
			if cmd.Process != nil {
				pgid = cmd.Process.Pid
			}
			if left := spawnOnGroupSurvivors(pgid); pgid != 0 && len(left) > 0 {
				t.Errorf("process group %d survived the kill: %v", pgid, left)
			}
			if errors.Is(res.contextErr, context.DeadlineExceeded) {
				t.Logf("SPAWN_ON_344 %s verdict=hang", row.name)
				return
			}
			t.Logf("SPAWN_ON_344 %s verdict=completed exit=%d", row.name, res.exitCode)
			t.Fatalf("exit %d stdout %q stderr %q within 10 s: RV2-DEBT-344 (a) seems fixed -- flip this leaf into a runner row at two shards and re-run the matrix",
				res.exitCode, res.stdout, res.stderr)
		})
	}
}
