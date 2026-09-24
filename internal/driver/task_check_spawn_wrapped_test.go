package driver

import "testing"

// RV2-DEBT-365, N-TASK-16. A spawned handle wrapped in `Some(..)`, a struct or an array names no task at the return
// that hands it out, so the pin its task holds is refused at the callee's exit. The last table records known
// fail-closed over-refusals: a spawn whose operand is neither a call nor a place keys the inner call's pins to an
// identity the spawned handle's join does not release. ROOT programs, diagnosed by taskCheckErrorCodes.

var taskCheckWrappedSpawnedHandles = []taskCheckProbe{
	{"wrap_some_over_a_parameter", "d88674db448e879d7e8182778a8bd6938e23a33b558c4bf7818b4e1e5dc0574a", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

fn wrap(x: &string) -> Option<Task<int>> {
    return Some(spawn sworker(x));
}

fn leak() -> Option<Task<int>> {
    let l: string = "abcdef";
    let o = wrap(&l);
    return o;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"wrap_some_over_a_local", "c291fcdd109966a093fa812d8f14a539807b40bb237259b8ec5b29e9a4bac007", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

fn wrap() -> Option<Task<int>> {
    let l: string = "abcdef";
    return Some(spawn sworker(&l));
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"wrap_struct_over_a_parameter", "7c2a030a25094610aca78a1f158835829cf0505533a76ea0ed1690769b48bbca", "SEM3021,SEM3107,SEM3110", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

type Holder = { t: Task<int> }

fn wrap(x: &string) -> Holder {
    return Holder { t = spawn sworker(x) };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"wrap_array_over_a_local", "f7a2bed0aa879ff32ae039a571acc8d031836e125743d2b88e1c1c4b6f2ba833", "SEM3021,SEM3110", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

fn wrap() -> Task<int>[] {
    let l: string = "abcdef";
    let ts: Task<int>[] = [spawn sworker(&l)];
    return ts;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rec_wrap_struct_borrowing_nothing", "53187f1920e978322ab83100b712e3f8da1ba95a6cdf58921f4cf9d59a144dbf", "SEM3107,SEM3110", `async fn work(n: int) -> int {
    return n + 1;
}

type Holder = { t: Task<int> }

fn wrap(n: int) -> Holder {
    return Holder { t = spawn work(n) };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckWrappedSpawnControls = []taskCheckProbe{
	{"ctl_wrap_some_borrowing_nothing", "b0cd68e0841b1301115e4a83cc11c87694772a29eb67a5a6ae2798e0958cc74f", "", `async fn work(n: int) -> int {
    return n + 1;
}

fn wrap(n: int) -> Option<Task<int>> {
    return Some(spawn work(n));
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckNonPlaceSpawnOperands = []taskCheckProbe{
	{"o1_ternary_operand", "889a007d62076125236dc13dd0525a6cbcc82531e71f11ea5ff314535cfb826a", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn o1() -> int {
    let a: string = "a";
    let b: string = "b";
    let c: bool = true;
    let t = spawn (c ? sworker(&a) : sworker(&b));
    return compare t.await() {
        Success(n) => n;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"o1_compare_operand", "ce62ba13ca4b198013adb07868d260391ffe6723496c2794ddf26e1faea0e9a3", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn o1() -> int {
    let a: string = "a";
    let b: string = "b";
    let c: bool = true;
    let t = spawn (compare c {
        true => sworker(&a);
        false => sworker(&b);
    });
    return compare t.await() {
        Success(n) => n;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"o1_value_block_operand", "821dc8ec7643277265533bbe71091f2b20582c29d2de652c649e2e414c85f07a", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn o1() -> int {
    let a: string = "a";
    let b: string = "b";
    let c: bool = true;
    let t = spawn ({
        ret sworker(&a);
    });
    return compare t.await() {
        Success(n) => n;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

// 6 RUN: 1 parent, 5 leaves.
func TestTaskCheckRefusesWrappedSpawnedHandles(t *testing.T) {
	for _, probe := range taskCheckWrappedSpawnedHandles {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a wrapped spawned handle left its frame unrefused (RV2-DEBT-365, N-TASK-16)", got, probe.want)
			}
		})
	}
}

// 2 RUN: 1 parent, 1 leaves.
func TestTaskCheckKeepsWrappedSpawnedHandles(t *testing.T) {
	for _, probe := range taskCheckWrappedSpawnControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}

// 4 RUN: 1 parent, 3 leaves.
func TestTaskCheckRecordsNonPlaceSpawnOperands(t *testing.T) {
	for _, probe := range taskCheckNonPlaceSpawnOperands {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: the recorded over-refusal of a non-place spawn operand moved (RV2-DEBT-365, N-TASK-16)", got, probe.want)
			}
		})
	}
}
