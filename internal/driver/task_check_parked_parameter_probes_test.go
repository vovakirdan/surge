package driver

import "testing"

// RV2-DEBT-365, R-b(call) (P1u-TC3). A task made by a plain call over a PARAMETER -- a reference, a
// value that carries one, or an array that may be a window into the caller's frame -- leaves its
// function only by `return`, a join or a drain: the caller's call is not Task-valued, so nothing the
// caller checks sees a task parked through `*out`, a container parameter or a copy. The controls are
// the sound programs those shapes could be mistaken for. ROOT programs, diagnosed through the public
// path by taskCheckErrorCodes.

var taskCheckParkedParameterTasks = []taskCheckProbe{
	{"rb_park_over_a_reference_parameter", "0efba601c64070ec101340705c45f31643372e0e1e4530ce7a81e187c43f9c27", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, out: &mut Task<int>) -> nothing {
    *out = worker(x);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_falls_off_its_end", "3277982c75530973bbc099c1d2a4d8f9490c2b31d75bdd5243a122ecfaca4dfd", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, out: &mut Task<int>) {
    *out = worker(x);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_through_a_let_copy", "b2ef3d1f8a97cc3c1d8caa46870d8eb272fb74a858a8f15fe188eea77c4a1451", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, out: &mut Task<int>) -> nothing {
    let r = x;
    *out = worker(r);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_over_a_reference_carrying_parameter", "c6a27b15a7d841a9fbaace3b5691d7be68f5a99a1a2dd94b3fdedbd33adda49b", "SEM3021", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn park(o: Option<&int>, out: &mut Task<int>) -> nothing {
    *out = peek(o);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_into_a_container_parameter", "61277721faf8373cb24cf7a6eb4c88ad663c7a868995d5c0d9ec7bda7cfe1c97", "SEM3021,SEM3107", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, out: &mut Task<int>[]) -> nothing {
    out.push(worker(x));
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_through_a_dereference", "7bdd6e33e0b8a322de2a38096cc028a9f7c1e4dc7d20a9f05bb8bfd5424add47", "SEM3021", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn park(p: &Option<&int>, out: &mut Task<int>) -> nothing {
    *out = peek(*p);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_a_bytes_view_parameter", "f4865048e10000ca026e4e0e6ed41551a06e9a5d488bf9892a27ea4710f6ec69", "SEM3021", `async fn count(v: BytesView) -> int {
    return 1;
}

fn park(v: BytesView, out: &mut Task<int>) -> nothing {
    *out = count(v);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_window_parameter", "e1d17406a4387bdff9a6c8e99afb79e89dced440b9ea50ddc33feac0210f9adc", "SEM3021", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn park2(xs: int64[], out: &mut Task<int64>) -> nothing {
    *out = first(xs);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rb_park_window_over_a_reference_parameter", "c0f79f5d991dae27bfb9c3ff47d3123b8f22e958e1c9721f47a36d06330c57aa", "SEM3021", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn park3(a: &int64[4], out: &mut Task<int64>) -> nothing {
    let mut v: int64[] = [];
    v = a[[0..2]];
    *out = first(v);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckParameterTaskControls = []taskCheckProbe{
	{"ctl_rb_parameter_task_returned_through_a_let", "dd98ba7aecdec87674e14428108827f7801d230db17bbf206a92884c06db1525", "", `async fn worker(x: &int) -> int {
    return *x;
}

fn fwd(x: &int) -> Task<int> {
    let r = x;
    return worker(r);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_rb_parameter_task_awaited", "7270f83c04e8e2c9c6499b72300ce9164c5104e99a3465f5836c96a65631ee84", "", `async fn worker(x: &int) -> int {
    return *x;
}

async fn run(x: &int) -> int {
    let t = worker(x);
    return compare t.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_rb_parameter_tasks_drained", "5bd9cf9f88723504ed6122750b066369a2661b49d545cbf311a4ebb89a6bd302", "", `async fn worker(x: &int) -> int {
    return *x;
}

async fn run(x: &int) -> int {
    let mut tasks: Task<int>[] = [];
    tasks.push(worker(x));
    tasks.push(worker(x));
    let z: int = 0;
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
	{"ctl_rb_reference_carrying_parameter_returned", "c6fc86ebf3c4bb78bbf3c9956c7831148c9387b94da3c858f73944c1004af237", "", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn fwd(o: Option<&int>) -> Task<int> {
    return peek(o);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_rb_parameter_copied_into_an_owned_task", "b7efd2ad7fc201ee6a394d6b766241a3bce416da70906b29e3db3bf32e1f180f", "", `async fn owned(v: int) -> int {
    return v;
}

fn detached(x: &int) -> Task<int> {
    return owned(x);
}

fn sound() -> Task<int> {
    let l: int = 5;
    let t = detached(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

func TestTaskCheckRefusesParkedParameterTasks(t *testing.T) {
	for _, probe := range taskCheckParkedParameterTasks {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a task over a parameter was parked (RV2-DEBT-365, P1u-TC3)", got, probe.want)
			}
		})
	}
}

func TestTaskCheckKeepsParameterTaskPrograms(t *testing.T) {
	for _, probe := range taskCheckParameterTaskControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
