package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestBoundsSemantics_AttachesBoundsToFunctionSymbol(t *testing.T) {
	src := `
contract A{}
contract B<T>{
    fn use(self: T);
}

fn f<T: A + B<T>>(value: T);
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}

	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(8)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}

	fnSym := lookupSymbolByName(symRes, builder.StringsInterner.Intern("f"))
	if !fnSym.IsValid() {
		t.Fatalf("function symbol not found")
	}
	contractASym := lookupSymbolByName(symRes, builder.StringsInterner.Intern("A"))
	contractBSym := lookupSymbolByName(symRes, builder.StringsInterner.Intern("B"))
	fn := symRes.Table.Symbols.Get(fnSym)
	if fn == nil {
		t.Fatalf("function symbol missing from table")
	}
	if len(fn.TypeParamSymbols) != 1 {
		t.Fatalf("expected 1 type parameter, got %d", len(fn.TypeParamSymbols))
	}
	bounds := fn.TypeParamSymbols[0].Bounds
	if len(bounds) != 2 {
		t.Fatalf("expected 2 bounds, got %d", len(bounds))
	}
	if bounds[0].Contract != contractASym {
		t.Fatalf("expected first bound to reference contract A")
	}
	if bounds[1].Contract != contractBSym {
		t.Fatalf("expected second bound to reference contract B")
	}
	if len(bounds[1].GenericArgs) != 1 {
		t.Fatalf("expected B bound to carry one generic arg, got %d", len(bounds[1].GenericArgs))
	}
	arg := bounds[1].GenericArgs[0]
	info, ok := res.TypeInterner.TypeParamInfo(arg)
	if !ok || info == nil {
		t.Fatalf("expected generic arg to resolve to a type param, got %v", arg)
	}
	if info.Name != builder.StringsInterner.Intern("T") {
		t.Fatalf("expected generic arg to bind to 'T'")
	}
}

func TestBoundsSemantics_AttachesBoundsToTypeSymbol(t *testing.T) {
	src := `
contract C<T>{
    fn take(self: T);
}

type Box<T: C<int>> = {};
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(8)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}

	typeSym := lookupSymbolByName(symRes, builder.StringsInterner.Intern("Box"))
	contractSym := lookupSymbolByName(symRes, builder.StringsInterner.Intern("C"))
	sym := symRes.Table.Symbols.Get(typeSym)
	if sym == nil {
		t.Fatalf("type symbol not found")
	}
	if len(sym.TypeParamSymbols) != 1 {
		t.Fatalf("expected one type param symbol, got %d", len(sym.TypeParamSymbols))
	}
	bounds := sym.TypeParamSymbols[0].Bounds
	if len(bounds) != 1 {
		t.Fatalf("expected one bound, got %d", len(bounds))
	}
	if bounds[0].Contract != contractSym {
		t.Fatalf("expected bound to reference contract C")
	}
	if len(bounds[0].GenericArgs) != 1 {
		t.Fatalf("expected bound to capture one type arg, got %d", len(bounds[0].GenericArgs))
	}
	argType := bounds[0].GenericArgs[0]
	if tt, ok := res.TypeInterner.Lookup(argType); !ok || tt.Kind != types.KindInt {
		t.Fatalf("expected bound arg to resolve to builtin int, got %v", argType)
	}
}

func TestBoundsSemantics_ShorthandAddsImplicitTypeArg(t *testing.T) {
	src := `
contract FooLike<T>{
    fn use(self: T);
}

fn f<T: FooLike>(value: T);
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(8)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}

	fnSym := lookupSymbolByName(symRes, builder.StringsInterner.Intern("f"))
	if !fnSym.IsValid() {
		t.Fatalf("function symbol not found")
	}
	fn := symRes.Table.Symbols.Get(fnSym)
	if fn == nil || len(fn.TypeParamSymbols) != 1 {
		t.Fatalf("expected one type param symbol on function, got %d", len(fn.TypeParamSymbols))
	}
	bounds := fn.TypeParamSymbols[0].Bounds
	if len(bounds) != 1 {
		t.Fatalf("expected one bound, got %d", len(bounds))
	}
	if len(bounds[0].GenericArgs) != 1 {
		t.Fatalf("expected shorthand bound to inject one generic arg, got %d", len(bounds[0].GenericArgs))
	}
	arg := bounds[0].GenericArgs[0]
	info, ok := res.TypeInterner.TypeParamInfo(arg)
	if !ok || info == nil {
		t.Fatalf("missing type param info for implicit arg: %v", arg)
	}
	if info.Name != builder.StringsInterner.Intern("T") {
		t.Fatalf("expected implicit arg to point to type param T, got %v", builder.StringsInterner.MustLookup(info.Name))
	}
}

func TestBoundsSemantics_MultiParamRequiresLongForm(t *testing.T) {
	src := `
contract Mix<A, B>{
    fn mix(self: A, other: B);
}

fn f<T: Mix<T, int>>(value: T);
`
	bag := runBoundsSema(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestBoundsSemantics_Errors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code diag.Code
	}{
		{
			name: "UnknownContract",
			src: `
fn f<T: Missing>();`,
			code: diag.SemaContractBoundNotFound,
		},
		{
			name: "NotAContract",
			src: `
fn f<T: int>();`,
			code: diag.SemaContractBoundNotContract,
		},
		{
			name: "DuplicateContract",
			src: `
contract A{}
fn f<T: A + A>();`,
			code: diag.SemaContractBoundDuplicate,
		},
		{
			name: "UnknownTypeArg",
			src: `
contract A<T>{
    fn ensure(self: T);
}
fn f<T: A<Missing>>();`,
			code: diag.SemaContractBoundTypeError,
		},
		{
			name: "MissingArgsForMultiParamContract",
			src: `
contract C<T, U>{
    fn use(self: T, other: U);
}
fn f<T: C>(value: T);`,
			code: diag.SemaTypeMismatch,
		},
		{
			name: "WrongArityForContractArgs",
			src: `
contract C<T, U>{
    fn use(self: T, other: U);
}
fn f<T: C<T>>(value: T);`,
			code: diag.SemaTypeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bag := runBoundsSema(t, tt.src)
			if !hasCodeContract(bag, tt.code) {
				t.Fatalf("expected diagnostic %v, got %s", tt.code, diagnosticsSummary(bag))
			}
		})
	}
}

func runBoundsSema(t *testing.T, src string) *diag.Bag {
	t.Helper()
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(16)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	return semaBag
}

func lookupSymbolByName(res *symbols.Result, name source.StringID) symbols.SymbolID {
	if res == nil || res.Table == nil || res.Table.Scopes == nil {
		return symbols.NoSymbolID
	}
	scope := res.Table.Scopes.Get(res.FileScope)
	if scope == nil {
		return symbols.NoSymbolID
	}
	ids := scope.NameIndex[name]
	if len(ids) == 0 {
		return symbols.NoSymbolID
	}
	return ids[len(ids)-1]
}
