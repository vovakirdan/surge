package driver

import "testing"

// RV2-DEBT-365 tripwire, the `spawn` runner (N-TASK-16). Once `spawn` has an origin transfer it is a runner:
// `spawn leak()` then an await (s0), a member left to an `async { }` block's implicit scope join with no await
// at all (s1), and a callee that returns what it spawned (s2). The first line: each leaking program is refused
// by the task check alone, in the leaking function. The second line: the same programs over a task that borrows
// nothing build, so the runner is live and nothing but the task check stands in front of the leaks;
// TestH2TripwireSpawnRunnerRuns (vm) runs them. One t.Run per program, so a counterfactual can redden one.

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
	h2TripwireS1SpawnScopeJoinDigest = "8d3745682aa90c0e65202036ad01cfd366e4c8eadd0beb2a9d67630c5045cc15"
	h2TripwireS1SpawnScopeJoin       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
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
	h2TripwireS2SpawnedReturnedTwinDigest = "268ece8059c111af44d2ef3a97c1336a854b66af515fc2010e80a614d47a48dc"
	h2TripwireS2SpawnedReturnedTwin       = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    return spawn worker(l);
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
)

func TestH2TripwireTaskCheckRefusesSpawnLeaks(t *testing.T) {
	for _, row := range []struct{ name, text, digest, code, at string }{
		{"s0_spawn_await", h2TripwireS0SpawnAwait, h2TripwireS0SpawnAwaitDigest, "SEM3139", "t"},
		{"s1_spawn_scope_join", h2TripwireS1SpawnScopeJoin, h2TripwireS1SpawnScopeJoinDigest, "SEM3139", "t"},
		{"s2_spawned_returned", h2TripwireS2SpawnedReturned, h2TripwireS2SpawnedReturnedDigest, "SEM3139", "spawn worker(&l)"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.text, row.digest, row.code, row.at); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}

func TestH2TripwireSpawnRunnerBuilds(t *testing.T) {
	for _, row := range []struct{ name, text, digest string }{
		{"s0", h2TripwireS0SpawnAwaitTwin, h2TripwireS0SpawnAwaitTwinDigest},
		{"s1", h2TripwireS1SpawnScopeJoinTwin, h2TripwireS1SpawnScopeJoinTwinDigest},
		{"s2", h2TripwireS2SpawnedReturnedTwin, h2TripwireS2SpawnedReturnedTwinDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			if own, core, built := h2TripwirePending(t, row.text, row.digest); !built {
				t.Errorf("the spawn runner does not build: own rows %+v, core rows %d", own, len(core))
			}
		})
	}
}
