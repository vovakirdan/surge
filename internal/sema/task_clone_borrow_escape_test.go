package sema

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// A `.clone()` on a Task<T> is a second handle on ONE running task, so it holds
// the borrows that task's spawn took. These rows pin that the return check
// judges a handle by the task it names, at the exit it actually leaves by, and
// that a clone leaves freely only once that task was joined on every path to
// that exit.
//
// Span offsets in the rows are relative to the row's own source; the helper
// adds the prelude length, so a row reads against the text it quotes.
const taskCloneBorrowPrelude = taskCloneEntitlementPrelude + `async fn worker(x: &int) -> int { return *x; }
async fn plain(x: int) -> int { return x; }
fn done(r: TaskResult<int>) -> bool { return true; }
fn step(i: int, r: TaskResult<int>) -> int { return i + 1; }
`

const (
	taskCloneBorrowMessage = "cannot return this task: it borrows 'l', which is freed when the function returns while the task may still be running"
	taskCloneBorrowNote    = "the task borrowed 'l' here"
	taskCloneCloneNote     = "this clone is a second handle on the same task, so it holds the same borrow"
	taskCloneBorrowHelp    = "await the task inside this function on every path to here"
)

// taskCloneSpan is one expected span, relative to the row's source.
type taskCloneSpan struct {
	start, end int
	text       string
}

// taskCloneCheck is one checked source: every diagnostic, the stable activation
// places named per callable, and the full text the spans index into.
type taskCloneCheck struct {
	src    string
	base   int
	diags  []*diag.Diagnostic
	stable map[string][]string
}

func checkTaskCloneBorrow(t *testing.T, body string) taskCloneCheck {
	t.Helper()
	src := taskCloneBorrowPrelude + body
	builder, fileID, parseBag := parseSource(t, src)
	for _, d := range parseBag.Items() {
		t.Logf("parse diag: %s %s", d.Code.ID(), d.Message)
	}
	if parseBag.Len() != 0 {
		t.Fatalf("snippet did not parse cleanly")
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	stable := make(map[string][]string)
	for owner, places := range res.StableActivationPlaces {
		if owner.IsBlock() {
			continue
		}
		fn := stableTestSymbolName(symRes, owner.Fn)
		for _, place := range places {
			stable[fn] = append(stable[fn], stableTestSymbolName(symRes, place))
		}
		sort.Strings(stable[fn])
	}
	for _, d := range semaBag.Items() {
		t.Logf("%v %s: %s", d.Severity, d.Code.ID(), d.Message)
	}
	return taskCloneCheck{src: src, base: len(taskCloneBorrowPrelude), diags: semaBag.Items(), stable: stable}
}

// errorCodes counts the error-severity codes, so a row can demand an exact set.
func (c taskCloneCheck) errorCodes() map[string]int {
	out := map[string]int{}
	for _, d := range c.diags {
		if d.Severity >= diag.SevError {
			out[d.Code.ID()]++
		}
	}
	return out
}

func equalCodeCounts(got, want map[string]int) bool {
	if len(got) != len(want) {
		return false
	}
	for code, n := range want {
		if got[code] != n {
			return false
		}
	}
	return true
}

func (c taskCloneCheck) assertSpan(t *testing.T, what string, got source.Span, want taskCloneSpan) {
	t.Helper()
	start, end := int(got.Start), int(got.End)
	if start != c.base+want.start || end != c.base+want.end {
		t.Fatalf("%s: got [%d:%d], want [%d:%d] (%q)", what, start-c.base, end-c.base, want.start, want.end, want.text)
	}
	if end > len(c.src) || c.src[start:end] != want.text {
		t.Fatalf("%s: span reads the wrong text, want %q", what, want.text)
	}
}

// taskCloneRefusal is a row the return check must refuse with SEM3139. notes
// lists the borrow first, then the clone call when it is not the primary.
type taskCloneRefusal struct {
	name    string
	body    string
	errors  map[string]int
	primary taskCloneSpan
	notes   []taskCloneSpan
	warning string
}

func assertTaskCloneRefusal(t *testing.T, row taskCloneRefusal) {
	t.Helper()
	c := checkTaskCloneBorrow(t, row.body)
	if got := c.errorCodes(); !equalCodeCounts(got, row.errors) {
		t.Fatalf("errors: got %v, want %v", got, row.errors)
	}
	var hit *diag.Diagnostic
	for _, d := range c.diags {
		if d.Code == diag.SemaBorrowEscapesReturn {
			hit = d
		}
	}
	if hit == nil {
		t.Fatalf("no SEM3139 reported")
	}
	if hit.Message != taskCloneBorrowMessage {
		t.Fatalf("message: got %q", hit.Message)
	}
	c.assertSpan(t, "primary", hit.Primary, row.primary)
	if len(hit.Notes) != len(row.notes) {
		t.Fatalf("notes: got %d, want %d: %v", len(hit.Notes), len(row.notes), hit.Notes)
	}
	messages := []string{taskCloneBorrowNote, taskCloneCloneNote}
	for i, want := range row.notes {
		c.assertSpan(t, fmt.Sprintf("note %d", i), hit.Notes[i].Span, want)
		if hit.Notes[i].Msg != messages[i] {
			t.Fatalf("note %d: got %q, want %q", i, hit.Notes[i].Msg, messages[i])
		}
	}
	if len(hit.Help) != 1 || !strings.HasPrefix(hit.Help[0].Msg, taskCloneBorrowHelp) {
		t.Fatalf("help: got %v, want one starting %q", hit.Help, taskCloneBorrowHelp)
	}
	if row.warning == "" {
		return
	}
	for _, d := range c.diags {
		if d.Severity == diag.SevWarning && d.Code.ID() == row.warning {
			return
		}
	}
	t.Fatalf("warning %s not reported", row.warning)
}

var (
	borrowAt59 = taskCloneSpan{59, 61, "&l"}
	borrowAt65 = taskCloneSpan{65, 67, "&l"}
	borrowAt71 = taskCloneSpan{71, 73, "&l"}
	borrowAt75 = taskCloneSpan{75, 77, "&l"}
	oneSEM3139 = map[string]int{"SEM3139": 1}
)

func TestTaskCloneCarriesItsOriginalsSpawnBorrows(t *testing.T) {
	rows := []taskCloneRefusal{
		{name: "U1_clone_after_hand_off",
			body:    `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); consume(t); return c; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{102, 103, "c"},
			notes:   []taskCloneSpan{borrowAt59, {72, 81, "t.clone()"}}},
		{name: "U2_clone_of_clone",
			body:    `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c1 = t.clone(); let c2 = c1.clone(); consume(t); consume(c1); return c2; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{137, 139, "c2"},
			notes:   []taskCloneSpan{borrowAt59, {93, 103, "c1.clone()"}}},
		{name: "U3_clone_through_ret_block",
			body:    `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); return { let c = t.clone(); consume(t); ret c; }; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{108, 109, "c"},
			notes:   []taskCloneSpan{borrowAt59, {81, 90, "t.clone()"}}},
		{name: "U4_clone_returned_in_place",
			body:    `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); return t.clone(); }`,
			errors:  map[string]int{"SEM3107": 1, "SEM3139": 1},
			primary: taskCloneSpan{71, 80, "t.clone()"},
			notes:   []taskCloneSpan{borrowAt59}},
		{name: "U5_joined_on_one_branch_only",
			body:    `async fn f(cond: bool) -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); if (cond) { let _ = t.await(); } else { consume(t); } return c; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{160, 161, "c"},
			notes:   []taskCloneSpan{borrowAt75, {88, 97, "t.clone()"}}},
		// The arm's `ret` leaves before the join written after the `if`; the
		// second `ret c` is judged at its own exit, after that join, and passes.
		{name: "U6_handed_off_in_one_ret_arm",
			body:    `async fn f(cond: bool) -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); return { if (cond) { consume(t); ret c; } let _ = t.await(); ret c; }; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{136, 137, "c"},
			notes:   []taskCloneSpan{borrowAt75, {88, 97, "t.clone()"}}},
		{name: "U7_joined_only_in_and_right_operand",
			body:    `async fn f(cond: bool) -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); let _ = cond && done(t.await()); return c; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{139, 140, "c"},
			notes:   []taskCloneSpan{borrowAt75, {88, 97, "t.clone()"}}},
		{name: "U7b_joined_only_in_or_right_operand",
			body:    `async fn f(cond: bool) -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); let _ = cond || done(t.await()); return c; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{139, 140, "c"},
			notes:   []taskCloneSpan{borrowAt75, {88, 97, "t.clone()"}}},
		// The post clause has not run when the body first does. Moving `c` in
		// the body would be refused (SEM3130: the next iteration uses a moved
		// value), so the body returns a clone of `c`, which is its own handle.
		{name: "U8_joined_only_in_for_post",
			body:    `async fn f(n: int) -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); for (let mut i: int = 0; i < n; i = step(i, t.clone().await())) { return c.clone(); } let _ = c.await(); let _ = t.await(); return spawn plain(1); }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{168, 177, "c.clone()"},
			notes:   []taskCloneSpan{borrowAt71}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertTaskCloneRefusal(t, row) })
	}
}

// Guards that stay refused: the original handle keeps today's verdict (K3), a
// join through a clone does not relax the ungated original (K5), and a legacy
// block tail has no exit record, so it is judged ungated (K9) even though this
// one is sound; with `ret` it is K7 and accepted.
func TestTaskCloneBorrowGuardsStayRefused(t *testing.T) {
	rows := []taskCloneRefusal{
		{name: "K3_original_returned",
			body:    `fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); return t; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{71, 72, "t"},
			notes:   []taskCloneSpan{borrowAt59}},
		{name: "K5_original_returned_after_clone_joined",
			body:    `async fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); let _ = c.await(); return t; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{115, 116, "t"},
			notes:   []taskCloneSpan{borrowAt65}},
		{name: "K9_legacy_tail_fails_closed",
			body:    `async fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); return { let _ = t.await(); c; }; }`,
			errors:  oneSEM3139,
			primary: taskCloneSpan{117, 118, "c"},
			notes:   []taskCloneSpan{borrowAt65, {78, 87, "t.clone()"}},
			warning: "SEM3135"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertTaskCloneRefusal(t, row) })
	}
}

// Acceptance rows assert no error AND something only an analysed program has:
// the spawn's captured borrow is recorded as a stable place of `f` (or, for the
// borrow-free row, that nothing is, while U1 in the same prelude is refused).
func TestTaskCloneReturnedAfterJoinStaysLegal(t *testing.T) {
	rows := []struct {
		name   string
		body   string
		stable []string
	}{
		{"K1_joined_then_clone_returned", `async fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); let _ = t.await(); return c; }`, []string{"l"}},
		{"K2_borrow_free_clone_returned", `fn f() -> Task<int> { let t = spawn work(); let c = t.clone(); consume(t); return c; }`, nil},
		{"K6_middle_clone_joined", `async fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c1 = t.clone(); let c2 = c1.clone(); consume(t); let _ = c1.await(); return c2; }`, []string{"l"}},
		{"K7_joined_before_ret", `async fn f() -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); return { let _ = t.await(); ret c; }; }`, []string{"l"}},
		{"K8_joined_in_left_operand", `async fn f(cond: bool) -> Task<int> { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); let _ = done(t.await()) && cond; return c; }`, []string{"l"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			c := checkTaskCloneBorrow(t, row.body)
			if got := c.errorCodes(); len(got) != 0 {
				t.Fatalf("expected a clean program, got %v", got)
			}
			if got := c.stable["f"]; strings.Join(got, ",") != strings.Join(row.stable, ",") {
				t.Fatalf("stable places of f: got %v, want %v", got, row.stable)
			}
		})
	}
}

// A clone that leaves by a non-Task exit is refused by the pin return edge
// exactly as its original is; nothing about the clone changes that verdict.
func TestTaskCloneBorrowParityOnNonTaskExits(t *testing.T) {
	rows := []struct {
		name, original, clone, mustHave string
	}{
		{"P1_struct_field",
			`type Holder = { t: Task<int> }; fn f() -> Holder { let l: int = 5; let t = spawn worker(&l); return Holder { t: t }; }`,
			`type Holder = { t: Task<int> }; fn f() -> Holder { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); consume(t); return Holder { t: c }; }`,
			"SEM3021"},
		{"P2_handed_away_then_value_returned",
			`fn f() -> int { let l: int = 5; let t = spawn worker(&l); consume(t); return 0; }`,
			`fn f() -> int { let l: int = 5; let t = spawn worker(&l); let c = t.clone(); consume(c); consume(t); return 0; }`,
			"SEM3021"},
		{"P3_callers_reference",
			`fn f(p: &int) -> Task<int> { let t = spawn worker(p); return t; }`,
			`fn f(p: &int) -> Task<int> { let t = spawn worker(p); let c = t.clone(); consume(t); return c; }`,
			""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			original := checkTaskCloneBorrow(t, row.original).errorCodes()
			clone := checkTaskCloneBorrow(t, row.clone).errorCodes()
			if !equalCodeCounts(clone, original) {
				t.Fatalf("clone %v, original %v", clone, original)
			}
			if row.mustHave != "" && original[row.mustHave] == 0 {
				t.Fatalf("original %v lacks %s", original, row.mustHave)
			}
		})
	}
}
