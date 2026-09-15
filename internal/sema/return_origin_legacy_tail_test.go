package sema

import "testing"

// A brace block that starts with a statement keeps its last `expr;` as its
// value: sema types it so and HIR rewrites it into `ret`, unless it is nothing.
func TestReturnOriginLegacyExprTailIsBlockValue(t *testing.T) {
	for _, tc := range []returnOriginTailCase{
		{name: "expr_tail_block_exit", digest: "24fecbf66951c1b0db768537f64b845b5abaa682d236fd4a4fb50b09c99bc6fe",
			complete: true, text: `pragma no_std;
fn probe() -> int {
    let escaped: &string = {
        let owned: string = "owned";
        &owned;
    };
    return 1;
}
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{109, 116, "&owned;"}, "owned",
				returnOriginTailSpan{72, 100, `let owned: string = "owned";`}}}},
		{name: "expr_tail_value_positive", digest: "b46e3f6bdf36f1cf03f2dedec586875a999d4982745585c643c9987f3982718d",
			complete: true, slots: []uint32{1, 2}, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let chosen: &string = {
        let unused: int = 0;
        a;
    };
    if flag { return chosen; }
    return b;
}
`},
		// Pin: a nothing-typed tail stays a statement, so the block closes at its own span.
		{name: "nothing_tail_stays_statement", digest: "cbc7d6c07b53dfd6fcb2384df40754e3452e94794989534daf8c66f189c995ff",
			contains: true, text: `pragma no_std;
fn touch(value: &string) -> nothing { return nothing; }
fn probe(f: fn() -> &string) -> nothing {
    let alias: &string = f();
    return {
        let unused: int = 0;
        touch(alias);
    };
}
`,
			pending: []returnOriginTailPending{{returnOriginTailSpan{154, 212, "{\n        let unused: int = 0;\n        touch(alias);\n    }"},
				returnOriginTailOutgoingReason}},
			absent: []returnOriginTailPending{{returnOriginTailSpan{193, 206, "touch(alias);"}, returnOriginTailOutgoingReason}}},
		// Pins the fail-closed tightening: a discarded block's legacy tail is checked like `ret`.
		{name: "discarded_tail_escape", digest: "2d54cdff7515b670b0b5c2db7c0214bb026e4313ab5f7a5e9b53797ab87da7c6",
			complete: true, text: `pragma no_std;
fn probe() -> int {
    let _ = {
        let s: string = "x";
        &s;
    };
    return 1;
}
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{86, 89, "&s;"}, "s",
				returnOriginTailSpan{57, 77, `let s: string = "x";`}}}},
	} {
		t.Run(tc.name, func(t *testing.T) { checkReturnOriginTailCase(t, tc) })
	}
}
