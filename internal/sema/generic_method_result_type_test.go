package sema

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const genericMethodValueSource = `pragma no_std;
tag Some<T>(T);
type Option<T> = Some(T) | nothing;
type Cursor<T> = { value: T };
extern<Cursor<T>> { fn next(self: &mut Cursor<T>) -> Option<T>; }
fn probe<U>(iter: &mut Cursor<U>) {
    let result%s = iter.next();
}
`

const genericMethodBorrowSource = `pragma no_std;
tag Some<T>(T);
type Option<T> = Some(T) | nothing;
type Store<K, V> = { value: V };
extern<Store<K, V>> { fn get_ref(self: &Store<K, V>, key: &K) -> Option<&V>; }
fn probe<K, V>(self: &Store<K, V>, key: &K) {
    let result%s = self.get_ref(key);
}
`

// These assertions read the call itself; an annotated destination can be typed
// even when its initializer has a present-but-zero ExprTypes entry.
func TestGenericMethodResultRetainsReceiverParam(t *testing.T) {
	for _, annotation := range []string{"", ": Option<U>"} {
		name := "inferred"
		if annotation != "" {
			name = "annotated"
		}
		t.Run(name, func(t *testing.T) {
			checkGenericMethodCall(t, fmt.Sprintf(genericMethodValueSource, annotation), "iter.next()", "next", false)
		})
	}
}

func TestGenericMethodResultRetainsBorrowedReceiverParam(t *testing.T) {
	for _, annotation := range []string{"", ": Option<&V>"} {
		name := "inferred"
		if annotation != "" {
			name = "annotated"
		}
		t.Run(name, func(t *testing.T) {
			checkGenericMethodCall(t, fmt.Sprintf(genericMethodBorrowSource, annotation), "self.get_ref(key)", "get_ref", true)
		})
	}
}

func checkGenericMethodCall(t *testing.T, text, callText, method string, borrowed bool) {
	t.Helper()
	b, file, parseBag := parseSnippet(t, text)
	if parseBag.Len() != 0 {
		t.Fatalf("parse: %s", diagnosticsSummary(parseBag))
	}
	res, syms, bag := checkWithSymbols(t, b, file)
	if bag.HasErrors() {
		t.Errorf("sema: %s", diagnosticsSummary(bag))
	}
	start, found := strings.Index(text, callText), 0
	for id, got := range res.ExprTypes {
		e := b.Exprs.Get(id)
		if e == nil || e.Kind != ast.ExprCall || int(e.Span.Start) != start || int(e.Span.End) != start+len(callText) {
			continue
		}
		found++
		call, _ := b.Exprs.Call(id)
		member, ok := b.Exprs.Member(call.Target)
		if !ok || member == nil {
			t.Fatal("selected call has no method receiver")
		}
		sym := syms.Table.Symbols.Get(syms.ExprSymbols[id])
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			t.Fatal("selected method symbol is absent")
		}
		name, _ := b.StringsInterner.Lookup(sym.Name)
		fn, ok := res.TypeInterner.FnInfo(sym.Type)
		if name != method || !ok || fn == nil || fn.Result == types.NoTypeID {
			t.Fatalf("selected callee %q has no result authority", name)
		}
		recv, present := res.ExprTypes[member.Target]
		rt, known := res.TypeInterner.Lookup(recv)
		if !present || !known || rt.Kind != types.KindReference {
			t.Fatal("receiver reference type is absent")
		}
		shape, ok := res.TypeInterner.StructInfo(rt.Elem)
		arity := 1
		if borrowed {
			arity = 2
		}
		if !ok || shape == nil || len(shape.TypeArgs) != arity {
			t.Fatal("receiver generic arguments are absent")
		}
		want := shape.TypeArgs[len(shape.TypeArgs)-1]
		if borrowed && shape.TypeArgs[0] == want {
			t.Fatal("receiver key and value arguments must have distinct identities")
		}
		if param, ok := res.TypeInterner.TypeParamInfo(want); !ok || param == nil {
			t.Fatal("receiver argument must remain a caller generic descriptor")
		}
		if borrowed {
			want = res.TypeInterner.Intern(types.MakeReference(want, false))
		}
		assertGenericMethodOption(t, res.TypeInterner, got, want)
	}
	if start < 0 || found != 1 {
		t.Fatalf("typed call entries reached=%d, want 1 for %q", found, callText)
	}
}

func TestGenericMethodResultKeepsDistinctDescriptorIdentity(t *testing.T) {
	for _, name := range []string{"same_owner", "different_owner", "unbound_foreign_result", "unbound_same_owner_result"} {
		t.Run(name, func(t *testing.T) {
			f := newGenericMethodFixture()
			actualOwner := uint32(10)
			if name == "different_owner" {
				actualOwner = 11
			}
			actual := f.in.RegisterTypeParam(f.name, actualOwner, 0, false, types.NoTypeID)
			result := f.formal
			want := actual
			if strings.HasPrefix(name, "unbound_") {
				owner := uint32(99)
				if name == "unbound_same_owner_result" {
					owner = 10
				}
				result = f.in.RegisterTypeParam(f.name, owner, 0, false, types.NoTypeID)
				want = types.NoTypeID
			}
			if actual == f.formal || (want == types.NoTypeID && result == f.formal) {
				t.Fatal("fixture did not allocate distinct descriptors")
			}
			// Neither global nor lexical spelling is the receiver's identity.
			f.tc.typeKeys["V"] = f.in.Builtins().String
			f.tc.typeParams = []map[source.StringID]types.TypeID{{f.name: f.in.Builtins().Bool}}
			f.tc.typeParamNames[actual], f.tc.typeParamNames[result] = f.name, f.name
			got := f.call(t, actual, result)
			if got != want {
				t.Errorf("selected method result=%d, want exact receiver-derived %d (formal=%d)", got, want, f.formal)
			}
			if want == types.NoTypeID {
				return
			}
			// Exercise reversed names through a receiver-only selected method,
			// independently of parsed argument-overload matching.
			keyName := f.in.Strings.Intern("K")
			formalKey := f.in.RegisterTypeParam(keyName, 10, 1, false, types.NoTypeID)
			actualKey := f.in.RegisterTypeParam(keyName, actualOwner, 1, false, types.NoTypeID)
			f.tc.typeParamNames[formalKey], f.tc.typeParamNames[actualKey] = keyName, keyName
			result = f.in.RegisterTuple([]types.TypeID{f.formal, formalKey})
			got = f.callWithReceiver(t, []types.TypeID{f.formal, formalKey}, []types.TypeID{actualKey, actual}, result)
			tuple, ok := f.in.TupleInfo(got)
			if !ok || tuple == nil || len(tuple.Elems) != 2 || tuple.Elems[0] != actualKey || tuple.Elems[1] != actual {
				t.Fatalf("swapped receiver result=%d, want exact tuple (%d, %d)", got, actualKey, actual)
			}
		})
	}
}

func TestGenericMethodResultIsIndependentOfExpectedTypeAndCache(t *testing.T) {
	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warm"
		}
		t.Run(name, func(t *testing.T) {
			f := newGenericMethodFixture()
			actual := f.in.RegisterTypeParam(f.name, 11, 0, false, types.NoTypeID)
			original := genericMethodOption(f.in, f.formal)
			if warm {
				genericMethodOption(f.in, actual)
			}
			// Existing engine only: no test-only replacement or future adapter.
			subst, err := callableCandidateSubstitution(f.in, []types.TypeID{f.formal}, []types.TypeID{actual})
			if err != nil || subst == nil {
				t.Fatalf("existing substitution setup: %v", err)
			}
			got, err := subst.typeID(original)
			if err != nil {
				t.Fatalf("tagged Option substitution: %v", err)
			}
			assertGenericMethodOption(t, f.in, got, actual)
		})
	}
}

func TestGenericMethodResultPreservesNestedTypeMetadata(t *testing.T) {
	for _, name := range []string{"all_inputs", "explicit_empty", "slot_zero"} {
		t.Run(name, func(t *testing.T) {
			f := newGenericMethodFixture()
			actual := f.in.RegisterTypeParam(f.name, 11, 0, false, types.NoTypeID)
			sources := types.ReturnSources{}
			if name == "explicit_empty" {
				sources = types.ExplicitReturnSources()
			} else if name == "slot_zero" {
				sources = types.ExplicitReturnSources(0)
			}
			ref := f.in.Intern(types.MakeReference(f.formal, false))
			fn := f.in.RegisterFnWithReturnSources([]types.TypeID{ref}, ref, sources)
			result := f.in.RegisterTuple([]types.TypeID{fn, f.in.Intern(types.MakeReference(f.formal, true))})
			f.tc.typeParamNames[actual] = f.name
			got := f.call(t, actual, result)
			tuple, ok := f.in.TupleInfo(got)
			if !ok || tuple == nil || len(tuple.Elems) != 2 {
				t.Fatalf("nested method result=%d, want tuple", got)
			}
			info, ok := f.in.FnInfo(tuple.Elems[0])
			wantRef := f.in.Intern(types.MakeReference(actual, false))
			if !ok || info == nil || len(info.Params) != 1 || info.Params[0] != wantRef || info.Result != wantRef || !info.ReturnSources().Equal(sources) {
				t.Fatalf("nested function descriptor lost caller type or return sources: %+v", info)
			}
			if tuple.Elems[1] != f.in.Intern(types.MakeReference(actual, true)) {
				t.Fatal("nested mutable reference lost its exact caller type")
			}
		})
	}
}

type genericMethodFixture struct {
	tc     *typeChecker
	in     *types.Interner
	name   source.StringID
	formal types.TypeID
}

func newGenericMethodFixture() genericMethodFixture {
	b, file := newTestBuilder()
	in := types.NewInterner()
	in.Strings = b.StringsInterner
	name := in.Strings.Intern("V")
	formal := in.RegisterTypeParam(name, 10, 0, false, types.NoTypeID)
	table := symbols.NewTable(symbols.Hints{}, in.Strings)
	tc := &typeChecker{
		builder: b, fileID: file, types: in,
		symbols:  &symbols.Result{Table: table},
		result:   &Result{TypeInterner: in, InstantiationTemplateParams: make(map[symbols.SymbolID][]types.TypeID)},
		typeKeys: make(map[string]types.TypeID), typeParamNames: map[types.TypeID]source.StringID{formal: name},
		magic:        make(map[symbols.TypeKey]map[string][]*symbols.FunctionSignature),
		magicSymbols: make(map[*symbols.FunctionSignature]symbols.SymbolID),
		reporter:     &diag.BagReporter{Bag: diag.NewBag(16)},
	}
	return genericMethodFixture{tc: tc, in: in, name: name, formal: formal}
}

func (f genericMethodFixture) call(t *testing.T, actual, result types.TypeID) types.TypeID {
	t.Helper()
	return f.callWithReceiver(t, []types.TypeID{f.formal}, []types.TypeID{actual}, result)
}

func (f genericMethodFixture) callWithReceiver(t *testing.T, formals, actuals []types.TypeID, result types.TypeID) types.TypeID {
	t.Helper()
	name := f.in.Strings.Intern("Cell")
	decl := source.Span{File: 1, Start: 10, End: 20}
	formalRecv := f.in.RegisterStructInstance(name, decl, formals)
	recv := f.in.RegisterStructInstance(name, decl, actuals)
	paramNames, paramKeys := make([]source.StringID, len(formals)), make([]string, len(formals))
	for i, formal := range formals {
		paramNames[i] = f.tc.typeParamNames[formal]
		paramKeys[i], _ = f.in.Strings.Lookup(paramNames[i])
	}
	key := symbols.TypeKey("Cell<" + strings.Join(paramKeys, ",") + ">")
	sig := &symbols.FunctionSignature{Params: []symbols.TypeKey{key}, Result: f.tc.typeKeyForType(result), HasSelf: true}
	id := f.tc.symbols.Table.Symbols.New(&symbols.Symbol{
		Name: f.in.Strings.Intern("read"), Kind: symbols.SymbolFunction, ReceiverKey: key,
		Signature: sig, Type: f.in.RegisterFn([]types.TypeID{formalRecv}, result), TypeParams: paramNames,
	})
	f.tc.result.InstantiationTemplateParams[id] = formals
	f.tc.magic[key] = map[string][]*symbols.FunctionSignature{"read": {sig}}
	f.tc.magicSymbols[sig] = id
	selected, _, _, _, matched := f.tc.matchMethodSignature("read", recv, ast.NoExprID, nil, nil, false)
	if selected != sig || !matched || f.tc.magicSymbolForSignature(selected) != id {
		t.Fatal("fixture did not reach the exact selected method")
	}
	info, ok := f.in.FnInfo(f.tc.symbols.Table.Symbols.Get(id).Type)
	if !ok || info == nil || info.Result != result || len(info.Params) != 1 || info.Params[0] != formalRecv {
		t.Fatal("selected method has no exact structural signature authority")
	}
	member := &ast.ExprMemberData{Field: f.in.Strings.Intern("read")}
	return f.tc.methodResultType(member, recv, ast.NoExprID, nil, nil, source.Span{}, false)
}

func genericMethodOption(in *types.Interner, payload types.TypeID) types.TypeID {
	id := in.RegisterUnionInstance(in.Strings.Intern("Option"), source.Span{File: 1, Start: 30, End: 40}, []types.TypeID{payload})
	in.SetUnionMembers(id, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: in.Strings.Intern("Some"), TagArgs: []types.TypeID{payload}},
		{Kind: types.UnionMemberNothing, Type: in.Builtins().Nothing},
	})
	return id
}

func assertGenericMethodOption(t *testing.T, in *types.Interner, got, payload types.TypeID) {
	t.Helper()
	info, ok := in.UnionInfo(got)
	if !ok || info == nil || len(info.TypeArgs) != 1 || info.TypeArgs[0] != payload {
		t.Fatalf("call result=%d, want Option with exact receiver payload=%d; got %+v", got, payload, info)
	}
	if len(info.Members) != 2 || info.Members[0].Kind != types.UnionMemberTag || len(info.Members[0].TagArgs) != 1 || info.Members[0].TagArgs[0] != payload || info.Members[1].Kind != types.UnionMemberNothing {
		t.Fatalf("Option payload metadata is incomplete: %+v", info.Members)
	}
	if info.Name != in.Strings.Intern("Option") || info.Members[0].TagName != in.Strings.Intern("Some") || info.Members[0].Type != types.NoTypeID || info.Members[1].Type != in.Builtins().Nothing {
		t.Fatalf("Option member kind or tag identity changed: %+v", info)
	}
}
