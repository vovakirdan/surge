package mono

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestMonoHasNoTypeParams(t *testing.T) {
	src := `
fn id<T>(x: T) -> T { return x; }

fn wrap<T>(x: T) -> T {
  return id(x);
}

fn main() {
  let a = wrap(1);
  let b = wrap("x");
}
`

	mm, typesIn, err := compileAndMonomorphize(t, src)
	if err != nil {
		t.Fatalf("failed to monomorphize: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("mono contains type params: %v", err)
	}
	if got, want := len(mm.Funcs), 5; got != want {
		t.Fatalf("unexpected mono func count: got=%d want=%d", got, want)
	}
}

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

func TestAuthoritativeRewriteRejectsGenericCallWithoutUseSite(t *testing.T) {
	b, caller, callee, _, fnType := newAuthoritativeGenericRewriteFixture(t)
	span := source.Span{File: 1, Start: 10, End: 20}
	call := &hir.Expr{
		Kind: hir.ExprCall,
		Span: span,
		Data: hir.CallData{
			SymbolID: callee,
			Callee: &hir.Expr{
				Kind: hir.ExprVarRef,
				Type: fnType,
				Data: hir.VarRefData{Name: "id", SymbolID: callee},
			},
		},
	}
	fn := &hir.Func{
		SymbolID: caller,
		Body: &hir.Block{Stmts: []hir.Stmt{{
			Kind: hir.StmtExpr,
			Data: hir.ExprStmtData{Expr: call},
		}}},
	}

	err := b.rewriteCallsInFunc(fn, caller, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no authoritative concrete instantiation") {
		t.Fatalf("missing generic call use-site error = %v", err)
	}
}

func TestAuthoritativeRewriteSkipsSelectDispatchDescriptor(t *testing.T) {
	in := types.NewInterner()
	caller := symbols.SymbolID(1)
	b, err := newMonoBuilder(
		&hir.Module{TypeInterner: in},
		NewInstantiationMap(),
		&sema.InstantiationClosure{LiveCallables: []symbols.SymbolID{caller}},
		testMonoInstantiationIdentity(in),
		nil,
		in,
		Options{},
	)
	if err != nil {
		t.Fatalf("create mono builder: %v", err)
	}
	receiver := &hir.Expr{Kind: hir.ExprVarRef, Type: in.Builtins().Int, Data: hir.VarRefData{Name: "task"}}
	call := &hir.Expr{
		Kind: hir.ExprCall,
		Data: hir.CallData{
			SelectDispatch: true,
			Callee: &hir.Expr{
				Kind: hir.ExprFieldAccess,
				Data: hir.FieldAccessData{Object: receiver, FieldName: "await"},
			},
		},
	}
	fn := &hir.Func{
		SymbolID: caller,
		Body: &hir.Block{Stmts: []hir.Stmt{{
			Kind: hir.StmtExpr,
			Data: hir.ExprStmtData{Expr: call},
		}}},
	}

	if err := b.rewriteCallsInFunc(fn, caller, nil, nil); err != nil {
		t.Fatalf("select descriptor was treated as unresolved callable: %v", err)
	}
	data := call.Data.(hir.CallData)
	if data.SymbolID.IsValid() || data.Callee.Kind != hir.ExprFieldAccess {
		t.Fatalf("select descriptor was rewritten as a callable: %+v", data)
	}
}

func TestAuthoritativeRewriteRejectsGenericFunctionValueWithoutUseSite(t *testing.T) {
	b, caller, callee, _, fnType := newAuthoritativeGenericRewriteFixture(t)
	value := &hir.Expr{
		Kind: hir.ExprVarRef,
		Type: fnType,
		Span: source.Span{File: 1, Start: 30, End: 40},
		Data: hir.VarRefData{Name: "id", SymbolID: callee},
	}
	fn := &hir.Func{
		SymbolID: caller,
		Body: &hir.Block{Stmts: []hir.Stmt{{
			Kind: hir.StmtExpr,
			Data: hir.ExprStmtData{Expr: value},
		}}},
	}

	err := b.rewriteFuncValuesInFunc(fn, caller, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no authoritative concrete instantiation") {
		t.Fatalf("missing generic function-value use-site error = %v", err)
	}
}

func TestAuthoritativeSeedRejectsNonConcreteEntry(t *testing.T) {
	b, _, _, param, _ := newAuthoritativeGenericRewriteFixture(t)
	b.closure.Instances[0].TemplateArgs = []types.TypeID{param}

	err := b.seed()
	if err == nil || !strings.Contains(err.Error(), "non-concrete type arguments") {
		t.Fatalf("non-concrete authoritative entry error = %v", err)
	}
}

func newAuthoritativeGenericRewriteFixture(t *testing.T) (*monoBuilder, symbols.SymbolID, symbols.SymbolID, types.TypeID, types.TypeID) {
	t.Helper()
	stringsIn := source.NewInterner()
	in := types.NewInterner()
	in.Strings = stringsIn
	table := symbols.NewTable(symbols.Hints{}, stringsIn)
	caller := table.Symbols.New(&symbols.Symbol{
		Name: stringsIn.Intern("caller"),
		Kind: symbols.SymbolFunction,
	})
	typeName := stringsIn.Intern("T")
	callee := table.Symbols.New(&symbols.Symbol{
		Name:       stringsIn.Intern("id"),
		Kind:       symbols.SymbolFunction,
		TypeParams: []source.StringID{typeName},
	})
	param := in.RegisterTypeParam(typeName, uint32(callee), 0, false, types.NoTypeID)
	fnType := in.RegisterFn([]types.TypeID{param}, param)
	table.Symbols.Get(callee).Type = fnType
	identity := testMonoInstantiationIdentity(in)
	key, err := sema.NewInstanceKey(*identity, callee, []types.TypeID{in.Builtins().Int})
	if err != nil {
		t.Fatalf("create canonical fixture key: %v", err)
	}
	closure := &sema.InstantiationClosure{
		LiveCallables: []symbols.SymbolID{caller},
		Instances: []sema.InstantiationInstance{{
			Key:          key,
			Template:     callee,
			TemplateArgs: []types.TypeID{in.Builtins().Int},
		}},
	}
	b, err := newMonoBuilder(
		&hir.Module{Symbols: &symbols.Result{Table: table}, TypeInterner: in},
		NewInstantiationMap(),
		closure,
		identity,
		map[symbols.SymbolID][]types.TypeID{callee: {param}},
		in,
		Options{},
	)
	if err != nil {
		t.Fatalf("create authoritative mono builder: %v", err)
	}
	return b, caller, callee, param, fnType
}

func TestDeferredRewriteUsesConcreteParamABI(t *testing.T) {
	stringsIn := source.NewInterner()
	in := types.NewInterner()
	in.Strings = stringsIn
	receiver := in.RegisterStruct(stringsIn.Intern("Holder"), source.Span{File: 1, Start: 1, End: 2})
	refInt := in.Intern(types.MakeReference(in.Builtins().Int, false))
	table := symbols.NewTable(symbols.Hints{}, stringsIn)
	callee := table.Symbols.New(&symbols.Symbol{
		Name: stringsIn.Intern("Take"), Kind: symbols.SymbolFunction,
		Signature: &symbols.FunctionSignature{
			Params: []symbols.TypeKey{"Holder<U>", "U"}, Result: "U", HasSelf: true, HasBody: true,
		},
	})
	b, err := newMonoBuilder(
		&hir.Module{Symbols: &symbols.Result{Table: table}, TypeInterner: in},
		NewInstantiationMap(), nil, nil, nil, in, Options{},
	)
	if err != nil {
		t.Fatalf("create legacy mono builder: %v", err)
	}
	receiverExpr := &hir.Expr{Kind: hir.ExprVarRef, Type: receiver, Data: hir.VarRefData{Name: "holder", SymbolID: 20}}
	arg := &hir.Expr{Kind: hir.ExprVarRef, Type: refInt, Data: hir.VarRefData{Name: "value", SymbolID: 21}}
	call := &hir.Expr{Kind: hir.ExprCall, Type: refInt}
	data := &hir.CallData{
		Callee: &hir.Expr{Kind: hir.ExprFieldAccess, Data: hir.FieldAccessData{Object: receiverExpr, FieldName: "Take"}},
		Args:   []*hir.Expr{arg},
	}
	resolved := &sema.ResolvedDeferredCall{
		UseID: "use", Kind: sema.DeferredMethodCall, Outcome: sema.DeferredCallableResolved,
		Callee: callee, CalleeParamTypes: []types.TypeID{receiver, refInt}, CalleeResultType: refInt,
	}
	if err := b.applyResolvedDeferredCall(call, data, resolved); err != nil {
		t.Fatalf("rewrite concrete deferred ABI: %v", err)
	}
	if len(data.Args) != 2 || data.Args[1] != arg || data.Args[1].Kind != hir.ExprVarRef || data.Args[1].Type != refInt {
		t.Fatalf("generic TypeKey changed concrete reference parameter: %+v", data.Args)
	}
}

func testMonoInstantiationIdentity(in *types.Interner) *sema.InstantiationIdentity {
	return &sema.InstantiationIdentity{
		Types: types.CanonicalKeyContext{Types: in},
		ResolveTemplate: func(id symbols.SymbolID) (string, error) {
			return fmt.Sprintf("test/template/%d", id), nil
		},
	}
}

func TestMonoPreservesCrossingRepresentation(t *testing.T) {
	src := `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
@intrinsic @copy
type Placement = { __opaque: int };

fn cross<T>(dst: Placement, value: int) -> TaskResult<int> {
  return on dst {
    ret value;
  };
}

fn main(dst: Placement) -> TaskResult<int> {
  return cross::<int>(dst, 7);
}
`
	mm, typesIn, err := compileAndMonomorphizeWithLowerOptions(t, src, hir.LowerOptions{
		CrossingForms: map[sema.CrossingLoweringKind]bool{
			sema.CrossingLoweringOnPlacement: true,
		},
	})
	if err != nil {
		t.Fatalf("failed to monomorphize: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("mono contains type params: %v", err)
	}
	data := requireMonoCrossing(t, mm, sema.CrossingLoweringOnPlacement)
	if got := types.Label(typesIn, data.PayloadType); got != "int" {
		t.Fatalf("payload type = %q, want int", got)
	}
	if got := types.Label(typesIn, data.ResultType); got != "TaskResult<int>" {
		t.Fatalf("result type = %q, want TaskResult<int>", got)
	}
	if len(data.Captures) != 1 {
		t.Fatalf("captures = %d, want 1", len(data.Captures))
	}
	if got := types.Label(typesIn, data.Captures[0].Type); got != "int" {
		t.Fatalf("capture type = %q, want int", got)
	}
	if data.Captures[0].Value == nil || data.Captures[0].Value.Type == types.NoTypeID {
		t.Fatalf("capture value was not preserved through mono: %+v", data.Captures[0].Value)
	}
}

func TestCloneCrossingRepresentationDoesNotAliasSlices(t *testing.T) {
	captureValue := &hir.Expr{
		Kind: hir.ExprLiteral,
		Type: types.TypeID(1),
		Data: hir.LiteralData{Kind: hir.LiteralInt, Text: "1", IntValue: 1},
	}
	remoteReceiver := &hir.Expr{
		Kind: hir.ExprVarRef,
		Type: types.TypeID(2),
		Data: hir.VarRefData{Name: "conn"},
	}
	original := &hir.Expr{
		Kind: hir.ExprCrossing,
		Type: types.TypeID(3),
		Data: hir.CrossingData{
			Kind: sema.CrossingLoweringOnFarHandle,
			Captures: []hir.CrossingCapture{{
				Type:  types.TypeID(4),
				Value: captureValue,
			}},
			RemoteOps: []hir.CrossingRemoteOp{{
				ReceiverType: types.TypeID(5),
				Receiver:     remoteReceiver,
			}},
		},
	}

	cloned := cloneExpr(original)
	origData := original.Data.(hir.CrossingData)
	cloneData := cloned.Data.(hir.CrossingData)
	if origData.Captures[0].Value != captureValue {
		t.Fatalf("clone mutated original capture value")
	}
	if origData.RemoteOps[0].Receiver != remoteReceiver {
		t.Fatalf("clone mutated original remote-op receiver")
	}
	if cloneData.Captures[0].Value == captureValue {
		t.Fatalf("clone reused capture expression pointer")
	}
	if cloneData.RemoteOps[0].Receiver == remoteReceiver {
		t.Fatalf("clone reused remote-op receiver expression pointer")
	}

	cloneData.Captures[0].Type = types.TypeID(44)
	cloneData.RemoteOps[0].ReceiverType = types.TypeID(55)
	if origData.Captures[0].Type == cloneData.Captures[0].Type {
		t.Fatalf("clone capture slice aliases original")
	}
	if origData.RemoteOps[0].ReceiverType == cloneData.RemoteOps[0].ReceiverType {
		t.Fatalf("clone remote-op slice aliases original")
	}
}

func requireMonoCrossing(t *testing.T, mm *MonoModule, kind sema.CrossingLoweringKind) hir.CrossingData {
	t.Helper()
	for _, mf := range mm.Funcs {
		if mf == nil || mf.Func == nil || mf.Func.Body == nil {
			continue
		}
		if data, ok := findCrossingInBlock(mf.Func.Body, kind); ok {
			return data
		}
	}
	t.Fatalf("missing mono crossing kind %d", kind)
	return hir.CrossingData{}
}

func findCrossingInBlock(block *hir.Block, kind sema.CrossingLoweringKind) (hir.CrossingData, bool) {
	if block == nil {
		return hir.CrossingData{}, false
	}
	for _, stmt := range block.Stmts {
		switch data := stmt.Data.(type) {
		case hir.ReturnData:
			if out, ok := findCrossingInExpr(data.Value, kind); ok {
				return out, true
			}
		case hir.ExprStmtData:
			if out, ok := findCrossingInExpr(data.Expr, kind); ok {
				return out, true
			}
		case hir.LetData:
			if out, ok := findCrossingInExpr(data.Value, kind); ok {
				return out, true
			}
		}
	}
	return hir.CrossingData{}, false
}

func findCrossingInExpr(expr *hir.Expr, kind sema.CrossingLoweringKind) (hir.CrossingData, bool) {
	if expr == nil {
		return hir.CrossingData{}, false
	}
	if expr.Kind == hir.ExprCrossing {
		data := expr.Data.(hir.CrossingData)
		if data.Kind == kind {
			return data, true
		}
	}
	switch data := expr.Data.(type) {
	case hir.CallData:
		if out, ok := findCrossingInExpr(data.Callee, kind); ok {
			return out, true
		}
		for _, arg := range data.Args {
			if out, ok := findCrossingInExpr(arg, kind); ok {
				return out, true
			}
		}
	case hir.BlockExprData:
		return findCrossingInBlock(data.Block, kind)
	}
	return hir.CrossingData{}, false
}

func compileAndMonomorphize(t *testing.T, src string) (*MonoModule, *types.Interner, error) {
	t.Helper()
	return compileAndMonomorphizeWithLowerOptions(t, src, hir.LowerOptions{})
}

func compileAndMonomorphizeWithLowerOptions(t *testing.T, src string, lowerOpts hir.LowerOptions) (*MonoModule, *types.Interner, error) {
	t.Helper()
	return compileAndMonomorphizeWithRecorder(t, src, lowerOpts, true)
}

func compileAndMonomorphizeWithRecorder(t *testing.T, src string, lowerOpts hir.LowerOptions, withRecorder bool) (*MonoModule, *types.Interner, error) {
	t.Helper()
	return compileAndMonomorphizeOptions(t, src, lowerOpts, withRecorder, true)
}

func compileAndMonomorphizeOptions(t *testing.T, src string, lowerOpts hir.LowerOptions, withRecorder, finalize bool) (*MonoModule, *types.Interner, error) {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	sharedStrings := source.NewInterner()
	typeInterner := types.NewInterner()
	instMap := NewInstantiationMap()

	bag := diag.NewBag(100)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, sharedStrings)

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 100,
	}

	result := parser.ParseFile(context.Background(), fs, lx, builder, opts)
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("parse errors: %s", monoDiagSummary(bag))
	}

	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "core",
		FilePath:   "test.sg",
	})
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("symbol errors: %s", monoDiagSummary(bag))
	}

	semaOpts := sema.Options{
		Reporter:   &diag.BagReporter{Bag: bag},
		Symbols:    &symbolsRes,
		Types:      typeInterner,
		ModulePath: builder.StringsInterner.Intern("core"),
	}
	if withRecorder {
		semaOpts.Instantiations = NewInstantiationMapRecorder(instMap)
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("sema errors: %s", monoDiagSummary(bag))
	}
	if finalize {
		identity, identityErr := sema.NewInstantiationKeyContext(typeInterner, &symbolsRes, func(id source.FileID) (string, error) {
			if fs.Get(id) == nil {
				return "", fmt.Errorf("unknown source file %d", id)
			}
			return "test.sg", nil
		})
		if identityErr != nil {
			return nil, nil, identityErr
		}
		semaRes.InstantiationIdentity = &identity
		if closureErr := semaRes.FinalizeInstantiationClosure(identity, 64); closureErr != nil {
			return nil, nil, closureErr
		}
	}

	mod, err := hir.LowerWithOptions(context.Background(), builder, result.File, &semaRes, &symbolsRes, lowerOpts)
	if err != nil {
		return nil, nil, err
	}

	mm, err := MonomorphizeModule(mod, instMap, &semaRes, Options{})
	return mm, typeInterner, err
}

func monoDiagSummary(bag *diag.Bag) string {
	if bag == nil || bag.Len() == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, d := range bag.Items() {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: %s", d.Code.ID(), d.Message)
	}
	return b.String()
}
