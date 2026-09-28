package sema

import (
	"testing"

	"surge/internal/diag"
)

// A free function declared without a body is a forward declaration. When it is
// not `@intrinsic` and no `@override` implementation completes it in its module,
// nothing supplies what a call would run, and SEM3224 refuses it.
func TestBodylessFunctionWithoutImplementationIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"plain_function": `fn encode(x: int) -> int;
`,
		"async_function": `async fn fetch() -> int;
`,
		"generic_function": `fn keep<T>(value: T) -> T;
`,
		"override_of_another_signature_does_not_complete": `fn encode(x: int) -> int;
@overload
fn encode(x: string) -> int { return 0; }
`,
	} {
		t.Run(name, func(t *testing.T) {
			bag := runOverloadSource(t, src)
			if !bagHasCode(bag, diag.SemaBodylessFunction) {
				t.Fatalf("body-less declaration was not refused: %s", diagnosticsSummary(bag))
			}
		})
	}
}

// Controls. Function types, function-typed parameters and function values are
// not declarations without a body; a forward declaration completed by
// `@override` is implemented; contract requirements are not functions; and a
// body-less member of an `extern<T>` block is a declaration the language
// allows (LANGUAGE.md §4.4.1), raw pointers included.
func TestBodylessFunctionControlsStayAccepted(t *testing.T) {
	for name, src := range map[string]string{
		"function_type_alias_parameter": `type Foo = fn(int, int) -> int;
fn foo(a: int, b: int) -> int { return a + b; }
fn bar(a: Foo) -> int { return a(1, 2); }
fn probe() -> int { return bar(foo); }
`,
		"function_value_passed_and_called": `fn mul(a: int, b: int) -> int { return a * b; }
fn apply(f: fn(int, int) -> int, x: int, y: int) -> int { return f(x, y); }
fn probe() -> int {
    let g: fn(int, int) -> int = mul;
    return apply(g, 3, 4) + g(5, 6);
}
`,
		"forward_declaration_with_override": `fn encode(x: int) -> int;
@override
fn encode(x: int) -> int { return x + 40; }
`,
		"extern_forward_declaration_with_override": `type Box = { value: int };
extern<Box> {
    fn get(self: &Box) -> int;
    @override fn get(self: &Box) -> int { return self.value; }
}
`,
		"contract_requirement": `contract Sized<T> {
    fn size(self: &T) -> int;
}
`,
		"extern_declaration_named_like_an_intrinsic": `type Box = { value: int };
extern<Box> {
    fn __index(self: &Box, i: int) -> int;
    fn __len(self: &Box) -> uint;
}
`,
		"extern_raw_pointer_declaration": `type C = {};
extern<C> {
    fn memcpy(dst: *uint8, src: *uint8, n: uint) -> nothing;
}
`,
		"extern_generic_declarations": `type Foo<T> = {};
extern<Foo<T>> {
    fn new() -> Foo<T>;
    fn wrap<U>(self: &Foo<T>, f: fn(T) -> U) -> Foo<U>;
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			bag := runOverloadSource(t, src)
			if bagHasCode(bag, diag.SemaBodylessFunction) || bag.HasErrors() {
				t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
			}
		})
	}
}

// An `@override` the resolver refuses (SEM3006) already explains why the
// forward declaration has no implementation; SEM3224 does not repeat it.
func TestBodylessFunctionFailedOverrideReportsOnlyTheOverride(t *testing.T) {
	bag := runSemaOnSnippetTables(t, `fn idf<T>(x: T) -> T;
@override
fn idf<U>(x: U) -> U { return x; }
`).semaBag
	if !bagHasCode(bag, diag.SemaFnOverride) || bagHasCode(bag, diag.SemaBodylessFunction) {
		t.Fatalf("want only the override refusal: %s", diagnosticsSummary(bag))
	}
}
