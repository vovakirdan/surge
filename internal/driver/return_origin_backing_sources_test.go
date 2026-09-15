package driver

// Frozen witness sources for the backing transfer (packet R4 §1.4, §7.5). The
// P0 fixtures are read from storageP0Cases by name; every other text is the
// packet's exact bytes and is checked against its SHA256 before it is used.
type backingSource struct {
	name, fixture, text, digest string
	// escape tolerates only root-file SEM3139 in the ordinary bags.
	escape bool
}

// backingW6RDigest freezes the one reviewed replacement for W6: its first 444
// bytes, used only if ordinary admission refuses inner_rebind.
const backingW6RDigest = "516cb778cde56438026940310a6ea5dd8b542156ea2dbc8005894e84f30a67cc"

const backingW1 = `fn both(first: &string, second: &string) -> Option<&string>[2] {
    let mut out: Option<&string>[2] = default::<Option<&string>[2]>();
    out[0] = Some::<&string>(first);
    out[1] = Some::<&string>(second);
    return out;
}
fn looped(first: &string, second: &string) -> Option<&string>[2] {
    let mut out: Option<&string>[2] = default::<Option<&string>[2]>();
    let mut i: int = 0;
    while i < 1 {
        out[0] = Some::<&string>(first);
        out[1] = Some::<&string>(second);
        i = i + 1;
    }
    return out;
}
`

const backingW2 = `fn copied_fixed(value: uint64) -> uint64[] {
    let src: uint64[2] = [value, value];
    return src.to_array();
}
`

const backingW4 = `fn via_binding(xs: &uint64[4]) -> uint64[] {
    let view: uint64[] = xs[[1..3]];
    return view;
}
`

const backingW5 = `fn pass(v: uint64[]) -> uint64[] {
    return v;
}
fn laundered() -> uint64[] {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    return pass(xs[[1..3]]);
}
`

const backingW6 = `fn sub(xs: &uint64[]) -> uint64[] {
    return xs[[1..3]];
}
fn keep_param(xs: &uint64[]) -> uint64[] {
    return sub(xs);
}
fn leak_param() -> uint64[] {
    let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let v: uint64[] = fixed[[0..4]];
    return sub(&v);
}
fn leak_local() -> uint64[] {
    let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let v: uint64[] = fixed[[0..4]];
    return v[[1..3]];
}
fn inner_rebind() -> uint64[] {
    let mut t: uint64[] = [];
    let mut w: uint64[] = [];
    {
        let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
        t = fixed[[0..4]];
        w = sub(&t);
        t = [];
    }
    return w;
}
`

const backingW6F = `fn sub(xs: &uint64[]) -> uint64[] {
    return xs[[1..3]];
}
fn inner_local() -> nothing {
    let mut w: uint64[] = [];
    {
        let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
        let t: uint64[] = fixed[[0..4]];
        w = sub(&t);
    }
    return nothing;
}
`

const backingW7 = `fn cursor_binding(xs: &uint64[]) -> Range<uint64> {
    let c: Range<uint64> = xs.__range();
    return c;
}
`

const backingW8 = `fn alias_formals<T>(dst: &mut Array<T>, src: &Array<T>, v: T) -> T {
    dst.push(v);
    return clone(src[0]);
}
`

const backingW9 = `fn boxed() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    return Some::<uint64[]>(xs[[1..3]]);
}
`

const backingW10 = `fn stash() -> nothing {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut vs: Array<uint64[]> = [];
    vs.push(xs[[1..3]]);
    return nothing;
}
`

const backingW11 = `fn pass(v: uint64[]) -> uint64[] {
    return v;
}
fn launder() -> uint64[] {
    let f = pass;
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    return f(xs[[1..3]]);
}
`

const backingW12 = `fn wrap<T>(x: T) -> Array<T> {
    let mut out: Array<T> = [];
    out.push(x);
    return out;
}
fn joined(first: &string) -> nothing {
    let mut a = wrap::<&string>(first);
    {
        let s: string = "local";
        let b = wrap::<&string>(&s);
        a.extend(&b);
    }
    return nothing;
}
`

const backingW13 = `type Tagged<T> = { n: uint64 };
fn keep<T>(v: Array<Tagged<T>>) -> Array<Tagged<T>> {
    return v;
}
fn through(xs: &Tagged<uint64>[4]) -> Array<Tagged<uint64>> {
    return keep::<uint64>(xs[[1..3]]);
}
`

const backingW14 = `fn id(x: uint64) -> uint64 {
    return x;
}
fn call_load(xs: &uint64[4]) -> uint64 {
    return id(xs[0]);
}
fn cast_load(xs: &uint64[4]) -> int {
    return xs[1] to int;
}
`

const backingW15 = `@intrinsic fn stash<T>(v: T) -> nothing;
fn hide() -> nothing {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    stash::<uint64[]>(xs[[1..3]]);
    return nothing;
}
`

const backingW16S = `fn store_load(xs: &uint64[4]) -> uint64[] {
    let mut out: uint64[] = [0:uint64];
    out[0] = xs[3];
    return out;
}
`

func backingSources() []backingSource {
	return []backingSource{
		{name: "p05_default_writes", fixture: "p05_default_writes", digest: "44421c2bb1a528625fe8cbf28a006dafb5d4ef7eab9a66c44a19df4c6a3d1877"},
		{name: "p06_core_array_paths", fixture: "p06_core_array_paths", digest: "c0f229fc5a4d0b7a7c1c314675944f3d824f6db6739c315acd5fe37f2f9f5dbf"},
		{name: "p07_repeated_site", fixture: "p07_repeated_site", digest: "16e35ae5e78700bac37e5ca2e6cafb513e5bbe08b2c5ae6121aceb5ce8e17097"},
		{name: "p10_view_cursor_lifetime", fixture: "p10_view_cursor_lifetime", digest: "9a4a59d9a9786bf43583c06cd42daa8e5ddc04053693d1ca3aebab8166a628e7"},
		{name: "w1_weak_sites", text: backingW1, digest: "93192d518be5656886c93f7c1a19937a69bbec4871a4617088dac06f270a1de9"},
		{name: "w2_to_array_contents", text: backingW2, digest: "29d8b9e1ee692c765b63ae3a1cecd81b35f64476605bedd87c40060c65b3f3ad", escape: true},
		{name: "w4_view_binding", text: backingW4, digest: "743268cb08367181e8e7cb7d9f67c5879f66de0364c1faa05c81ec870bc7548a"},
		{name: "w5_loan_discard", text: backingW5, digest: "218fbcbab2fd8d219610c48cc31291c5a6e8ecf1a54eb79d50fd5d2d7c7865a2"},
		{name: "w6_view_of_view", text: backingW6, digest: "7a075eed7d85bcdc15a0fda6c8494cdcf51ffc0fd8bca23f652d1e533740efad", escape: true},
		{name: "w6f_inner_local", text: backingW6F, digest: "912053ae9507f6095c1dac97c67d23fb5f6b2c4226517234d95baacfe0674c09", escape: true},
		{name: "w7_cursor_binding", text: backingW7, digest: "152bdfea9679dfea68eebe07f8e9418ba3669a5e0818a5e06c8fde417177a2a0"},
		{name: "w8_alias_formals", text: backingW8, digest: "7db495241d67d4a643e7a082f56dbee804997f88aff1d3b3efb9e37fb5d96598"},
		{name: "w9_constructor_discard", text: backingW9, digest: "e4d6065bd988e868987e41a0cd7de76f960702b769e19a17424c5f0cb97c0411"},
		{name: "w10_push_discard", text: backingW10, digest: "8b276388cd572875c5379478e2eb9510684ffe0686aa5c044c7c58ec1507c7c7"},
		{name: "w11_callable_launder", text: backingW11, digest: "3feadb51dbb994f504f94a5e55cb3481435682d24d60b95270ab15f51872eef2"},
		{name: "w12_extend_union", text: backingW12, digest: "bd2c9fff5d286c668bc83b025e30d348e581e432a28f1b3e47cada3f70291304", escape: true},
		{name: "w13_phantom_floor", text: backingW13, digest: "3965f00fd7029489b51454d4a9b5a6aef20acb66c49c993b6ff2bfbe62aa96ce"},
		{name: "w14_scalar_load", text: backingW14, digest: "31f9de193aa01079c9ed7ae7a05e87a3a0bd0d94d6385637ea45af01c08fc55e"},
		{name: "w15_opaque_generic", text: backingW15, digest: "cbf937246f8d7340b0298302a4cd00b8239a3d9b6c04df8db3bbadf4579cbbc4"},
		{name: "w16s_scalar_store", text: backingW16S, digest: "018ce89e5cdd7397460e498485c68108d43286108777a01b75768cdbb936055c"},
	}
}
