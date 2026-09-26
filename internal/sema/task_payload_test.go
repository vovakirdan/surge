package sema

import "testing"

func TestTaskPayloadMayNotContainTask(t *testing.T) {
	rows := []struct {
		name, source string
		want         int
	}{
		{"async_block", `fn f() -> Task<int> { return async { let t = async { ret 7; }; ret t; }.await(); }`, 1},
		{"async_fn", `async fn f() -> Task<int> { return async { ret 7; }; }`, 1},
		{"type_direct", `fn f(t: Task<Task<int>>) -> int { return 0; }`, 1},
		{"type_option", `type Option<T> = T | nothing; fn f(t: Task<Option<Task<int>>>) -> int { return 0; }`, 1},
		{"type_tuple", `fn f(t: Task<(int, Task<int>)>) -> int { return 0; }`, 1},
		{"type_array", `fn f(t: Task<Task<int>[]>) -> int { return 0; }`, 1},
		{"type_struct", `type Box = { child: Task<int> }; fn f(t: Task<Box>) -> int { return 0; }`, 1},
		{"control_int", `fn f(t: Task<int>) -> int { return 0; }`, 0},
		{"control_struct", `type Box = { value: int }; fn f(t: Task<Box>) -> int { return 0; }`, 0},
		{"control_channel", `fn f(ch: Channel<Task<int>>) -> int { return 0; }`, 0},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			check := checkTaskCloneBorrow(t, row.source)
			if got := check.errorCodes()["SEM3223"]; got != row.want {
				t.Fatalf("SEM3223 count = %d, want %d (all errors: %v)", got, row.want, check.errorCodes())
			}
		})
	}
}
