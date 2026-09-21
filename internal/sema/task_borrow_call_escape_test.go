package sema

import (
	"sort"
	"strings"
	"testing"
)

// A task that borrows a frame must not outlive it, however the task was made and however its
// handle leaves. `spawn f(&l)` returned in place was the one shape the check knew. These rows
// are the others: a task answered by a plain call, a handle that leaves through something the
// return cannot name, a receiver lent without an `&`, and a `ret` out of a body -- each with
// the control that must stay accepted next to it.
const taskCallEscapePrelude = `type Cell = { n: int };
async fn cell_len(c: &Cell) -> int { return (*c).n; }
extern<Cell> {
    pub fn size(self: &Cell) -> Task<int> { return cell_len(self); }
}
type AsyncRef = async fn(&int) -> int;
fn pass(t: Task<int>) -> Task<int> { return t; }
fn same<T>(x: T) -> T { return x; }
async fn waiter(t: Task<int>) -> int { let _ = t.await(); return 0; }
fn start(p: &int) -> Task<int> { return async { ret *p; }; }
fn fwd(p: &int) -> Task<int> { return worker(p); }
fn peek(p: &int) -> int { return *p; }
async fn owned(v: int) -> int { return v; }
fn detached(p: &int) -> Task<int> { return owned(peek(p)); }
fn pick(p: &int, c: bool) -> Task<int> { if c { return worker(p); } return owned(1); }
fn echo(x: &int) -> &int { return x; }
fn longer(a: &int, b: &int) -> &int { return a; }
async fn bump(x: &mut int) -> int { return 0; }
async fn sworker(x: &string) -> int { return 0; }
`

// taskCallEscapeCodes is the sorted set of error codes one body draws.
func taskCallEscapeCodes(t *testing.T, body string) (codes string, notes []string) {
	t.Helper()
	c := checkTaskCloneBorrow(t, taskCallEscapePrelude+body)
	set := c.errorCodes()
	keys := make([]string, 0, len(set))
	for code := range set {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	for _, d := range c.diags {
		for _, note := range d.Notes {
			notes = append(notes, note.Msg)
		}
	}
	return strings.Join(keys, ","), notes
}

func TestTaskBorrowedFrameCannotBeOutlived(t *testing.T) {
	const leaked, held = "SEM3139", "SEM3021"
	rows := []struct {
		name, body, want string
	}{
		// A task answered by a plain call (the a-forms).
		{"a1_bound_call_returned", `fn f() -> Task<int> { let l: int = 5; let t = worker(&l); return t; }`, leaked},
		{"a5_callable_value", `fn f() -> Task<int> { let l: int = 5; let g: AsyncRef = worker; let t = g(&l); return t; }`, leaked},
		{"a6_block_behind_a_call", `fn f() -> Task<int> { let l: int = 5; let t = start(&l); return t; }`, leaked},
		{"a7_handed_away", `fn f() -> int { let l: int = 5; let t = worker(&l); consume(t); return 0; }`, held},
		{"a12_spawned_then_returned", `fn f() -> Task<int> { let l: int = 5; let t = worker(&l); return spawn t; }`, leaked},
		{"renamed_then_returned", `fn f() -> Task<int> { let l: int = 5; let t = worker(&l); let u = t; return u; }`, held},
		{"forwarder_holds_what_it_was_lent", `fn f() -> Task<int> { let l: int = 5; let t = fwd(&l); return t; }`, leaked},
		{"one_lending_return_is_enough", `fn f() -> Task<int> { let l: int = 5; let t = pick(&l, true); return t; }`, leaked},
		{"write_under_a_lent_place", `async fn f() -> int { let mut l: int = 5; let t = worker(&l); l = 6; let _ = t.await(); return 0; }`, "SEM3019"},
		// A reference the call CARRIES rather than takes: no `&` is written in the call.
		{"ref_binding_returned", `fn f() -> Task<int> { let l: int = 5; let r = &l; let t = worker(r); return t; }`, leaked},
		{"ref_binding_handed_away", `fn f() -> int { let l: int = 5; let r = &l; let t = worker(r); consume(t); return 0; }`, held},
		{"mut_ref_binding_returned", `fn f() -> Task<int> { let mut l: int = 5; let r = &mut l; let t = bump(r); return t; }`, leaked},
		{"ref_binding_as_receiver", `fn f() -> Task<int> { let c: Cell = { n = 5 }; let rc = &c; let t = rc.size(); return t; }`, leaked},
		{"ref_laundered_through_a_call", `fn f() -> Task<int> { let l: int = 5; let t = worker(echo(&l)); return t; }`, leaked},
		{"ref_out_of_two_reference_arguments", `fn f() -> Task<int> { let l: int = 5; let k: int = 6; let t = worker(longer(&l, &k)); return t; }`, leaked},
		{"ternary_of_borrows_returned", `fn f(c: bool) -> Task<int> { let l: int = 5; let k: int = 6; let t = worker(c ? &l : &k); return t; }`, leaked},
		{"ternary_of_borrows_returned_directly", `fn f(c: bool) -> Task<int> { let l: int = 5; let k: int = 6; return worker(c ? &l : &k); }`, leaked},
		{"ternary_of_borrows_handed_away", `fn f(c: bool) -> int { let l: int = 5; let k: int = 6; let t = worker(c ? &l : &k); consume(t); return 0; }`, held},
		{"ternary_of_borrows_spawned", `fn f(c: bool) -> Task<int> { let l: int = 5; let k: int = 6; return spawn worker(c ? &l : &k); }`, leaked},
		{"compare_of_borrows_returned", `fn f(c: bool) -> Task<int> { let l: int = 5; let k: int = 6; let t = worker(compare c { true => &l; false => &k; }); return t; }`, leaked},
		{"value_block_of_a_borrow_returned", `fn f() -> Task<int> { let l: int = 5; let t = worker({ ret &l; }); return t; }`, leaked},
		{"ref_laundered_then_spawned", `fn f() -> Task<int> { let l: int = 5; return spawn worker(echo(&l)); }`, leaked},
		// What a function's returned Task can hold is published once, after its body.
		{"recursion_reads_no_half_written_fact", `fn rec(x: &int, n: int) -> Task<int> { if n == 0 { return owned(1); } if n == 2 { let l: int = 5; let t = rec(&l, 1); return t; } return worker(x); }`, leaked},
		// A top-level function cannot call one declared below it in a single file (symbol resolution
		// declares and walks each item in turn), so the callee-below order is spelled with methods,
		// which sema resolves after every declaration exists.
		{"mutual_recursion_callee_below", `type Pp = { n: int };
extern<Pp> {
    pub fn ping(self: &Pp, x: &int, n: int) -> Task<int> { if n == 0 { return owned(1); } let l: int = 5; let t = self.pong(&l, n); return t; }
    pub fn pong(self: &Pp, x: &int, n: int) -> Task<int> { if n == 0 { return owned(2); } return worker(x); }
}`, leaked},
		{"mutual_recursion_callee_above", `fn pong(x: &int, n: int) -> Task<int> { if n == 0 { return owned(2); } return worker(x); }
fn ping(x: &int, n: int) -> Task<int> { if n == 0 { return owned(1); } let l: int = 5; let t = pong(&l, n); return t; }`, leaked},
		// Two more ways to free or lose what a task holds.
		{"drop_under_a_lent_place", `async fn f() -> int { let l: string = "abc"; let t = spawn sworker(&l); @drop l; let _ = t.await(); return 0; }`, "SEM3019"},
		{"reassigned_handle_keeps_no_old_identity", `async fn f() -> int { let l: int = 5; let mut t = spawn worker(&l); t = plain(1); let _ = t.await(); return 0; }`, "SEM3021,SEM3107"},
		// A handle the return cannot name (the b-forms and c10).
		{"b1_pass_concrete", `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); return pass(t); }`, held},
		{"b2_pass_generic", `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); return same::<Task<int>>(t); }`, held},
		{"b3_spawn_captures_handle", `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); return spawn waiter(t); }`, held},
		{"b4_handed_away_then_task_return", `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); consume(t); return spawn plain(1); }`, held},
		{"b1p_pass_concrete_over_a_param", `fn f(l: int) -> Task<int> { let t = spawn worker(&l); return pass(t); }`, held},
		{"b3p_spawn_captures_handle_over_a_param", `fn f(l: int) -> Task<int> { let t = spawn worker(&l); return spawn waiter(t); }`, held},
		{"b4p_handed_away_over_a_param", `fn f(l: int) -> Task<int> { let t = spawn worker(&l); consume(t); return spawn plain(1); }`, held},
		{"c10_block_captures_handle", `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); return async { let _ = t.await(); ret 0; }; }`, held},
		// A body is a frame (the c-forms).
		{"c1_ret_hands_out_a_borrower", `fn f() -> Task<Task<int>> { return async { let bl: int = 5; let t = spawn worker(&bl); ret t; }; }`, leaked},
		{"c2p_blocking_ret_plain_call", `fn f() -> Task<Task<int>> { return blocking { let bl: int = 5; let t = worker(&bl); ret t; }; }`, leaked},
		{"c4_ret_with_a_borrower_handed_away", `fn f() -> Task<int> { return async { let bl: int = 5; let t = spawn worker(&bl); consume(t); ret 0; }; }`, held},
		{"c4p_ret_with_a_captured_param_lent", `fn f(p: int) -> Task<int> { return async { let t = spawn worker(&p); consume(t); ret 0; }; }`, held},
		{"c4e_body_falls_off_its_end", `fn f(p: int) -> Task<nothing> { return async { let t = spawn worker(&p); consume(t); }; }`, held},
		{"e1_function_falls_off_its_end", `fn f(l: int) -> nothing { let t = spawn worker(&l); consume(t); }`, held},
		{"e1l_local_dies_with_its_block", `fn f() -> nothing { let l: int = 5; let t = spawn worker(&l); consume(t); }`, held},
		// The forms read open and never measured (R-O2 to R-O7).
		{"ro2_receiver_direct", `fn f() -> Task<int> { let c: Cell = { n = 5 }; return c.size(); }`, leaked},
		{"ro3_receiver_bound", `fn f() -> Task<int> { let c: Cell = { n = 5 }; let t = c.size(); return t; }`, leaked},
		{"ro3s_receiver_spawned", `fn f() -> Task<int> { let c: Cell = { n = 5 }; return spawn c.size(); }`, leaked},
		{"ro4_ternary", `fn f(c: bool) -> Task<int> { let l: int = 5; return c ? worker(&l) : worker(&l); }`, held},
		{"ro5_ret_block", `fn f() -> Task<int> { let l: int = 5; return { let t = worker(&l); ret t; }; }`, leaked},
		{"ro6_pass_plain_call", `fn f() -> Task<int> { let l: int = 5; let t = worker(&l); return pass(t); }`, held},
		{"ro7_stored_through_mut", `fn f(out: &mut Task<int>) -> nothing { let l: int = 5; *out = worker(&l); return nothing; }`, held},
		// Controls: every one of these is a sound program and stays accepted.
		{"ctl_awaited_in_place", `async fn f() -> int { let l: int = 5; let _ = worker(&l).await(); return 0; }`, ""},
		{"ctl_bound_then_awaited", `async fn f() -> int { let l: int = 5; let t = worker(&l); let _ = t.await(); return 0; }`, ""},
		{"ctl_dropped_where_it_stands", `fn f() -> int { let l: int = 5; worker(&l); return 0; }`, ""},
		{"ctl_dropped_by_wildcard", `fn f() -> int { let l: int = 5; let _ = worker(&l); return 0; }`, ""},
		{"ctl_dropped_receiver_call", `fn f() -> int { let c: Cell = { n = 5 }; c.size(); return 0; }`, ""},
		{"ctl_parameter_forwarded", `fn f(p: &int) -> Task<int> { return worker(p); }`, ""},
		{"ctl_callee_reads_before_the_task_exists", `async fn f() -> int { let l: int = 5; let t = detached(&l); consume(t); return 0; }`, ""},
		{"ctl_callee_reads_before_returned", `fn f() -> Task<int> { let l: int = 5; let t = detached(&l); return t; }`, ""},
		{"ctl_spawned_then_joined", `async fn f() -> int { let l: int = 5; let t = worker(&l); let s = spawn t; let _ = s.await(); return 0; }`, ""},
		{"ctl_c5_block_owns_its_capture", `fn f() -> Task<int> { let l: int = 5; let t = spawn async { ret peek(&l); }; return t; }`, ""},
		{"ctl_host_pin_survives_a_body", `async fn f() -> int { let l: int = 5; let t = spawn worker(&l); let b = async { ret 1; }; let _ = b.await(); let _ = t.await(); return 0; }`, ""},
		{"ctl_joined_then_another_returned", `async fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let _ = t.await(); return spawn plain(1); }`, ""},
		{"ctl_ternary_of_borrows_awaited", `async fn f(c: bool) -> int { let l: int = 5; let k: int = 6; let t = worker(c ? &l : &k); let _ = t.await(); return 0; }`, ""},
		{"ctl_ref_binding_awaited", `async fn f() -> int { let l: int = 5; let r = &l; let t = worker(r); let _ = t.await(); return 0; }`, ""},
		// Red on the base: only a `let` bound an identity, so the second await joined the first task again (SEM3107).
		{"reassigned_after_join_joins_the_new_task", `async fn f() -> int { let l: int = 5; let mut t = spawn worker(&l); let _ = t.await(); t = spawn plain(1); let _ = t.await(); return 0; }`, ""},
		{"ctl_borrow_free_spawn_returned", `fn f() -> Task<int> { return spawn plain(1); }`, ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got, _ := taskCallEscapeCodes(t, row.body); got != row.want {
				t.Fatalf("error codes %q, want %q", got, row.want)
			}
		})
	}
}

// A `spawn t` is a second handle on the task `t` names, not a clone of it: the refusal must not
// call it one.
func TestSpawnedHandleIsNotCalledAClone(t *testing.T) {
	codes, notes := taskCallEscapeCodes(t, `fn f() -> Task<int> { let l: int = 5; let t = worker(&l); let s = spawn t; return s; }`)
	if codes != "SEM3139" {
		t.Fatalf("error codes %q, want SEM3139", codes)
	}
	for _, note := range notes {
		if strings.Contains(note, "clone") {
			t.Fatalf("a spawn was reported as a clone: %q", note)
		}
	}
}
