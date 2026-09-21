package driver

// Frozen programs the task check must refuse. The p1u-* and run-* texts are the measured probe
// files byte for byte (the two receiver probes with their `len(&c.s)` fixture defect repaired);
// each digest is checked before use, so a changed byte is a different program.
var taskCheckLeakProbes = []taskCheckProbe{
	{"a1_bound_async_call", "54989243adfc603243cebe7be52b7f590b243510e72a5c778cea9303c72c85e5", "SEM3139", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"a5_callable_value_borrow", "e11e1f20571b1ac31f1d751eb878e57790da26e8fc0907fb1a0e0694cef47fa1", "SEM3139", `async fn worker(x: &int64) -> int64 {
    return *x;
}

type AsyncRef = async fn(&int64) -> int64;

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let f: AsyncRef = worker;
    let t = f(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"a6_async_block_param_capture", "1862fa0cc8eced7e65bf27ad81685b3213ddc627e6704d42e7ad8862e20ae2c4", "SEM3139", `fn start(p: &int64) -> Task<int64> {
    return async {
        ret *p;
    };
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = start(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"a7_plain_call_into_channel", "adfd7dbc04e1f020cb261292eeff1af04fa906d4d43bf52882a634e04c6b0f32", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak(ch: Channel<Task<int64>>) -> int {
    let l: int64 = 5;
    let t = worker(&l);
    ch.send(own t);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"b1_pass_concrete", "8c02aff1fe03918b2bbb4b38e72e1d266df3d66cbaef2f255557b42d5982979a", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn pass(t: Task<int64>) -> Task<int64> {
    return t;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = spawn worker(&l);
    return pass(t);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"b2_pass_generic", "04cd18fcf0f2f0c075bd62d1fa7daa78adeea28b54e45aabe7fe411bf412eae4", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn pass<T>(x: T) -> T {
    return x;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = spawn worker(&l);
    return pass::<Task<int64>>(t);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"b3_spawn_captures_handle", "1a09b6f6348eb3c5529ab30419e3164a82e561b5e8d67e06e82ecc2059e4ca6d", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

async fn waiter(t: Task<int64>) -> int64 {
    return compare t.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = spawn worker(&l);
    return spawn waiter(t);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"b4_channel_then_task_return", "7a73a8b667b76736c626a4899dbf84efb6a45c43c1f272e9131aa16df6ea6dae", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

async fn plain(x: int64) -> int64 {
    return x;
}

fn leak(ch: Channel<Task<int64>>) -> Task<int64> {
    let l: int64 = 5;
    let t = spawn worker(&l);
    ch.send(own t);
    return spawn plain(1);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"c1_async_body_ret_handle", "831a199683070b995a7bee97db4ce9baef680c672e48926ecf5d1cc55e450ff5", "SEM3139", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak() -> Task<Task<int64>> {
    return async {
        let bl: int64 = 5;
        let t = spawn worker(&bl);
        ret t;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"c4_async_body_hand_off_channel", "4cc2f9cad9485e3bd45809acc269f7416c87d827bf6fb1520f2fa055860f80cd", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak(ch: Channel<Task<int64>>) -> Task<int> {
    return async {
        let bl: int64 = 5;
        let t = spawn worker(&bl);
        ch.send(own t);
        ret 0;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"c10_async_capture_borrowing_task", "6dda2c6acb96b4b71fca71340577c054645f70215b8995bf9e4abb22b31bbf44", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = spawn worker(&l);
    return async {
        ret compare t.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ro2_receiver_direct", "cee4615b7bf4d504e032ad774f1fb409dcdeec6eb320cec51ee032594e4c7cac", "SEM3139", `type Cell = { s: string };

async fn cell_len(c: &Cell) -> int64 {
    return len(c.s) to int64;
}

extern<Cell> {
    pub fn size(self: &Cell) -> Task<int64> {
        return cell_len(self);
    }
}

fn leak() -> Task<int64> {
    let c: Cell = { s = "abcdef" };
    return c.size();
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ro3_receiver_bound", "05ec6acb6fb3b62989ef04da30697c8fb341cedb5371de1627f1ac96b17e9545", "SEM3139", `type Cell = { s: string };

async fn cell_len(c: &Cell) -> int64 {
    return len(c.s) to int64;
}

extern<Cell> {
    pub fn size(self: &Cell) -> Task<int64> {
        return cell_len(self);
    }
}

fn leak() -> Task<int64> {
    let c: Cell = { s = "abcdef" };
    let t = c.size();
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ro4_ternary_plain_calls", "0f980b2f3c47f548103937b4de74273850683da9fb3519a725936c8ada6aff91", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak(cond: bool) -> Task<int64> {
    let l: int64 = 5;
    return cond ? worker(&l) : worker(&l);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ro5_ret_block_plain_call", "e579481139882d0ebe729d111e0b86b3c3a0c6fa5043024445068fcd574c1041", "SEM3139", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    return {
        let t = worker(&l);
        ret t;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ro6_pass_plain_call", "194820d12ca4db5030d4b75bc0e5c7a1414539ea8d8ce375ab63e5e321c8be65", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn pass(t: Task<int64>) -> Task<int64> {
    return t;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = worker(&l);
    return pass(t);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ro7_mut_out_plain_call", "61d146d9cc210518f7d1e1d89549d81258f9380db1c708fbdb8c767d7b7f8fc2", "SEM3021", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak(out: &mut Task<int64>) -> nothing {
    let l: int64 = 5;
    *out = worker(&l);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"a12_spawn_of_plain_call_binding", "11962ab54d27bb0515aee484e6720849a63d468339823c396bb6c47ec4593649", "SEM3139", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = worker(&l);
    return spawn t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"c2p_blocking_body_plain_call", "ca502fcd8efe0bcc6f2abe5cf085467fad60d04fe97041b3209bcd14cca196ba", "SEM3139", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn leak() -> Task<Task<int64>> {
    return blocking {
        let bl: int64 = 5;
        let t = worker(&bl);
        ret t;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"run_a1_bound_async_call", "69bafea14ef44cc61164719c5846d0c0cbf1e38469f2159d4a0e7d1ad6534e78", "SEM3139", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    let t = leak();
    return compare t.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
`},
	{"run_b1_pass_concrete", "2569ba88eb435a4da2063eb365c99aa6ecc8078ba319b899bf986e53c62feade", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn pass(t: Task<int>) -> Task<int> {
    return t;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = spawn worker(&l);
    return pass(t);
}

@entrypoint
fn main() -> int {
    let t = leak();
    return compare t.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
`},
}
