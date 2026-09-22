package sema

import "testing"

// RV2-DEBT-365, R-g (P1u-TC2). Every arm head of a select or race runs before the select does, so a
// task a head creates is live whichever arm wins; only the arm taken has joined the task its own
// head awaits.
func TestSelectArmHeadTasksOutliveTheirLosingArms(t *testing.T) {
	rows := []struct {
		name, body, want string
	}{
		{"rg_select_head_spawn_outlives_its_losing_arm", `async fn f() -> int { let l: int = 5; let other = spawn plain(1); let r = select { (spawn worker(&l)).await() => 1; other.await() => 2; }; return r; }`, "SEM3021"},
		{"rg_race_head_call_outlives_its_losing_arm", `async fn f() -> int { let l: int = 5; let other = spawn plain(1); let r = race { worker(&l).await() => 1; other.await() => 2; }; return r; }`, "SEM3021"},
		{"rg_head_spawn_pinned_when_its_arm_returns", `async fn f() -> int { let l: int = 5; let other = spawn plain(1); let r = select { (spawn worker(&l)).await() => { return 1; }; other.await() => 2; }; return r; }`, "SEM3021"},
		{"ctl_rg_head_spawn_in_the_only_arm", `async fn f() -> int { let l: int = 5; let r = select { (spawn worker(&l)).await() => 1; }; return r; }`, ""},
		{"ctl_rg_bound_loser_joined_through_its_clone", `async fn f() -> int { let l: int = 5; let t = spawn worker(&l); let t2 = t.clone(); let u = spawn plain(1); let r = select { t.await() => 1; u.await() => 2; }; let _ = t2.await(); return r; }`, ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got, _ := taskCallEscapeCodes(t, row.body); got != row.want {
				t.Fatalf("error codes %q, want %q", got, row.want)
			}
		})
	}
}
