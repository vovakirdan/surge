package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type genericP0Case struct {
	name, text, digest, subject              string
	templates, edges, roots, instances, uses int
	generator, pattern                       bool
}

const genericP0Relay = `fn run<T>(g: fn() -> T) -> T { return g(); }
fn relay<U>(g: fn() -> U) -> U { return run::<U>(g); }
`
const genericP0Owned = `fn make() -> string { return "x"; }
fn probe() -> string { return relay::<string>(make); }
`
const genericP0Option = `tag Carry<T>(T);
type Maybe<T> = Carry(T) | nothing;
`

func genericP0Cases() []genericP0Case {
	return []genericP0Case{
		{name: "unused_relay", digest: "c1eb6c7a274365796391eb9dc4e04b4b1917c62af043969eb1af422f7380e9b4", text: genericP0Relay, subject: "run", templates: 1, edges: 1, generator: true},
		{name: "owned_relay", digest: "f8264078da9ad8c3c8e89efeabe7c3453fa9b3892638b98590f28336f5a6b75f", text: genericP0Relay + genericP0Owned, subject: "run", templates: 1, edges: 1, roots: 1, instances: 2, uses: 2, generator: true},
		{name: "discarded_conditional", digest: "dc1220bd25e49c8586762489bcea41e454792d8b6be79bc574ced99df9917fa0", subject: "use", templates: 1, roots: 1, instances: 1, uses: 1, generator: true, text: `fn use<T>(g: fn() -> T, flag: bool) -> nothing {
    if flag { let _ = g(); }
    return nothing;
}
fn probe(g: fn() -> string, flag: bool) -> nothing { use::<string>(g, flag); }
`},
		{name: "recursive_relay", digest: "652a2f4117a5c8211f5685ce7ef3990d28c1d3fc95dee7b15cdbdac79baec8b3", subject: "run", templates: 1, edges: 2, roots: 1, instances: 2, uses: 3, generator: true, text: `fn run<T>(g: fn() -> T) -> T { return g(); }
fn relay<U>(g: fn() -> U, again: bool) -> U {
    if again { return relay::<U>(g, false); }
    return run::<U>(g);
}
fn make() -> string { return "x"; }
fn probe() -> string { return relay::<string>(make, false); }
`},
		{name: "local_escape_in_template", digest: "f7ab874dd0706e023cf4ce068d043d914cc33c4e7e6850c38789eeb3b5c04b82", subject: "run", templates: 1, generator: true, text: `fn run<T>(g: fn() -> T) -> T {
    let escaped = { let owned: int64 = 7; ret &owned; };
    return g();
}
`},
		{name: "opaque_borrowed_result", digest: "3af0e79b3337cd3c94621b9b6f306f0470826577ece2983f5b06250ab0480357", text: genericP0Relay + `fn probe(g: fn() -> &string) -> &string { return relay::<&string>(g); }
`, subject: "run", templates: 1, edges: 1, roots: 1, instances: 2, uses: 2, generator: true},
		{name: "known_nothing", digest: "cb7de55a5d0765dd247ea4b78b4b6ede8b529580d74c0acaa2b0db946fc0d532", subject: "none", text: genericP0Option + `fn none() -> Maybe<&string> { return nothing; }
fn probe() -> Maybe<&string> { return none(); }
`},
		{name: "known_noreturn", digest: "dbf34a486ce697aff6b0ff96b147304bac7c2a71a6f8f39242a6aa6894d1e9c0", subject: "never", text: `fn never() -> &string { return never(); }
fn probe() -> &string { return never(); }
`},
		{name: "nested_option_payload", digest: "53962029a90fd6651286b85390fb8b31f8539db2eecfad19b41e86088b317ba7", subject: "project", templates: 1, pattern: true, text: genericP0Option + `fn project<T>(value: Maybe<T>, fallback: T) -> T {
    return compare value { Carry(v) => v; _ => fallback; };
}
`},
		{name: "nested_either_payload", digest: "8c48e08590c6a0eac2c8e48af61e14eb11e4e04a75a364d9734cb356b50354d8", subject: "project", templates: 2, pattern: true, text: `tag Good<T>(T);
tag Bad<E>(E);
type Either<T, E> = Good(T) | Bad(E);
fn project<T, E>(value: Either<T, E>, fallback: T) -> T {
    return compare value { Good(v) => v; _ => fallback; };
}
`},
		{name: "pattern_storage_escape", digest: "b13ab1c04987fec807f3167649129a20e776916cc7704e1b3d04eb0a52a1ec38", subject: "arm", templates: 1, pattern: true, text: genericP0Option + `fn arm<T>(value: Maybe<T>, fallback: &T) -> int64 {
    let leaked = compare value { Carry(v) => { ret &v; } _ => fallback; };
    return 1;
}
`},
	}
}

// P0 passes only source admission and a complete observation. Current origin
// Pending/errors are captured, not accepted as a permanent semantic oracle.
func TestCaptureGenericConditionsP0(t *testing.T) {
	for _, tc := range genericP0Cases() {
		t.Run(tc.name, func(t *testing.T) {
			admitted := false
			t.Cleanup(func() {
				if !admitted {
					t.Log("PRECONDITION: P0 admission/metadata did not complete; no semantic outcome claimed")
				}
			})
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text)))
			if tc.digest != got {
				t.Fatalf("PRECONDITION: frozen source changed: %s", got)
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_p0_source", "case": tc.name, "source": tc.text, "sha256": got})
			fixture := originalGenericSignatureFixture(t, tc.text, false, false)
			res, unit := fixture.owner, fixture.unit
			if string(res.File.Content) != tc.text || len(fixture.inputs.units) != 1 || res.Bag.HasErrors() {
				t.Fatal("PRECONDITION: strict owning source admission changed")
			}
			caller := originalGenericSignatureCandidate(t, res, fixture.authority, unit, tc.subject)
			if !caller.HasBody || len(caller.TemplateParams) != tc.templates {
				t.Fatal("PRECONDITION: selected original body/template arity changed")
			}
			genericP0Graph(t, tc, fixture)
			switch {
			case tc.generator:
				genericP0Generator(t, res, caller)
			case tc.pattern:
				genericP0Pattern(t, tc, res, caller)
			case tc.name == "known_nothing":
				id := genericP0Expression(t, res, "return nothing;", "nothing")
				literal, ok := res.Builder.Exprs.Literal(id)
				info, hasUnion := res.Sema.TypeInterner.UnionInfo(caller.ResultType)
				if !ok || literal.Kind != ast.ExprLitNothing || res.Sema.ExprTypes[id] != res.Sema.TypeInterner.Builtins().Nothing ||
					!hasUnion || info == nil || len(info.TypeArgs) != 1 || len(info.Members) != 2 {
					t.Fatal("PRECONDITION: known nothing is not an actual empty value in a reference-bearing union")
				}
				typ, present := res.Sema.TypeInterner.Lookup(info.TypeArgs[0])
				if !present || typ.Kind != types.KindReference || typ.Elem != res.Sema.TypeInterner.Builtins().String {
					t.Fatal("PRECONDITION: optional payload lost its borrowed string descriptor")
				}
			case tc.name == "known_noreturn":
				id := genericP0Expression(t, res, "return never(); }\nfn probe", "never()")
				if res.Symbols.ExprSymbols[id] != originalGenericSignatureLocal(t, unit, caller) ||
					res.Sema.ExprTypes[id] != caller.ResultType {
					t.Fatal("PRECONDITION: nonreturn candidate lost its exact recursive source call")
				}
			}
			if tc.name == "local_escape_in_template" || tc.name == "pattern_storage_escape" {
				fragment, address := "ret &owned;", "&owned"
				if tc.pattern {
					fragment, address = "ret &v;", "&v"
				}
				id := genericP0Expression(t, res, fragment, address)
				typ, present := res.Sema.TypeInterner.Lookup(res.Sema.ExprTypes[id])
				if !present || typ.Kind != types.KindReference {
					t.Fatal("PRECONDITION: source escape address is not a typed borrow")
				}
			}
			genericP0Evidence(t, tc, fixture)
			admitted = true
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), fixture.authority, fixture.inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_p0_observed", "case": tc.name,
				"capture_complete": true, "admission_complete": true, "analysis": analysis, "analysis_error": errorReturnOriginCallText(err),
				"analysis_present": analysis != nil, "semantic_complete": analysis != nil && err == nil && analysis.Complete()})
			if tc.name == "owned_relay" {
				genericP0InnerAuthority(t, fixture)
			}
		})
	}
}

func genericP0Expression(t *testing.T, res *DiagnoseResult, fragment, expression string) ast.ExprID {
	t.Helper()
	text := string(res.File.Content)
	start, offset := strings.Index(text, fragment), strings.Index(fragment, expression)
	if start < 0 || offset < 0 || strings.LastIndex(text, fragment) != start {
		t.Fatal("PRECONDITION: exact operation fragment is not unique")
	}
	span := source.Span{File: res.File.ID, Start: uint32(start + offset), End: uint32(start + offset + len(expression))}
	var found ast.ExprID
	for id, typ := range res.Sema.ExprTypes {
		if node := res.Builder.Exprs.Get(id); node != nil && node.Span == span {
			if found.IsValid() || typ == types.NoTypeID {
				t.Fatal("PRECONDITION: operation lacks one nonzero typed expression")
			}
			found = id
		}
	}
	if !found.IsValid() {
		t.Fatalf("PRECONDITION: missing typed operation %q at %+v", expression, span)
	}
	return found
}

func genericP0Generator(t *testing.T, res *DiagnoseResult, caller sema.CallableCandidate) {
	t.Helper()
	id := genericP0Expression(t, res, "g()", "g()")
	call, ok := res.Builder.Exprs.Call(id)
	if !ok || call == nil || len(call.Args) != 0 || len(caller.TemplateParams) != 1 || len(caller.ParamTypes) == 0 {
		t.Fatal("PRECONDITION: generator is not the original zero-input callable parameter")
	}
	target := res.Symbols.ExprSymbols[call.Target]
	sym := res.Symbols.Table.Symbols.Get(target)
	info, hasFn := res.Sema.TypeInterner.FnInfo(res.Sema.ExprTypes[call.Target])
	if sym == nil || sym.Kind != symbols.SymbolParam || sym.Decl.ASTFile != res.FileID || sym.Decl.SourceFile != res.File.ID ||
		!hasFn || info == nil || sym.Type != caller.ParamTypes[0] || res.Sema.ExprTypes[call.Target] != sym.Type ||
		len(info.Params) != 0 || info.Result != caller.TemplateParams[0] || res.Sema.ExprTypes[id] != info.Result || !info.ReturnSources().IsAllInputs() {
		t.Fatal("PRECONDITION: opaque generator lost original FnInfo, AllInputs or result T")
	}
}

func genericP0Pattern(t *testing.T, tc genericP0Case, res *DiagnoseResult, caller sema.CallableCandidate) {
	t.Helper()
	info, present := res.Sema.TypeInterner.UnionInfo(caller.ParamTypes[0])
	if !present || info == nil || !slices.Equal(info.TypeArgs, caller.TemplateParams) || len(info.Members) != 2 {
		t.Fatal("PRECONDITION: nested parameter lost its original union/template arguments")
	}
	var binding *symbols.Symbol
	var bindingID symbols.SymbolID
	for raw, candidate := range res.Symbols.Table.Symbols.Data() {
		name, _ := res.Builder.StringsInterner.Lookup(candidate.Name)
		if name == "v" && candidate.Kind == symbols.SymbolLet && candidate.Decl.ASTFile == res.FileID {
			if binding != nil {
				t.Fatal("PRECONDITION: pattern payload binding is ambiguous")
			}
			bindingID, binding = symbols.SymbolID(raw+1), res.Symbols.Table.Symbols.Get(symbols.SymbolID(raw+1))
		}
	}
	if binding == nil || !binding.Scope.IsValid() || binding.Type != caller.TemplateParams[0] || res.Sema.BindingTypes[bindingID] != binding.Type {
		t.Fatal("PRECONDITION: pattern payload lost original generic type and binding scope")
	}
	tagName := "Carry"
	if tc.name == "nested_either_payload" {
		tagName = "Good"
	}
	start := strings.Index(tc.text, tagName+"(v)") + len(tagName) + 1
	if start < len(tagName)+1 || binding.Span.File != res.File.ID || binding.Span.Start != uint32(start) || binding.Span.End != uint32(start+1) {
		t.Fatal("PRECONDITION: payload binding does not belong to the frozen pattern")
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_p0_pattern", "case": tc.name,
		"union": info, "binding_id": bindingID, "binding": binding, "scope": res.Symbols.Table.Scopes.Get(binding.Scope)})
}
