package driver

// Frozen RV2-DEBT-365 tripwire probes; each digest is checked before use. Generated from
// the measured probe files: a changed byte is a different program.
const (
	h2TripwireG0AwaitDigest = "69bafea14ef44cc61164719c5846d0c0cbf1e38469f2159d4a0e7d1ad6534e78"
	h2TripwireG0Await       = `async fn worker(x: &string) -> int {
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
`
	h2TripwireG0dAwaitDiscDigest = "0c68d987237ee42acb029eaa198ed80b35940cdee7fc9afb5eb9db03ba107fd5"
	h2TripwireG0dAwaitDisc       = `async fn worker(x: &string) -> int {
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
        Success(n) => n + 40;
        Cancelled() => 100;
    };
}
`
	h2TripwireG5ModuleTimeoutDigest = "ae0fb3a21d93217409d8248e8d56731797d145a42331ccdf43c8721f71a7708f"
	h2TripwireG5ModuleTimeout       = `import core/intrinsics as ci;

async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return compare ci.timeout(leak(), 1000) {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
`
	h2TripwireG5bAliasTimeoutDigest = "ab2072707e0db4b5cf3894796d3e63eaaf28a9e65e942053fc2264625ae7c5d3"
	h2TripwireG5bAliasTimeout       = `import core/intrinsics::{timeout as tm};

async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return compare tm(leak(), 1000) {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
`
	h2TripwireG4dTwinDigest = "3d2c97df917a853a40469811a07ea7672519ba3391b210edbf7577b9454f1cf3"
	h2TripwireG4dTwin       = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

@entrypoint
async fn main() -> int {
    return compare timeout(leak(), 1000) {
        Success(n) => n + 40;
        Cancelled() => 100;
    };
}
`
)
