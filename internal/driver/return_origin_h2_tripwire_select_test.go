package driver

import "testing"

// RV2-DEBT-365 tripwire, the `select`/`race` runner (N-TASK-20/21). Once they have an origin transfer, an arm is a
// runner: a head that joins the leaked task (sel_await), a published loser left to the scope's join (sel_default_loser)
// and a race (race_await). The first line: each leaking program is refused by the task check alone, IN THE LEAKER, which
// no runner reaches, so it says nothing about the runner. The second line carries the runner's liveness: the same
// programs over a task that borrows nothing build; TestH2TripwireSelectRunnerRuns (vm) runs them. One t.Run per
// program, so a counterfactual can redden one.

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
	h2TripwireSel2DefaultLoserDigest = "737004d8575b2617c71d0369b23cc49338288cc21b1757518a6b78a2babcbb93"
	h2TripwireSel2DefaultLoser       = `async fn worker(x: &string) -> int {
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
        ret select { leak().await() => 46; default => 47; };
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

func TestH2TripwireTaskCheckRefusesSelectLeaks(t *testing.T) {
	for _, row := range []struct{ name, text, digest, code, at string }{
		{"sel_await", h2TripwireSel1Await, h2TripwireSel1AwaitDigest, "SEM3139", "t"},
		{"sel_default_loser", h2TripwireSel2DefaultLoser, h2TripwireSel2DefaultLoserDigest, "SEM3139", "t"},
		{"race_await", h2TripwireRaceAwait, h2TripwireRaceAwaitDigest, "SEM3139", "t"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.text, row.digest, row.code, row.at); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}

func TestH2TripwireSelectRunnerBuilds(t *testing.T) {
	for _, row := range []struct{ name, text, digest string }{
		{"sel1", h2TripwireSel1AwaitTwin, h2TripwireSel1AwaitTwinDigest},
		{"sel2", h2TripwireSel2DefaultLoserTwin, h2TripwireSel2DefaultLoserTwinDigest},
		{"race", h2TripwireRaceAwaitTwin, h2TripwireRaceAwaitTwinDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			if own, core, built := h2TripwirePending(t, row.text, row.digest); !built {
				t.Errorf("the select runner does not build: own rows %+v, core rows %d", own, len(core))
			}
		})
	}
}
