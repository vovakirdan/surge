package driver

import (
	"strings"
	"testing"
)

// RV2-DEBT-365 / RV2-DEBT-370, TC-1d narrowed (W4-G1 R2.2). A Task-valued call dropped where it stands
// leaves nothing to join, and that is sound only when the dropped value is the only handle on a task
// that is still cold: the runtime discards it unrun (docs/RUNTIME.md §3.1). A direct `async fn` call,
// or a plain function that returns one directly, gives that; a function that cancels its task, or a
// function value, may hand back a running task, whose dropped handle is refused (SEM3021). Every
// program is a ROOT program over the real core (taskCheckErrorCodes). The refused rows' programs are
// the coordinator's measured probes: before the narrowing they diagnosed clean and ran the task.

var taskCheckDroppedRunningTaskProbes = []taskCheckProbe{
	{"tc1d_hot_cancel", "0169475d854b2c0f417c545ffc7bf97305f47345f54f185a46ff9b70880e9442", "SEM3021", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn hot(x: &string) -> Task<int> { let t = worker(x); t.cancel(); return t; }
fn leak() -> int { let l: string = "abcdef"; hot(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_hot_ref_cancel", "f2715e3b6ea61e20e8714ad75ff5ac0bbb56acd3d4be4bf96cedf47321458296", "SEM3021", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn hot(x: &string) -> Task<int> { let t = worker(x); (&t).cancel(); return t; }
fn leak() -> int { let l: string = "abcdef"; hot(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_hot_cancel_member", "6f624f163e1fd3471e6eb2ef7f920705549b885b8df4d23db05b6e2ea6f7ab31", "SEM3021", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn hot(x: &string) -> Task<int> { let t = worker(x); t.cancel(); return t; }
async fn outer() -> int { let l: string = "abcdef"; hot(&l); let _ = checkpoint().await(); return 0; }
@entrypoint
fn main() -> int { let r = compare outer().await() { Success(n) => n; Cancelled() => 41; }; let _ = checkpoint().await(); return r; }
`},
	{"tc1d_hot_ref_cancel_member", "7689dc2c1f1a791e5837919249df795850e7211bc32ea8693ac3aa03f73fb1f0", "SEM3021", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn hot(x: &string) -> Task<int> { let t = worker(x); (&t).cancel(); return t; }
async fn outer() -> int { let l: string = "abcdef"; hot(&l); let _ = checkpoint().await(); return 0; }
@entrypoint
fn main() -> int { let r = compare outer().await() { Success(n) => n; Cancelled() => 41; }; let _ = checkpoint().await(); return r; }
`},
	{"tc1d_function_value_callee", "4ec99aa164d3285612adffd262acc397f07f8234fc30ddc9e64cac618b5298a1", "SEM3021", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn fwd(x: &string) -> Task<int> { return worker(x); }
fn leak(f: fn(&string) -> Task<int>) -> int { let l: string = "abcdef"; f(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(fwd); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_let_underscore_hot", "583fb75fa3edf5e8360e0c7aee5624e121f58881e2dc14812d3456c5c4a5ab19", "SEM3021", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn hot(x: &string) -> Task<int> { let t = worker(x); t.cancel(); return t; }
fn leak() -> int { let l: string = "abcdef"; let _ = hot(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_callee_declared_below", "c6e05f85abacd2372c37a7b38b8a7103dfcb6a941efde0a08ff21ed19b675818", "SEM3021", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn leak() -> int { let l: string = "abcdef"; fwd_below(&l); return 0; }
fn fwd_below(x: &string) -> Task<int> { return worker(x); }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_user_method_cancels", "4ab12759e01806f670287cd709cc4e3230cff52fe5852015b0e0cc844633b01e", "SEM3021", `type Holder = { s: string }

async fn peek(b: &Holder) -> int { rt_exit(len(b.s) to int + 40); return len(b.s) to int; }

extern<Holder> {
    pub fn go(self: &Holder) -> Task<int> {
        let t = peek(self);
        t.cancel();
        return t;
    }
}

fn leak() -> int { let b: Holder = Holder { s = "abcdef" }; b.go(); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
}

var taskCheckDroppedColdTaskControls = []taskCheckProbe{
	{"tc1d_ctl_direct_async_call", "ea70c571e5f85f0974074bd5d68c571b9d8f0b2b0d185ff8e253aa02d9796c59", "SEM3218", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn leak() -> int { let l: string = "abcdef"; worker(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_ctl_forwarder", "870a598fee1ed9c6c61a409b0beb20523eb2f17a25ae0eb5b0758229c63dfbac", "SEM3218", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn fwd(x: &string) -> Task<int> { return worker(x); }
fn leak() -> int { let l: string = "abcdef"; fwd(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_ctl_forwarder_member", "c0fc9168515340d671c9d2fcd0c7788e464fae8229543bb400232b5a62976e87", "SEM3218", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn fwd(x: &string) -> Task<int> { return worker(x); }
async fn outer() -> int { let l: string = "abcdef"; fwd(&l); let _ = checkpoint().await(); return 0; }
@entrypoint
fn main() -> int { let r = compare outer().await() { Success(n) => n; Cancelled() => 41; }; let _ = checkpoint().await(); return r; }
`},
	{"tc1d_ctl_forwarder_ret_block", "35175be31713fa9ded0106d639a397981f062952a3c19e3d25458736082fcc26", "SEM3218", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn fwdb(x: &string) -> Task<int> { return { ret worker(x); }; }
fn leak() -> int { let l: string = "abcdef"; fwdb(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_ctl_transitive_forwarder", "504e86c82fe625e2a503821ceecec05780f164b3e490b863c311ce09b529ab25", "SEM3218", `async fn worker(x: &string) -> int { rt_exit(len(x) to int + 40); return len(x) to int; }
fn fwd(x: &string) -> Task<int> { return worker(x); }
fn fwd2(x: &string) -> Task<int> { return fwd(x); }
fn leak() -> int { let l: string = "abcdef"; fwd2(&l); return 0; }
@entrypoint
fn main() -> int { let r = leak(); let _ = checkpoint().await(); let _ = checkpoint().await(); return r; }
`},
	{"tc1d_ctl_lock_dropped", "177af91fa3c6ed1472c13095113eb9dad2c02642d5a814c3e76d6a3e49484c03", "SEM3218", `fn locked() -> int {
    let m = Mutex.new();
    m.lock();
    m.unlock();
    return 0;
}

@entrypoint
fn main() -> int {
    return locked();
}
`},
}

// 9 RUN: 1 parent, 8 leaves.
func TestTaskCheckRefusesDroppedRunningTasks(t *testing.T) {
	for _, probe := range taskCheckDroppedRunningTaskProbes {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a dropped handle on a task that may be running is not refused (TC-1d)", got, probe.want)
			}
		})
	}
}

// 7 RUN: 1 parent, 6 leaves. A cold task dropped where it stands is not a borrow hazard (the runtime discards it
// unrun), and it is still refused: by the dropped-task rule, SEM3218, not by the task check.
func TestTaskCheckRefusesDroppedColdTasks(t *testing.T) {
	for _, probe := range taskCheckDroppedColdTaskControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a dropped cold task is not refused by the dropped-task rule alone", got, probe.want)
			}
		})
	}
}

// The refusal points at the dropped call, names what the task borrows and where, says why the callee
// does not give a cold task, and says how to keep a handle in a way the enclosing function can write:
// `.await()` only where await is legal (an async body, an `@entrypoint` function), otherwise an
// `async fn`, an owned value, or a callee that returns its `async fn` call directly.
func TestTaskCheckDroppedRunningTaskSaysHowToKeepIt(t *testing.T) {
	for _, row := range []struct {
		name, probe string
		async       bool
	}{
		{"plain_fn", "tc1d_hot_cancel", false},
		{"async_fn", "tc1d_hot_cancel_member", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			var probe taskCheckProbe
			for _, p := range taskCheckDroppedRunningTaskProbes {
				if p.name == row.probe {
					probe = p
				}
			}
			_, errs := taskCheckErrorCodes(t, probe)
			if len(errs) != 1 {
				t.Fatalf("got %d errors, want 1", len(errs))
			}
			d := errs[0]
			if got := probe.text[d.Primary.Start:d.Primary.End]; got != "hot(&l)" {
				t.Fatalf("primary span reads %q, want the dropped call", got)
			}
			if !strings.Contains(d.Message, "borrows 'l'") || !strings.Contains(d.Message, "dropped here") {
				t.Fatalf("message does not name the borrow and the drop: %q", d.Message)
			}
			if len(d.Notes) != 2 || probe.text[d.Notes[0].Span.Start:d.Notes[0].Span.End] != "&l" ||
				!strings.Contains(d.Notes[1].Msg, "'hot' does not return a fresh `async fn` call directly") {
				t.Fatalf("notes do not show the borrow and the callee: %+v", d.Notes)
			}
			if len(d.Help) != 1 {
				t.Fatalf("want one help, got %+v", d.Help)
			}
			help := d.Help[0].Msg
			if row.async != strings.Contains(help, ".await()") || row.async == strings.Contains(help, "make it an `async fn`") {
				t.Fatalf("help does not fit the context (async=%v): %q", row.async, help)
			}
			if strings.Contains(help, "owned value") || strings.Contains(help, "return its `async fn` call directly") {
				t.Fatalf("help offers a fix that still drops the task (SEM3218): %q", help)
			}
		})
	}
}

// A callee not judged yet at the call -- declared below it -- is named as such.
func TestTaskCheckDroppedCallOfUncheckedCalleeSaysWhy(t *testing.T) {
	var probe taskCheckProbe
	for _, p := range taskCheckDroppedRunningTaskProbes {
		if p.name == "tc1d_callee_declared_below" {
			probe = p
		}
	}
	_, errs := taskCheckErrorCodes(t, probe)
	if len(errs) != 1 || len(errs[0].Notes) != 2 || !strings.Contains(errs[0].Notes[1].Msg, "'fwd_below' is not fully checked yet at this call") {
		t.Fatalf("the unchecked-callee note is missing: %+v", errs)
	}
}
