package driver

// Frozen programs of the dropped-task rows (task_dropped_test.go).

// Refused: each program is exactly SEM3218.
var taskDroppedRefusedProbes = []taskCheckProbe{
	{"dt_expr_stmt_plain_fn", "cc641e7e24259e55639a3a03a92dc9d93c94694436b299cfabd0b8af54160043", "SEM3218", `async fn work() -> int { return 1; }
fn f() -> int { work(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_let_underscore_plain_fn", "687d91abb7d7168a7c97e098afe62ff63022d590c981abbfe8aaaec8d09a6cba", "SEM3218", `async fn work() -> int { return 1; }
fn f() -> int { let _ = work(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_expr_stmt_async_fn", "d994c0eb88b4b1cd5c9590611fc26c3b3af6b1e2cb7a5fb98d08482e1643c948", "SEM3218", `async fn work() -> int { return 1; }
async fn f() -> int { work(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_expr_stmt_entrypoint", "ae949deea7ff2dc9019afcf55208cb8cbeac1117b95332ae992335466ed1acaa", "SEM3218", `async fn work() -> int { return 1; }
@entrypoint
fn main() -> int { work(); return 0; }
`},
	{"dt_in_async_block", "e5f313b986c74b35379ac55484bd4b142804ba51e7ebba7f17fb0391253c6a9b", "SEM3218", `async fn work() -> int { return 1; }
async fn f() -> int { let b = async { work(); ret 1; }; let _ = b.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_async_block_stmt", "f6fcc599d094de8ae4fad70016bb98f78931df2d5caf6a9dfe69752c33974796", "SEM3218", `async fn f() -> int { async { ret 1; }; return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_async_block_let_underscore", "3f207d4bea80f40e9e7a643ce353144fb008cddd185db7d4aa0ee71a206357a7", "SEM3218", `async fn f() -> int { let _ = async { ret 1; }; return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_ternary_branches", "3e524259c0d64bc366af5be6862770b0a0b36c3ff4aaa81cad219d14fa89557d", "SEM3218", `async fn work() -> int { return 1; }
fn f(c: bool) -> int { c ? work() : work(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_compare_arms", "e20a9c703abe1328b9bc607b2103d9845ac102c411abcddcbb8321019560a931", "SEM3218", `async fn work() -> int { return 1; }
fn f(o: Option<int>) -> int { compare o { Some(x) => work(); nothing => work(); }; return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_value_block_ret", "552ac77161376515d4f0f60e560a79cf3f902ca9d87988e5954444f818227ada", "SEM3218", `async fn work() -> int { return 1; }
fn f() -> int { let _ = { ret work(); }; return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_for_step", "a5ea9efbce3f2af4fb2489d646ef326d7016b2e92f2cd235bcca3515ab260a14", "SEM3218", `async fn work() -> int { return 1; }
fn f() -> int { for (let mut i: int = 0; i < 2; work()) { i = i + 1; } return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_lock_plain_fn", "4bd38364ecad202a05d10e214b0421586cc83466186d76c21ce66b14baadf5ae", "SEM3218", `fn f() -> int { let m = Mutex.new(); m.lock(); m.unlock(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_lock_async_fn", "5666e3b95ba6d6725f6f52bbd849a7a07409da994fc7a694daf1b8d48486d30a", "SEM3218", `async fn f() -> int { let m = Mutex.new(); m.lock(); m.unlock(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_acquire_async_fn", "150096e4030e8a936dd7ec8ff81d8c3eb127981f2f09f4ac54229d33e468195d", "SEM3218", `async fn f() -> int { let s = Semaphore.new(1:uint); s.acquire(); s.release(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_arrive_async_fn", "7836331f0fcf6257ffe01271a728d2d2edfd34d40fb583851a95ed4c70ca3853", "SEM3218", `async fn f() -> int { let b = Barrier.new(1:uint); b.arrive_and_wait(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_condition_wait_async_fn", "b988310280c958860abd77f4434275d470ee49bf578251189a6388bbbea66884", "SEM3218", `async fn f() -> int { let c = Condition.new(); let m = Mutex.new(); m.lock().await(); c.wait(&m); m.unlock(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_checkpoint", "6dcb8bff399420a10da6393c0e6c6b39bf3a56174206eef4c321bd3145fe389b", "SEM3218", `async fn f() -> int { checkpoint(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_sleep", "fed1d9401e6c0c187922dd6d4a55425655d20707acfb1dc2485adc5b2477c9b2", "SEM3218", `async fn f() -> int { sleep(5:uint); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_sleep_let_underscore_entrypoint", "55df096983418f270d494eef88ddffc409645c09fbdb5632027048945593825a", "SEM3218", `@entrypoint
fn main() -> int { let _ = sleep(5:uint); return 0; }
`},
	{"dt_blocking", "4a0cb21b8203ffd4fe9811e4c85168349a9ce22f98013afa90c3307c8673f25e", "SEM3218", `async fn f() -> int { blocking { ret 1; }; return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_forwarder", "c0327b2e1f1b268bcdec83ff89a9b6859d4c6f90b39096f6e943bf106c5cf174", "SEM3218", `async fn work() -> int { return 1; }
fn fwd() -> Task<int> { return work(); }
fn f() -> int { fwd(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dt_function_value", "4f9a0791c686dfe4b4eec3e40229347298e7f90bd5e9701c4704fee92819fe49", "SEM3218", `async fn work() -> int { return 1; }
fn fwd() -> Task<int> { return work(); }
fn f(g: fn() -> Task<int>) -> int { g(); return 0; }
@entrypoint
fn main() -> int { return f(fwd); }
`},
	{"dt_async_method", "8ec8627e13ef4bf8de30d0ade82cc2bebd22bbc7e775935b9710010b1b159859", "SEM3218", `type C = { n: int }

extern<C> {
    pub async fn size(self: &C) -> int { return self.n; }
}

fn f() -> int { let c: C = C { n = 1 }; c.size(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"ctl_a14_discarded_plain_call", "f9dca7a5141b888ecf83b476b6c5e86199a74a3f40befe6755646d342aac6c75", "SEM3218", `async fn worker(x: &int64) -> int64 {
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
	{"ctl_lock_task_bound_and_dropped", "39bf752046bff03d0f07ce713efa8ba8d0ad5f4ea4344fc5514a0da690ed7091", "SEM3218", `async fn cond_waiter(mtx: Mutex) -> int {
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

// Kept: a task awaited, spawned, bound, returned or passed. A spawn or a clone nothing awaits is SEM3107 alone.
var taskDroppedKeptProbes = []taskCheckProbe{
	{"dk_awaited_stmt", "7e2f8f0252e14cd14c97206ee5465493ccbccb7ba0c5ce83da1c4c111520ec44", "", `async fn work() -> int { return 1; }
async fn f() -> int { work().await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_awaited_let_underscore", "90446020c357f7a9f9d048601b0464dcdcf680decbfa2713e0c325fed47a1e09", "", `async fn work() -> int { return 1; }
async fn f() -> int { let _ = work().await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_bound_then_awaited", "71c0726e3fc7aa472d013df62fbb1c3228790bbbfcf23c96801878e1d4d1bf8a", "", `async fn work() -> int { return 1; }
async fn f() -> int { let t = work(); let _ = t.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_spawned_then_awaited", "7795a65c7761a815a5a69b1a41114b8bfa40aed45f001c25f719a212fabd205c", "", `async fn work() -> int { return 1; }
async fn f() -> int { let t = spawn work(); let _ = t.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_spawn_stmt_in_async_block", "e676c4f7445697b54d17014a4b5142175fdd48a26cf0b6c37abd70db2c397f5c", "", `async fn work() -> int { return 1; }
async fn f() -> int { let b = async { spawn work(); ret 1; }; let _ = b.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_returned", "fe0d70eaf5eb2e12346f75b440cac3c6a14c4f1fc4e8da5185fe15ed32d9aa73", "", `async fn work() -> int { return 1; }
fn f() -> Task<int> { return work(); }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_passed", "b5e3db08d33eb6f3b30f70298cf2f1d559085413a9b674354a1ae94b20873b94", "", `async fn work() -> int { return 1; }
fn sink(t: Task<int>) -> nothing { return nothing; }
fn f() -> int { sink(work()); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_bound_unused", "e3f7997298167d7b733c54b6f3cf1a9a05bed34463eed68de8811cd8782023f1", "", `async fn work() -> int { return 1; }
fn f() -> int { let t = work(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_async_block_awaited", "8520d477057d0c17af961d59b8d5b98b272409b1ca01c2f093ab420b3620e8b2", "", `async fn f() -> int { async { ret 1; }.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_lock_awaited", "6f5ea05d89fe2531ecb8167af1c241f26507e0abc8cafacd35d3f182fa2a302d", "", `async fn f() -> int { let m = Mutex.new(); m.lock().await(); m.unlock(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_rwlock_is_not_a_task", "088106bf6d3eb050deeadf20351633f1d0c438fe8b5e30a35e3eb1be40d2b4b2", "", `fn f() -> int { let mut rw: RwLock = RwLock.new(); rw.read_lock(); rw.read_unlock(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_place_let_bound_call", "b6187d980163361919720edc9fd8d47b3abb0637d9014cce4f9e2c834535823c", "", `async fn work() -> int { return 1; }
fn f() -> int { let t = work(); let _ = t; return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_place_stmt_bound_call", "19916c201b3063608dbe8c08fd98f68f378b16af3cf284d2bedc5e578930a4bb", "", `async fn work() -> int { return 1; }
fn f() -> int { let t = work(); t; return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_place_let_spawn", "cef3b003be4e2bc2da4c8c30f5b36db3b29189d11ae12bddded1c8cfb35fd53d", "", `async fn work() -> int { return 1; }
async fn f() -> int { let t = spawn work(); let _ = t; let _ = t.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_place_stmt_spawn", "cc58d621e3f170f2185fdb87a215f2c3b3648cafcf8a821238bfe8cfff651169", "", `async fn work() -> int { return 1; }
async fn f() -> int { let t = spawn work(); t; let _ = t.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_place_let_blocking", "add1b141e010cb764f06594c9fb765533638ae35efb00e100e7bf1c20c655071", "", `async fn f() -> int { let b = blocking { ret 1; }; let _ = b; let _ = b.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_compare_arm_returns", "fbaba8f1ab5871db07054c871409370510612ecffeaf9089ecbbfc145a49e634", "", `async fn work() -> int { return 1; }
fn fwd(o: Option<int>) -> Task<int> { compare o { Some(x) => { return work(); } nothing => { return work(); } }; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_place_stmt_blocking", "7665e4da1d857ee40d33d2c171294f99730b32110085225341991294e565dbe6", "", `async fn f() -> int { let b = blocking { ret 1; }; b; let _ = b.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_hot_awaited", "ad056319e831bcb308560ab4b3584f2d4c8d2d59545ac9a73b05d2e7cb7c1d58", "", `async fn f() -> int { checkpoint().await(); sleep(1:uint).await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_clone_dropped_is_sem3107", "b4f59c0cbf13fa50473e611347bc7415381cd24674a2e44fd9920d5a99b2e922", "SEM3107", `async fn work() -> int { return 1; }
async fn f() -> int { let t = spawn work(); let _ = t.clone(); let _ = t.await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"dk_spawn_stmt_in_async_fn_is_sem3107", "c5d2d1b70372e738ce847c7c49bf8ae7edac12a5624e55ac0c2654a3cf342bfb", "SEM3107", `async fn work() -> int { return 1; }
async fn f() -> int { spawn work(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
}

// The lock-balance and @nonblocking walks read `X.await()` through to X; an async method may await.
var taskDroppedLockProbes = []taskCheckProbe{
	{"la_awaited_double_lock", "2d916a92d6874be3c4a3d5a87998027bf261b0fd15038a6bbef9d071e955fba5", "SEM3079,SEM3080", `async fn f() -> int { let m = Mutex.new(); m.lock().await(); m.lock().await(); m.unlock(); m.unlock(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"la_awaited_not_released", "2ecdf16b6a5d63db2562063a2dd699f04d93daca2cee065a0fe49096680f0d6b", "SEM3081", `async fn f() -> int { let m = Mutex.new(); m.lock().await(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"la_awaited_guarded_write", "2446d765648427f47872df01eca85bac80416edce44bb9645622003ea0d8543c", "", `type Counter = {
    lock: Mutex,
    @guarded_by("lock") value: int,
}

async fn f() -> int { let mut c: Counter = Counter { lock: Mutex.new(), value: 0 }; c.lock.lock().await(); c.value = c.value + 1; c.lock.unlock(); return 0; }
@entrypoint
fn main() -> int { return 0; }
`},
	{"la_awaited_acquires_contract_held", "cabf533160733517b3994cb257e2c2a0c93fad1871ab67b896204c45f5262c96", "SEM3079", `type Resource = { lock: Mutex }

extern<Resource> {
    @acquires_lock("lock")
    async fn acquire(self: &mut Resource) -> nothing {
        self.lock.lock().await();
    }

    pub async fn twice(self: &mut Resource) -> nothing {
        self.lock.lock().await();
        self.acquire().await();
        self.lock.unlock();
    }
}
@entrypoint
fn main() -> int { return 0; }
`},
	{"nb_awaited_lock_in_nonblocking", "d0d530d720b9317b63df4fe75ec28086e17d0ea1befb8f05bb2873d1b89e666d", "SEM3084", `@nonblocking
pub async fn f(m: &Mutex) -> nothing {
    m.lock().await();
    m.unlock();
}
@entrypoint
fn main() -> int { return 0; }
`},
	{"nb_awaited_task_in_nonblocking", "c671609fbc60017f55031c3fd95e6c8a326174951c0826b904804670349eff98", "SEM3084", `async fn work() -> int { return 1; }
@nonblocking
pub async fn f() -> nothing {
    let _ = work().await();
}
@entrypoint
fn main() -> int { return 0; }
`},
	{"nb_sync_try_ops_kept", "995bd7c7dcc2b80605edd59b7a14a2d5f3b941597949bc26c0b951a9b46e6581", "", `@nonblocking
pub fn f(m: &Mutex) -> bool {
    if m.try_lock() {
        m.unlock();
        return true;
    }
    return false;
}
@entrypoint
fn main() -> int { return 0; }
`},
	{"ext_async_method_awaits", "c31e740fcbd2860f0c13a402e14b366e3849cce87cf18f0681ea91acd0ae92e3", "", `type R = { lock: Mutex }

extern<R> {
    pub async fn bump(self: &R) -> nothing {
        self.lock.lock().await();
        self.lock.unlock();
    }
}
@entrypoint
fn main() -> int { return 0; }
`},
}

// The programs the runtime rows ran before this rule, byte for byte from their files at the base: each drops
// `worker(&l)` (or a forwarder of it) where it stands, so the language now refuses the surface form of the defect
// those rows detect. The rows keep their names and now drop a borrow-free task (a binding, a by-value parameter);
// the runtime half stays pinned by the stand rows (TestRuntimeV2ColdTask*).
var taskDroppedBorrowedOriginals = []taskCheckProbe{
	{"dropped_call", "d1b4c8e45ff5c6f127d721ac962e3629805fa93e965a64f11a61d0be472a9c9e", "SEM3218", `async fn worker(x: &string) -> int {
    rt_exit(len(x) to int + 40);
    return len(x) to int;
}

fn leak() -> int {
    let l: string = "abcdef";
    worker(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    let r = leak();
    let _ = checkpoint().await();
    let _ = checkpoint().await();
    return r;
}
`},
	{"dropped_forwarded_call", "d3b4e7161efa6f364c182072025eb67a3e92c2bdbafc1be83fd0f915f5839d16", "SEM3218", `async fn worker(x: &string) -> int {
    rt_exit(len(x) to int + 40);
    return len(x) to int;
}

fn fwd(x: &string) -> Task<int> {
    return worker(x);
}

fn leak() -> int {
    let l: string = "abcdef";
    fwd(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    let r = leak();
    let _ = checkpoint().await();
    let _ = checkpoint().await();
    return r;
}
`},
	{"dropped_member_in_async_body", "f89f8a2b12d19eeb67846c69d401b5fc10ada566287ace267bce1cf91fb54224", "SEM3218", `async fn worker(x: &string) -> int {
    rt_exit(len(x) to int + 40);
    return len(x) to int;
}

async fn outer() -> int {
    let l: string = "abcdef";
    worker(&l);
    let _ = checkpoint().await();
    return 0;
}

@entrypoint
fn main() -> int {
    return compare outer().await() {
        Success(n) => n;
        Cancelled() => 41;
    };
}
`},
	{"valgrind_dropped_call", "aefda32e1b74d20245727a0418006794651779aaa409a0c328a47db71ba8e225", "SEM3218", `async fn worker(x: &string, owned: string) -> int {
    rt_exit(len(x) to int + len(owned) to int + 40);
    return len(x) to int;
}

fn leak() -> int {
    let l: string = "abcdef";
    let owned: string = "q" + l;
    worker(&l, owned);
    return 0;
}

@entrypoint
fn main() -> int {
    let mut i: int = 0;
    while i < 1 {
        let _ = leak();
        i = i + 1;
    }
    let _ = checkpoint().await();
    return 0;
}
`},
	{"failfast_member", "e93bcb5516d1b912744dc3454de02cdbbb071a9c23ce43506ba73294b66a59a9", "SEM3218", `async fn worker(x: &string) -> int {
    rt_exit(len(x) to int + 40);
    return len(x) to int;
}

async fn sibling() -> int {
    let mut i: int = 0;
    while i < 1000 {
        let _ = checkpoint().await();
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let outcome = (@failfast async {
        let l: string = "abcdef";
        let s = sibling();
        s.cancel();
        let mut k: int = 0;
        while k < 100000 {
            worker(&l);
            k = k + 1;
        }
        let _ = s.await();
        ret 0;
    }).await();
    return compare outcome {
        Success(n) => n;
        Cancelled() => 0;
    };
}
`},
}
