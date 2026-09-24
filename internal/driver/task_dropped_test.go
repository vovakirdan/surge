package driver

import (
	"strings"
	"testing"
)

// The dropped-task rule (SEM3218, owner ruling 2026-09-23): a task made and dropped where it stands is refused,
// whatever made it -- a call of an `async fn` or of a function returning a Task, an `async { }` or `blocking { }`
// block, `checkpoint()`, `sleep(n)` -- and wherever the value is dropped: an expression statement, `let _ = ...`,
// the arms and branches that hand the value on, a for loop's step. Every program is a ROOT program over the real
// core (taskCheckErrorCodes). The last two refused programs are the former controls ctl_a14_discarded_plain_call
// and ctl_lock_task_bound_and_dropped, byte for byte: the task check still accepts them, the new rule does not.

func TestTaskDroppedRefusesTheBorrowedOriginals(t *testing.T) {
	for _, probe := range taskDroppedBorrowedOriginals {
		t.Run(probe.name, func(t *testing.T) {
			if got, errs := taskCheckErrorCodes(t, probe); got != probe.want || len(errs) != 1 {
				t.Fatalf("error codes %q (%d errors), want exactly one %s at the dropped borrowing call", got, len(errs), probe.want)
			}
		})
	}
}

func TestTaskDroppedRefused(t *testing.T) {
	for _, probe := range taskDroppedRefusedProbes {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a task dropped where it is made is not refused", got, probe.want)
			}
		})
	}
}

// The leaf walk reports EVERY dropped branch: two errors, not one code.
func TestTaskDroppedReportsEveryBranch(t *testing.T) {
	for _, name := range []string{"dt_ternary_branches", "dt_compare_arms"} {
		t.Run(name, func(t *testing.T) {
			for _, p := range taskDroppedRefusedProbes {
				if p.name == name {
					if _, errs := taskCheckErrorCodes(t, p); len(errs) != 2 {
						t.Fatalf("got %d errors, want 2 (one per dropped branch)", len(errs))
					}
					return
				}
			}
			t.Fatalf("no probe %s", name)
		})
	}
}

func TestTaskDroppedKept(t *testing.T) {
	for _, probe := range taskDroppedKeptProbes {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q", got, probe.want)
			}
		})
	}
}

func TestTaskDroppedAwaitedLockIsSeen(t *testing.T) {
	for _, probe := range taskDroppedLockProbes {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q", got, probe.want)
			}
		})
	}
}

// The refusal points at the dropped task, says what dropping it lost, and says how to keep it in a way the
// enclosing function can write: `.await()` (with the fix) where awaiting is legal, `spawn` inside an async
// block, an `async fn` in a plain function, outside the body in a `blocking { }` body.
func TestTaskDroppedSaysHowToKeepIt(t *testing.T) {
	for _, row := range []struct {
		name, probe, span, message, note, help, notHelp string
		fix                                             bool
	}{
		{"plain_fn", "dt_lock_plain_fn", "m.lock()", "never runs", "nothing is locked", "make it an `async fn`", ".await()", false},
		{"async_fn", "dt_lock_async_fn", "m.lock()", "never runs", "nothing is locked", "keep the handle", "make it an `async fn`", true},
		{"async_block", "dt_in_async_block", "work()", "never runs", "", "`spawn` it", "make it an `async fn`", true},
		{"entrypoint", "dt_expr_stmt_entrypoint", "work()", "never runs", "", "(`.await()`)", "make it an `async fn`", true},
		{"sleep", "dt_sleep", "sleep(5:uint)", "does not pause", "only when its task is awaited", "(`.await()`)", "", true},
		{"checkpoint", "dt_checkpoint", "checkpoint()", "does not yield", "only when its task is awaited", "(`.await()`)", "", true},
		{"blocking", "dt_blocking", "blocking { ret 1; }", "is not waited for", "blocking pool", "(`.await()`)", "", true},
		{"async_block_value", "dt_async_block_stmt", "async { ret 1; }", "its body never runs", "without starting it", "(`.await()`)", "", true},
		{"function_value", "dt_function_value", "g()", "nothing can wait for it", "", "make it an `async fn`", "", false},
		{"acquire", "dt_acquire_async_fn", "s.acquire()", "never runs", "no permit is taken", "(`.await()`)", "", true},
		{"condition_wait", "dt_condition_wait_async_fn", "c.wait(&m)", "never runs", "stays released", "(`.await()`)", "", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			var probe taskCheckProbe
			for _, p := range taskDroppedRefusedProbes {
				if p.name == row.probe {
					probe = p
				}
			}
			_, errs := taskCheckErrorCodes(t, probe)
			if len(errs) != 1 {
				t.Fatalf("got %d errors, want 1", len(errs))
			}
			d := errs[0]
			if got := probe.text[d.Primary.Start:d.Primary.End]; got != row.span {
				t.Fatalf("primary span reads %q, want %q", got, row.span)
			}
			if !strings.Contains(d.Message, row.message) {
				t.Fatalf("message %q does not say %q", d.Message, row.message)
			}
			if row.note != "" && (len(d.Notes) != 1 || !strings.Contains(d.Notes[0].Msg, row.note)) {
				t.Fatalf("notes %+v do not say %q", d.Notes, row.note)
			}
			if len(d.Help) != 1 || !strings.Contains(d.Help[0].Msg, row.help) || (row.notHelp != "" && strings.Contains(d.Help[0].Msg, row.notHelp)) {
				t.Fatalf("help %+v does not fit the context (want %q, not %q)", d.Help, row.help, row.notHelp)
			}
			if row.fix != (len(d.Fixes) == 1) {
				t.Fatalf("fix offered = %v, want %v: %+v", len(d.Fixes) == 1, row.fix, d.Fixes)
			}
		})
	}
}
