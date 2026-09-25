package driver

import "testing"

// RV2-DEBT-365, R-d's call form (P1u-TC3). A reference carried inside a value -- an `Option<&V>`, a
// `BytesView`, an aggregate holding one, an `own` of a place -- reaches a task through a `let`, an arm
// binding or directly, where the borrow table names no loan: what was written into the binding is
// opened and pinned. ROOT programs, diagnosed through the public path by taskCheckErrorCodes.

var taskCheckLetCarriedReferenceCalls = []taskCheckProbe{
	{"rd_let_carried_reference_returned", "f1ebe5f9d04e71a03f7e0a53cd9aed72428b29172878c3d67eb41640ea225bb4", "SEM3139", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let o = m.get_ref(&"k");
    let t = peek(o);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_let_carried_reference_handed_away", "347a4ace48b01e82fee0ca0261c52526ad58ad08e15f671a6ab5bc824c2eff0f", "SEM3021", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak(out: &mut Task<int>) -> nothing {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let o = m.get_ref(&k);
    *out = peek(o);
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_compare_arm_binding_handed_away", "f33b2f2575c52873d6474ecb588415c81bff10949df767ea6f75f20e4aa722f5", "SEM3021", `async fn worker(x: &int) -> int {
    return *x;
}

async fn plain(n: int) -> int {
    return n;
}

fn leak(out: &mut Task<int>) -> nothing {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    compare m.get_ref(&k) {
        Some(r) => {
            *out = worker(r);
        }
        nothing => {
            *out = plain(0);
        }
    };
    return nothing;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_struct_carrying_a_reference_returned", "2dca09d035ac1c2a80ade96ae6fdecd038acd124799a1d7c7e9717d9f34b1042", "SEM3139", `type Holder = { o: Option<&int> };

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
    let t = peek_holder(h);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_field_of_a_carrier_let_returned", "61433e183aea5f4971672c36e87c576f98d60f51f98ad212142e9330ce65c3c4", "SEM3139", `type Holder = { o: Option<&int> };

async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let h: Holder = { o = m.get_ref(&k) };
    let t = peek(own h.o);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_bytes_view_let_returned", "3220a5a2d638df639a9e69b62b0cdd90b67f19d50df60e3d8cdc2f002e28d05e", "SEM3139", `async fn count(v: BytesView) -> int {
    return 1;
}

fn leak() -> Task<int> {
    let s: string = "abcdef";
    let v = s.bytes();
    let t = count(v);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_bytes_view_direct_returned", "60574a4513f7876359a68e8b8250a4a8d0f5c86b8057ace7bc3770e7420f70e4", "SEM3139", `async fn count(v: BytesView) -> int {
    return 1;
}

fn leak() -> Task<int> {
    let s: string = "abcdef";
    let t = count(s.bytes());
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_bytes_view_through_a_helper_returned", "19db13439d9a3f9217249804313825b0840232d3009b693ded973348f0400da7", "SEM3139", `async fn count(v: BytesView) -> int {
    return 1;
}

fn view_of(s: &string) -> BytesView {
    return s.bytes();
}

fn leak() -> Task<int> {
    let s: string = "abcdef";
    let t = count(view_of(&s));
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_mutable_let_assigned_after_the_call_in_a_loop", "bc9b1e8d84a94b2cd240076af90257282946c84d592205f40015188b972ba21c", "SEM3019,SEM3021", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak(out: &mut Task<int>) -> nothing {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let mut o: Option<&int> = nothing;
    let mut i: int = 0;
    while i < 2 {
        *out = peek(o);
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
	{"rd_tuple_pattern_binding_returned", "1a27c88b8e47c3310896186f4fcae8c11368da455fe5883bc223b6da1f2d1708", "SEM3139,SEM3220", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let (o, n) = (m.get_ref(&k), 1);
    let t = peek(o);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_own_reference_let_returned", "7d940c434c978a7f8f375ae13ef35fec097b4eff2d30e58b2b5f9e7a2982d618", "SEM3139", `fn keep(o: own &int) -> Task<int> {
    return async {
        let _ = o;
        ret 0;
    };
}

fn leak() -> Task<int> {
    let xs: int[] = [5];
    let o = own xs[0];
    let t = keep(o);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_own_index_argument_returned", "294cd5b795ffaf2b0ff43679bbe3c28774739b062ce4f47ac936d0b788826b32", "SEM3139", `fn keep(o: own &int) -> Task<int> {
    return async {
        let _ = o;
        ret 0;
    };
}

fn leak() -> Task<int> {
    let xs: int[] = [5];
    let t = keep(own xs[0]);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckLetCarriedReferenceControls = []taskCheckProbe{
	{"ctl_rd_let_carried_reference_awaited", "cce86aefa9d28d8d42f4663c573f23d46c3ed003953284daaa65b82fc126c31e", "", `async fn peek(o: Option<&int>) -> int {
    return compare o {
        Some(r) => *r;
        nothing => 0;
    };
}

async fn sound() -> int {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let o = m.get_ref(&k);
    let t = peek(o);
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
	{"ctl_rd_compare_arm_binding_of_a_parameter_returned", "f14ae15a41fb579d9a282630f23b501c6224d065cc2b495d57dda6752830d768", "", `async fn worker(x: &int) -> int {
    return *x;
}

async fn plain(n: int) -> int {
    return n;
}

fn fwd(o: Option<&int>) -> Task<int> {
    compare o {
        Some(r) => {
            return worker(r);
        }
        nothing => {
            return plain(0);
        }
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

func TestTaskCheckRefusesLetCarriedReferenceCalls(t *testing.T) {
	for _, probe := range taskCheckLetCarriedReferenceCalls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: a carried reference reached a task unpinned (RV2-DEBT-365, P1u-TC3)", got, probe.want)
			}
		})
	}
}

func TestTaskCheckKeepsLetCarriedReferencePrograms(t *testing.T) {
	for _, probe := range taskCheckLetCarriedReferenceControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
