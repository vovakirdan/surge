package vm_test

import "testing"

const loopExitContinueInTernarySource = `fn f(v: int) -> int { return v + 4611686018427387904; }

@entrypoint
fn main() -> int {
    let i = 0;
    let total: int = 0;
    while i < 6 {
        i = i + 1;
        let skip: bool = i % 2 == 0;
        let v: int = skip ? { continue; } : f(i);
        total = total + v - 4611686018427387904;
    }
    if total != 9 { return 1; }
    print("ternary-continue-ok");
    return 0;
}
`

const loopExitBreakInTernarySource = `fn f(v: int) -> int { return v + 4611686018427387904; }

@entrypoint
fn main() -> int {
    let i = 0;
    let total: int = 0;
    while i < 6 {
        i = i + 1;
        let v: int = i == 4 ? { break; } : f(i);
        total = total + v - 4611686018427387904;
    }
    if total != 6 { return 1; }
    print("ternary-break-ok");
    return 0;
}
`

const loopExitReturnInTernarySource = `fn g(v: float) -> float { return v + 0.5; }

fn pick(i: int) -> int {
    let v: float = i == 3 ? { return 7; } : g(1.5);
    if v != 2.0 { return 1; }
    return 0;
}

@entrypoint
fn main() -> int {
    let i = 0;
    let bad = 0;
    while i < 5 {
        i = i + 1;
        if pick(i) == 1 { bad = bad + 1; }
    }
    if bad != 0 { return 1; }
    print("ternary-return-float-ok");
    return 0;
}
`

const loopExitContinueInSelectSource = `fn f(v: int) -> int { return v + 4611686018427387904; }

async fn run() -> int {
    let ch = Channel::<int>::new(1:uint);
    let i = 0;
    let total: int = 0;
    while i < 6 {
        i = i + 1;
        if i % 2 == 0 { ch.send(1); }
        let v: int = select {
            ch.recv() => { continue; };
            default => f(i);
        };
        total = total + v - 4611686018427387904;
    }
    if total != 9 { return 1; }
    print("select-continue-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// A choice's result slot is written by the branch that runs. A branch that
// leaves the loop or the function first must not release it, because on that
// path the slot holds nothing. With the release registered when the slot was
// born, break and continue panicked on the VM with VM3301 (use after free) and
// return with VM1001 (read before initialization).
func TestLoopExitBeforeChoiceWritesItsResult(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"continue_in_ternary", loopExitContinueInTernarySource, "ternary-continue-ok\n"},
		{"break_in_ternary", loopExitBreakInTernarySource, "ternary-break-ok\n"},
		{"return_in_ternary", loopExitReturnInTernarySource, "ternary-return-float-ok\n"},
		{"continue_in_select", loopExitContinueInSelectSource, "select-continue-ok\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runProgramFromSource(t, tc.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != tc.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, tc.want, res.stderr)
			}
		})
	}
}
