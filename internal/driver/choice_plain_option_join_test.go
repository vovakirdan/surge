package driver

import (
	"testing"

	"surge/internal/diag"
)

type choiceJoinRow struct{ name, text, snippet string }

func choiceJoinAccepted(t *testing.T, rows []choiceJoinRow) {
	t.Helper()
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { bytesViewAccepted(t, row.text) })
	}
}

func choiceJoinRefused(t *testing.T, code diag.Code, rows []choiceJoinRow) {
	t.Helper()
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { bytesViewRefusal(t, row.text, code, row.snippet) })
	}
}

// A choice joining a plain value with `nothing` is an Option (owner ruling
// 2026-09-29): each row reads the choice's value as an `Option<int>` through
// its `Some` and `nothing` cases, which the checker refused while the ternary
// was typed `int`.
func TestChoiceOfAPlainValueAndNothingIsAnOption(t *testing.T) {
	choiceJoinAccepted(t, []choiceJoinRow{
		{name: "ternary_value_first", text: `fn pick(c: bool) -> int {
    let e = c ? 5 : nothing;
    return compare e { Some(v) => v; nothing => 0; };
}
`},
		{name: "ternary_nothing_first", text: `fn pick(c: bool) -> int {
    let e = c ? nothing : 5;
    return compare e { Some(v) => v; nothing => 0; };
}
`},
		{name: "nested_ternary", text: `fn pick(c: bool, d: bool) -> int {
    let e = c ? 4 : d ? 5 : nothing;
    return compare e { Some(v) => v; nothing => 0; };
}
`},
		{name: "compare_nothing_first", text: `fn pick(k: int) -> int {
    let e = compare k { 1 => nothing; _ => 5; };
    return compare e { Some(v) => v; nothing => 0; };
}
`},
		{name: "compare_arm_is_a_ternary", text: `fn pick(k: int, d: bool) -> int {
    let e = compare k { 1 => 4; _ => d ? 5 : nothing; };
    return compare e { Some(v) => v; nothing => 0; };
}
`},
		{name: "annotated_option_target", text: `fn pick(c: bool) -> Option<int> {
    let e: Option<int> = c ? 5 : nothing;
    return e;
}
`},
	})
}

// The Option a plain value and `nothing` join to is not what an `int` target
// holds, and no implicit conversion makes it so:
// an annotated binding and a return are refused at the `nothing` branch.
func TestChoiceOfAPlainValueAndNothingIntoAPlainTargetIsRefused(t *testing.T) {
	choiceJoinRefused(t, diag.SemaTypeMismatch, []choiceJoinRow{
		{name: "annotated_int_binding", snippet: "nothing;", text: `fn pick(c: bool) -> int {
    let r: int = c ? 5 : nothing;
    return r;
}
`},
		{name: "int_result", snippet: "nothing;", text: `fn pick(c: bool) -> int {
    return c ? 5 : nothing;
}
`},
	})
}

// A branch that leaves instead of answering has no value to join: the
// ternary keeps the other branch's type, so an `int` target still takes it.
func TestChoiceBranchThatLeavesJoinsNothing(t *testing.T) {
	choiceJoinAccepted(t, []choiceJoinRow{
		{name: "continue_and_break_branches", text: `fn f(v: int) -> int { return v + 1; }
fn walk() -> int {
    let mut i = 0;
    let mut t = 0;
    while i < 6 {
        i = i + 1;
        let v: int = i % 2 == 0 ? { continue; } : f(i);
        let w: int = i > 4 ? { break; } : v;
        t = t + w;
    }
    return t;
}
`},
	})
}

// A discarded compare keeps its old typing: no arm is wrapped in `Some`, so a
// plain arm and an Option arm do not join, in either order. Joining them left
// the plain arm unwrapped under an Option result (VM1003, a native segfault).
func TestDiscardedCompareOfAPlainAndAnOptionArmIsRefused(t *testing.T) {
	choiceJoinRefused(t, diag.SemaTypeMismatch, []choiceJoinRow{
		{name: "option_arm_first", snippet: "7; }", text: `fn f(k: int, o: int?) -> nothing {
    compare k { 1 => o; _ => 7; };
    return nothing;
}
`},
		{name: "plain_arm_first", snippet: "o; }", text: `fn f(k: int, o: int?) -> nothing {
    compare k { 1 => 5; _ => o; };
    return nothing;
}
`},
	})
}

// A target union with a plain case and `nothing` takes each branch directly,
// with no Option, and a literal branch under an Option target takes the
// target's payload: both are accepted where the join alone would refuse.
func TestChoiceIntoAUnionOrANarrowOptionTargetIsAccepted(t *testing.T) {
	choiceJoinAccepted(t, []choiceJoinRow{
		{name: "union_with_int_and_nothing", text: `type MaybeInt = int | nothing;
fn f(c: bool) -> MaybeInt {
    let x: MaybeInt = c ? 5 : nothing;
    return c ? 6 : nothing;
}
`},
		{name: "narrow_option_literal", text: `fn f(c: bool) -> int8? {
    let y: int8? = c ? 6 : nothing;
    return y;
}
`},
		{name: "narrow_option_nested", text: `fn f(c: bool, d: bool) -> int8? {
    return c ? 7 : d ? 8 : nothing;
}
`},
	})
}

// A union target without the plain value's type is refused at that value.
func TestChoiceIntoAUnionWithoutThePlainTypeIsRefused(t *testing.T) {
	choiceJoinRefused(t, diag.SemaTypeMismatch, []choiceJoinRow{
		{name: "string_union_int_value", snippet: "5 :", text: `type MaybeStr = string | nothing;
fn f(c: bool) -> MaybeStr {
    return c ? 5 : nothing;
}
`},
	})
}

// A plain arm beside a `Some<T>` arm and `nothing` joins into `Option<T>`,
// as the nested ternary does: the plain join and the union join compose.
func TestCompareOfSomeAPlainValueAndNothingIsAnOption(t *testing.T) {
	choiceJoinAccepted(t, []choiceJoinRow{
		{name: "some_arm_first", text: `fn pick(k: int) -> int {
    let e = compare k { 1 => Some(5); 2 => 6; _ => nothing; };
    return compare e { Some(v) => v; nothing => 0; };
}
`},
		{name: "plain_arm_first", text: `fn pick(k: int) -> int {
    let e = compare k { 1 => 7; 2 => Some(8); _ => nothing; };
    return compare e { Some(v) => v; nothing => 0; };
}
`},
	})
}

// A choice of a reference and `nothing` is an `Option<&T>`, legal as a local
// (owner ruling 2026-09-29) and refused as a tuple or array element like any
// reference at depth (SEM3138): the join's type goes through the deep check.
func TestChoiceOfAReferenceAndNothingInAnAggregateIsRefused(t *testing.T) {
	choiceJoinRefused(t, diag.SemaRefInAggregate, []choiceJoinRow{
		{name: "tuple_element", snippet: "c ? &x : nothing, 1", text: `fn f(c: bool) -> int {
    let x = 5;
    let t = (c ? &x : nothing, 1);
    return 0;
}
`},
		{name: "tuple_of_a_bound_choice", snippet: "o, 1)", text: `fn f(c: bool) -> int {
    let x = 5;
    let o = c ? &x : nothing;
    let t = (o, 1);
    return 0;
}
`},
		{name: "array_element", snippet: "c ? &x : nothing]", text: `fn f(c: bool) -> int {
    let x = 5;
    let arr = [c ? &x : nothing];
    return 0;
}
`},
	})
}
