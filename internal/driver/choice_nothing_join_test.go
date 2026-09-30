package driver

import (
	"testing"

	"surge/internal/diag"
)

// A ternary joining a union that cannot hold `nothing` with `nothing` is
// refused, as the same compare is: `Success<int>` has no `nothing` case and
// no Option to widen to, and its value on the `nothing` path had no case to
// live in (the VM panicked VM1003).
func TestChoiceOfAUnionWithoutNothingAndNothingIsRefused(t *testing.T) {
	text := `fn pick(c: bool) -> int {
    let e = c ? Success(1) : nothing;
    compare e { Success(v) => { return v; } nothing => { return 0; } };
}
`
	bytesViewRefusal(t, text, diag.SemaTypeMismatch, "nothing;")
}

// `Some<T>` joined with `nothing` is an `Option<T>` in a ternary and a compare.
func TestChoiceOfSomeAndNothingIsAnOption(t *testing.T) {
	for _, text := range []string{`fn pick(c: bool) -> int {
    let e = c ? Some(1) : nothing;
    let o: Option<int> = e;
    return compare o { Some(v) => v; nothing => 0; };
}
`, `fn pick(k: int) -> int {
    let e = compare k { 1 => Some(1); _ => nothing; };
    let o: Option<int> = e;
    return compare o { Some(v) => v; nothing => 0; };
}
`} {
		bytesViewAccepted(t, text)
	}
}
