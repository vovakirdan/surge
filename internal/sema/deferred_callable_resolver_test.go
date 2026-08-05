package sema

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestDeferredCallableResolverIsDeterministicUnderCandidateOrder(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	request := DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiver, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}
	left := deferredResolverMethod(10, "app|a.sg:1:2|Pick", "Pick", receiver, in.Builtins().Int)
	right := deferredResolverMethod(20, "app|b.sg:1:2|Pick", "Pick", receiver, in.Builtins().Int)

	_, forwardErr := resolveDeferredCallable("use", &request, []CallableCandidate{left, right}, in, nil)
	_, reverseErr := resolveDeferredCallable("use", &request, []CallableCandidate{right, left}, in, nil)
	if forwardErr == nil || reverseErr == nil || forwardErr.Error() != reverseErr.Error() {
		t.Fatalf("candidate order changed ambiguity:\nforward=%v\nreverse=%v", forwardErr, reverseErr)
	}
	if !strings.Contains(forwardErr.Error(), left.BodyKey+", "+right.BodyKey) {
		t.Fatalf("ambiguity candidates are not canonically ordered: %v", forwardErr)
	}
}

func TestDeferredCallableResolverRejectsConflictingCanonicalBodyRecords(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	request := DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiver, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}
	left := deferredResolverMethod(10, "app|shared.sg:1:2|Pick", "Pick", receiver, in.Builtins().Int)
	right := left
	right.Symbol = 20
	right.SourceKey = "other.sg"

	_, forwardErr := resolveDeferredCallable("use", &request, []CallableCandidate{left, right}, in, nil)
	_, reverseErr := resolveDeferredCallable("use", &request, []CallableCandidate{right, left}, in, nil)
	if forwardErr == nil || reverseErr == nil || forwardErr.Error() != reverseErr.Error() {
		t.Fatalf("conflicting aliases were order-dependent:\nforward=%v\nreverse=%v", forwardErr, reverseErr)
	}
	if !strings.Contains(forwardErr.Error(), "canonical body identity maps to inconsistent callable records") {
		t.Fatalf("conflicting aliases did not fail closed: %v", forwardErr)
	}
}

func TestDeferredCallableResolverKeepsSameBasenameNominalsDistinct(t *testing.T) {
	in := deferredResolverTestInterner()
	name := in.Strings.Intern("Value")
	leftType := in.RegisterStruct(name, source.Span{File: 1, Start: 10, End: 20})
	rightType := in.RegisterStruct(name, source.Span{File: 2, Start: 10, End: 20})
	left := deferredResolverMethod(10, "left|left/shared.sg:10:20|Pick", "Pick", leftType, in.Builtins().Int)
	right := deferredResolverMethod(20, "right|right/shared.sg:10:20|Pick", "Pick", rightType, in.Builtins().Int)
	left.ModulePath, left.SourceKey = "left", "left/shared.sg"
	right.ModulePath, right.SourceKey = "right", "right/shared.sg"

	resolution, err := resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: rightType, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "right", SourceKey: "right/shared.sg",
	}, []CallableCandidate{left, right}, in, nil)
	if err != nil {
		t.Fatalf("resolve same-basename nominal: %v", err)
	}
	if resolution.Callee != right.Symbol || resolution.CalleeKey != right.BodyKey {
		t.Fatalf("same-basename nominal selected %+v, want symbol %d", resolution, right.Symbol)
	}
}

func TestDeferredCallableResolverInstantiatesGenericSignature(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	callee := symbols.SymbolID(10)
	param := in.RegisterTypeParam(in.Strings.Intern("U"), uint32(callee), 0, false, types.NoTypeID)
	candidate := deferredResolverMethod(callee, "app|main.sg:1:2|Echo", "Echo", receiver, param)
	candidate.ParamTypes = []types.TypeID{receiver, param}
	candidate.Defaults = []bool{false, false}
	candidate.Variadic = []bool{false, false}
	candidate.TemplateParams = []types.TypeID{param}
	candidate.TypeParams = []string{"U"}

	resolution, err := resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiver, Method: "Echo",
		Args: []types.TypeID{in.Builtins().Int}, ExpectedResult: in.Builtins().Int,
		AccessModule: "app", SourceKey: "main.sg",
	}, []CallableCandidate{candidate}, in, nil)
	if err != nil {
		t.Fatalf("resolve generic callable: %v", err)
	}
	if len(resolution.TemplateArgs) != 1 || resolution.TemplateArgs[0] != in.Builtins().Int ||
		len(resolution.ParamTypes) != 2 || resolution.ParamTypes[1] != in.Builtins().Int ||
		resolution.ResultType != in.Builtins().Int {
		t.Fatalf("generic callable signature was not concretized: %+v", resolution)
	}
}

func TestDeferredCallableResolverRejectsInvalidCloneShape(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	invalid := deferredResolverMethod(10, "app|main.sg:1:2|__clone", "__clone", receiver, receiver)
	request := DeferredCallableRequest{
		Kind: DeferredCloneCall, Receiver: receiver, Method: "__clone",
		ExpectedResult: receiver, AccessModule: "app", SourceKey: "main.sg",
	}
	if _, err := resolveDeferredCallable("use", &request, []CallableCandidate{invalid}, in, nil); err == nil ||
		!strings.Contains(err.Error(), "has __clone but with invalid signature") {
		t.Fatalf("by-value __clone shape error = %v", err)
	}

	valid := invalid
	valid.Symbol = 11
	valid.BodyKey = "app|main.sg:3:4|__clone"
	valid.ParamTypes = []types.TypeID{in.Intern(types.MakeReference(receiver, false))}
	resolution, err := resolveDeferredCallable("use", &request, []CallableCandidate{valid}, in, nil)
	if err != nil || resolution.Callee != valid.Symbol {
		t.Fatalf("valid __clone resolution = %+v, err=%v", resolution, err)
	}
}

func TestDeferredClonePrivateCanonicalHookIsNotVisibleCrossModule(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	candidate := deferredResolverMethod(10, "model|value.sg:1:2|__clone", "__clone", receiver, receiver)
	candidate.ModulePath = "model"
	candidate.SourceKey = "model/value.sg"
	candidate.Public = false
	candidate.ParamTypes = []types.TypeID{in.Intern(types.MakeReference(receiver, false))}
	request := DeferredCallableRequest{
		Kind: DeferredCloneCall, Receiver: receiver, Method: "__clone", ExpectedResult: receiver,
		AccessModule: "consumer", SourceKey: "consumer/main.sg",
	}
	_, err := resolveDeferredCallable("use", &request, []CallableCandidate{candidate}, in, nil)
	if err == nil || !strings.Contains(err.Error(), "is not visible from module \"consumer\"") {
		t.Fatalf("private canonical clone hook was reachable across modules: %v", err)
	}
	var canonicality *CloneCanonicalityError
	if !errors.As(err, &canonicality) || canonicality.Diagnostic() == nil ||
		canonicality.Diagnostic().Code != diag.SemaCloneHookNotVisible {
		t.Fatalf("cross-module clone visibility failure is not a source diagnostic: %v", err)
	}

	sameModule := request
	sameModule.AccessModule = "model"
	sameModule.SourceKey = "model/other.sg"
	resolution, err := resolveDeferredCallable("use", &sameModule, []CallableCandidate{candidate}, in, nil)
	if err != nil || resolution.Callee != candidate.Symbol {
		t.Fatalf("module-private clone hook rejected inside its own module: resolution=%+v err=%v", resolution, err)
	}

	request.Kind = DeferredMethodCall
	if _, err := resolveDeferredCallable("ordinary", &request, []CallableCandidate{candidate}, in, nil); err == nil || !strings.Contains(err.Error(), "not accessible") {
		t.Fatalf("ordinary private method call bypassed visibility: %v", err)
	}
}

func TestDeferredCallableResolverBindsConstGenericReceiverValue(t *testing.T) {
	in := deferredResolverTestInterner()
	name := in.Strings.Intern("Fixed")
	decl := source.Span{File: 1, Start: 1, End: 2}
	callee := symbols.SymbolID(10)
	elemParam := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(callee), 0, false, types.NoTypeID)
	lenParam := in.RegisterTypeParam(in.Strings.Intern("N"), uint32(callee), 1, true, in.Builtins().Int)
	pattern := in.RegisterStructInstanceWithValues(name, decl, []types.TypeID{elemParam, lenParam}, nil)
	length := in.Intern(types.MakeConstUint(3))
	actual := in.RegisterStructInstanceWithValues(name, decl, []types.TypeID{in.Builtins().Int, length}, []uint64{3})
	candidate := deferredResolverMethod(callee, "core|fixed.sg:1:2|__len", "__len", pattern, in.Builtins().Uint)
	candidate.ModulePath = "core"
	candidate.SourceKey = "fixed.sg"
	candidate.TemplateParams = []types.TypeID{elemParam, lenParam}
	candidate.TypeParams = []string{"T", "N"}
	candidate.ReceiverTemplateArity = 2
	candidate.ParamTypes = []types.TypeID{in.Intern(types.MakeReference(pattern, false))}

	resolution, err := resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: actual, Method: "__len", ExpectedResult: in.Builtins().Uint,
		AccessModule: "core", SourceKey: "main.sg",
	}, []CallableCandidate{candidate}, in, nil)
	if err != nil {
		t.Fatalf("resolve const-generic receiver: %v", err)
	}
	if !slices.Equal(resolution.TemplateArgs, []types.TypeID{in.Builtins().Int, length}) {
		t.Fatalf("const-generic receiver args = %v", resolution.TemplateArgs)
	}
}

func TestDeferredCallableResolverSeparatesStaticAndInstanceForms(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	instance := deferredResolverMethod(10, "app|main.sg:1:2|Build-instance", "Build", receiver, in.Builtins().Int)
	static := instance
	static.Symbol = 11
	static.BodyKey = "app|main.sg:3:4|Build-static"
	static.HasSelf = false
	static.ParamTypes = nil

	resolution, err := resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiver, Method: "Build", StaticReceiver: true,
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}, []CallableCandidate{instance, static}, in, nil)
	if err != nil || resolution.Callee != static.Symbol {
		t.Fatalf("static resolution = %+v, err=%v", resolution, err)
	}

	resolution, err = resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiver, Method: "Build",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}, []CallableCandidate{static, instance}, in, nil)
	if err != nil || resolution.Callee != instance.Symbol {
		t.Fatalf("instance resolution = %+v, err=%v", resolution, err)
	}
}

func TestDeferredCallableResolverBindsExplicitMethodArgsAfterReceiverArgs(t *testing.T) {
	in := deferredResolverTestInterner()
	callee := symbols.SymbolID(10)
	receiverParam := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(callee), 0, false, types.NoTypeID)
	methodParam := in.RegisterTypeParam(in.Strings.Intern("U"), uint32(callee), 1, false, types.NoTypeID)
	name := in.Strings.Intern("Box")
	decl := source.Span{File: 1, Start: 1, End: 2}
	receiverTemplate := in.RegisterStructInstance(name, decl, []types.TypeID{receiverParam})
	receiverConcrete := in.RegisterStructInstance(name, decl, []types.TypeID{in.Builtins().Int})
	candidate := deferredResolverMethod(callee, "app|main.sg:1:2|Echo", "Echo", receiverTemplate, methodParam)
	candidate.ParamTypes = []types.TypeID{receiverTemplate, methodParam}
	candidate.TemplateParams = []types.TypeID{receiverParam, methodParam}
	candidate.ReceiverTemplateArity = 1
	candidate.TypeParams = []string{"T", "U"}

	resolution, err := resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiverConcrete, Method: "Echo",
		Args: []types.TypeID{in.Builtins().String}, ExplicitTypeArgs: []types.TypeID{in.Builtins().String},
		ExpectedResult: in.Builtins().String, AccessModule: "app", SourceKey: "main.sg",
	}, []CallableCandidate{candidate}, in, nil)
	if err != nil {
		t.Fatalf("resolve generic receiver method: %v", err)
	}
	if !slices.Equal(resolution.TemplateArgs, []types.TypeID{in.Builtins().Int, in.Builtins().String}) {
		t.Fatalf("template args = %v, want receiver int then method string", resolution.TemplateArgs)
	}
}

func TestDeferredCallableResolverUsesExactContractABI(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	refReceiver := in.Intern(types.MakeReference(receiver, false))
	refInt := in.Intern(types.MakeReference(in.Builtins().Int, false))

	exact := deferredResolverMethod(10, "app|main.sg:1:2|Pick-exact", "Pick", receiver, in.Builtins().Int)
	exact.ParamTypes = []types.TypeID{receiver, in.Builtins().Int}
	exact.Defaults = []bool{false, false}
	exact.Variadic = []bool{false, false}
	borrowedDecoy := exact
	borrowedDecoy.Symbol = 11
	borrowedDecoy.BodyKey = "app|main.sg:3:4|Pick-borrowed"
	borrowedDecoy.ReceiverType = refReceiver
	borrowedDecoy.ParamTypes = []types.TypeID{refReceiver, refInt}

	resolution, err := resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiver, Method: "Pick",
		Args: []types.TypeID{in.Builtins().Int}, ExpectedResult: in.Builtins().Int,
		AccessModule: "app", SourceKey: "main.sg",
		Requirement: DeferredCallableRequirement{
			Contracts: []symbols.SymbolID{100}, Name: "Pick",
			Params: []types.TypeID{receiver, in.Builtins().Int}, Result: in.Builtins().Int,
		},
	}, []CallableCandidate{borrowedDecoy, exact}, in, nil)
	if err != nil {
		t.Fatalf("resolve exact contract ABI: %v", err)
	}
	if resolution.Callee != exact.Symbol {
		t.Fatalf("contract ABI selected symbol %d, want exact symbol %d", resolution.Callee, exact.Symbol)
	}
}

func TestDeferredCallableResolverTreatsVisibilityAndAttrsAsMinimumRequirements(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	candidate := deferredResolverMethod(10, "app|main.sg:1:2|Pick", "Pick", receiver, in.Builtins().Int)
	candidate.Public = true
	candidate.Attrs = []string{"cold", "intrinsic"}
	request := DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: receiver, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
		Requirement: DeferredCallableRequirement{
			Name: "Pick", Result: in.Builtins().Int, Attrs: []string{"cold"},
		},
	}
	resolution, err := resolveDeferredCallable("use", &request, []CallableCandidate{candidate}, in, nil)
	if err != nil || resolution.Callee != candidate.Symbol {
		t.Fatalf("more-visible intrinsic implementation should satisfy minimum contract requirements: resolution=%+v err=%v", resolution, err)
	}

	request.Requirement.Public = true
	private := candidate
	private.Public = false
	if _, err := resolveDeferredCallable("use", &request, []CallableCandidate{private}, in, nil); err == nil {
		t.Fatalf("private implementation satisfied public contract member")
	}
	missingAttr := candidate
	missingAttr.Attrs = []string{"intrinsic"}
	if _, err := resolveDeferredCallable("use", &request, []CallableCandidate{missingAttr}, in, nil); err == nil {
		t.Fatalf("implementation missing required contract attribute was accepted")
	}
}

func TestDeferredCallableResolverPrefersAliasOverride(t *testing.T) {
	in := deferredResolverTestInterner()
	base := in.RegisterStruct(in.Strings.Intern("Base"), source.Span{File: 1, Start: 1, End: 2})
	alias := in.RegisterAlias(in.Strings.Intern("Alias"), source.Span{File: 1, Start: 3, End: 4})
	in.SetAliasTarget(alias, base)
	baseMethod := deferredResolverMethod(10, "app|main.sg:1:2|Pick-base", "Pick", base, in.Builtins().Int)
	aliasMethod := deferredResolverMethod(11, "app|main.sg:3:4|Pick-alias", "Pick", alias, in.Builtins().Int)
	baseMethod.ParamTypes[0] = in.Intern(types.MakeReference(base, false))
	aliasMethod.ParamTypes[0] = in.Intern(types.MakeReference(alias, false))
	request := DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: alias, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}

	for _, candidates := range [][]CallableCandidate{{baseMethod, aliasMethod}, {aliasMethod, baseMethod}} {
		resolution, err := resolveDeferredCallable("use", &request, candidates, in, nil)
		if err != nil {
			t.Fatalf("resolve alias override: %v", err)
		}
		if resolution.Callee != aliasMethod.Symbol {
			t.Fatalf("alias override selected %d, want %d", resolution.Callee, aliasMethod.Symbol)
		}
	}

	request.Receiver = base
	resolution, err := resolveDeferredCallable("use", &request, []CallableCandidate{aliasMethod, baseMethod}, in, nil)
	if err != nil || resolution.Callee != baseMethod.Symbol {
		t.Fatalf("base receiver selected %+v, err=%v", resolution, err)
	}
}

func TestDeferredCallableResolverUsesStructBaseFallbackAndOverride(t *testing.T) {
	in := deferredResolverTestInterner()
	base := in.RegisterStruct(in.Strings.Intern("Base"), source.Span{File: 1, Start: 1, End: 2})
	derived := in.RegisterStruct(in.Strings.Intern("Derived"), source.Span{File: 1, Start: 3, End: 4})
	in.SetStructBase(derived, base)
	baseMethod := deferredResolverMethod(10, "app|main.sg:1:2|Pick-base", "Pick", base, in.Builtins().Int)
	derivedMethod := deferredResolverMethod(11, "app|main.sg:3:4|Pick-derived", "Pick", derived, in.Builtins().Int)
	baseMethod.ParamTypes[0] = in.Intern(types.MakeReference(base, false))
	derivedMethod.ParamTypes[0] = in.Intern(types.MakeReference(derived, false))
	request := DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: derived, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}

	resolution, err := resolveDeferredCallable("use", &request, []CallableCandidate{baseMethod}, in, nil)
	if err != nil || resolution.Callee != baseMethod.Symbol {
		t.Fatalf("base fallback selected %+v, err=%v", resolution, err)
	}
	for _, candidates := range [][]CallableCandidate{{baseMethod, derivedMethod}, {derivedMethod, baseMethod}} {
		resolution, err = resolveDeferredCallable("use", &request, candidates, in, nil)
		if err != nil {
			t.Fatalf("resolve derived override: %v", err)
		}
		if resolution.Callee != derivedMethod.Symbol {
			t.Fatalf("derived override selected %d, want %d", resolution.Callee, derivedMethod.Symbol)
		}
	}
}

func TestDeferredCallableResolverDoesNotBypassInaccessibleExactOverride(t *testing.T) {
	in := deferredResolverTestInterner()
	base := in.RegisterStruct(in.Strings.Intern("Base"), source.Span{File: 1, Start: 1, End: 2})
	derived := in.RegisterStruct(in.Strings.Intern("Derived"), source.Span{File: 1, Start: 3, End: 4})
	in.SetStructBase(derived, base)
	baseMethod := deferredResolverMethod(10, "app|main.sg:1:2|Pick-base", "Pick", base, in.Builtins().Int)
	privateOverride := deferredResolverMethod(11, "app|private.sg:3:4|Pick-derived", "Pick", derived, in.Builtins().Int)
	privateOverride.FilePrivate = true
	privateOverride.SourceKey = "private.sg"

	_, err := resolveDeferredCallable("use", &DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: derived, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}, []CallableCandidate{baseMethod, privateOverride}, in, nil)
	if err == nil || !strings.Contains(err.Error(), "not accessible") || strings.Contains(err.Error(), baseMethod.BodyKey) {
		t.Fatalf("inaccessible exact override fallback error = %v", err)
	}
}

func TestDeferredCallableResolverUsesNumericFamilyAndExactOverride(t *testing.T) {
	in := deferredResolverTestInterner()
	family := deferredResolverMethod(10, "app|main.sg:1:2|Pick-int", "Pick", in.Builtins().Int, in.Builtins().Int)
	exact := deferredResolverMethod(11, "app|main.sg:3:4|Pick-int32", "Pick", in.Builtins().Int32, in.Builtins().Int)
	request := DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: in.Builtins().Int32, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}

	resolution, err := resolveDeferredCallable("use", &request, []CallableCandidate{family}, in, nil)
	if err != nil || resolution.Callee != family.Symbol {
		t.Fatalf("numeric family fallback selected %+v, err=%v", resolution, err)
	}
	for _, candidates := range [][]CallableCandidate{{family, exact}, {exact, family}} {
		resolution, err = resolveDeferredCallable("use", &request, candidates, in, nil)
		if err != nil || resolution.Callee != exact.Symbol {
			t.Fatalf("numeric exact override selected %+v, err=%v", resolution, err)
		}
	}
}

func TestDeferredCallableResolverMatchesNormalAliasAndBaseDepth(t *testing.T) {
	in := deferredResolverTestInterner()
	base := in.RegisterStruct(in.Strings.Intern("Base"), source.Span{File: 1, Start: 1, End: 2})
	middleAlias := in.RegisterAlias(in.Strings.Intern("Middle"), source.Span{File: 1, Start: 3, End: 4})
	outerAlias := in.RegisterAlias(in.Strings.Intern("Outer"), source.Span{File: 1, Start: 5, End: 6})
	in.SetAliasTarget(middleAlias, base)
	in.SetAliasTarget(outerAlias, middleAlias)
	middleMethod := deferredResolverMethod(10, "app|main.sg:1:2|Pick-middle", "Pick", middleAlias, in.Builtins().Int)
	baseMethod := deferredResolverMethod(11, "app|main.sg:3:4|Pick-base", "Pick", base, in.Builtins().Int)
	request := DeferredCallableRequest{
		Kind: DeferredMethodCall, Receiver: outerAlias, Method: "Pick",
		ExpectedResult: in.Builtins().Int, AccessModule: "app", SourceKey: "main.sg",
	}

	resolution, err := resolveDeferredCallable("use", &request, []CallableCandidate{middleMethod, baseMethod}, in, nil)
	if err != nil || resolution.Callee != baseMethod.Symbol {
		t.Fatalf("terminal alias fallback selected %+v, err=%v", resolution, err)
	}

	middle := in.RegisterStruct(in.Strings.Intern("MiddleBase"), source.Span{File: 1, Start: 7, End: 8})
	leaf := in.RegisterStruct(in.Strings.Intern("Leaf"), source.Span{File: 1, Start: 9, End: 10})
	in.SetStructBase(middle, base)
	in.SetStructBase(leaf, middle)
	request.Receiver = leaf
	if _, err = resolveDeferredCallable("use", &request, []CallableCandidate{baseMethod}, in, nil); err == nil || !strings.Contains(err.Error(), "no exact implementation") {
		t.Fatalf("transitive base fallback drift error = %v", err)
	}
}

func deferredResolverTestInterner() *types.Interner {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	return in
}

func deferredResolverMethod(sym symbols.SymbolID, key, name string, receiver, result types.TypeID) CallableCandidate {
	return CallableCandidate{
		Symbol: sym, BodyKey: key, Name: name,
		ReceiverType: receiver, ParamTypes: []types.TypeID{receiver}, ResultType: result,
		Defaults: []bool{false}, Variadic: []bool{false}, HasSelf: true, HasBody: true,
		ModulePath: "app", Source: source.Span{File: 1, Start: 1, End: 2}, SourceKey: "main.sg",
	}
}
