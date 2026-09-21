package driver

// Frozen programs the task check must keep accepting: each is sound, and each sits next to a
// refused shape it could be mistaken for.
var taskCheckControlProbes = []taskCheckProbe{
	{"ctl_c5_spawn_async_body_borrow", "4e8b70b8397af430ed8dcda83b9951c4aafddeb21e2d8e03eb377d34907f6300", "", `fn read(x: &int64) -> int64 {
    return *x;
}

fn leak() -> Task<int64> {
    let l: int64 = 5;
    let t = spawn async {
        ret read(&l);
    };
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_a14_discarded_plain_call", "f9dca7a5141b888ecf83b476b6c5e86199a74a3f40befe6755646d342aac6c75", "", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn ok() -> int64 {
    let l: int64 = 5;
    worker(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_a15_param_forwarding", "5854fe00ba19d838ed468437e5034f503c5cb7a506678fd717630083f589e1c1", "", `async fn worker(x: &int64) -> int64 {
    return *x;
}

fn fwd(p: &int64) -> Task<int64> {
    return worker(p);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_d1_borrow_free_spawn_return", "430f4cadcf43feb06b1e7fa2b234aab08cf0b925d381b90ff36832051318536f", "", `async fn plain(x: int64) -> int64 {
    return x;
}

fn ok(v: int64) -> Task<int64> {
    return spawn plain(v);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_d5_spawn_borrow_joined", "c3ecaf6c0166bb5fc8a53ef56b60c5e802cd5749ac4fa9c2129c8337eee35fa1", "", `async fn worker(x: &int64) -> int64 {
    return *x;
}

async fn ok() -> int64 {
    let l: int64 = 5;
    let t = spawn worker(&l);
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
	{"ctl_m1_view_awaited", "0bad17f4a7f6ef7af6122f185821375a67dbba751e231dc77ec0b61cfb0ec1cc", "", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

async fn sound() -> int64 {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let t = first(a[[0..2]]);
    return compare t.await() {
        Success(v) => v;
        Cancelled() => 0:int64;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_m1_dynamic_view_returned", "5350ab6356acaa398ff5a040cdc94d197be222b7966fd9bf7ce48414aba881e8", "", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn sound() -> Task<int64> {
    let d: int64[] = [1:int64, 2:int64, 3:int64, 4:int64];
    let t = first(d[[0..2]]);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_m3_fan_out_drained", "202a4a164c11bac7760dbbdc46de58343a4e144962a3cb7231e03e8d15ccfeef", "", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn sound() -> int {
    let l: string = "abcdef";
    let mut tasks: Task<int>[] = [];
    {
        tasks.push(spawn worker(&l));
        tasks.push(spawn worker(&l));
    }
    let mut total: int = 0;
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
	{"ctl_f1_carried_reference_awaited", "3fa57ca92ec81312d9a94a12c7d128a6bb33b3021d9677f852d71ea541d4fdca", "", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

async fn sound() -> int {
    let mut m = Map::<string, int>.new();
    let t = peek(m.get_ref(&"k"));
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
	{"ctl_blk_fixed_array_captured_by_value", "5089313da12374ce18d5922abb48066c1c139b688d06567240d6478ecc5557da", "", `fn sound() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    return blocking {
        let e: int64 = a[0];
        ret e;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_net_wrapper_under_timeout", "cddde4961ddaf12856d0dd2717aee04ceaae846a9be9f9a23069056f54852034", "", `import stdlib/net as net;

async fn read_budget(conn: TcpConn, budget: uint) -> int {
    let read_task = net.read_some(&conn, 16:uint);
    let read_res = timeout(read_task, budget);
    compare read_res {
        Success(net_res) => {
            let _ = net_res;
            return 0;
        }
        Cancelled() => {
            return 7;
        }
    };
    return 9;
}

async fn accept_loop(listener: TcpListener, budget: uint) -> int {
    let mut served: int = 0;
    while served < 2 {
        let accept_task = net.accept(&listener);
        let accept_res = timeout(accept_task, budget);
        compare accept_res {
            Success(net_res) => {
                let _ = net_res;
                served = served + 1;
            }
            Cancelled() => {
                return 7;
            }
        };
    }
    return served;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_lock_task_bound_and_dropped", "39bf752046bff03d0f07ce713efa8ba8d0ad5f4ea4344fc5514a0da690ed7091", "", `async fn cond_waiter(mtx: Mutex) -> int {
    let m = mtx;
    let lock_task = m.lock();
    lock_task.await();
    m.unlock();
    return 0;
}

fn drop_the_lock_task() -> int {
    let m = Mutex.new();
    let _ = m.lock();
    m.unlock();
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}
