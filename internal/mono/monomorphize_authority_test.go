package mono

import (
	"strings"
	"testing"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestMonoUsesAlwaysOnClosureWhenRecorderIsNil(t *testing.T) {
	src := `
fn h<T>(x: T) -> T { return x; }
fn g<T>(x: T) -> T { return h(x); }
fn f<T>(x: T) -> T { return g(x); }
fn unused<T>(x: T) -> T { return h(x); }
fn main() { let _ = f(1); }
`
	mm, typesIn, err := compileAndMonomorphizeWithRecorder(t, src, hir.LowerOptions{}, false)
	if err != nil {
		t.Fatalf("nil-recorder monomorphization failed: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("nil-recorder mono contains type params: %v", err)
	}
	if got, want := len(mm.Funcs), 4; got != want {
		t.Fatalf("mono funcs = %d, want main + f<int> + g<int> + h<int>; unused generic must stay deferred", got)
	}
}

func TestMonoClosureReachesGenericLeafThroughDeferredBoundMethod(t *testing.T) {
	src := `
fn leaf<U>(value: U) -> U { return value; }

type Foo = { value: int }

extern<Foo> {
  fn Bar(self: Foo) -> int { return leaf(self.value); }
}

contract FooLike<T> {
  fn Bar(self: T) -> int;
}

fn invoke<T: FooLike<T>>(value: T) -> int { return value.Bar(); }

fn main() {
  let _ = invoke(Foo { value = 1 });
}
`
	mm, typesIn, err := compileAndMonomorphize(t, src)
	if err != nil {
		t.Fatalf("deferred bound-method closure failed: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("deferred bound-method mono contains type params: %v", err)
	}
	if got, want := len(mm.Funcs), 4; got != want {
		t.Fatalf("mono funcs = %d, want main + invoke<Foo> + Foo.Bar + leaf<int>; closure lost a deferred dispatch edge", got)
	}
}

func TestMonoImplicitTagInjectionUsesSemaSelectedConstructor(t *testing.T) {
	src := `
tag Some<T>(T);
type Option<T> = Some(T) | nothing;

fn main() {
  let value: Option<int> = 7;
}
`
	mm, typesIn, err := compileAndMonomorphize(t, src)
	if err != nil {
		t.Fatalf("implicit tag injection monomorphization failed: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("implicit tag injection left generic types: %v", err)
	}
	if got, want := len(mm.Funcs), 2; got != want {
		t.Fatalf("mono funcs = %d, want main + Some<int>", got)
	}
}

func TestMonoUserConversionAuthorizesSyntheticDefaultTarget(t *testing.T) {
	src := `
@intrinsic fn default<T>() -> T;
type Point = { x: int };
extern<Point> {
  fn __to(self: &Point, target: int) -> int { return self.x; }
}
fn main() -> int {
  let point = Point { x = 7 };
  return point to int;
}
`
	mm, typesIn, err := compileAndMonomorphize(t, src)
	if err != nil {
		t.Fatalf("synthetic conversion target monomorphization failed: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("synthetic conversion target left generic types: %v", err)
	}
	if got, want := len(mm.Funcs), 3; got != want {
		t.Fatalf("mono funcs = %d, want main + Point.__to + default<int>", got)
	}
}

func TestMonoRejectsUnfinalizedNonEmptyInstantiationGraph(t *testing.T) {
	_, _, err := compileAndMonomorphizeOptions(t, `
fn id<T>(x: T) -> T { return x; }
fn main() { let _ = id(1); }
`, hir.LowerOptions{}, false, false)
	if err == nil || !strings.Contains(err.Error(), "non-empty instantiation graph is not finalized") {
		t.Fatalf("unfinalized graph error = %v", err)
	}
}

func TestFinalizedEmptyClosureDoesNotFallBackToLegacyInstantiations(t *testing.T) {
	legacy := NewInstantiationMap()
	legacy.Record(InstFn, 7, []types.TypeID{1}, source.Span{File: 1, Start: 1, End: 2}, 0, "legacy")
	closure := &sema.InstantiationClosure{}
	result := &sema.Result{InstantiationIdentity: &sema.InstantiationIdentity{}, InstantiationClosure: closure}

	authoritative, gotClosure, err := authoritativeInstantiationMap(legacy, result)
	if err != nil {
		t.Fatalf("empty authoritative closure: %v", err)
	}
	if gotClosure != closure {
		t.Fatalf("empty finalized closure was treated as legacy")
	}
	if len(authoritative.Entries) != 0 {
		t.Fatalf("legacy function instantiations leaked through empty closure: %+v", authoritative.Entries)
	}
}

func TestMonoCannotMaterializeCallableMissingFromAuthoritativeClosure(t *testing.T) {
	in := types.NewInterner()
	b, err := newMonoBuilder(
		&hir.Module{TypeInterner: in},
		NewInstantiationMap(),
		&sema.InstantiationClosure{},
		testMonoInstantiationIdentity(in),
		nil,
		in,
		Options{},
	)
	if err != nil {
		t.Fatalf("create mono builder: %v", err)
	}
	if _, err := b.ensureFunc(7, nil, nil); err == nil || !strings.Contains(err.Error(), "not retained by the authoritative instantiation closure") {
		t.Fatalf("unauthorized callable materialization error = %v", err)
	}
}

func TestCallableMapCanonicalizesNonGenericAliasesWithEmptyArgs(t *testing.T) {
	in := types.NewInterner()
	identity := &sema.InstantiationIdentity{
		Types: types.CanonicalKeyContext{Types: in},
		ResolveTemplate: func(symbols.SymbolID) (string, error) {
			return "test/shared/non-generic", nil
		},
	}
	callables := newCallableMap(identity)
	if err := callables.bind(7, nil, 99); err != nil {
		t.Fatalf("bind retained non-generic alias: %v", err)
	}
	instance, found, err := callables.LookupChecked(11, nil)
	if err != nil {
		t.Fatalf("lookup non-generic alias: %v", err)
	}
	if !found || instance != 99 {
		t.Fatalf("non-generic alias lookup = (%d, %t), want (99, true)", instance, found)
	}
}

// Under the whole-program authority, which body clones a type is already
// decided; mono is not allowed to look for one. Its old scan picked whatever
// __clone the symbol table happened to hold for the type name, which is exactly
// how one program could end up cloning a value two different ways. Reaching this
// point means a decision was lost upstream, so it is an error rather than a
// second opinion.
func TestCloneRewriteRefusesToRediscoverAnImplementation(t *testing.T) {
	stringsIn := source.NewInterner()
	in := types.NewInterner()
	in.Strings = stringsIn
	// A struct is not Copy, so this clone genuinely needs a chosen body.
	holder := in.RegisterStruct(stringsIn.Intern("Holder"), source.Span{File: 1, Start: 1, End: 2})
	b := &monoBuilder{
		mod:     &hir.Module{},
		types:   in,
		closure: &sema.InstantiationClosure{},
	}
	call := &hir.Expr{Kind: hir.ExprCall, Type: holder, Span: source.Span{File: 1, Start: 10, End: 20}}
	data := &hir.CallData{Args: []*hir.Expr{{Kind: hir.ExprVarRef, Type: holder}}}

	handled, err := b.rewriteCloneCall(call, data, nil)
	if !handled {
		t.Fatal("a clone of a non-Copy type was passed through untouched")
	}
	if err == nil || !strings.Contains(err.Error(), "no authoritative implementation") {
		t.Fatalf("clone rewrite error = %v", err)
	}
	if data.Callee != nil || data.SymbolID.IsValid() {
		t.Fatalf("the refused clone was rewritten anyway: %+v", data)
	}
}

func TestDCERootsRejectMissingSemaSelectedCallable(t *testing.T) {
	in := types.NewInterner()
	identity := testMonoInstantiationIdentity(in)
	b := &monoBuilder{
		mod:      &hir.Module{},
		identity: identity,
		mm:       &MonoModule{Callables: newCallableMap(identity)},
		entrypointBindings: []sema.EntrypointCallableBinding{{
			Outcome:   sema.EntrypointCallableUser,
			Callee:    7,
			CalleeKey: "test/missing",
		}},
	}
	if _, err := b.dceRoots(); err == nil || !strings.Contains(err.Error(), "has no emitted instance") {
		t.Fatalf("missing sema-selected DCE root error = %v", err)
	}
}
