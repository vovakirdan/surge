package sema

import "testing"

// A function return refused with SEM3139 is a finished refusal, not an unproved
// source; Unknown roots and callers of a refused body still keep their Pending.
func TestReturnOriginRefusedResultCompletes(t *testing.T) {
	for _, tc := range []returnOriginTailCase{
		{name: "owned_parameter_storage", digest: "1e7aedfc8bd9e2bf2979ddbefa8d44b30e4fd9cec1f0da0b521254c7c93c8633",
			allowOldEscape: true, complete: true, unknown: true, text: `pragma no_std;
fn probe(owned: string) -> &string { return &owned; }
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{52, 66, "return &owned;"}, "owned",
				returnOriginTailSpan{24, 37, "owned: string"}}}},
		// Pin: an Unknown root next to the refused one keeps the function-result obligation.
		{name: "unknown_root_stays_pending", digest: "79c98066ec0e1507d98b1bef4bc9b3d335163a5cddaa37700f978ae9e2ef9c22",
			allowOldEscape: true, contains: true, unknown: true, text: `pragma no_std;
fn probe(flag: bool, f: fn() -> &string) -> &string {
    let owned: string = "owned";
    if flag { return &owned; }
    return f();
}
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{116, 130, "return &owned;"}, "owned",
				returnOriginTailSpan{73, 101, `let owned: string = "owned";`}}},
			pending: []returnOriginTailPending{{returnOriginTailSpan{56, 66, "-> &string"}, returnOriginTailResultReason}}},
		{name: "implicit_owned_argument", digest: "7cd5de45b04781fd58677101e988f7944b7fafb84ba9beb5776f38535c081fa0",
			allowOldEscape: true, complete: true, unknown: true, text: `fn same(value: &string) -> &string { return value; }
fn probe(owned: string) -> &string { return same(owned); }`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{90, 109, "return same(owned);"}, "owned",
				returnOriginTailSpan{62, 75, "owned: string"}}}},
		// Pin: a caller of a refused body still pends on the callee's Unknown summary.
		{name: "refused_callee_caller", digest: "ebbc17a65c4135c11451360c7bb41052ac6d7119c41bf5b85b6c879ac46ca498",
			allowOldEscape: true, text: `pragma no_std;
fn make_local() -> &string {
    let l: string = "x";
    return &l;
}
fn probe() -> int {
    let r: &string = make_local();
    return 1;
}
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{73, 83, "return &l;"}, "l",
				returnOriginTailSpan{48, 68, `let l: string = "x";`}}},
			pending: []returnOriginTailPending{{returnOriginTailSpan{127, 139, "make_local()"}, "callee returned an unproved source"}}},
	} {
		t.Run(tc.name, func(t *testing.T) { checkReturnOriginTailCase(t, tc) })
	}
}
