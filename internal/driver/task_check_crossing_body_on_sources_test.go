package driver

// TC-XB (RV2-DEBT-378): the frozen programs of TestTaskCheckCrossingBodyIsAFrame and TestTaskCheckCrossingBodyKeepsSoundPrograms, the `on` forms (the review's q18 and its variants, and a4).
// Each is diagnosed through taskCheckErrorCodes, which checks its digest first.

const crossingFrameOnAsyncHostSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let host = async {
        let ft = on distributed {
            let h = keep(peek(&s));
            ret 1;
        };
        ret compare ft {
            Success(v) => v;
            Cancelled() => 0;
        };
    };
    let r = compare host.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnAwaitInBodySource = `async fn worker(x: int) -> int {
    return x + 40;
}

async fn run() -> int {
    let r = on shard(1:ShardId) {
        ret compare worker(6).await() {
            Success(n) => n;
            Cancelled() => 100;
        };
    };
    return compare r {
        Success(n) => n;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const crossingFrameOnBodyLocalSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = on distributed {
        let bl: int = s;
        let h = keep(peek(&bl));
        ret 1;
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnByValueSource = `async fn peek(x: int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = on distributed {
        let h = keep(peek(s));
        ret 1;
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnDiscardedByValueSource = `async fn peek(x: int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    on distributed {
        let h = keep(peek(s));
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnDiscardedSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    on distributed {
        let h = keep(peek(&s));
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnHotTaskSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = on distributed {
        let h = keep(peek(&s));
        ret 1;
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnJoinedSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = on distributed {
        let h = peek(&s);
        ret compare h.await() { Success(v) => v; Cancelled() => 0; };
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnKeptAwaitedSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = on distributed {
        let h = keep(peek(&s));
        ret compare h.await() { Success(v) => v; Cancelled() => 0; };
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnLetCaptureSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(p: int) -> nothing {
    let s: int = p;
    let ft = on distributed {
        let h = keep(peek(&s));
        ret 1;
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const (
	crossingFrameOnAsyncHostSourceDigest        = "d0bbc17f2c7a9e602528165f5caa3fe2ec316daaa076d6ec54bcb6e6af5b87fa"
	crossingFrameOnAwaitInBodySourceDigest      = "a6c5af905559f8602cf839caf66e4c308519b31a2aa400672fb258cd87330e43"
	crossingFrameOnBodyLocalSourceDigest        = "12a25835f8aa1112cef7ddea9cedb9a116f5416756ef1cb9e6348037c72fb74a"
	crossingFrameOnByValueSourceDigest          = "02dc7cdb2c15a0458b51422c72b431e2cbd3203baded637c2daa47bd560be017"
	crossingFrameOnDiscardedByValueSourceDigest = "f50849fd20a288fa8ed84263f2883bff21d1d9fde1b270d3e96ae750f7836fbb"
	crossingFrameOnDiscardedSourceDigest        = "a130c55fc20a12036bb9c7d3b93ba882c0ccf66473b506b353b82d8a42ee66b0"
	crossingFrameOnHotTaskSourceDigest          = "c723e54fe8e587d8ba5caa741ccebdbafecb16015b88e26f1f95c4b4270245ed"
	crossingFrameOnJoinedSourceDigest           = "b764d3951810f05ea94796e518d40cdf00eb929a585bef99af65801505bffbc1"
	crossingFrameOnKeptAwaitedSourceDigest      = "69e1ce95cc729602668be757249426ede9d1580e2923c50bacd63001fe9f8f7d"
	crossingFrameOnLetCaptureSourceDigest       = "565c0c24459c22e84527387ec328a2340006c2aa746b083056901949595c0a25"
)
