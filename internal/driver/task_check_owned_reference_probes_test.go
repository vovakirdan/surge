package driver

import "testing"

// RV2-DEBT-365, the `own &T` capture form (P1u-TC2 R2). `own` is a move annotation, not a copy: an
// `own &T` capture is a borrow, and each capture gate asks about the type under the `own` --
// `blocking` (SEM3133), `on` and `spawn on` (SEM3165); a `let` of that type captured by `async` is
// refused with R-d's capture half (SEM3021). Each body passes the capture on (`peek(o)`), the shape
// measured to reach the `on` gate on D2. A parameter captured by `async` is the caller's and stays.

var taskCheckOwnedReferenceCaptures = []taskCheckProbe{
	{"ownref_let_captured_by_blocking", "d310f43c7af9f24a6dddd964064dd6f8082f3b7f57b216cf9263af2a83e7cf1b", "SEM3133", `fn peek(r: own &int) -> int {
    let _ = r;
    return 7;
}

fn leak() -> Task<int> {
    let xs: int[] = [5];
    let o = own xs[0];
    return blocking {
        ret peek(o);
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ownref_param_captured_by_blocking", "a996d3596981af81dea03a67637fe256e17c6a0af07098a1f1619cdbdffb59e0", "SEM3133", `fn peek(r: own &int) -> int {
    let _ = r;
    return 7;
}

fn leak(o: own &int) -> Task<int> {
    return blocking {
        ret peek(o);
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ownref_let_captured_by_async", "4ac3e43fc1e0313cc03c1e323d3d5db2c5b1c87c7b367a06e468c079c87de5b1", "SEM3021", `fn peek(r: own &int) -> int {
    let _ = r;
    return 7;
}

fn leak() -> Task<int> {
    let xs: int[] = [5];
    let o = own xs[0];
    return async {
        ret peek(o);
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ownref_param_captured_by_on", "bb329e13bd615390c8ce2d11c1547dff901f53a403ad509871c977e4fe79b495", "SEM3165", `fn peek(r: own &int) -> int {
    let _ = r;
    return 7;
}

async fn read_on(o: own &int) -> int {
    let res = on pool {
        ret peek(o);
    };
    return compare res {
        Success(v) => v;
        _ => 99;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ownref_param_captured_by_spawn_on", "7425b9a5eda8998c60896529b10435dfcb6a2da1748b9e6b58a0c5d696ed9688", "SEM3165", `fn peek(r: own &int) -> int {
    let _ = r;
    return 7;
}

async fn leak(o: own &int) -> int {
    let t: far Task<int> = spawn on shard(1:ShardId) {
        ret peek(o);
    };
    return compare t.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckOwnedReferenceControls = []taskCheckProbe{
	{"ctl_ownref_param_captured_by_async", "c83a84ae5ea9cd5bd704dc2ff2e754ad4832f43d35bd62dd160f14468c15f7c3", "", `fn peek(r: own &int) -> int {
    let _ = r;
    return 7;
}

fn sound(o: own &int) -> Task<int> {
    return async {
        ret peek(o);
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

func TestTaskCheckRefusesOwnedReferenceCaptures(t *testing.T) {
	for _, probe := range taskCheckOwnedReferenceCaptures {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: an own-reference capture passed its gate (RV2-DEBT-365)", got, probe.want)
			}
		})
	}
}

func TestTaskCheckKeepsOwnedReferenceParameterCaptures(t *testing.T) {
	for _, probe := range taskCheckOwnedReferenceControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
