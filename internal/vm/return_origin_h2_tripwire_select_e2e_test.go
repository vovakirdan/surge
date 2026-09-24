package vm_test

import "testing"

// RV2-DEBT-365 tripwire, runtime rows for the `select`/`race` runner (N-TASK-20/21). The leaking programs are refused
// by the task check on either backend. Their twins borrow nothing and exit 46: sel1 through the arm the joined head
// wins; sel2 and race through the worker's own rt_exit(46), so they pin that the task ran (select does not cancel, and
// the scope's join runs a published loser), not which arm won.

const (
	h2TripwireSel1AwaitDigest = "7664232b478dc7cc53c8dd5eece935ccc427f3961be5954790c02d57621366d9"
	h2TripwireSel1Await       = `async fn worker(x: &string) -> int {
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
        ret select { leak().await() => 46; };
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`
	h2TripwireRaceAwaitDigest = "e3cc89355ca00129fe72aa8b015b22e46dbb9eea319843f5235e02b3ea3f96b6"
	h2TripwireRaceAwait       = `async fn worker(x: &string) -> int {
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
        ret race { leak().await() => 46; sleep(1000).await() => 47; };
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`
	h2TripwireSel1AwaitTwinDigest = "0d45d3c0520f5c550285c651716f065a4e8a9e621c3f8af00a8b2902a1e0ad39"
	h2TripwireSel1AwaitTwin       = `async fn worker(x: string) -> int {
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
        ret select { leak().await() => 46; };
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`
	h2TripwireSel2DefaultLoserTwinDigest = "1894f704a600b989198e4f6b17108c886eee5c5b0bdd2b7f02f127ca5e94eb65"
	h2TripwireSel2DefaultLoserTwin       = `async fn worker(x: string) -> int {
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
        ret select { leak().await() => 46; default => 47; };
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`
	h2TripwireRaceAwaitTwinDigest = "8bd7d31cf6f8d98e215893888b49c09d2e00ea4c8bd4bd1f88ffe2edfaf1def6"
	h2TripwireRaceAwaitTwin       = `async fn worker(x: string) -> int {
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
        ret race { leak().await() => 46; sleep(1000).await() => 47; };
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`
)

func TestH2TripwireTaskCheckRefusesSelectLeakedRuns(t *testing.T) {
	for _, row := range []struct{ name, text, digest, code, at string }{
		{"sel_await", h2TripwireSel1Await, h2TripwireSel1AwaitDigest, "SEM3139", "t"},
		{"race_await", h2TripwireRaceAwait, h2TripwireRaceAwaitDigest, "SEM3139", "t"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.name, row.text, row.digest, row.code, row.at); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}

func TestH2TripwireSelectRunnerRuns(t *testing.T) {
	for _, row := range []struct{ name, text, digest string }{
		{"sel1", h2TripwireSel1AwaitTwin, h2TripwireSel1AwaitTwinDigest},
		{"sel2", h2TripwireSel2DefaultLoserTwin, h2TripwireSel2DefaultLoserTwinDigest},
		{"race", h2TripwireRaceAwaitTwin, h2TripwireRaceAwaitTwinDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			out := h2TripwireRun(t, "select_runner_"+row.name, row.text, row.digest)
			if verdict := h2HarnessExit.holds(testBackend(t), out); verdict != "" {
				t.Errorf("the select runner did not run its task to exit 46: %s", verdict)
			}
		})
	}
}
