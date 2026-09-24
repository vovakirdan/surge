package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// RV2-DEBT-365, N-TASK-16. Once `spawn` has an origin transfer, a spawned task is a runner, and what it borrows is
// the task check's alone. A member spawned in an `async { }` body and left to the body's implicit scope join meets
// no await rule (SEM3107 is exempted there) and runs after the body's locals are released (docs/RUNTIME.md 3.1),
// so the pin is its only fence: every exit of the body -- a `ret`, the body's end, a nested block's end -- must
// refuse a pin still live, and each row pins WHICH edge refused it by the message and the borrow it points at.
// ROOT programs, diagnosed through the public path by taskCheckErrorCodes.

// implicitJoinProbe is a refusal row that also names the edge: the message of one SEM3021 and the source text
// its primary span covers. An empty message asks only for the codes.
type implicitJoinProbe struct {
	probe       taskCheckProbe
	message, at string
}

var taskCheckImplicitJoinMembers = []implicitJoinProbe{
	{taskCheckProbe{"ij_member_left_to_the_join", "9ead30d44928817c79c954ba4412ed77ea8cb3803f766c23a33b4e2cdb8954e6", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let bl: string = "abc";
        let _s = spawn sworker(&bl);
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"ij_member_dropped", "08910888d9f3808ea8f162c99497d1a88862bee45791fd6d02d7cf061024eb72", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let bl: string = "abc";
        spawn sworker(&bl);
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"ij_member_at_the_body_end", "ca72b4c228d59ce37d1dfa59ebdd7ef1952ecaf737c1d397996c78edb9c58bfc", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let outer: string = "abc";
    let job: Task<nothing> = async {
        let _s = spawn sworker(&outer);
    };
    let r = job.await();
    return 0;
}
`}, "a task still borrows 'outer' at this async body end", "&outer"},
	{taskCheckProbe{"ij_member_early_ret", "026983d5a926fabb23065d6a154e0758ecae0b71b60b39e0e0c07b1e59f91195", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let c: bool = true;
    let r = (async {
        let bl: string = "abc";
        let _s = spawn sworker(&bl);
        if c {
            ret 9;
        }
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`}, "a task still borrows 'bl' at this ret", "&bl"},
	{taskCheckProbe{"ij_member_in_a_nested_block", "93706592817a11b927d83f0940891c28a074766571235d1468ed7569b4d86ad8", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        {
            let bl: string = "abc";
            let _s = spawn sworker(&bl);
        }
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`}, "a task still borrows 'bl' at the end of the block that declares it", "&bl"},
	{taskCheckProbe{"ij_member_through_a_let_reference", "05f008eab165c21f96d554eeb07e6423ac9cf6c512f9f051f862497a90a8b293", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let bl: string = "abc";
        let r = &bl;
        let _s = spawn sworker(r);
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`}, "", ""},
	{taskCheckProbe{"ij_member_through_a_bytes_view", "6bb675787d08c63bbb5fa55d7cd00b4936ba1c285100271166858b5f254e5da8", "SEM3021", `async fn vworker(v: BytesView) -> int {
    checkpoint().await();
    return 1;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let bl: string = "abc";
        let v = bl.bytes();
        let _s = spawn vworker(v);
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`}, "", ""},
	{taskCheckProbe{"ij_member_through_a_receiver", "58b6e599b39041f6411dfce779e65d3c6e5a7d79f24b307c8dc0a34d6a1305fe", "SEM3021", `type Box = { s: string }

extern<Box> {
    pub async fn size(self: &Box) -> int {
        checkpoint().await();
        return len(self.s) to int;
    }
}

@entrypoint
fn main() -> int {
    let r = (async {
        let bl: Box = Box { s = "abc" };
        let _s = spawn bl.size();
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`}, "", ""},
	{taskCheckProbe{"ij_member_in_an_async_fn", "4f525b346c2a3900ebe38b6f360e6f97bab4823d75461f09464e1563940e1792", "SEM3021,SEM3107", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn f() -> int {
    let bl: string = "abc";
    let _s = spawn sworker(&bl);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`}, "a task still borrows 'bl' at this return", "&bl"},
	{taskCheckProbe{"ij_captured_parameter_over_refusal", "371a3b3cf0a7153077a5e4ebc9eac4ebf076d7ebf5fafd2ad30702d8b701afee", "SEM3021", `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn f(x: &string) -> int {
    return compare (async {
        let _s = spawn sworker(x);
        ret 0;
    }).await() {
        Success(n) => n;
        Cancelled() => 1;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`}, "a task still borrows 'x' at this ret", "x"},
}

var taskCheckImplicitJoinControls = []taskCheckProbe{
	{"ctl_ij_member_borrowing_nothing", "27d8c3ff5e8ac9ba2552e52e77b1f8991b3602527cf254b3fabcb28802d97bae", "", `@entrypoint
fn main() -> int {
    let r = (async {
        let _s = spawn async {
            checkpoint().await();
            ret 0;
        };
        ret 0;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 1;
    };
}
`},
}

// implicitJoinEdge answers "" when one SEM3021 of the program carries the row's message at the row's text, and
// otherwise every SEM3021 it saw, as "message @ text".
func implicitJoinEdge(row implicitJoinProbe, errs []*diag.Diagnostic) string {
	var seen []string
	for _, d := range errs {
		if d.Code.ID() != "SEM3021" {
			continue
		}
		at := ""
		if int(d.Primary.End) <= len(row.probe.text) && d.Primary.Start <= d.Primary.End {
			at = row.probe.text[d.Primary.Start:d.Primary.End]
		}
		if d.Message == row.message && at == row.at {
			return ""
		}
		seen = append(seen, d.Message+" @ "+at)
	}
	return strings.Join(seen, "; ")
}

// 11 RUN: 1 parent, 10 leaves.
func TestTaskCheckRefusesImplicitJoinMembers(t *testing.T) {
	for _, row := range taskCheckImplicitJoinMembers {
		t.Run(row.probe.name, func(t *testing.T) {
			got, errs := taskCheckErrorCodes(t, row.probe)
			if got != row.probe.want {
				t.Fatalf("error codes %q, want %q: a member left to the implicit join is not refused (RV2-DEBT-365, N-TASK-16)", got, row.probe.want)
			}
			if row.message == "" {
				return
			}
			if seen := implicitJoinEdge(row, errs); seen != "" {
				t.Fatalf("refusal edge %q, want %q at %q: another edge refused the member", seen, row.message, row.at)
			}
		})
	}
}

// 2 RUN: 1 parent, 1 leaves.
func TestTaskCheckKeepsImplicitJoinMembers(t *testing.T) {
	for _, probe := range taskCheckImplicitJoinControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
