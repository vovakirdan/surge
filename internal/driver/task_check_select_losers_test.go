package driver

import "testing"

// RV2-DEBT-365, N-TASK-20/21. Once `select` and `race` have an origin transfer, an arm is a runner, and what a task
// named in a head borrows is the task check's alone. A head's join releases pins only in its own arm; a race's
// cancel and a timeout release none (task_select_arm_joins.go, type_expr_select.go). A loser left running reaches
// the scope's implicit join, which runs after the frame's locals are released (docs/RUNTIME.md 3.1), so every
// exit must refuse its live pin; each row names WHICH edge refused it by the message and the borrow it points at.
// ROOT programs, diagnosed through the public path by taskCheckErrorCodes; the rows reuse N-TASK-16's
// implicitJoinProbe and implicitJoinEdge.

var taskCheckSelectArmLosers = []implicitJoinProbe{
	{taskCheckProbe{"sj_select_default_loser_left_to_the_join", "72bd2468d9ae934fbe826d5f3f3ba993323b14c20e7ec40493d969b7e42eb299", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { sworker(&bl).await() => 1; default => 2; };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"sj_race_timer_loser_left_to_the_join", "4fb285e6341f2a7b61b2812ecce7e8420b940799873ea7f395650dfc0d5ee26c", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = race { sworker(&bl).await() => 1; sleep(1).await() => 2; };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"sj_race_loser_reads_before_its_first_suspension", "4ecc3e3330ae3842473837cfcd0c328b34a775b7e750496a3e2a910a1d2513f7", "SEM3021", `async fn rworker(x: &string) -> int {
    print("worker read " + x);
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = race { rworker(&bl).await() => 1; default => 2; };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"sj_race_cold_loser_after_the_winner", "32ab58a0d4a12b6797efd43a2732bdf6301c30027d10d78d900cf977544b699c", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let ch = Channel::<int>::new(1:uint);
        ch.send(5);
        let v = race { ch.recv() => 1; sworker(&bl).await() => 2; };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"sj_timeout_head_never_joins", "39ef5666da525f88ff936d8f12d9991faec2d025b245098471da5e5d0d78349e", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let t = sworker(&bl);
        let v = select { timeout(t, 5) => 3; default => 4; };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"sj_timeout_only_arm", "a43d5400519b363651e2655989a7f5c3de81e51cc95ba809f52e0e1d3b0c1ab5", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let t = sworker(&bl);
        let v = select { timeout(t, 5) => 3; };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"sj_send_task_through_a_select", "a9b8a04891c3d21ac5eaebdecf400d73bfa24e7457c7e4bf4fa1fd2aeae5ce6e", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn f(ch: Channel<Task<int>>) -> int {
    let mut l: string = "abc";
    l = l + "def";
    let t = worker(&l);
    return select {
        ch.send(own t) => 1;
        default => 2;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`}, "a task still borrows 'l' at this return", "&l"},
	{taskCheckProbe{"sj_nested_block_loser", "1e21d93fdaf977f5ca839a3a9bbbeaf9f079ad3271f988165a308d718f6c429e", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        {
            let mut bl: string = "abc";
            bl = bl + "def";
            let v = select { sworker(&bl).await() => 1; default => 2; };
        }
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`}, "a task still borrows 'bl' at the end of the block that declares it", "&bl"},
}

var taskCheckSelectArmLoserControls = []taskCheckProbe{
	{"sj_ctl_only_arm_joins", "aa31254ba964fb200a3af01b8bf58f3f7044991aa4a4c459f12ac5c07e24aba9", "", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { sworker(&bl).await() => 3; };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`},
	{"sj_ctl_loser_joined_through_its_clone", "f27a42cd92d2872571809f46e349ce91258f1b1864a10536c7eac3aa1ebe4e3f", "", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let t = sworker(&bl);
        let t2 = t.clone();
        let v = select { t.await() => 3; sleep(1).await() => 4; };
        let _ = t2.await();
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`},
	{"sj_ctl_bound_task_awaited_again", "e1ff6515e455296fe5943c724ee408168a69e69f64080f464356d98692f3fd08", "", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let t = sworker(&bl);
        let v = select { t.await() => 3; default => 4; };
        let _ = t.await();
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`},
}

// 9 RUN: 1 parent, 8 leaves.
func TestTaskCheckRefusesSelectArmLosers(t *testing.T) {
	for _, row := range taskCheckSelectArmLosers {
		t.Run(row.probe.name, func(t *testing.T) {
			got, errs := taskCheckErrorCodes(t, row.probe)
			if got != row.probe.want {
				t.Fatalf("error codes %q, want %q: a select or race loser left running is not refused (RV2-DEBT-365, N-TASK-20/21)", got, row.probe.want)
			}
			if row.message == "" {
				return
			}
			if seen := implicitJoinEdge(row, errs); seen != "" {
				t.Fatalf("refusal edge %q, want %q at %q: another edge refused the loser", seen, row.message, row.at)
			}
		})
	}
}

// 4 RUN: 1 parent, 3 leaves. A control passes on an unfinished program too (taskCheckErrorCodes tolerates one), so each
// is also run: E-ONLY-ARM, E-RACE-CLONE and the golden t25_select_task_vs_timer.
func TestTaskCheckKeepsSelectArmLosers(t *testing.T) {
	for _, probe := range taskCheckSelectArmLoserControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
