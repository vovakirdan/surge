package vm_test

import "testing"

// RV2-DEBT-365 tripwire, runtime rows for the `spawn` runner (N-TASK-16). The leaking programs are refused by
// the task check on either backend (TestH2TripwireTaskCheckRefusesSpawnLeakedRuns). Their twins borrow nothing
// and run to exit 46: s0 through `spawn leak()` and an await (len 6 + 40), s1 through the implicit scope join of
// an `async { }` block alone -- its child calls rt_exit(46), so a join that did not run the child exits 0.

const (
	h2TripwireS0SpawnAwaitDigest = "740925b6442764fc96aa81c5128e0e870a2651201e847bfa2eea58b73967e11a"
	h2TripwireS0SpawnAwait       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    let t = spawn leak();
    return compare t.await() {
        Success(n) => n + 40;
        Cancelled() => 100;
    };
}
`
	h2TripwireS2SpawnedReturnedDigest = "030ed7806c4d46343940c39d1692f82744503f265f6019c995137e72de07bdc8"
	h2TripwireS2SpawnedReturned       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    return spawn worker(&l);
}

@entrypoint
fn main() -> int {
    let t = spawn leak();
    return compare t.await() {
        Success(n) => n + 40;
        Cancelled() => 100;
    };
}
`
	h2TripwireS0SpawnAwaitTwinDigest = "5fc8471a52d16850b5364fc86f121e524a75f398494f2747e76f2c09eff6df71"
	h2TripwireS0SpawnAwaitTwin       = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

@entrypoint
fn main() -> int {
    let t = spawn leak();
    return compare t.await() {
        Success(n) => n + 40;
        Cancelled() => 100;
    };
}
`
	h2TripwireS1SpawnScopeJoinTwinDigest = "b3d259a87cf8cd51183d4f60d53e0b58e9f572be9a437cee90cbbfc75fba6851"
	h2TripwireS1SpawnScopeJoinTwin       = `async fn worker(x: string) -> int {
    rt_exit(len(x) to int + 40);
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let _t = spawn leak();
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`
)

func TestH2TripwireTaskCheckRefusesSpawnLeakedRuns(t *testing.T) {
	for _, row := range []struct{ name, text, digest, code, at string }{
		{"s0_spawn_await", h2TripwireS0SpawnAwait, h2TripwireS0SpawnAwaitDigest, "SEM3139", "t"},
		{"s2_spawned_returned", h2TripwireS2SpawnedReturned, h2TripwireS2SpawnedReturnedDigest, "SEM3139", "spawn worker(&l)"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.name, row.text, row.digest, row.code, row.at); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}

func TestH2TripwireSpawnRunnerRuns(t *testing.T) {
	for _, row := range []struct{ name, text, digest string }{
		{"s0", h2TripwireS0SpawnAwaitTwin, h2TripwireS0SpawnAwaitTwinDigest},
		{"s1", h2TripwireS1SpawnScopeJoinTwin, h2TripwireS1SpawnScopeJoinTwinDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			out := h2TripwireRun(t, "spawn_runner_"+row.name, row.text, row.digest)
			if verdict := h2HarnessExit.holds(testBackend(t), out); verdict != "" {
				t.Errorf("the spawn runner did not run its task to exit 46: %s", verdict)
			}
		})
	}
}
