package driver

import "testing"

// RV2-DEBT-365, the spawn forms of R-d and R-f (P1u-TC4 R5). A reference carried inside a value -- an
// `Option<&V>`, a `BytesView`, an aggregate holding one -- reaches a SPAWNED task through a `let`, an arm
// binding or directly, where the borrow table names no loan: what was written into the binding is opened
// and pinned, as for a plain call. A spawned task still holding a binding at the end of the block
// expression or compare arm that declares it is refused by the edge P1u-TC3 added, which reads a pin
// whoever opened it. ROOT programs, diagnosed through the public path by taskCheckErrorCodes.

var taskCheckLetCarriedReferenceSpawns = []taskCheckProbe{
	{"rds_let_carried_reference_returned", "aed7def6e9d53ee9edf93d7f70c51d9b48346e41bf8489f9ba10829be4a340ba", "SEM3139", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let o = m.get_ref(&k);
    let t = spawn peek(o);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rds_let_carried_reference_sent", "90a1a00b260a251a2f794373f30a51e5a3ea8bd2be82fcfb71883c31efb7cb6f", "SEM3021", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak(ch: Channel<Task<int>>) -> nothing {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let o = m.get_ref(&k);
    let t = spawn peek(o);
    ch.send(own t);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rds_compare_arm_binding_sent", "b2a639400e0af242dcedabf6db5bd3bf408f666cc6a8f470f194f18b2e93fd24", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

async fn plain(n: int) -> int {
    return n;
}

fn leak(ch: Channel<Task<int>>) -> nothing {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    compare m.get_ref(&k) {
        Some(r) => {
            let t = spawn worker(r);
            ch.send(own t);
        }
        nothing => {
            let u = spawn plain(0);
            ch.send(own u);
        }
    };
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rds_struct_carrying_a_reference_returned", "7bf202c7dd22c467f64ffd1d7a8b8c1432b70793559dc4f2c2dec650b30b038c", "SEM3139", `type Holder = { o: Option<&int> };

fn read_opt(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

async fn peek_holder(h: Holder) -> int {
    return read_opt(own h.o);
}

fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let h: Holder = { o = m.get_ref(&k) };
    let t = spawn peek_holder(h);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rds_bytes_view_let_returned", "1976f8f066fb882942edd4443bcb15d281ca0b17b69b3e4d8c09f545fbb1056a", "SEM3139", `async fn count(v: BytesView) -> int {
    return 1;
}

fn leak() -> Task<int> {
    let s: string = "abcdef";
    let v = s.bytes();
    let t = spawn count(v);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rds_bytes_view_direct_returned", "619f6c618a179f36a60beaa2c5e5ffe2918861d7160395236ff4de0fec4d2484", "SEM3139", `async fn count(v: BytesView) -> int {
    return 1;
}

fn leak() -> Task<int> {
    let s: string = "abcdef";
    let t = spawn count(s.bytes());
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rds_tuple_pattern_binding_returned", "4ad2efc7acabe1f0f6fcec827f1ce5ec5b003b7ef43388c6cb44fc805fefcaea", "SEM3139,SEM3220", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let (o, n) = (m.get_ref(&k), 1);
    let t = spawn peek(o);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rds_mutable_let_assigned_after_the_spawn_in_a_loop", "9b27fd39cbcd1131076168145b006d049308d6612b6f0958cc7d1e2f02b3e561", "SEM3019,SEM3021", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak(ch: Channel<Task<int>>) -> nothing {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let mut o: Option<&int> = nothing;
    let mut i: int = 0;
    while i < 2 {
        let t = spawn peek(o);
        ch.send(own t);
        o = m.get_ref(&k);
        i = i + 1;
    }
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckLetCarriedReferenceSpawnControls = []taskCheckProbe{
	{"ctl_rds_let_carried_reference_awaited", "c8dffbed74f1dd56e09f89ffd32e48411176de9154c2006209022a4d487f5ae2", "", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

async fn sound() -> int {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let o = m.get_ref(&k);
    let t = spawn peek(o);
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
	{"ctl_rds_arm_binding_of_a_parameter_returned", "7ea250e609702fe7cf1abd638659f7f3a39f955d27207648a5e4b6dcf17e7fed", "", `async fn worker(x: &int) -> int {
    return *x;
}

async fn plain(n: int) -> int {
    return n;
}

fn fwd(o: Option<&int>) -> Task<int> {
    compare o {
        Some(r) => {
            return spawn worker(r);
        }
        nothing => {
            return spawn plain(0);
        }
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckBlockExpressionEndSpawnPins = []taskCheckProbe{
	{"rfs_value_block_end_then_drained", "412cc9a5a2b951c9fe5630fb8838bafd737f607091579d4af941ce6267fa64fe", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn leak() -> int {
    let mut tasks: Task<int>[] = [];
    let z: int = {
        let l: string = "abcdef";
        tasks.push(spawn worker(&l));
        ret 0;
    };
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
	{"rfs_compare_arm_body_end_then_drained", "a867c17504c256698b3fbfdf1cde6a4240ed3607ee59ed4653dd2c791b244865", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn leak() -> int {
    let mut tasks: Task<int>[] = [];
    let flag: bool = true;
    let z: int = compare flag {
        true => {
            let l: string = "abcdef";
            tasks.push(spawn worker(&l));
            ret 0;
        };
        false => 0;
    };
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
	{"rfs_compare_arm_binding_end_then_drained", "d9ea4485ce62e22052088d083e1891fdeb70c811b544a9d58a40293619fc2e61", "SEM3021", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

fn make() -> Option<string> {
    return Some("abcdef");
}

async fn leak() -> int {
    let mut tasks: Task<int>[] = [];
    let z: int = compare make() {
        Some(s) => {
            tasks.push(spawn worker(&s));
            ret 0;
        };
        nothing => 0;
    };
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
}

var taskCheckBlockExpressionSpawnControls = []taskCheckProbe{
	{"ctl_rfs_value_block_joins_before_its_end", "e4042b990d35270213e594a2cf53dc1e4b50eb25e2343a5278995e96bbb9b445", "", `async fn worker(x: &string) -> int {
    return len(x) to int;
}

async fn sound() -> int {
    let z: int = {
        let l: string = "abcdef";
        let t = spawn worker(&l);
        ret compare t.await() { Success(v) => v; Cancelled() => 0; };
    };
    return z;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

// 9 RUN: 1 parent, 8 leaves.
func TestTaskCheckRefusesLetCarriedReferenceSpawns(t *testing.T) {
	for _, probe := range taskCheckLetCarriedReferenceSpawns {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a carried reference reached a spawned task unpinned (RV2-DEBT-365, P1u-TC4)", got, probe.want)
			}
		})
	}
}

// 3 RUN: 1 parent, 2 leaves.
func TestTaskCheckKeepsLetCarriedReferenceSpawnPrograms(t *testing.T) {
	for _, probe := range taskCheckLetCarriedReferenceSpawnControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}

// 4 RUN: 1 parent, 3 leaves.
func TestTaskCheckRefusesBlockExpressionEndSpawnPins(t *testing.T) {
	for _, probe := range taskCheckBlockExpressionEndSpawnPins {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a spawned task outlived the block that declares what it borrows (RV2-DEBT-365, P1u-TC4)", got, probe.want)
			}
		})
	}
}

// 2 RUN: 1 parent, 1 leaves.
func TestTaskCheckKeepsBlockExpressionSpawnPrograms(t *testing.T) {
	for _, probe := range taskCheckBlockExpressionSpawnControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
