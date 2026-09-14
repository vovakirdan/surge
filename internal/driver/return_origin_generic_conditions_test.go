package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const genericConditionRefuted = "opaque result type may carry borrowed state"
const genericConditionUnsupported = "opaque result borrowed-state classification is unsupported"
const genericConditionCopy = `fn copy_only<T>(g: fn() -> T) -> nothing {
    let copied = g;
    return nothing;
}
fn probe(g: fn() -> &string) -> nothing { copy_only::<&string>(g); }
`

type genericConditionCase struct {
	genericP0Case
	dependency, result string
}

func genericConditionCases() []genericConditionCase {
	var out []genericConditionCase
	for _, tc := range genericP0Cases() {
		out = append(out, genericConditionCase{genericP0Case: tc})
	}
	for _, tc := range []struct{ name, typ, digest string }{
		{"inactive_owned_dependency", "string", "8a7c853b4caf4d52edd82e1271977dbdfacc8834cab0e9211258867a278c51a0"},
		{"inactive_borrowed_dependency", "&string", "fbbef0845598672504565ec588aa5c5fe53ba895bbc56eb83ea8f1c97846fcc7"},
		{"opaque_array_result", "uint64[]", "7c1522b47674f218ab323886a7a4f863860387555961c2e637eefbed63cfb8bf"},
		{"opaque_range_result", "Range<uint64>", "af91438e3e8d90ed6a5352682f7b698caa2eaa4c0013e9ec5402bd1446d9e8d0"},
	} {
		text := "pragma module::dep, no_std;\n" + genericP0Relay + fmt.Sprintf(`type Holder = { marker: int64 };
extern<Holder> {
    fn dormant(self: &Holder, g: fn() -> %s) -> %s {
        return relay::<%s>(g);
    }
}
`, tc.typ, tc.typ, tc.typ)
		if tc.typ == "uint64[]" {
			text = strings.Replace(text, "type Holder", "type Wrapper = { items: uint64[] };\ntype Holder", 1)
			text = strings.TrimSuffix(text, "}\n") + `    fn wrapped(self: &Holder, g: fn() -> Wrapper) -> Wrapper { return relay::<Wrapper>(g); }
}
`
		}
		out = append(out, genericConditionCase{genericP0Case: genericP0Case{name: tc.name, text: text, digest: tc.digest, subject: "dormant"}, dependency: "dep", result: tc.typ})
	}
	return append(out, genericConditionCase{genericP0Case: genericP0Case{name: "copy_only_borrowed_callback", text: genericConditionCopy, digest: "8b30e391c61ddbbe60d4a065d7aa87e12680b281e7b87bd000c92d5ff63eb040", subject: "copy_only", templates: 1}})
}

// These are desired semantic regressions, separate from P0 admission capture.
// Full dependency inputs remain present; only explicitly named source-local
// assertions tolerate unrelated core obligations.
func TestAnalyzeGenericBorrowedState(t *testing.T) {
	for _, tc := range genericConditionCases() {
		t.Run(tc.name, func(t *testing.T) {
			admitted := false
			t.Cleanup(func() {
				if !admitted {
					t.Log("PRECONDITION: source/authority admission incomplete; no semantic result claimed")
				}
			})
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text)))
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_condition_source", "case": tc.name, "source": tc.text, "sha256": got})
			if got != tc.digest {
				t.Fatal("PRECONDITION: frozen generic condition source changed")
			}
			fixture := originalGenericSignatureFixture(t, tc.text, false, tc.dependency != "")
			caller := originalGenericSignatureCandidate(t, fixture.owner, fixture.authority, fixture.unit, tc.subject)
			if !caller.HasBody || len(caller.TemplateParams) != tc.templates {
				t.Fatal("PRECONDITION: original body/template arity changed")
			}
			switch {
			case tc.dependency != "":
				genericConditionDependency(t, tc, fixture, caller)
			case tc.name == "copy_only_borrowed_callback":
				genericConditionCopyWitness(t, fixture, caller)
			default:
				genericP0Graph(t, tc.genericP0Case, fixture)
				switch {
				case tc.generator:
					genericP0Generator(t, fixture.owner, caller)
				case tc.pattern:
					genericP0Pattern(t, tc.genericP0Case, fixture.owner, caller)
				case tc.name == "known_noreturn":
					genericP0NoReturn(t, fixture, caller)
				case tc.name == "known_nothing":
					id := genericP0Expression(t, fixture.owner, "return nothing;", "nothing")
					lit, ok := fixture.owner.Builder.Exprs.Literal(id)
					if !ok || lit.Kind != ast.ExprLitNothing || fixture.owner.Sema.ExprTypes[id] != fixture.owner.Sema.TypeInterner.Builtins().Nothing {
						t.Fatal("PRECONDITION: known empty result is not the actual nothing literal")
					}
				}
				genericP0Evidence(t, tc.genericP0Case, fixture)
			}
			admitted = true
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), fixture.authority, fixture.inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_condition_analysis", "case": tc.name, "admission_complete": true,
				"analysis": analysis, "analysis_error": errorReturnOriginCallText(err), "source_local_only": tc.dependency != ""})
			if err != nil || analysis == nil {
				t.Fatalf("typed origin analysis failed: %v", err)
			}
			genericConditionOutcome(t, tc, fixture, caller, analysis)
		})
	}
}

func genericConditionDependency(t *testing.T, tc genericConditionCase, f originalGenericFixture, dormant sema.CallableCandidate) {
	t.Helper()
	run := originalGenericSignatureCandidate(t, f.owner, f.authority, f.unit, "run")
	relay := originalGenericSignatureCandidate(t, f.owner, f.authority, f.unit, "relay")
	closure := f.authority.InstantiationClosure
	_, dormantSeeded := f.authority.InstantiationCallableSeeds[dormant.Symbol]
	if len(f.inputs.units) != 12 || closure == nil || len(closure.Instances) != 0 || len(closure.UseSites) != 0 ||
		slices.Contains(closure.LiveCallables, dormant.Symbol) || dormantSeeded ||
		!dormant.HasSelf || len(dormant.ParamTypes) != 2 || len(run.TemplateParams) != 1 || len(relay.TemplateParams) != 1 {
		t.Fatal("PRECONDITION: real dependency is not the original inactive body with zero current uses/instances")
	}
	genericP0Generator(t, f.owner, run)
	outer := genericConditionSelectedCall(t, f, "relay::<"+tc.result+">(g)", relay)
	inner := genericConditionSelectedCall(t, f, "run::<U>(g)", run)
	outerSpan, innerSpan := f.owner.Builder.Exprs.Get(outer).Span, f.owner.Builder.Exprs.Get(inner).Span
	info, ok := f.owner.Sema.TypeInterner.FnInfo(dormant.ParamTypes[1])
	if !ok || info == nil || len(info.Params) != 0 || info.Result != dormant.ResultType || !info.ReturnSources().IsAllInputs() ||
		f.owner.Sema.ExprTypes[outer] != dormant.ResultType || f.owner.Sema.ExprTypes[inner] != relay.TemplateParams[0] {
		t.Fatal("PRECONDITION: callback/result lost its original typed substitution")
	}
	var roots []sema.InstantiationRoot
	var edges []sema.InstantiationEdge
	for _, root := range f.authority.InstantiationGraph.Roots() {
		if root.Witness.SourceKey == f.unit.SourceKey {
			roots = append(roots, root)
		}
	}
	for _, edge := range f.authority.InstantiationGraph.Edges() {
		if edge.Witness.SourceKey == f.unit.SourceKey {
			edges = append(edges, edge)
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_condition_inactive_authority", "case": tc.name,
		"roots": roots, "edges": edges, "closure": closure, "run": run, "relay": relay, "dormant": dormant,
		"callback": info, "result_type": dormant.ResultType, "outer_span": outerSpan, "inner_span": innerSpan})
	wantRoots := 1
	if tc.result == "uint64[]" {
		wantRoots = 2
	}
	if len(roots) != wantRoots || len(edges) != 1 {
		t.Fatal("PRECONDITION: dependency lacks one original concrete root and one original relay edge")
	}
	var root sema.InstantiationRoot
	for _, candidate := range roots {
		if candidate.Witness.Caller == dormant.Symbol {
			root = candidate
		}
	}
	edge := edges[0]
	if root.Kind != sema.InstantiationFunction || root.Template != relay.Symbol || root.Witness.Caller != dormant.Symbol || root.Witness.Site != outerSpan ||
		!slices.Equal(root.TemplateArgs, []types.TypeID{dormant.ResultType}) || edge.Kind != sema.InstantiationFunction ||
		edge.Caller != relay.Symbol || edge.Witness.Caller != relay.Symbol || edge.Callee != run.Symbol || edge.Witness.Site != innerSpan ||
		edge.CallerTemplateArity != 1 || len(edge.CallerBindings) != 1 || !slices.Equal(edge.CalleeTemplateArgs, relay.TemplateParams) {
		t.Fatal("PRECONDITION: original root/edge does not match the selected physical operations")
	}
	binding := edge.CallerBindings[0]
	param, found := f.owner.Sema.TypeInterner.TypeParamInfo(binding.Param)
	if !found || param == nil || binding.Param != relay.TemplateParams[0] || binding.ArgIndex != 0 || binding.ParamIndex != param.Index ||
		binding.Owner != relay.Symbol || symbols.SymbolID(param.Owner) != originalGenericSignatureLocal(t, f.unit, relay) {
		t.Fatal("PRECONDITION: relay binding lost canonical/original owner identity")
	}
	genericConditionConcreteType(t, tc, f, dormant.ResultType)
	if tc.result == "uint64[]" {
		wrapped := originalGenericSignatureCandidate(t, f.owner, f.authority, f.unit, "wrapped")
		id := genericConditionSelectedCall(t, f, "relay::<Wrapper>(g)", relay)
		span := f.owner.Builder.Exprs.Get(id).Span
		info, ok := f.owner.Sema.TypeInterner.StructInfo(wrapped.ResultType)
		callback, hasFn := f.owner.Sema.TypeInterner.FnInfo(wrapped.ParamTypes[1])
		_, seeded := f.authority.InstantiationCallableSeeds[wrapped.Symbol]
		if seeded || slices.Contains(closure.LiveCallables, wrapped.Symbol) || !ok || info == nil ||
			info.Decl.File != f.owner.File.ID || len(info.Fields) != 1 || info.Fields[0].Type != dormant.ResultType ||
			!hasFn || callback == nil || len(callback.Params) != 0 || callback.Result != wrapped.ResultType ||
			!callback.ReturnSources().IsAllInputs() || f.owner.Sema.ExprTypes[id] != wrapped.ResultType {
			t.Fatal("PRECONDITION: nested hidden-state root lost its source-owned Wrapper containing the same Array")
		}
		matches := 0
		for _, r := range roots {
			if r.Kind == sema.InstantiationFunction && r.Template == relay.Symbol && r.Witness.Caller == wrapped.Symbol &&
				r.Witness.Site == span && slices.Equal(r.TemplateArgs, []types.TypeID{wrapped.ResultType}) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatal("PRECONDITION: nested Wrapper root lacks one exact original authority")
		}
		logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_condition_nested_hidden_type", "wrapper": info, "caller": wrapped, "span": span})
	}
}

func genericConditionSelectedCall(t *testing.T, f originalGenericFixture, text string, callee sema.CallableCandidate) ast.ExprID {
	t.Helper()
	id := genericP0Expression(t, f.owner, text, text)
	call, ok := f.owner.Builder.Exprs.Call(id)
	if !ok || call == nil || len(call.Args) != 1 || f.owner.Symbols.ExprSymbols[id] != originalGenericSignatureLocal(t, f.unit, callee) ||
		f.owner.Sema.ExprTypes[call.Args[0].Value] == types.NoTypeID {
		t.Fatal("PRECONDITION: source operation lost the selected declaration or typed callback operand")
	}
	return id
}

func genericConditionConcreteType(t *testing.T, tc genericConditionCase, f originalGenericFixture, id types.TypeID) {
	t.Helper()
	in := f.owner.Sema.TypeInterner
	typ, ok := in.Lookup(id)
	if !ok {
		t.Fatal("PRECONDITION: concrete result descriptor is missing")
	}
	switch tc.result {
	case "string":
		if id != in.Builtins().String {
			t.Fatal("PRECONDITION: inactive owned result is not string")
		}
	case "&string":
		if typ.Kind != types.KindReference || typ.Mutable || typ.Elem != in.Builtins().String {
			t.Fatal("PRECONDITION: inactive borrowed result is not &string")
		}
	default:
		info, found := in.StructInfo(id)
		if !found || info == nil || !slices.Equal(info.TypeArgs, []types.TypeID{in.Builtins().Uint64}) {
			t.Fatal("PRECONDITION: hidden-state result lost its concrete nominal argument")
		}
		if tc.result == "uint64[]" {
			base, present := in.StructInfo(in.ArrayNominalType())
			if !present || base == nil || info.Name != base.Name || info.Decl != base.Decl {
				t.Fatal("PRECONDITION: array result is not the registered Array declaration")
			}
		} else {
			file := f.owner.FileSet.Get(info.Decl.File)
			if file == nil || !strings.HasSuffix(file.Path, "/core/intrinsics.sg") || info.Decl.End > uint32(len(file.Content)) ||
				!strings.Contains(string(file.Content[info.Decl.Start:info.Decl.End]), "type Range<T>") {
				t.Fatal("PRECONDITION: range result does not retain the original core Range declaration")
			}
		}
		logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_condition_hidden_type", "case": tc.name, "id": id, "type": typ, "nominal": info})
	}
}

func genericConditionCopyWitness(t *testing.T, f originalGenericFixture, copyFn sema.CallableCandidate) {
	t.Helper()
	probe := originalGenericSignatureCandidate(t, f.owner, f.authority, f.unit, "probe")
	in := f.owner.Sema.TypeInterner
	if len(copyFn.ParamTypes) != 1 || len(probe.ParamTypes) != 1 || len(copyFn.TemplateParams) != 1 {
		t.Fatal("PRECONDITION: copy-only parameter/template arity changed")
	}
	info, ok := in.FnInfo(copyFn.ParamTypes[0])
	actual, actualOK := in.FnInfo(probe.ParamTypes[0])
	if !ok || !actualOK || info == nil || actual == nil || len(info.Params) != 0 || len(actual.Params) != 0 ||
		info.Result != copyFn.TemplateParams[0] || !info.ReturnSources().IsAllInputs() || !actual.ReturnSources().IsAllInputs() ||
		copyFn.ResultType != in.Builtins().Nothing || probe.ResultType != in.Builtins().Nothing {
		t.Fatal("PRECONDITION: copying callback lost its symbolic and concrete original contracts")
	}
	typ, found := in.Lookup(actual.Result)
	if !found || typ.Kind != types.KindReference || typ.Mutable || typ.Elem != in.Builtins().String {
		t.Fatal("PRECONDITION: copy-only actual callback must return &string")
	}
	read := genericP0Expression(t, f.owner, "let copied = g;", "g")
	sym := f.owner.Symbols.Table.Symbols.Get(f.owner.Symbols.ExprSymbols[read])
	if sym == nil || sym.Kind != symbols.SymbolParam || sym.Type != copyFn.ParamTypes[0] || f.owner.Sema.ExprTypes[read] != sym.Type {
		t.Fatal("PRECONDITION: callback copy did not read its exact incoming parameter")
	}
	copies := 0
	for _, binding := range f.owner.Symbols.Table.Symbols.Data() {
		name, _ := f.owner.Builder.StringsInterner.Lookup(binding.Name)
		if name == "copied" && binding.Kind == symbols.SymbolLet && binding.Decl.ASTFile == f.owner.FileID && binding.Type == sym.Type {
			copies++
		}
	}
	if copies != 1 {
		t.Fatal("PRECONDITION: copied callback lost its actual typed let binding")
	}
	callID := genericConditionSelectedCall(t, f, "copy_only::<&string>(g)", copyFn)
	calls := 0
	for id := range f.owner.Sema.ExprTypes {
		if node := f.owner.Builder.Exprs.Get(id); node != nil && node.Kind == ast.ExprCall {
			calls++
			if id != callID {
				t.Fatal("PRECONDITION: copy-only source unexpectedly invokes its opaque callback")
			}
		}
	}
	closure, roots, edges := f.authority.InstantiationClosure, f.authority.InstantiationGraph.Roots(), f.authority.InstantiationGraph.Edges()
	if calls != 1 || len(f.inputs.units) != 1 || len(roots) != 1 || len(edges) != 0 || closure == nil || len(closure.Instances) != 1 || len(closure.UseSites) != 1 {
		t.Fatal("PRECONDITION: copy-only source lost its one actual root/current use")
	}
	root, use, instance := roots[0], closure.UseSites[0], closure.Instances[0]
	span := f.owner.Builder.Exprs.Get(callID).Span
	key, err := sema.NewInstanceKey(*f.authority.InstantiationIdentity, copyFn.Symbol, []types.TypeID{actual.Result})
	if err != nil || root.Template != copyFn.Symbol || root.Witness.Caller != probe.Symbol || root.Witness.Site != span ||
		!slices.Equal(root.TemplateArgs, []types.TypeID{actual.Result}) || instance.Key != key || instance.Template != copyFn.Symbol ||
		!slices.Equal(instance.TemplateArgs, root.TemplateArgs) || use.Callee != key || use.CalleeTemplate != copyFn.Symbol ||
		use.CallerTemplate != probe.Symbol || use.Caller != (sema.InstanceKey{}) || len(use.CallerTemplateArgs) != 0 ||
		use.Site != span || use.SourceKey != f.unit.SourceKey || !slices.Equal(use.TemplateArgs, root.TemplateArgs) {
		t.Fatal("PRECONDITION: copy-only generic call lost its original/current canonical binding")
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_condition_copy_authority", "read": read, "symbol": sym,
		"copy": copyFn, "probe": probe, "original_fn": info, "actual_fn": actual, "root": root, "closure": closure, "span": span})
}

func genericConditionOutcome(t *testing.T, tc genericConditionCase, f originalGenericFixture, caller sema.CallableCandidate, analysis *sema.ReturnOriginAnalysis) {
	t.Helper()
	if tc.name == "opaque_borrowed_result" || tc.dependency != "" && tc.result != "string" {
		text, reason := "relay::<&string>(g)", genericConditionRefuted
		if tc.dependency != "" {
			text = "relay::<" + tc.result + ">(g)"
		}
		if tc.result == "uint64[]" || tc.result == "Range<uint64>" {
			reason = genericConditionUnsupported
		}
		operations := []string{text}
		if tc.result == "uint64[]" {
			operations = append(operations, "relay::<Wrapper>(g)")
		}
		for _, operation := range operations {
			start := strings.Index(tc.text, operation)
			want := sema.ReturnOriginPending{SourceKey: f.unit.SourceKey, Span: source.Span{File: f.owner.File.ID, Start: uint32(start), End: uint32(start + len(operation))}, Reason: reason}
			if start < 0 || !slices.Contains(analysis.Pending, want) || analysis.Complete() {
				t.Errorf("missing source-specific borrowed-state refusal: want=%+v", want)
			}
		}
	} else {
		for _, pending := range analysis.Pending {
			if tc.dependency == "" || pending.SourceKey == f.unit.SourceKey || pending.Span.File == f.owner.File.ID {
				t.Errorf("unfinished source obligation: %+v", pending)
			}
		}
		if tc.dependency == "" && !analysis.Complete() {
			t.Error("single-unit semantic analysis is not complete")
		}
		for _, summary := range analysis.Summaries {
			if summary.Source.File != f.owner.File.ID {
				continue
			}
			var slots []uint32
			if strings.HasPrefix(tc.name, "nested_") {
				slots = []uint32{0, 1}
			}
			if summary.Unknown || summary.NoNormalReturn != (tc.name == "known_noreturn") || !slices.Equal(summary.ParamSlots, slots) {
				t.Errorf("wrong original source summary: %+v wantSources=%v", summary, slots)
			}
		}
	}
	for _, declared := range f.authority.CallableCandidates {
		if !declared.HasBody || declared.Source.File != f.owner.File.ID {
			continue
		}
		found := false
		for _, summary := range analysis.Summaries {
			found = found || summary.BodyKey == declared.BodyKey && summary.Source == declared.Source
		}
		if !found {
			t.Errorf("analysis omitted original body %s (subject %s)", declared.Name, caller.Name)
		}
	}
	escape := tc.name == "local_escape_in_template" || tc.name == "pattern_storage_escape"
	matched := 0
	for _, d := range analysis.Diagnostics {
		if tc.dependency != "" && d.Primary.File != f.owner.File.ID {
			continue
		}
		owner, start, end, ownerStart, ownerEnd := "owned", uint32(73), uint32(84), uint32(51), uint32(72)
		if tc.pattern {
			owner, start, end, ownerStart, ownerEnd = "v", 150, 161, 144, 145
		}
		primary := source.Span{File: f.owner.File.ID, Start: start, End: end}
		wantNote := diag.Note{Span: source.Span{File: f.owner.File.ID, Start: ownerStart, End: ownerEnd}, Msg: fmt.Sprintf("'%s' owns storage that ends in this scope", owner)}
		if !escape || d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError || d.Primary != primary ||
			d.Message != fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", owner) || !slices.Contains(d.Notes, wantNote) || len(d.Help) == 0 {
			t.Errorf("unexpected source diagnostic: %+v", d)
		} else {
			matched++
		}
	}
	if escape && matched != 1 {
		t.Errorf("want exactly one real local-owner SEM3139; got %d", matched)
	}
}
