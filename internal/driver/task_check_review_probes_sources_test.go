package driver

// Frozen programs the task check must refuse that no probe had measured: the forms the two
// non-author reviews of the packet found by reading. Each digest is checked before use.
var taskCheckReviewLeakProbes = []taskCheckProbe{
	{"m1_view_direct", "df930abc30741505b44f59f83fef61ce03c0449d3c9d3c50640e9c87356ff9c2", "SEM3139", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let t = first(a[[0..2]]);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"m1_view_through_wrapper", "11e93c3d576cddd8ffbed3337b4000e47b2106c94ed2f86fd30403d4b5ab490e", "SEM3139", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn window(a: &int64[4]) -> Task<int64> {
    return first(a[[0..2]]);
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let t = window(&a);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"m1_view_spawned", "2258e40a83edd48b91df0d19df47a95390a5299ca9127c67f22aec04fbc08069", "SEM3139", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    return spawn first(a[[0..2]]);
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"m3_block_end_then_drained", "818a50d2033d60ecca0a28c619667b7027d19e535123318b8dc5d5b015677d0d", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn leak() -> int {
    let mut tasks: Task<int>[] = [];
    {
        let l: string = "abcdef";
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
	{"f1_carried_reference_lender", "465092be3a78fefdda93646df1a847ed8121a9bd6bc12cdf4a0b73ca1995b55f", "SEM3139", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let t = peek(m.get_ref(&"k"));
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"blk_view_captured", "00502fce775392f756174e8181f6a9bfc7440497504f21e77d1819d8b0e9bb1b", "SEM3198", `fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let v = a[[0..2]];
    return blocking {
        let e: int64 = v[0];
        ret e;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"async_view_captured", "7ab1c26a4e57b2f6298482c46de1455c48eea7dc539be259335e422219bca6cd", "SEM3198", `fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let v = a[[0..2]];
    return async {
        let e: int64 = v[0];
        ret e;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}
