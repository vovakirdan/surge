package sema

import (
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
	Check(builder, fileID, Options{
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
