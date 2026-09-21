package vm_test

// Frozen RV2-DEBT-365 tripwire probes; each digest is checked before use. Generated from
// the measured probe files: a changed byte is a different program.
const (
	h2TripwireG1OwnBindingDigest = "65c43e44e52c6ac2dc667787be62bcfedb43d7de80c9a98fd94d66d722a01116"
	h2TripwireG1OwnBinding       = `async fn worker(x: &string) -> int {
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
    let o: own Task<int> = own t;
    return compare o.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
`
	h2TripwireG1bOwnExprDigest = "76128d56e604219bee0657d79d4004d93bca516ab8685fb74f963c2dcf2131c8"
	h2TripwireG1bOwnExpr       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return compare (own leak()).await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
`
	h2TripwireG3ScopeJoinDigest = "4bc47822e424a0b7219a34bd04d112bd5534929ee52cbc0ec460de91e0f3aa27"
	h2TripwireG3ScopeJoin       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    let scope = rt_scope_enter(false);
    rt_scope_register_child(scope, leak());
    let joined = rt_scope_join_all(scope);
    rt_scope_exit(scope);
    return joined ? 0 : 1;
}
`
	h2TripwireG4xAsyncEntryDigest = "bb8cab58da15ab82a12a6a12672e9c6609a3781c558ab68ecf4e59762f770519"
	h2TripwireG4xAsyncEntry       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
async fn main() -> int {
    compare timeout(leak(), 1000) {
        Success(n) => rt_exit(n + 40);
        Cancelled() => rt_exit(41);
    };
    return 0;
}
`
	h2TripwireRo6xAsyncEntryDigest = "370abe25fdcb8bd8307e1c5d1779bcbf220b0c5b4f4d637f6f76706db781b0e6"
	h2TripwireRo6xAsyncEntry       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn pass(t: Task<int>) -> Task<int> {
    return t;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return pass(t);
}

@entrypoint
async fn main() -> int {
    compare timeout(leak(), 1000) {
        Success(n) => rt_exit(n + 40);
        Cancelled() => rt_exit(41);
    };
    return 0;
}
`
	h2TripwireRo7xAsyncEntryDigest = "db7eb163fe330eee3341c1d8344f26c26ad4a4725184c60fd3cb0dd1baba2ee1"
	h2TripwireRo7xAsyncEntry       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn zero() -> int {
    return 6;
}

fn leak(out: &mut Task<int>) -> nothing {
    let l: string = "abcdef";
    *out = worker(&l);
    return nothing;
}

@entrypoint
async fn main() -> int {
    let mut t: Task<int> = zero();
    leak(&mut t);
    compare timeout(t, 1000) {
        Success(n) => rt_exit(n + 40);
        Cancelled() => rt_exit(41);
    };
    return 0;
}
`
	h2TripwireG4cWitnessControlDigest = "dc1815749056048e4c6fa0940b0c54767df4bc42123086c6bae9fe916d0a8d93"
	h2TripwireG4cWitnessControl       = `async fn plain() -> int {
    return 6;
}

@entrypoint
async fn main() -> int {
    compare timeout(plain(), 1000) {
        Success(n) => rt_exit(n + 40);
        Cancelled() => rt_exit(41);
    };
    return 0;
}
`
	h2TripwireSyncRtExitDigest = "216c163a4f3640bab731938648132b427a5772bf5bd10c84868e16b3c9847618"
	h2TripwireSyncRtExit       = `@entrypoint
fn main() -> int {
    rt_exit(46);
    return 0;
}
`
)
