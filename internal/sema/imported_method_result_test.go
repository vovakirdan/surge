package sema

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	untypableMethodPlainHelp = "call it where the receiver's type is concrete"
	untypableMethodUnionHelp = untypableMethodPlainHelp + ", or match on the value with `compare` instead of calling the method"

	// o.get() is 159:166 and the declaration's name get is 90:93.
	untypableUnionMethodSource = `pragma no_std;
tag Some<T>(T);
type Option<T> = Some(T) | nothing;
extern<Option<T>> { fn get(self: Option<T>) -> T; }
fn probe<U>(o: Option<U>) {
    let v = o.get();
}
`
	untypableUnionMethodDigest = "83a2f8441f95e3d79d71f79b7ee725314b2246299f4a0348d9ee2635325a1611"

	// o.touch() is 214:223 (a union receiver) and p.poke() is 253:261 (no type arguments): both reach the fallback.
	methodWithoutResultSource = `pragma no_std;
tag Some<T>(T);
type Option<T> = Some(T) | nothing;
extern<Option<T>> { fn touch(self: Option<T>); }
type Plain = { v: int };
extern<Plain> { fn poke(self: &Plain); }
fn probe<U>(o: Option<U>) {
    o.touch();
}
fn plain(p: &Plain) {
    p.poke();
}
`
	methodWithoutResultDigest = "255b177b1c82f7b8a132363659dada0295b18f482c4a306d718c2ddce6379907"
)

// callWithCopies selects one `read` signature through Builtin table copies of one export, none with a
// magic ID. results[i] is copy i's result over the formal parameter; equal results share one descriptor.
func callWithCopies(t *testing.T, f genericMethodFixture, results []types.TypeID) (got, actual types.TypeID, items []*diag.Diagnostic) {
	t.Helper()
	name, decl := f.in.Strings.Intern("Cell"), source.Span{File: 1, Start: 10, End: 20}
	formalRecv := f.in.RegisterStructInstance(name, decl, []types.TypeID{f.formal})
	actual = f.in.RegisterTypeParam(f.name, 11, 0, false, types.NoTypeID)
	f.tc.typeParamNames[actual] = f.name
	key := symbols.TypeKey("Cell<V>")
	sig := &symbols.FunctionSignature{Params: []symbols.TypeKey{key}, Result: f.tc.typeKeyForType(results[0]), HasSelf: true}
	fns := make(map[types.TypeID]types.TypeID)
	for _, result := range results {
		if fns[result] == types.NoTypeID {
			fns[result] = f.in.RegisterFn([]types.TypeID{formalRecv}, result)
		}
		id := f.tc.symbols.Table.Symbols.New(&symbols.Symbol{
			Name: f.in.Strings.Intern("read"), Kind: symbols.SymbolFunction, Flags: symbols.SymbolFlagBuiltin,
			ReceiverKey: key, Signature: sig, Type: fns[result], TypeParams: []source.StringID{f.name},
		})
		f.tc.result.InstantiationTemplateParams[id] = []types.TypeID{f.formal}
	}
	f.tc.magic[key] = map[string][]*symbols.FunctionSignature{"read": {sig}}
	if f.tc.magicSymbolForSignature(sig).IsValid() {
		t.Fatal("PRECONDITION: copies of one export must carry no magic ID")
	}
	member := &ast.ExprMemberData{Field: f.in.Strings.Intern("read")}
	recv := f.in.RegisterStructInstance(name, decl, []types.TypeID{actual})
	got = f.tc.methodResultType(member, recv, ast.NoExprID, nil, nil, source.Span{}, false)
	return got, actual, f.tc.reporter.(*diag.BagReporter).Bag.Items()
}

// Two copies of one export type the call exactly as one copy does; one copy is the precondition.
func TestSelectedMethodResultCountsCopiesOfOneExportOnce(t *testing.T) {
	for _, copies := range []int{1, 2} {
		t.Run(fmt.Sprintf("copies_%d", copies), func(t *testing.T) {
			f := newGenericMethodFixture()
			option := genericMethodOption(f.in, f.formal)
			got, actual, items := callWithCopies(t, f, []types.TypeID{option, option}[:copies])
			if len(items) != 0 {
				t.Errorf("unexpected diagnostics: %+v", items)
			}
			assertGenericMethodOption(t, f.in, got, actual)
		})
	}
}

// A synthetic guard (production never gives two declarations one pointer): each copy alone types the call.
func TestSelectedMethodResultRefusesDisagreeingCopies(t *testing.T) {
	for i, alone := range []string{"option_copy_alone", "param_copy_alone"} {
		t.Run(alone, func(t *testing.T) {
			f := newGenericMethodFixture()
			results := []types.TypeID{genericMethodOption(f.in, f.formal), f.formal}
			got, actual, items := callWithCopies(t, f, results[i:i+1])
			if len(items) != 0 || (i == 1 && got != actual) {
				t.Fatalf("PRECONDITION: copy %d alone: result=%d diagnostics=%+v", i, got, items)
			}
			if i == 0 {
				assertGenericMethodOption(t, f.in, got, actual)
			}
		})
	}
	f := newGenericMethodFixture()
	results := []types.TypeID{genericMethodOption(f.in, f.formal), f.formal}
	key := "`" + string(f.tc.typeKeyForType(results[0])) + "`"
	got, _, items := callWithCopies(t, f, results)
	if got != types.NoTypeID || len(items) != 1 || items[0].Code != diag.SemaTypeMismatch ||
		!strings.Contains(items[0].Message, "`read`") || !strings.Contains(items[0].Message, key) {
		t.Fatalf("disagreeing copies: result=%d diagnostics=%+v", got, items)
	}
	if d := items[0]; len(d.Help) != 1 || d.Help[0].Msg != untypableMethodPlainHelp || len(d.Notes) != 0 {
		t.Errorf("a struct receiver's refusal needs the plain help and no declaration note: %+v", d)
	}
}

// An untypable selected result is refused in the caller's spelling, with a way out and the declaration.
func TestUntypableMethodResultReportsWithHelp(t *testing.T) {
	if untypableUnionMethodSource[159:166] != "o.get()" || untypableUnionMethodSource[90:93] != "get" {
		t.Fatal("PRECONDITION: frozen spans moved")
	}
	_, _, items := checkFrozenMethodSnippet(t, untypableUnionMethodSource, untypableUnionMethodDigest)
	if len(items) != 1 || items[0].Code != diag.SemaTypeMismatch || items[0].Primary.Start != 159 || items[0].Primary.End != 166 ||
		!strings.Contains(items[0].Message, "`get`") || !strings.Contains(items[0].Message, "`U`") || strings.Contains(items[0].Message, "`T`") {
		t.Fatalf("untypable union result at 159:166: %+v", items)
	}
	d := items[0]
	if len(d.Help) != 1 || d.Help[0].Msg != untypableMethodUnionHelp {
		t.Errorf("a union receiver's refusal needs the compare help: %+v", d.Help)
	}
	if len(d.Notes) != 1 || d.Notes[0].Msg != "declared here" || d.Notes[0].Span.Start != 90 || d.Notes[0].Span.End != 93 {
		t.Errorf("the refusal needs a declaration note at 90:93: %+v", d.Notes)
	}
}

// A method with no declared result is never refused: the parser gives it the result nothing.
func TestMethodWithoutDeclaredResultStaysQuiet(t *testing.T) {
	if methodWithoutResultSource[214:223] != "o.touch()" || methodWithoutResultSource[253:261] != "p.poke()" {
		t.Fatal("PRECONDITION: frozen spans moved")
	}
	b, res, items := checkFrozenMethodSnippet(t, methodWithoutResultSource, methodWithoutResultDigest)
	typed := 0
	for id, ty := range res.ExprTypes {
		if e := b.Exprs.Get(id); e != nil && e.Kind == ast.ExprCall && ty == res.TypeInterner.Builtins().Nothing &&
			(e.Span.Start == 214 && e.Span.End == 223 || e.Span.Start == 253 && e.Span.End == 261) {
			typed++
		}
	}
	if len(items) != 0 || typed != 2 {
		t.Fatalf("a method without a declared result: %d of two calls typed nothing, refusals %+v", typed, items)
	}
}

// checkFrozenMethodSnippet parses and checks a frozen source: its builder, result and error diagnostics.
func checkFrozenMethodSnippet(t *testing.T, text, digest string) (*ast.Builder, *Result, []*diag.Diagnostic) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	b, file, parseBag := parseSnippet(t, text)
	if parseBag.Len() != 0 {
		t.Fatalf("PRECONDITION: parse: %s", diagnosticsSummary(parseBag))
	}
	res, _, bag := checkWithSymbols(t, b, file)
	var refusals []*diag.Diagnostic
	for _, d := range bag.Items() {
		if d.Severity >= diag.SevError {
			refusals = append(refusals, d)
		}
	}
	return b, res, refusals
}
