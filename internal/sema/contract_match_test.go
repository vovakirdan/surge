package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestContractMatching_Positive(t *testing.T) {
	src := `
contract FooLike<T> {
    field bar: int;
    fn get(self: T) -> int;
}

type Foo = { bar: int }

extern<Foo> {
    fn get(self: Foo) -> int;
}
`
	tc, bag, syms := newContractChecker(t, src)

	fooID := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Foo"))
	contractID := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("FooLike"))
	if !fooID.IsValid() || !contractID.IsValid() {
		t.Fatalf("symbols not found")
	}
	fooSym := syms.Table.Symbols.Get(fooID)
	contractSym := syms.Table.Symbols.Get(contractID)
	args := []types.TypeID{fooSym.Type}
	bound := symbols.BoundInstance{
		Contract:    contractID,
		GenericArgs: args,
		Span:        contractSym.Span,
	}

	if !tc.checkContractSatisfaction(fooSym.Type, bound, fooSym.Span, "") {
		t.Fatalf("expected contract to be satisfied")
	}
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestContractMatching_Negative(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code diag.Code
	}{
		{
			name: "MissingField",
			src: `
contract C { field value: int; }
type Foo = { }
`,
			code: diag.SemaContractMissingField,
		},
		{
			name: "FieldTypeMismatch",
			src: `
contract C { field value: string; }
type Foo = { value: int }
`,
			code: diag.SemaContractFieldTypeError,
		},
		{
			name: "MissingMethod",
			src: `
contract C<T> { fn touch(self: T) -> int; }
type Foo = { }
`,
			code: diag.SemaContractMissingMethod,
		},
		{
			name: "MethodSignatureMismatch",
			src: `
contract C<T> { fn touch(self: T, other: int) -> int; }
type Foo = { }
extern<Foo> { fn touch(self: Foo) -> int; }
`,
			code: diag.SemaContractMethodMismatch,
		},
		{
			name: "SelfTypeMismatch",
			src: `
contract C { fn touch(self: int); }
type Foo = { }
extern<Foo> { fn touch(self: Foo); }
`,
			code: diag.SemaContractSelfType,
		},
		{
			name: "MissingOverload",
			src: `
contract C<T> {
    fn touch(self: T) -> int;
    @overload fn touch(self: T, other: int) -> int;
}
type Foo = { }
extern<Foo> { fn touch(self: Foo) -> int; }
`,
			code: diag.SemaContractMethodMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc, bag, syms := newContractChecker(t, tt.src)
			fooID := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Foo"))
			contractID := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("C"))
			if !fooID.IsValid() || !contractID.IsValid() {
				t.Fatalf("symbols not found")
			}
			fooSym := syms.Table.Symbols.Get(fooID)
			contractSym := syms.Table.Symbols.Get(contractID)
			args := []types.TypeID{}
			if len(contractSym.TypeParams) > 0 {
				args = []types.TypeID{fooSym.Type}
			}
			bound := symbols.BoundInstance{
				Contract:    contractID,
				GenericArgs: args,
				Span:        contractSym.Span,
			}
			tc.checkContractSatisfaction(fooSym.Type, bound, contractSym.Span, "")
			if !hasCodeContract(bag, tt.code) {
				t.Fatalf("expected diagnostic %v, got %s", tt.code, diagnosticsSummary(bag))
			}
		})
	}
}

func newContractChecker(t *testing.T, src string) (*typeChecker, *diag.Bag, *symbols.Result) {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)

	typeInterner := types.NewInterner()
	semaBag := diag.NewBag(64)
	res := Result{
		TypeInterner: typeInterner,
		ExprTypes:    make(map[ast.ExprID]types.TypeID),
		ExprBorrows:  make(map[ast.ExprID]BorrowID),
	}

	tc := &typeChecker{
		builder:  builder,
		fileID:   fileID,
		reporter: &diag.BagReporter{Bag: semaBag},
		symbols:  symRes,
		result:   &res,
		types:    typeInterner,
	}
	tc.run()
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}

	contractBag := diag.NewBag(8)
	tc.reporter = &diag.BagReporter{Bag: contractBag}
	return tc, contractBag, symRes
}

func TestContractMatching_CallUsesConcreteTypeInDiag(t *testing.T) {
	src := `
contract ErrorLike {
    field msg: string;
    field code: uint;
}

type Error0 = { msg: string; }

fn print_err<E: ErrorLike>(e: E) {}

fn main() {
    let e0: Error0 = { msg: "error" };
    print_err(e0);
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(8)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})

	found := false
	for _, d := range bag.Items() {
		if d.Code != diag.SemaContractMissingField {
			continue
		}
		if strings.Contains(d.Message, "Error0") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected contract diagnostic to mention concrete type Error0, got %s", diagnosticsSummary(bag))
	}
}

func TestTypeParamBoundsExposeContractFields(t *testing.T) {
	src := `
contract ErrorLike {
    field msg: string;
}

fn print_err<E: ErrorLike>(e: E) {
    let _ = e.msg;
}
`
	tc, _, _ := newContractChecker(t, src)
	var id types.TypeID
	for tid, name := range tc.typeParamNames {
		if tc.lookupName(name) == "E" {
			id = tid
			break
		}
	}
	if id == types.NoTypeID {
		t.Fatalf("type param E not registered")
	}
	info, ok := tc.result.TypeInterner.TypeParamInfo(id)
	if !ok || info == nil || info.Owner == 0 {
		t.Fatalf("type param info missing owner: %+v", info)
	}
	bounds := tc.typeParamContractBounds(id)
	if len(bounds) == 0 {
		t.Fatalf("type param bounds missing for E")
	}
	if ty := tc.boundFieldType(id, tc.builder.StringsInterner.Intern("msg")); ty == types.NoTypeID {
		t.Fatalf("expected bound field type for msg")
	}
}

func TestContractsPositiveSample(t *testing.T) {
	src := `// Valid contract declarations demonstrating the new 'contract' syntax.

contract ErrorLike{
    field msg: string;
    field code: uint;
}

pub contract Hashable<T>{
    pub fn hash(self: T) -> uint;
}

type Error0 = {
    msg: string;
}

type Error1 = {
    msg: string;
    code: uint;
}

type Error2 = {
    msg: string;
    code: uint;
    name: string;
}

// stub for test (this test runs without stdlib)
fn print(s: string) {
    return nothing;
}

fn print_err<E: ErrorLike>(e: E) {
    print(e.msg);
}

fn foo<H: Hashable<H>>(h: H) -> uint {
    return h.hash();
}

fn bar(e: Error0) -> nothing {
    print(e.msg);
}

fn main() {
    let e0: Error0 = { msg: "error" };
    let e1: Error1 = { msg: "error", code: 1 to uint };
    let e2: Error2 = { msg: "error", code: 1 to uint, name: "error" };
    // print_err(e0);
    print_err(e1);
    print_err(e2);
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(32)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if semaBag.HasErrors() {
		t.Fatalf("unexpected semantic diagnostics: %s", diagnosticsSummary(semaBag))
	}
}

func TestTypeInstantiationEnforcesContractBounds(t *testing.T) {
	src := `
contract HasBar<T> {
    field bar: string;
}

type Missing = { baz: int }
type Box<T: HasBar<T>> = { value: T }

fn demo(value: Missing) {
    let _ : Box<Missing> = { value: value };
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(8)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if !hasCodeContract(bag, diag.SemaContractMissingField) {
		t.Fatalf("expected contract satisfaction error, got %s", diagnosticsSummary(bag))
	}
}

func TestTypeInstantiationChecksNestedArgs(t *testing.T) {
	src := `
contract Eq<T> {
    fn eq(self: T, other: T) -> bool;
}

type Pair<A: Eq<A>, B: Eq<B>> = { a: A, b: B }

type Good = {}
extern<Good> {
    fn eq(self: Good, other: Good) -> bool { return true; }
}

type Bad = {}

fn demo() {
    let good: Pair<Good, Good>;
    let bad: Pair<Good, Bad>;
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(8)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if !hasCodeContract(bag, diag.SemaContractMissingMethod) {
		t.Fatalf("expected contract diagnostic for nested args, got %s", diagnosticsSummary(bag))
	}
}

func TestContractMatching_ShortFormFunctionCall(t *testing.T) {
	src := `
type Foo = { bar: string }

extern<Foo> {
    fn Bar(self: Foo) -> string { return self.bar; }
}

contract FooLike<T> {
    field bar: string;
    fn Bar(self: T) -> string;
}

fn join<T: FooLike>(x: T) -> string {
    let _ = x.bar;
    return x.Bar();
}

fn demo() {
    let _ = join(Foo{ bar: "ok" });
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(8)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if bag.HasErrors() {
		t.Fatalf("unexpected semantic diagnostics: %s", diagnosticsSummary(bag))
	}
}
