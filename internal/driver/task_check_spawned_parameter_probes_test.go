package driver

import "testing"

// RV2-DEBT-365, R-b(spawn) (P1u-TC4 R5). A task that `spawn` publishes over a PARAMETER -- a reference, a value
// that carries one, or an array that may be a window into the caller's frame -- leaves its function only by
// `return`, a join or a drain: the caller is shown no Task when the handle is sent into a channel, parked
// through `*out` or pushed into a container parameter. The controls are the sound programs those shapes could
// be mistaken for. ROOT programs, diagnosed through the public path by taskCheckErrorCodes.

var taskCheckParkedSpawnedParameterTasks = []taskCheckProbe{
	{"rbs_spawned_over_a_reference_parameter_sent", "19f3aa153a6e4eee60ae5ac6bce04268aebe6706bab60d90c8d1431bcec9712d", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, ch: Channel<Task<int>>) -> nothing {
    let t = spawn worker(x);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_over_a_reference_parameter_falls_off_its_end", "0bca8dcdfd064da43764c4d52d277e0ae6f499c910640dac87e232b3c4e219df", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, ch: Channel<Task<int>>) {
    let t = spawn worker(x);
    ch.send(own t);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_through_a_let_copy_sent", "0390e2a918f1351a7b37915a2034d0f78cfa78255b0566e2139085419277514c", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, ch: Channel<Task<int>>) -> nothing {
    let r = x;
    let t = spawn worker(r);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_over_a_reference_carrying_parameter_sent", "6c06d450a564fdd46d4921a46d9a7fd44bcc3b74e33c5618fab0d5ad559a23db", "SEM3021", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn park(o: Option<&int>, ch: Channel<Task<int>>) -> nothing {
    let t = spawn peek(o);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_over_a_bytes_view_parameter_sent", "deeb0c647dd3505ac4adabfb48971a95579d1b8060b592fcbea8c73310701097", "SEM3021", `async fn count(v: BytesView) -> int {
    return 1;
}

fn park(v: BytesView, ch: Channel<Task<int>>) -> nothing {
    let t = spawn count(v);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_over_a_window_parameter_sent", "339c553e40d717e08d0bc4fa47f3eac753cdf0709129e85b357eb3d7a3d9ac1d", "SEM3021", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn park2(xs: int64[], ch: Channel<Task<int64>>) -> nothing {
    let t = spawn first(xs);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_parked_through_a_dereference", "f1389f5cf91fc859e4a5bf81da010047e410c34a08aed63f9d5a551d4f275f4c", "SEM3021,SEM3107", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, out: &mut Task<int>) -> nothing {
    *out = spawn worker(x);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_parked_into_a_container_parameter", "f6cfc964b1b138fe6f560f165231909cd305630ee0ab1060db65c0ec984ddd30", "SEM3021,SEM3107", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, out: &mut Task<int>[]) -> nothing {
    out.push(spawn worker(x));
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_spawned_forwarder_over_a_parameter_sent", "e578acdf2b253f4e6e2e8bbb033e7b8524b0a50cc14a2d9862704ce5924c4355", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn fwd(x: &int) -> Task<int> {
    return worker(x);
}

fn park(x: &int, ch: Channel<Task<int>>) -> nothing {
    let t = spawn fwd(x);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rbs_cold_task_spawned_by_name_sent", "d8215ce063dc6b997e72dd8ccc2ffa870d4ba7933b77c10b82df5aeb6e520061", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

fn park(x: &int, ch: Channel<Task<int>>) -> nothing {
    let t = worker(x);
    let s = spawn t;
    ch.send(own s);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckSpawnedParameterTaskControls = []taskCheckProbe{
	{"ctl_rbs_spawned_over_a_parameter_returned", "199ce53a89c3affa3c921b93d018f41a09906bb7079bd01ba45bd74c628b5021", "", `async fn worker(x: &int) -> int {
    return *x;
}

fn fwd(x: &int) -> Task<int> {
    return spawn worker(x);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_rbs_spawned_over_a_parameter_bound_and_returned", "2de7fed40d22c5e4997a5316048e3044c37845bd0f044167f98797fd419f5556", "", `async fn worker(x: &int) -> int {
    return *x;
}

fn fwd(x: &int) -> Task<int> {
    let t = spawn worker(x);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_rbs_spawned_over_a_parameter_awaited", "9a3419f94f1af9ea77cd27427fadd83bee20fda3b27f4a04816fcb5169560560", "", `async fn worker(x: &int) -> int {
    return *x;
}

async fn run(x: &int) -> int {
    let t = spawn worker(x);
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
	{"ctl_rbs_spawned_over_a_parameter_drained", "d26b291d9fcd0537421ba4cf6a698a5a1ad759607e004dcb5054cc4791ac2a8d", "", `async fn worker(x: &int) -> int {
    return *x;
}

async fn run(x: &int) -> int {
    let mut tasks: Task<int>[] = [];
    tasks.push(spawn worker(x));
    tasks.push(spawn worker(x));
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
	{"ctl_rbs_spawned_owned_value_sent", "55db7bbc8f0a17d60217ff8eca0af7115c14f0fc9adce4f8daae0f84e1ad7611", "", `async fn plain(n: int) -> int {
    return n;
}

fn park(n: int, ch: Channel<Task<int>>) -> nothing {
    let t = spawn plain(n);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_rbs_spawned_task_its_callee_records_as_owned", "10d9f690e2b7eb50473bb2822b88552e6c9eacafd8941b5f972abe336c75cec8", "", `async fn owned(v: int) -> int {
    return v;
}

fn detached(x: &int) -> Task<int> {
    return owned(*x);
}

fn park(x: &int, ch: Channel<Task<int>>) -> nothing {
    let t = spawn detached(x);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

// 11 RUN: 1 parent, 10 leaves.
func TestTaskCheckRefusesParkedSpawnedParameterTasks(t *testing.T) {
	for _, probe := range taskCheckParkedSpawnedParameterTasks {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a spawned task over a parameter was parked (RV2-DEBT-365, P1u-TC4)", got, probe.want)
			}
		})
	}
}

// 7 RUN: 1 parent, 6 leaves.
func TestTaskCheckKeepsSpawnedParameterTaskPrograms(t *testing.T) {
	for _, probe := range taskCheckSpawnedParameterTaskControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
