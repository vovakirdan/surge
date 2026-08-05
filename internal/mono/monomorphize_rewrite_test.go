package mono

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
