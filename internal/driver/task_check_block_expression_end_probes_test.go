package driver

import "testing"

// RV2-DEBT-365, R-f (P1u-TC3). A block expression and a compare arm are scopes: a binding they declare
// dies at their end, so a task still holding it there -- its handle parked in an outer container and
// drained after -- is refused, as at the end of a statement block. An arm binding out of a borrowed
// subject names the owner's storage and is spared. ROOT programs, diagnosed by taskCheckErrorCodes.

var taskCheckBlockExpressionEndPins = []taskCheckProbe{
	{"rf_value_block_end_then_drained", "2a0c753280f87ad45287ee6e3cad1aac68997c6b841eb0410364ea5b90ade6c8", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn leak() -> int {
    let mut tasks: Task<int>[] = [];
    let z: int = {
        let l: string = "abcdef";
        tasks.push(worker(&l));
        ret 0;
    };
    let mut total: int = z;
    while tasks.__len() > 0:uint {
        let t = tasks.pop().safe();
        total = total + compare t.await() { Success(v) => v; Cancelled() => 0; };
    }
    return total;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rf_compare_arm_body_end_then_drained", "1c793f14314cc8eae7a4a9e9dae6a3e43989c20a4a64b46b1c65e01fdbada813", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn leak() -> int {
    let mut tasks: Task<int>[] = [];
    let flag: bool = true;
    let z: int = compare flag {
        true => {
            let l: string = "abcdef";
            tasks.push(worker(&l));
            ret 0;
        };
        false => 0;
    };
    let mut total: int = z;
    while tasks.__len() > 0:uint {
        let t = tasks.pop().safe();
        total = total + compare t.await() { Success(v) => v; Cancelled() => 0; };
    }
    return total;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rf_compare_arm_binding_end_then_drained", "7807e9a038151db2a55b63c1eb74e73e1a818c00a20982c1f8b995ff61082917", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn make() -> Option<string> {
    return Some("abcdef");
}

async fn leak() -> int {
    let mut tasks: Task<int>[] = [];
    let z: int = compare make() {
        Some(s) => {
            tasks.push(worker(&s));
            ret 0;
        };
        nothing => 0;
    };
    let mut total: int = z;
    while tasks.__len() > 0:uint {
        let t = tasks.pop().safe();
        total = total + compare t.await() { Success(v) => v; Cancelled() => 0; };
    }
    return total;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckBlockExpressionControls = []taskCheckProbe{
	{"ctl_rf_value_block_joins_before_its_end", "81026a1ea7ba9b6b472a712ee09a3bb785a3d7502255a227afe432cceb76cfcd", "", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn sound() -> int {
    let z: int = {
        let l: string = "abcdef";
        let t = worker(&l);
        ret compare t.await() { Success(v) => v; Cancelled() => 0; };
    };
    return z;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_rf_arm_binding_of_a_borrowed_subject_drained", "0ff88e7107de2eb1117872894c9dc7faa7ba52863c2e2aaeb7e3269b4bbf697e", "", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn make() -> Option<string> {
    return Some("abcdef");
}

async fn sound() -> int {
    let owned: Option<string> = make();
    let r: &Option<string> = &owned;
    let mut tasks: Task<int>[] = [];
    let z: int = compare *r {
        Some(s) => {
            tasks.push(worker(&s));
            ret 0;
        };
        nothing => 0;
    };
    let mut total: int = z;
    while tasks.__len() > 0:uint {
        let t = tasks.pop().safe();
        total = total + compare t.await() { Success(v) => v; Cancelled() => 0; };
    }
    return total;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

func TestTaskCheckRefusesBlockExpressionEndPins(t *testing.T) {
	for _, probe := range taskCheckBlockExpressionEndPins {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a task outlived the block that declares what it borrows (RV2-DEBT-365, P1u-TC3)", got, probe.want)
			}
		})
	}
}

func TestTaskCheckKeepsBlockExpressionPrograms(t *testing.T) {
	for _, probe := range taskCheckBlockExpressionControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
