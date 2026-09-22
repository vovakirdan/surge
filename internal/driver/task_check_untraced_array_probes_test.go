package driver

import "testing"

// RV2-DEBT-365, R-e and R-d's capture half (P1u-TC2). A value that can carry a dynamic array or a
// range reaches a task where the checker holds no window or cursor provenance for it: refused
// when the function holds a fixed array by value (any array, for a range) or slices one it never
// named. A `let` whose value carries a reference and is captured by a body is refused. The
// controls are the sound programs those shapes could be mistaken for. ROOT programs, diagnosed
// through the public path by taskCheckErrorCodes.

var taskCheckUntracedArrayLeaks = []taskCheckProbe{
	{"re_assigned_window_handed_to_a_task", "a0c18464238ad36ab8c330c2df27f58b9a1cab0e0b54f8fdd92162430a1e8624", "SEM3198", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let mut v: int64[] = [];
    v = a[[0..2]];
    let t = first(v);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"re_window_held_in_a_struct_handed_to_a_task", "7bc05710f1943b113f26435b07fdb69564331ccdbda359d01020a3542a5caaa6", "SEM3198", `type Hold = { xs: int64[] };

async fn first_of(h: Hold) -> int64 {
    return h.xs[0];
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let h = Hold { xs = a[[0..2]] };
    let t = first_of(h);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"re_window_returned_by_a_helper_handed_to_a_task", "e9fe324f5754c3628c9057acd9e8c8263e484e3924447a65ea3f1ac92cd3f03a", "SEM3198", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn pass_on(xs: int64[]) -> int64[] {
    return xs;
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let v = pass_on(a[[0..2]]);
    let t = first(v);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"re_window_of_a_window_handed_to_a_task", "41df8e75d83e6f8d5c922c0a2e97e5b74567cf32b64e6f9e08290ce5a395630a", "SEM3198", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let v = a[[0..3]];
    let t = first(v[[0..2]]);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"re_window_holder_as_a_receiver", "6a5c073cac8bac9031f4e84df47179ac84ea5cccbcdcf323e6f9d2ed02cc9be0", "SEM3198", `type Hold = { xs: int64[] };

extern<Hold> {
    pub async fn head(self: Hold) -> int64 {
        return self.xs[0];
    }
}

fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let h = Hold { xs = a[[0..2]] };
    let t = h.head();
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"re_assigned_window_captured_by_async", "ef8a4eb97d608e35bce4a0b7b958bb38b842763f128c706cca235cbc21ce6460", "SEM3198", `fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let mut v: int64[] = [];
    v = a[[0..2]];
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
	{"re_assigned_window_captured_by_blocking", "9ebbf7645bfe777131bdfde656045a0dc7711ad5d1e7621444f135d5e6f1e2ec", "SEM3198", `fn leak() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let mut v: int64[] = [];
    v = a[[0..2]];
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
	{"re_window_over_a_reference_parameter_returned", "613517d18b0118aa2c573fa9e40a839696703dff72d67e2b03ef9a586a011b69", "SEM3139", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn window(a: &int64[4]) -> Task<int64> {
    let mut v: int64[] = [];
    v = a[[0..2]];
    return first(v);
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
	{"re_cursor_handed_to_a_task", "8de2c04ef22415d8bceb0511c7f6f388dbbda2d9ad87d221f42fedf5c2487fc9", "SEM3139", `async fn walk(r: Range<int64>) -> int64 {
    let mut c = r;
    return compare c.next() {
        Some(v) => v;
        nothing => 0:int64;
    };
}

fn leak() -> Task<int64> {
    let mut xs: int64[] = [];
    xs.push(1:int64);
    let c = xs.__range();
    let t = walk(c);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"re_traced_cursor_captured_by_blocking_returned", "c52c8a95608b8988ad4c39c9264e8829690b757ff58c8656965d4fa2d1ab2cf7", "SEM3139", `fn leak() -> Task<int64> {
    let mut xs: int64[] = [];
    xs.push(1:int64);
    let c = xs.__range();
    let job: Task<int64> = blocking {
        let mut n: int64 = 0:int64;
        for v: int64 in c { n = n + v; }
        ret n;
    };
    return job;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"re_traced_cursor_captured_by_async_returned_directly", "bdfafb7733eadb61e582728d36d6be0f516f1d1e0e7d45596b3f5815a2bcd990", "SEM3139", `fn leak() -> Task<int64> {
    let mut xs: int64[] = [];
    xs.push(1:int64);
    let c = xs.__range();
    return async {
        let mut n: int64 = 0:int64;
        for v: int64 in c { n = n + v; }
        ret n;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckCarriedReferenceCaptures = []taskCheckProbe{
	{"rd_carried_reference_captured_by_async", "7689aaf7fbd0c6e94454c439b6464cc7b05dac0fadcadfaf15e34c1a3ade92ed", "SEM3021", `fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let o = m.get_ref(&k);
    return async {
        let got: int = compare o {
            Some(r) => *r;
            nothing => 0;
        };
        ret got;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"rd_carried_reference_captured_by_blocking", "badf7ccee6ba449054ac9d928698cf6d83b5fabf4c2fc163491ac17620022d17", "SEM3021", `fn leak() -> Task<int> {
    let mut m = Map::<string, int>.new();
    let k: string = "k";
    let o = m.get_ref(&k);
    return blocking {
        let got: int = compare o {
            Some(r) => *r;
            nothing => 0;
        };
        ret got;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

var taskCheckTracedArrayControls = []taskCheckProbe{
	{"ctl_re_owned_array_with_no_fixed_storage_returned", "a566bba1966fd4395031a62bc529fdcb5ce9c7c61661e5676ab10acfcc2ff1df", "", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn build() -> int64[] {
    let mut out: int64[] = [];
    out.push(1:int64);
    return out;
}

fn sound() -> Task<int64> {
    let bytes = build();
    let t = first(bytes);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_re_window_over_a_reference_parameter_awaited", "f09aacebc0f43dc40152b716ccd36423c1d421cdebe1aec6770a7c5d56a59b99", "", `async fn first(xs: int64[]) -> int64 {
    return xs[0];
}

fn window(a: &int64[4]) -> Task<int64> {
    let mut v: int64[] = [];
    v = a[[0..2]];
    return first(v);
}

async fn sound() -> int64 {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let t = window(&a);
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
	{"ctl_re_fixed_array_handed_by_value", "ca27dd3dfa24f9db2a86f42c7527f5c0977eb7d0aff711b70936bd5a02e4cdd3", "", `async fn sum4(xs: int64[4]) -> int64 {
    return xs[0] + xs[3];
}

fn sound() -> Task<int64> {
    let a: int64[4] = [1:int64, 2:int64, 3:int64, 4:int64];
    let t = sum4(a);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_re_dynamic_window_with_no_fixed_storage_captured", "9d4f6b68a4f1a3fdc916c65157486f83623ba12597c586488252931e680ae23b", "", `fn sound() -> Task<int64> {
    let d: int64[] = [1:int64, 2:int64, 3:int64, 4:int64];
    let mut v: int64[] = [];
    v = d[[0..2]];
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
	{"ctl_re_cursor_handed_to_a_task_awaited", "c584d0f44bf17908d344bcaa9a0b6a56394719cc541debc27e1c5383d22f7789", "", `async fn walk(r: Range<int64>) -> int64 {
    let mut c = r;
    return compare c.next() {
        Some(v) => v;
        nothing => 0:int64;
    };
}

async fn sound() -> int64 {
    let mut xs: int64[] = [];
    xs.push(1:int64);
    let c = xs.__range();
    let t = walk(c);
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
	{"ctl_rd_carried_reference_parameter_captured", "cf0ec9739cd50c6dfa0962f017564058749a9901e4c3917b1fa0416c363d6af9", "", `fn sound(o: Option<&int>) -> Task<int> {
    return async {
        let got: int = compare o {
            Some(r) => *r;
            nothing => 0;
        };
        ret got;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
	{"ctl_re_traced_cursor_captured_by_blocking_awaited", "9868073a9a3de713dd2052d81013143948cb28ceb56fee7e3cfc02d5c1279e71", "", `async fn sound() -> int64 {
    let mut xs: int64[] = [];
    xs.push(1:int64);
    let c = xs.__range();
    let job: Task<int64> = blocking {
        let mut n: int64 = 0:int64;
        for v: int64 in c { n = n + v; }
        ret n;
    };
    return compare job.await() {
        Success(v) => v;
        Cancelled() => 0:int64;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`},
}

func TestTaskCheckRefusesUntracedArrays(t *testing.T) {
	for _, probe := range taskCheckUntracedArrayLeaks {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q (RV2-DEBT-365, P1u-TC2)", got, probe.want)
			}
		})
	}
}

func TestTaskCheckRefusesCarriedReferenceCaptures(t *testing.T) {
	for _, probe := range taskCheckCarriedReferenceCaptures {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q (RV2-DEBT-365, P1u-TC2)", got, probe.want)
			}
		})
	}
}

func TestTaskCheckKeepsTracedArrayPrograms(t *testing.T) {
	for _, probe := range taskCheckTracedArrayControls {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
