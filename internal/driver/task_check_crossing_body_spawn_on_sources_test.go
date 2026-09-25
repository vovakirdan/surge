package driver

// TC-XB (RV2-DEBT-378): the frozen programs of TestTaskCheckCrossingBodyIsAFrame and TestTaskCheckCrossingBodyKeepsSoundPrograms, the `spawn on` forms (the review's probes, verbatim).
// Each is diagnosed through taskCheckErrorCodes, which checks its digest first.

const crossingFrameSpawnOnByValueSource = `async fn peek(x: int) -> int {
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
    let ft = spawn on distributed {
        let h = keep(peek(s));
        ret 1;
    };
    let r = compare ft.await() {
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

const crossingFrameSpawnOnCallerAwaitsSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    return *x;
}

async fn run(n: int) -> int {
    let k: int = n;
    let ft = spawn on distributed {
        let t = peek(&k);
        ret 1;
    };
    let r = compare ft.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    return r;
}
`

const crossingFrameSpawnOnCaptureJoinedSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    return *x;
}

async fn start(n: int) -> far Task<int> {
    let k: int = n;
    return spawn on distributed {
        let t = spawn peek(&k);
        ret compare t.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
    };
}
`

const crossingFrameSpawnOnColdTaskSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    return *x;
}

async fn run(n: int) -> nothing {
    let ft = spawn on distributed {
        let t = peek(&n);
        ret 1;
    };
    let r = compare ft.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    panic("stop");
}
`

const crossingFrameSpawnOnHotTaskSource = `async fn peek(x: &int) -> int {
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
    let ft = spawn on distributed {
        let h = keep(peek(&s));
        ret 1;
    };
    let r = compare ft.await() {
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

const crossingFrameSpawnOnKeptAwaitedSource = `async fn peek(x: &int) -> int {
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
    let ft = spawn on distributed {
        let h = keep(peek(&s));
        ret compare h.await() { Success(v) => v; Cancelled() => 0; };
    };
    let r = compare ft.await() {
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

const crossingFrameSpawnOnLocalJoinedSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn start() -> far Task<int> {
    return spawn on pool {
        let bl: string = "abc";
        let t = worker(&bl);
        ret compare t.await() {
            Success(n) => n;
            Cancelled() => 0;
        };
    };
}
`

const crossingFrameSpawnOnTaskPayloadSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    return *x;
}

fn start(n: int) -> far Task<Task<int>> {
    let k: int = n;
    return spawn on distributed {
        ret peek(&k);
    };
}
`

const (
	crossingFrameSpawnOnByValueSourceDigest       = "5cfee14909589d7fe6a95bbd3331f631a359ec7421039d305662795c3ff1684d"
	crossingFrameSpawnOnCallerAwaitsSourceDigest  = "6aa71104e33ef4b999c1e56266fd61460d3b2eafa35c895ce144846117e03e7c"
	crossingFrameSpawnOnCaptureJoinedSourceDigest = "a1644723c40ba0733b09c466e3953d374fe2fc1c7b53e569dcd776c7b695b3e3"
	crossingFrameSpawnOnColdTaskSourceDigest      = "dce2c9957be0e9d4bd7e886debf24b5eb76a0fde994208083d1176c3fc62fea5"
	crossingFrameSpawnOnHotTaskSourceDigest       = "ecb4539359e8324973ef1eab5e4aa886b3d5f95c99fcf2f24cb9c22d695614ec"
	crossingFrameSpawnOnKeptAwaitedSourceDigest   = "7c9537200bc3d1e8304699308d5f3942428b9efe5743724a6ca3319f91280a7a"
	crossingFrameSpawnOnLocalJoinedSourceDigest   = "09536a147117fffba1bb016f33ffe41379e40f70442454606fb3f72742522efc"
	crossingFrameSpawnOnTaskPayloadSourceDigest   = "52ac9c76124edc2e5f790e4b6f1266c7e69420a1fa8bf194e16849df8aa6955c"
)
