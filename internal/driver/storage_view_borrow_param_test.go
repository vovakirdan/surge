package driver

import (
	"testing"

	"surge/internal/diag"
)

// An element reference taken through a REFERENCE parameter -- `ident(p[1])`,
// `ident((*p)[1])` with `p: &mut string[]` -- and stored from an inner block
// stands on p's referent: `*p = [...]` frees what the binding reads. Before,
// the index kept no loan (its target is a reference) or kept it keyed on the
// `(*p)` group, and the binding held nothing (valgrind: 2 invalid reads).
//
// Core's `ArrayFixed::to_array` builds an owned copy and is certified by its
// declaration, so it keeps no loan; a user function of the same name that
// returns a window still does.

const storageViewThroughIndex = `fn through(p: &mut string[]) -> int {
    let a = "zz";
    let mut r: &string = &a;
    { r = ident(p[1]); }
    *p = [a + "p", a + "q"];
    return r.__len() to int;
}

`

const storageViewThroughDerefIndex = `fn through(p: &mut string[]) -> int {
    let a = "zz";
    let mut r: &string = &a;
    { r = ident((*p)[1]); }
    *p = [a + "p", a + "q"];
    return r.__len() to int;
}

`

const storageViewUserToArray = `fn to_array(c: &uint64[4]) -> uint64[] {
    return c[[0..2]];
}

`

func TestStorageViewThroughAReferenceParameter(t *testing.T) {
	call := storageViewFixture(`    let mut xs: string[] = [a + "x", a + "y"];
    return through(&mut xs);`)
	t.Run("element_of_a_reference_parameter_through_a_call_in_an_inner_block", func(t *testing.T) {
		refOutwardRefusal(t, storageViewThroughIndex+call, diag.SemaBorrowMutation, "*p = [a")
	})
	t.Run("element_of_a_dereferenced_parameter_through_a_call_in_an_inner_block", func(t *testing.T) {
		refOutwardRefusal(t, storageViewThroughDerefIndex+call, diag.SemaBorrowMutation, "*p = [a")
	})
	t.Run("user_function_named_to_array_returning_a_window", func(t *testing.T) {
		refOutwardRefusal(t, storageViewUserToArray+storageViewFixture(`    let mut xs: Array<uint64[4]> = [];
    xs.push([11:uint64, 22:uint64, 33:uint64, 44:uint64]);
    let v = to_array(&xs[0]);
    xs.push([1:uint64, 2:uint64, 3:uint64, 4:uint64]);
    return v[1] to int;`), diag.SemaBorrowConflict, "xs.push([1:uint64")
	})
}

func TestStorageViewParameterControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"element_of_a_reference_parameter_consumed_in_its_statement", `fn g(p: &mut string[]) -> int {
    let n = ident(p[1]).__len();
    p.push("z");
    return n to int;
}

` + storageViewFixture(`    let mut xs: string[] = [a + "x", a + "y"];
    return g(&mut xs);`)},
		{"window_of_a_reference_binding_to_a_parameter_returned", `fn g(p: &uint64[4]) -> uint64[] {
    let r: &uint64[4] = p;
    return first(r);
}

` + storageViewFixture(`    let c: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let v = g(&c);
    return v[0] to int;`)},
		{"core_to_array_then_mutate_the_fixed_array", storageViewFixture(`    let mut c: int[4] = [1, 2, 3, 4];
    let v = c.to_array();
    c[0] = 100;
    return v[0] + c[0];`)},
		{"core_to_array_of_an_element_then_realloc", storageViewFixture(`    let mut xs: Array<uint64[4]> = [];
    xs.push([11:uint64, 22:uint64, 33:uint64, 44:uint64]);
    let v = xs[0].to_array();
    xs.push([1:uint64, 2:uint64, 3:uint64, 4:uint64]);
    return v[1] to int;`)},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, row.text)
		})
	}
}
