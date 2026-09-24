package driver

import "testing"

// RV2-DEBT-365, R-a (P1u-TC4 R5). A Task-valued call dropped where it stands is exempt from the task check only
// when the dropped value is the only handle on a task that is still COLD (TC-1d as W4-G1 R2.2 narrowed it,
// task_discarded_call.go; docs/RUNTIME.md 3.1). A callee that hands back what `spawn` made -- directly, through a
// binding, a forwarder, or itself while it is not yet judged -- hands back a published task, so its dropped call
// is refused at the call (SEM3021). These rows witness that R-a is closed; a dropped task that borrows nothing is
// refused by the dropped-task rule (SEM3218). ROOT programs, diagnosed by taskCheckErrorCodes.

var taskCheckDroppedSpawningCalleeTasks = []taskCheckProbe{
	{"ra_dropped_spawning_callee", "9a5fbfe960333d3b602e7862c46ea4e6f0ad17d6980f239e43e043ffcb595c05", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start3(x: &string) -> Task<int> {
    return spawn worker(x);
}

fn leak() -> int {
    let l: string = "abcdef";
    start3(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_spawning_callee_let_underscore", "6f293039a7d3b950d7f0f10cedfc2bf571ed9b2cef054f491eb89aa761880e17", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start3(x: &string) -> Task<int> {
    return spawn worker(x);
}

fn leak() -> int {
    let l: string = "abcdef";
    let _ = start3(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_spawning_callee_value_block", "53ea30acc66d9e30271aa6769fabc461427a75edee55fc0b10c4a1416791650b", "SEM3021,SEM3218", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start3(x: &string) -> Task<int> {
    return spawn worker(x);
}

fn leak() -> int {
    let l: string = "abcdef";
    let _ = {
        ret start3(&l);
    };
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_spawning_callee_compare_arms", "85362de14cea9667309dbe83c44aecef2744bede51fe91187b651e3e3c9993b0", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start3(x: &string) -> Task<int> {
    return spawn worker(x);
}

fn leak() -> int {
    let l: string = "abcdef";
    let flag: bool = true;
    compare flag {
        true => start3(&l);
        false => start3(&l);
    };
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_forwarder_of_spawning_callee", "d36f5285b142974b40b9166d3fe0850467fc72e2e1edbbe505210f4da13bb486", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start3(x: &string) -> Task<int> {
    return spawn worker(x);
}

fn start4(x: &string) -> Task<int> {
    return start3(x);
}

fn leak() -> int {
    let l: string = "abcdef";
    start4(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_callee_binding_a_spawn", "7688f38514a3879556ae342379081196b8ad9b564c4699ddbf8062637221e30a", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start5(x: &string) -> Task<int> {
    let t = spawn worker(x);
    return t;
}

fn leak() -> int {
    let l: string = "abcdef";
    start5(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_spawning_callee_over_a_parameter", "e958c6fa46d1c1dacc12cf9636392b8e603131790ab314fe4fec904b76b7e84d", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start3(x: &string) -> Task<int> {
    return spawn worker(x);
}

fn relay(x: &string) -> int {
    start3(x);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_self_call_not_yet_judged", "c8724ede0c17ef0d5a27c4aa65a53351f3fd8cc09ec2c6f36be677ee95362400", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start7(x: &string, n: int) -> Task<int> {
    if n > 0 {
        start7(x, n - 1);
    }
    return spawn worker(x);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ra_dropped_spawning_callee_borrowing_nothing", "ed1b101f4cf2cbbbcd56d9a432bfe93b9c95816db66dc9adb5ecc1118a1ec95f", "SEM3218", `async fn plain(n: int) -> int {
    return n;
}

fn start_owned(n: int) -> Task<int> {
    return spawn plain(n);
}

fn ok() -> int {
    start_owned(1);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckSpawningCalleeTaskControls = []taskCheckProbe{
	{"ctl_ra_spawning_callee_bound_and_awaited", "e73eaa573c05f3ce4920e0703ec49b7b162372de2022803fd53ff10bac42d149", "", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn start3(x: &string) -> Task<int> {
    return spawn worker(x);
}

async fn ok() -> int {
    let l: string = "abcdef";
    let t = start3(&l);
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

// 10 RUN: 1 parent, 9 leaves.
func TestTaskCheckRefusesDroppedSpawningCalleeTasks(t *testing.T) {
	for _, probe := range taskCheckDroppedSpawningCalleeTasks {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a dropped call left a published task borrowing (RV2-DEBT-365, P1u-TC4)", got, probe.want)
			}
		})
	}
}

// 2 RUN: 1 parent, 1 leaves.
func TestTaskCheckKeepsSpawningCalleeTaskPrograms(t *testing.T) {
	for _, probe := range taskCheckSpawningCalleeTaskControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
