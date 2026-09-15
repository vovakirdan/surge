package sema

import "testing"

// A store into a reference-free place with a reference-free value changes no
// reference-bearing contents, so it needs no taint; other places keep it.
func TestReturnOriginReferenceFreePlaceStore(t *testing.T) {
	for _, tc := range []returnOriginTailCase{
		{name: "local_field_store", digest: "0c0c9d3b3d3ca4e21a0b8e8bf9109ae98478e24d2be2975865769e9d763c8386",
			complete: true, text: `pragma no_std;
type Counter = { count: uint, gen: uint };
fn probe() -> uint {
    let mut c: Counter = Counter { count = 0:uint, gen = 0:uint };
    c.count = c.count + 1:uint;
    c.gen = 2:uint;
    return c.count;
}
`},
		{name: "mut_param_field_store", digest: "ef5bc1f52789e37f2e3da6dda8e040324cc6b3f255272856417c402b4e0f3409",
			complete: true, text: `pragma no_std;
type Counter = { count: uint, gen: uint };
fn probe(p: &mut Counter) -> nothing {
    p.count = p.count + 1:uint;
    return nothing;
}
`},
		// Control: a callable field has no proven shape, so its store keeps the taint.
		{name: "callable_field_control", digest: "2216c607e184ddaae821031fe8f61f5c396d503d8792a956a00066044c774ef2",
			contains: true, text: `pragma no_std;
type Hook = { run: fn(int) -> int };
fn twice(x: int) -> int { return x + x; }
fn probe() -> int {
    let mut h: Hook = Hook { run = twice };
    h.run = twice;
    return 1;
}
`,
			pending: []returnOriginTailPending{{returnOriginTailSpan{162, 175, "h.run = twice"}, returnOriginTailStoreReason}}},
	} {
		t.Run(tc.name, func(t *testing.T) { checkReturnOriginTailCase(t, tc) })
	}
}
