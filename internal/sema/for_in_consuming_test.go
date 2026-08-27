package sema

import (
	"context"
	"sort"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// A `for` loop READS its elements; it never consumes them. Owner ruling
// 2026-08-26 (RV2-DEBT-258): moving out of the loop binding is refused, a
// container of affine elements is drained by popping, and `for x in own xs` is
// a feature the language does not have yet rather than a spelling it silently
// ignores.
//
// The harness is taskCloneCodes: the rows need the real `await`, `pop`,
// `__len` and `safe` signatures, not a bare struct, and every clean row asserts
// NO error at all so a prelude that fails to type-check cannot pass vacuously.
// The refused rows beside them prove the same prelude reaches the analysis.
const forInConsumingPrelude = `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
tag Some<T>(T);
type Option<T> = Some(T) | nothing;
@intrinsic
type Task<T> = { __opaque: int };
extern<Task<T>> {
    @intrinsic pub fn await(self: own Task<T>) -> TaskResult<T>;
}
extern<Array<T>> {
    @intrinsic pub fn push(self: &mut Array<T>, value: T) -> nothing;
    @intrinsic pub fn pop(self: &mut Array<T>) -> Option<T>;
    @intrinsic pub fn __len(self: &Array<T>) -> uint;
}
extern<Option<T>> {
    @intrinsic pub fn safe(self: Option<T>) -> T;
}
extern<string> {
    @intrinsic pub fn __clone(self: &string) -> string;
}
type Item = { name: string, id: int };
type Holder = { opt: Option<int>, name: string };
async fn work() -> int { return 1; }
fn take(s: string) -> nothing { return nothing; }
fn peek(s: &string) -> nothing { return nothing; }
fn peek_task(t: &Task<int>) -> nothing { return nothing; }
fn take_int(i: int) -> nothing { return nothing; }
fn take_float(f: float) -> nothing { return nothing; }
fn take_opt(o: Option<int>) -> nothing { return nothing; }
`

func TestForInDoesNotConsumeItsElements(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // every error code the program must report; nil means clean
	}{
		// Moving out of the loop binding, three spellings.
		{"string_moved_into_call", `fn f(names: string[]) -> nothing { for s in names { take(s); } return nothing; }`, []string{"SEM3205"}},
		{"string_moved_into_let", `fn f(names: string[]) -> nothing { for s in names { let held = s; take(held); } return nothing; }`, []string{"SEM3205"}},
		{"field_moved_out_of_binding", `fn f(items: Item[]) -> nothing { for it in items { take(own it.name); } return nothing; }`, []string{"SEM3205"}},
		// A task container iterated by `for` is not drained: the await moves the
		// binding, and the container is still pending at scope exit.
		{"task_awaited_through_for", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); for t in tasks { let _ = t.await(); } return 0; }`, []string{"SEM3107", "SEM3205"}},
		// Reading the tasks is fine; forgetting to drain afterwards is the
		// tracker's existing refusal, and nothing about the loop itself.
		{"task_read_then_not_drained", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); for t in tasks { peek_task(&t); } return 0; }`, []string{"SEM3107"}},
		// `own` in the iterable position asks for the consuming loop the
		// language does not have: refused by name, and the body's move is not
		// reported a second time on top of it.
		{"own_iterable", `fn f(names: string[]) -> nothing { for s in own names { take(s); } return nothing; }`, []string{"SEM3206"}},
		// Parentheses are the same request, not a way around it.
		{"own_iterable_parenthesised", `fn f(names: string[]) -> nothing { for s in (own names) { take(s); } return nothing; }`, []string{"SEM3206"}},
		// A union element is read by `compare`, which moves a non-Copy subject
		// by the language's rule. When the union owns no heap, what leaves is
		// bits the container never frees, so there is no double free to refuse
		// -- whole, by value, or through a field of an element that does own
		// heap elsewhere.
		{"compare_reads_copy_payload", `fn f(opts: Option<int>[]) -> nothing { for r in opts { compare r { Some(v) => take_int(v); nothing => take_int(0); } } return nothing; }`, nil},
		{"heap_free_union_passed_by_value", `fn f(opts: Option<int>[]) -> nothing { for r in opts { take_opt(r); } return nothing; }`, nil},
		{"compare_reads_heap_free_field", `fn f(hs: Holder[]) -> nothing { for h in hs { compare own h.opt { Some(v) => take_int(v); nothing => take_int(0); } } return nothing; }`, nil},
		// A payload the container owns -- a string, a task handle, and the
		// reference-counted `float`, whose count an owned compare moves out of
		// the envelope for the binding to release -- is refused however the arm
		// uses it: the arm's binding owes a release the container also owes.
		{"compare_owns_string_payload", `fn f(opts: Option<string>[]) -> nothing { for r in opts { compare r { Some(s) => peek(&s); nothing => take_int(0); } } return nothing; }`, []string{"SEM3205"}},
		{"compare_moves_string_payload", `fn f(opts: Option<string>[]) -> nothing { for r in opts { compare r { Some(s) => take(s); nothing => take_int(0); } } return nothing; }`, []string{"SEM3205"}},
		{"compare_owns_counted_payload", `fn f(opts: Option<float>[]) -> nothing { for r in opts { compare r { Some(x) => take_float(x); nothing => take_int(0); } } return nothing; }`, []string{"SEM3205"}},
		{"compare_owns_task_payload", `async fn f(opts: Option<Task<int>>[]) -> int { for r in opts { compare r { Some(t) => peek_task(&t); nothing => take_int(0); } } return 0; }`, []string{"SEM3205"}},
		// The borrow form compiles for all of them: a reference-typed subject
		// is inspected, and its payload bindings own nothing.
		{"compare_through_borrow_reads_string_payload", `fn f(opts: Option<string>[]) -> nothing { for r in opts { compare &r { Some(s) => peek(s); nothing => take_int(0); } } return nothing; }`, nil},
		{"compare_through_borrow_reads_task_payload", `async fn f(opts: Option<Task<int>>[]) -> int { for r in opts { compare &r { Some(t) => peek_task(t); nothing => take_int(0); } } return 0; }`, nil},
		// The legal forms.
		{"strings_drained_by_pop", `fn f() -> nothing { let mut xs: string[] = []; xs.push("a"); while xs.__len() > 0:uint { let s = xs.pop().safe(); take(s); } return nothing; }`, nil},
		{"tasks_drained_by_pop", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let _ = t.await(); } return 0; }`, nil},
		{"tasks_drained_by_pop_inline", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let r = tasks.pop().safe().await(); let _ = r; } return 0; }`, nil},
		{"tasks_read_then_drained_by_pop", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); for t in tasks { peek_task(&t); } while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let _ = t.await(); } return 0; }`, nil},
		{"strings_read_in_for", `fn f(names: string[]) -> nothing { for s in names { peek(&s); } return nothing; }`, nil},
		{"copy_elements_passed_by_value", `fn f(ns: int[]) -> nothing { for n in ns { take_int(n); } return nothing; }`, nil},
		// A `break` or `continue` in a loop NESTED inside the drain leaves that
		// inner loop; the drain runs to its own condition and empties the
		// container. The tracker used to blame the innermost DRAIN loop for
		// every `break` it walked, because only drain loops were on its stack,
		// so these three programs were refused with SEM3107 telling the author
		// their `break` abandoned a container it never left.
		{"drain_with_break_in_a_nested_while", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let _ = t.await(); let mut i: int = 0; while i < 3 { if i == 1 { break; } i = i + 1; } } return 0; }`, nil},
		{"drain_with_continue_in_a_nested_while", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let _ = t.await(); let mut i: int = 0; while i < 3 { i = i + 1; if i == 1 { continue; } } } return 0; }`, nil},
		{"drain_with_break_in_a_nested_for_in", `async fn f(ns: int[]) -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let _ = t.await(); for n in ns { if n == 1 { break; } take_int(n); } } return 0; }`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedCodes(taskCloneCodes(t, forInConsumingPrelude+tc.src))
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected a clean program, got %v", got)
				}
				return
			}
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				if len(got) == 0 {
					t.Fatalf("expected %v, got clean", want)
				}
				t.Fatalf("expected %v, got %v", want, got)
			}
		})
	}
}

// The code numbers are kicked LITERALLY, not through the constants, for the
// same reason for_in_readonly_test.go kicks 3201 and 3202: a test that reads
// the constant agrees with whatever a parallel lane made it.
const (
	moveOutOfLoopBindingCode = 3205
	ownedIterableCode        = 3206
)

func TestForInConsumingCodeNumbers(t *testing.T) {
	if got := int(diag.SemaMoveOutOfLoopBinding); got != moveOutOfLoopBindingCode {
		t.Fatalf("SemaMoveOutOfLoopBinding is %d, want %d", got, moveOutOfLoopBindingCode)
	}
	if got := int(diag.SemaOwnedIterable); got != ownedIterableCode {
		t.Fatalf("SemaOwnedIterable is %d, want %d", got, ownedIterableCode)
	}
}

// THE WAY OUT IS TESTED HERE because the golden corpus records headlines only:
// a help clause that stopped naming the drain, or named a form the tracker
// does not accept, would not move a golden byte. Each refusal must carry the
// drain in its Help channel, spelled with the container the loop named and in
// the `__len` / `pop().safe()` shape taskContainerDrainLoop recognises.
func TestForInRefusalsNameTheDrain(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		code      diag.Code
		wantHelp  []string // every clause the Help channel must carry, in order
		wantNote  string
		cloneFree bool // no Help clause may name a clone
	}{
		{
			name:     "move_out_of_binding",
			src:      `fn f(names: string[]) -> nothing { for s in names { take(s); } return nothing; }`,
			code:     diag.SemaMoveOutOfLoopBinding,
			wantHelp: []string{"while names.__len() > 0:uint { let s = names.pop().safe(); ... }", "clone(s)"},
			wantNote: "`s` is a copy of an element the container still owns",
		},
		// A task handle's `.clone()` is an entitlement and the container would
		// still need its drain, so the clone clause is withheld here.
		{
			name:      "task_awaited_through_for",
			src:       `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); for t in tasks { let _ = t.await(); } return 0; }`,
			code:      diag.SemaMoveOutOfLoopBinding,
			wantHelp:  []string{"while tasks.__len() > 0:uint { let t = tasks.pop().safe(); ... }"},
			wantNote:  "`t` is a copy of an element the container still owns",
			cloneFree: true,
		},
		{
			name:      "own_iterable",
			src:       `fn f(names: string[]) -> nothing { for s in own names { take(s); } return nothing; }`,
			code:      diag.SemaOwnedIterable,
			wantHelp:  []string{"while names.__len() > 0:uint { let s = names.pop().safe(); ... }"},
			wantNote:  "this `own` would be accepted and then ignored",
			cloneFree: true,
		},
		{
			name:      "container_read_by_for_not_drained",
			src:       `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); for t in tasks { peek_task(&t); } return 0; }`,
			code:      diag.SemaTaskNotAwaited,
			wantHelp:  []string{"while tasks.__len() > 0:uint { let t = tasks.pop().safe(); ... }"},
			wantNote:  "the `for` loop here only reads the tasks in `tasks`",
			cloneFree: true,
		},
		// A union the container owns is refused with the borrow NAMED, twice:
		// the generic `&r`, and the `compare &r { ... }` that reads its tags
		// without taking a payload. `Option<string>` has no clone, so the
		// clone clause must not appear.
		{
			name:      "compare_over_a_union_the_container_owns",
			src:       `fn f(opts: Option<string>[]) -> nothing { for r in opts { compare r { Some(s) => peek(&s); nothing => take_int(0); } } return nothing; }`,
			code:      diag.SemaMoveOutOfLoopBinding,
			wantHelp:  []string{"while opts.__len() > 0:uint { let r = opts.pop().safe(); ... }", "through a borrow (`&r`)", "compare &r { ... }"},
			wantNote:  "`r` is a copy of an element the container still owns",
			cloneFree: true,
		},
		// A drain left early is refused AT the exit, and the way out is to
		// finish the drain, not to add a clone.
		{
			name:      "drain_abandoned_by_return",
			src:       `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let r = t.await(); compare r { Success(v) => { if v == 0 { return 1; } } Cancelled() => { return 2; } } } return 0; }`,
			code:      diag.SemaTaskNotAwaited,
			wantHelp:  []string{"finish the drain first"},
			wantNote:  "`tasks` still holds tasks here",
			cloneFree: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var found *diag.Diagnostic
			for _, d := range forInDiagnostics(t, forInConsumingPrelude+tc.src) {
				if d.Code == tc.code {
					found = d
					break
				}
			}
			if found == nil {
				t.Fatalf("%s was not reported", tc.code.ID())
			}
			if len(found.Help) != len(tc.wantHelp) {
				t.Fatalf("want %d help clause(s) %q, got %+v", len(tc.wantHelp), tc.wantHelp, found.Help)
			}
			for i, want := range tc.wantHelp {
				if !strings.Contains(found.Help[i].Msg, want) {
					t.Fatalf("help clause %d must say %q, got %q", i, want, found.Help[i].Msg)
				}
				if found.Help[i].Span == (source.Span{}) {
					t.Fatalf("help clause %d must be anchored in the source, got an empty span", i)
				}
				if tc.cloneFree && strings.Contains(found.Help[i].Msg, "clone(") {
					t.Fatalf("help clause %d names a clone where none is a way out: %q", i, found.Help[i].Msg)
				}
			}
			noted := false
			for _, note := range found.Notes {
				if strings.Contains(note.Msg, tc.wantNote) {
					noted = true
				}
			}
			if !noted {
				t.Fatalf("note must say %q, got %+v", tc.wantNote, found.Notes)
			}
		})
	}
}

// A drain loop left by `return` or `break` abandons the tasks not yet popped,
// and SEM3107 must say so AT THAT STATEMENT: the lead met the refusal five
// times at the container's declaration before reading the rule off the
// tracker. The container itself is the note. A loop that only tested the
// length without popping is not a drain, and a `for` that read the tasks is
// not an exit, so those keep the refusal at the container.
func TestAbandonedDrainIsRefusedAtTheExit(t *testing.T) {
	cases := []struct {
		name string
		src  string
		at   string // the source text the primary span must start on
	}{
		{"return_inside_drain", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let r = t.await(); compare r { Success(v) => { if v == 0 { return 1; } } Cancelled() => { return 2; } } } return 0; }`, "return 1;"},
		{"break_inside_drain", `async fn f(stop: bool) -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { let t = tasks.pop().safe(); let _ = t.await(); if stop { break; } } return 0; }`, "break;"},
		{"return_in_a_length_test_that_pops_nothing", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); while tasks.__len() > 0:uint { return 1; } return 0; }`, "tasks.push"},
		{"read_by_for_then_returned", `async fn f() -> int { let mut tasks: Task<int>[] = []; tasks.push(spawn work()); for t in tasks { peek_task(&t); } return 0; }`, "tasks.push"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := forInConsumingPrelude + tc.src
			var found *diag.Diagnostic
			for _, d := range forInDiagnostics(t, full) {
				if d.Code == diag.SemaTaskNotAwaited {
					found = d
					break
				}
			}
			if found == nil {
				t.Fatalf("SEM3107 was not reported")
			}
			want := len(forInConsumingPrelude) + strings.Index(tc.src, tc.at)
			if int(found.Primary.Start) != want {
				t.Fatalf("SEM3107 points at offset %d (%q), want %d (%q)",
					found.Primary.Start, full[found.Primary.Start:min(int(found.Primary.End), len(full))], want, tc.at)
			}
		})
	}
}

// forInDiagnostics is taskCloneCodes keeping the diagnostics whole, for the
// structure assertions above.
func forInDiagnostics(t *testing.T, src string) []*diag.Diagnostic {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("prelude does not parse: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	return semaBag.Items()
}

func sortedCodes(codes map[string]bool) []string {
	out := make([]string, 0, len(codes))
	for code := range codes {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}
