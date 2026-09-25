package driver

// N-TASK-27S: the frozen programs of the `spawn on` rows. Each is checked against its digest before it is read.

const spawnOnC05CapChannel64Source = `fn start(ch: Channel<int64>) -> far Task<int> {
    return spawn on pool {
        ch.send(1:int64);
        ret 1;
    };
}
`

const spawnOnC06CapFnValueSource = `fn one() -> int {
    return 1;
}

fn start() -> far Task<int> {
    let f = one;
    return spawn on pool {
        ret f();
    };
}
`

const spawnOnLeakSonAwaitSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        ret compare leak().await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
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

const spawnOnLeakSonRetSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

async fn run() -> int {
    let ft: far Task<Task<int>> = spawn on distributed {
        ret leak();
    };
    return compare ft.await() {
        Success(t) => compare t.await() { Success(n) => n + 40; Cancelled() => 100; };
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

const spawnOnLeakSonSpawnSource = `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(&l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        let t = spawn leak();
        ret compare t.await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
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

const spawnOnP1RestorePanicSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    let n: int = *x;
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

async fn run(s: int) -> int {
    on distributed {
        let h = keep(peek(&s));
        panic("boom");
    };
    return 0;
}

@entrypoint
fn main() -> int {
    return compare run(4).await() {
        Success(v) => v;
        Cancelled() => 1;
    };
}
`

const spawnOnP9AsyncPanicSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    let n: int = *x;
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

async fn run(s: int) -> int {
    let b = async {
        let h = keep(peek(&s));
        panic("boom");
    };
    return 0;
}

@entrypoint
fn main() -> int {
    return compare run(4).await() {
        Success(v) => v;
        Cancelled() => 1;
    };
}
`

const spawnOnR01PayloadTaskSource = `async fn plain(x: int) -> int {
    return x;
}

fn start() -> far Task<Task<int>> {
    return spawn on pool {
        ret plain(1);
    };
}
`

const spawnOnR06BodyTaskUnjoinedSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

fn start() -> far Task<int> {
    return spawn on pool {
        let bl: string = "abc";
        let t = worker(&bl);
        ret 0;
    };
}
`

const spawnOnR07BodyTaskReturnedSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

fn start() -> far Task<Task<int>> {
    return spawn on pool {
        let bl: string = "abc";
        let t = worker(&bl);
        ret t;
    };
}
`

const spawnOnR09BodyStringTempSource = `async fn wk(s: &string) -> int {
    return len(s) to int;
}

async fn start() -> far Task<int> {
    return spawn on pool {
        ret compare wk("abc").await() {
            Success(n) => n;
            Cancelled() => 0;
        };
    };
}
`

const spawnOnR10BodyLocalEscapeSource = `fn start() -> far Task<int> {
    return spawn on pool {
        let v: int = 1;
        let mut p: &int = &v;
        {
            let w: int = 2;
            p = &w;
        }
        ret *p;
    };
}
`

const spawnOnR14CapFarTaskSource = `fn start() -> far Task<int> {
    let inner = spawn on pool {
        ret 1;
    };
    return spawn on pool {
        ret compare inner.await() {
            Success(n) => n;
            Cancelled() => 0;
        };
    };
}
`

const spawnOnR16PayloadArraySource = `fn start() -> far Task<int[]> {
    return spawn on pool {
        let xs: int[] = [1, 2];
        ret xs;
    };
}
`

const spawnOnR17AfterRowSource = `fn route(k: &string) -> int {
    return 1;
}

fn start() -> int {
    let ft = spawn on distributed {
        ret 1;
    };
    let _ = ft.await();
    return route("abc");
}
`

const spawnOnR18DestStringTempSource = `fn route(k: &string) -> Placement {
    return distributed;
}

fn start() -> far Task<int> {
    return spawn on route("abc") {
        ret 1;
    };
}
`

const spawnOnR20BodySpawnOverLocalReturnedSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn start() -> far Task<Task<int>> {
    return spawn on distributed {
        let bl: string = "abc";
        ret spawn worker(&bl);
    };
}
`

const spawnOnR21BodySpawnOverCaptureReturnedSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    return *x;
}

async fn start(n: int) -> far Task<Task<int>> {
    let k: int = n;
    return spawn on distributed {
        ret spawn peek(&k);
    };
}
`

const spawnOnSAwaitedSource = `fn run(dst: Placement) -> TaskResult<int> {
    let task: far Task<int> = spawn on dst {
        ret 3;
    };
    return task.await();
}
`

const spawnOnSBoundSource = `fn start(n: int) -> far Task<int> {
    let k: int = n;
    let t = spawn on pool {
        ret k + 1;
    };
    return t;
}
`

const spawnOnSReturnedSource = `fn double(x: int) -> int {
    return x * 2;
}

fn start(n: int) -> far Task<int> {
    return spawn on distributed {
        ret double(n);
    };
}
`

const spawnOnSonDetachSource = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on shard(1:ShardId) {
        let t = spawn leak();
        ret 46;
    };
    return compare ft.await() {
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

const spawnOnTwinSonAwaitSource = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        ret compare leak().await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
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

const spawnOnTwinSonSpawnSource = `async fn worker(x: string) -> int {
    return len(x) to int;
}

fn leak() -> Task<int> {
    let l: string = "abcdef";
    let t = worker(l);
    return t;
}

async fn run() -> int {
    let ft: far Task<int> = spawn on distributed {
        let t = spawn leak();
        ret compare t.await() { Success(n) => n + 40; Cancelled() => 100; };
    };
    return compare ft.await() {
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

const (
	spawnOnC05CapChannel64SourceDigest                 = "d0875dc60b59cdc0a0f7c00cbac5fe9b038e27bd05c6898e921621e37f38e5a9"
	spawnOnC06CapFnValueSourceDigest                   = "02dd8f531ad99ead5a151698fe0ff7ffcb9b07a63a5c34f98f31568c72c6e147"
	spawnOnLeakSonAwaitSourceDigest                    = "4eab26811973c93b1276eea7b3559026ff367c7bbe37c7c2d95b0f01fca976e1"
	spawnOnLeakSonRetSourceDigest                      = "bf0d226e9b3f4529c30ce8ca12fcd0ca2a5e8628cd8f9aef7d7b658c181484bd"
	spawnOnLeakSonSpawnSourceDigest                    = "e20a45fe60918c2a8cee385c95f5396795124f74e1995d7b1494458d88321bf3"
	spawnOnP1RestorePanicSourceDigest                  = "896a7eff7773aea208152bf10b81ed1e9fd1340281e397dc59adf70ca867262e"
	spawnOnP9AsyncPanicSourceDigest                    = "cdb4bb0b2176c495df771f206b8ab10f067d501033966be66a3f4b98fc1aca84"
	spawnOnR01PayloadTaskSourceDigest                  = "d74f2b5f3bc22bf2fb7556eb16ee13a0dec5427ed82358ee50bb07e06a0f74aa"
	spawnOnR06BodyTaskUnjoinedSourceDigest             = "b68cbff117332f0328f040f8f5ecfbf56030915079cc10ac2e5b6727ae55cdc7"
	spawnOnR07BodyTaskReturnedSourceDigest             = "b1667d79ffa7b0cd95501969661fef19d21a982b001e42ad95914cf44503858f"
	spawnOnR09BodyStringTempSourceDigest               = "41d169b88a5fe04d72d824b972081dc8f91dccd7f315b504a21a69a9129aa69b"
	spawnOnR10BodyLocalEscapeSourceDigest              = "1fad3841283e1e3e35b07d72c780c3da86c31c21e54a355ed8b8963d03885292"
	spawnOnR14CapFarTaskSourceDigest                   = "91dccff93fbd884dae34cbc0b39028ac8a2521b6c263bc77cadd044c7ac4a686"
	spawnOnR16PayloadArraySourceDigest                 = "40efc29859aa8c3bdb61bcfbcee61863eb44f3cb8a44d915c5261636aad9c079"
	spawnOnR17AfterRowSourceDigest                     = "a917f9751133fe82c6415cafa8fa36e36e2959cf66942ef815dcd1fc9ed7b0ab"
	spawnOnR18DestStringTempSourceDigest               = "85c77eca9102ed78ce806f394e415d4bcb46bec0671d21c2a10a6ca5049574a7"
	spawnOnR20BodySpawnOverLocalReturnedSourceDigest   = "6757e2f2e8c49ea1c16ba9a854d654a837ca6836059307b4a373c9368e45ece6"
	spawnOnR21BodySpawnOverCaptureReturnedSourceDigest = "987a6fd9469a213f7531794a566cf0268730441cd49487db75e780cf1a0e1147"
	spawnOnSAwaitedSourceDigest                        = "c6520baaf71785146556bd7ee66f85bea28f218c899964bd7347ae47796b82de"
	spawnOnSBoundSourceDigest                          = "6a4e078f23a4ce9a59cf97d363438bef1188c835be9fd59a3e6474d83858a8eb"
	spawnOnSReturnedSourceDigest                       = "7bd831ae4ab75900cf034ba8e04264e06aa25668cbbd83893f7b1e921d18a1cc"
	spawnOnSonDetachSourceDigest                       = "4f6568dc4a94656702e6649f174d376a4557211153e2519bc34e00a232326c89"
	spawnOnTwinSonAwaitSourceDigest                    = "081382c31d91f133613792ecee43e1a49d3edd5980f33117d40e3ccd52ba6ba1"
	spawnOnTwinSonSpawnSourceDigest                    = "391d58f0a0de7c6b85dfa91ab1e5d1ce700d02ad30538196eb2a628e6d48b345"
)
