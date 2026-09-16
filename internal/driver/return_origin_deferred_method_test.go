package driver

import (
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Deferred contract methods and a template's by-value actual. Every leaf
// freezes its source and each span it reads before any fixture runs.

const deferredOpaqueRefusal = "opaque call may change reference-bearing or callable contents"

const deferredLoanRefusal = "storage loan would be discarded by a payload-free value"

const deferredTypeRefused = "opaque result type may carry borrowed state"

type deferredMethodLeaf struct {
	name, text, digest, body string
	stays, gone              []originRefusal
	only                     bool
}

func deferredMethodLocal(analysis *sema.ReturnOriginAnalysis, f originalGenericFixture) []sema.ReturnOriginPending {
	var local []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == f.unit.SourceKey || pending.Span.File == f.owner.File.ID {
			local = append(local, pending)
		}
	}
	return local
}

// A body leaf keeps each stays row, loses each gone row (an empty reason is any
// reason), and when body is named leaves no local Pending and a sourceless summary.
func checkDeferredMethodLeaf(t *testing.T, analysis *sema.ReturnOriginAnalysis, f originalGenericFixture, leaf deferredMethodLeaf) {
	t.Helper()
	local := deferredMethodLocal(analysis, f)
	for _, want := range leaf.stays {
		if !originPendingAt(analysis, f.unit.SourceKey, want.span, want.reason) {
			t.Errorf("lost %q at %d:%d %q: %+v", want.reason, want.span.start, want.span.end, want.span.snippet, local)
		}
	}
	for _, gone := range leaf.gone {
		if originPendingAt(analysis, f.unit.SourceKey, gone.span, gone.reason) {
			t.Errorf("still refuses %q at %d:%d %q", gone.reason, gone.span.start, gone.span.end, gone.span.snippet)
		}
	}
	if leaf.only && len(local) != len(leaf.stays) {
		t.Errorf("local Pending is not exactly %+v: %+v", leaf.stays, local)
	}
	if leaf.body == "" {
		return
	}
	for _, pending := range local {
		t.Errorf("%s left unfinished: %s at %d:%d", leaf.body, pending.Reason, pending.Span.Start, pending.Span.End)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, leaf.body, false, nil)
}

func TestAnalyzeDeferredContractMethodBodies(t *testing.T) {
	for _, leaf := range []deferredMethodLeaf{
		{name: "m1_ref_free_result", body: "size", digest: "fc6200e00f9e95db79d7d88b97ab1bd5a276607d694e8432dd875270acb0299f",
			gone: []originRefusal{{originSpan{124, 134, "x.__size()"}, ""}},
			text: "pragma module::dep;\ncontract Sized<T> {\n    fn __size(self: &T) -> uint;\n}\nfn size<T: Sized<T>>(x: &T) -> uint {\n    return x.__size();\n}\n"},
		{name: "m2_template_result", body: "top", digest: "abb156cbf17fbb2efb9e8218a2f41e71d3dbb462091ba7c711086d5799014db3",
			gone: []originRefusal{{originSpan{99, 108, "T.__top()"}, ""}, {originSpan{92, 109, "return T.__top();"}, ""}, {originSpan{81, 85, "-> T"}, ""}},
			text: "pragma module::dep;\ncontract Top<T> {\n    fn __top() -> T;\n}\nfn top<T: Top<T>>() -> T {\n    return T.__top();\n}\n"},
		{name: "m3_value_receiver", body: "absorb", digest: "e6eb8744324ad055e04c1f40e7af49ce40f18f9854288cbf36901871c74a4fa5",
			gone: []originRefusal{{originSpan{128, 140, "x.__absorb()"}, ""}},
			text: "pragma module::dep;\ncontract Absorb<T> {\n    fn __absorb(self: T) -> uint;\n}\nfn absorb<T: Absorb<T>>(x: T) -> uint {\n    return x.__absorb();\n}\n"},
		// A reference-free type is not a borrow-free value: a returned array view keeps a refusal.
		{name: "mc3_view_result", digest: "a97ab740a8a739e9aae354a76b8cf07dfe9456d0a4494a0288b1747083682d03",
			stays: []originRefusal{{originSpan{133, 143, "x.__view()"}, genericConditionUnsupported}},
			gone:  []originRefusal{{originSpan{133, 143, "x.__view()"}, originCallRefusal}},
			text:  "pragma module::dep;\ncontract Viewer<T> {\n    fn __view(self: &T) -> uint64[];\n}\nfn get<T: Viewer<T>>(x: &T) -> uint64[] {\n    return x.__view();\n}\n"},
		// A contract member's @return_source is never believed.
		{name: "mc1_promise_not_trusted", digest: "987519c14fc0cd616b64ae67c9d71a16fdde850cb0e16a8ee71edefe12f3f6ec",
			stays: []originRefusal{{originSpan{171, 182, "x.__pick(s)"}, originCallRefusal}, {originSpan{164, 183, "return x.__pick(s);"}, originOutgoingRefusal},
				{originSpan{147, 157, "-> &string"}, originResultRefusal}},
			text: "pragma module::dep;\ncontract Pick<T> {\n    fn __pick(@return_source self: &T, other: &string) -> &string;\n}\nfn pick<T: Pick<T>>(x: &T, s: &string) -> &string {\n    return x.__pick(s);\n}\n"},
		{name: "mc2_loan_actual", only: true, digest: "831d46922e75c63242decc789711451db9214b92a17c6563a7fd55bb051f2b88",
			stays: []originRefusal{{originSpan{198, 217, "x.__eat(xs[[1..3]])"}, deferredLoanRefusal}},
			text:  "pragma module::dep;\ncontract Eat<T> {\n    fn __eat(self: &T, v: uint64[]) -> uint;\n}\nfn feed<T: Eat<T>>(x: &T) -> uint {\n    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];\n    return x.__eat(xs[[1..3]]);\n}\n"},
		// Outside core, exit cannot take a generic E (ErrorLike's fields resolve only in core's builder);
		// core Erring.exit's own relay is witnessed by i3 and the census.
		{name: "e1_template_relay", body: "relay", digest: "b0e08342179ba31845f7fa43cc55812c02196a0d4d243049a870ab6d2dd901bc",
			gone: []originRefusal{{originSpan{96, 104, "stash(v)"}, ""}},
			text: "pragma module::dep;\n@intrinsic fn stash<X>(v: X) -> nothing;\nfn relay<T>(v: T) -> nothing {\n    stash(v);\n}\n"},
		{name: "ec1_reference_formal", digest: "a74799974148b57d1dd7f916f7bc0c3b5a394b526623a13dedf26a74ecd10e91",
			stays: []originRefusal{{originSpan{95, 102, "peek(v)"}, ""}},
			text:  "pragma module::dep;\n@intrinsic fn peek<X>(x: &X) -> nothing;\nfn look<T>(v: T) -> nothing {\n    peek(v);\n}\n"},
		{name: "ec2_wrapped_actual", digest: "739592e32a77dbd80489c70628b343484976f025e7afad6e71ba902bd779178c",
			stays: []originRefusal{{originSpan{94, 107, "keep(Some(v))"}, deferredOpaqueRefusal}},
			text:  "pragma module::dep;\n@intrinsic fn keep<X>(x: X) -> nothing;\nfn wrap<T>(v: T) -> nothing {\n    keep(Some(v));\n}\n"},
		{name: "ec3_callback", digest: "6aed24ed221f5b692c45a1c43f2db65ac26feaa03828558cbf27824799757f37",
			stays: []originRefusal{{originSpan{80, 84, "f(v)"}, deferredOpaqueRefusal}},
			text:  "pragma module::dep;\nfn call_with<T>(f: fn(T) -> nothing, v: T) -> nothing {\n    f(v);\n}\n"},
	} {
		t.Run(leaf.name, func(t *testing.T) {
			var spans []originSpan
			for _, refusal := range append(slices.Clone(leaf.stays), leaf.gone...) {
				spans = append(spans, refusal.span)
			}
			checkOriginSource(t, leaf.text, leaf.digest, spans...)
			f, analysis := analyzeOriginDependency(t, "deferred_method_body_"+leaf.name, leaf.text, nil)
			checkDeferredMethodLeaf(t, analysis, f, leaf)
		})
	}
}

// deferredCoreSpan freezes a span of one original core unit by its own text.
func deferredCoreSpan(t *testing.T, f originalGenericFixture, key string, start, end int, snippet string) originSpan {
	t.Helper()
	for _, unit := range f.inputs.units {
		if unit.SourceKey != key {
			continue
		}
		content := f.owner.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File).Content
		if start >= 0 && start < end && end <= len(content) && string(content[start:end]) == snippet {
			return originSpan{start, end, snippet}
		}
	}
	t.Fatalf("PRECONDITION: %s %d:%d is not %q", key, start, end, snippet)
	return originSpan{}
}

func deferredMethodOutcomes(t *testing.T, f originalGenericFixture, key string, span originSpan) []sema.ResolvedDeferredCall {
	t.Helper()
	var out []sema.ResolvedDeferredCall
	for _, call := range f.authority.InstantiationClosure.ResolvedDeferredCalls {
		if call.Kind == sema.DeferredMethodCall && call.SourceKey == key && int(call.Site.Start) == span.start && int(call.Site.End) == span.end {
			out = append(out, call)
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"method_outcomes": out, "source_key": key, "site": span})
	return out
}

func deferredCandidateName(f originalGenericFixture, symbol symbols.SymbolID) string {
	for _, candidate := range f.authority.CallableCandidates {
		if candidate.Symbol == symbol {
			return candidate.Name
		}
	}
	return ""
}

func deferredReferenceTo(f originalGenericFixture, id, elem types.TypeID) bool {
	info, ok := f.authority.TypeInterner.Lookup(id)
	return ok && info.Kind == types.KindReference && (elem == types.NoTypeID || info.Elem == elem)
}

func deferredUsesAt(f originalGenericFixture, key string, span originSpan) []sema.ConcreteInstantiationUse {
	var out []sema.ConcreteInstantiationUse
	for _, use := range f.authority.InstantiationClosure.UseSites {
		if use.SourceKey == key && int(use.Site.Start) == span.start && int(use.Site.End) == span.end {
			out = append(out, use)
		}
	}
	return out
}

func deferredCleared(t *testing.T, analysis *sema.ReturnOriginAnalysis, key string, spans ...originSpan) {
	t.Helper()
	for _, span := range spans {
		if originPendingAt(analysis, key, span, "") {
			t.Errorf("%s %d:%d %q still refuses: %+v", key, span.start, span.end, span.snippet, originPendingWithin(analysis, key, span.start, span.end))
		}
	}
}

type deferredInstanceLeaf struct {
	name, text, digest string
	spans              []originSpan
	prepare            func(t *testing.T, f originalGenericFixture)
	check              func(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis)
}

func TestAnalyzeDeferredMethodInstances(t *testing.T) {
	baseLen := func(t *testing.T, f originalGenericFixture) originSpan {
		return deferredCoreSpan(t, f, "core/base.sg", 2225, 2237, "self.__len()")
	}
	rootClean := func(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
		for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, 0, 1<<30) {
			t.Errorf("root left unfinished: %s at %d:%d", pending.Reason, pending.Span.Start, pending.Span.End)
		}
	}
	for _, leaf := range []deferredInstanceLeaf{
		{name: "i1_len_string", digest: "b1af4a84e63c86637924d8183541ae0851da096dd439495e25dcf349181ab923",
			text: "fn probe(s: &string) -> uint {\n    return len(s);\n}\n", spans: []originSpan{{42, 48, "len(s)"}},
			prepare: func(t *testing.T, f originalGenericFixture) {
				calls := deferredMethodOutcomes(t, f, "core/base.sg", baseLen(t, f))
				if len(calls) != 1 || calls[0].UseID != "core/base.sg/2225:2237/1/0" || calls[0].Outcome != sema.DeferredCallableResolved ||
					len(calls[0].CalleeParamTypes) != 1 || !deferredReferenceTo(f, calls[0].CalleeParamTypes[0], f.authority.TypeInterner.Builtins().String) {
					t.Fatal("PRECONDITION: len<string> lacks its one resolved &string method outcome")
				}
			},
			check: func(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
				deferredCleared(t, analysis, "core/base.sg", baseLen(t, f))
				// Measured before P1p and outside it: len(s) calls a self function with no receiver expression.
				if root := originPendingWithin(analysis, f.unit.SourceKey, 0, 1<<30); len(root) != 1 ||
					!originPendingAt(analysis, f.unit.SourceKey, originSpan{42, 48, "len(s)"}, "generic original method call lacks its receiver expression") {
					t.Errorf("root Pending is not exactly the receiver-expression row at len(s): %+v", root)
				}
			}},
		{name: "i2_max_int32", digest: "bb52e1f8c4433221ced15e3098d79edb7188f575ae998a3e4658a7065b6d7ad8",
			text: "fn probe() -> int32 {\n    return max_value::<int32>();\n}\n", spans: []originSpan{{33, 53, "max_value::<int32>()"}},
			prepare: func(t *testing.T, f originalGenericFixture) {
				calls := deferredMethodOutcomes(t, f, "core/intrinsics.sg", deferredCoreSpan(t, f, "core/intrinsics.sg", 11606, 11621, "T.__max_value()"))
				if len(calls) != 1 || calls[0].Outcome != sema.DeferredCallableResolved || len(calls[0].CalleeParamTypes) != 0 {
					t.Fatal("PRECONDITION: max_value<int32> lacks its one resolved static method outcome")
				}
			},
			check: func(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
				deferredCleared(t, analysis, "core/intrinsics.sg", deferredCoreSpan(t, f, "core/intrinsics.sg", 11588, 11592, "-> T"),
					deferredCoreSpan(t, f, "core/intrinsics.sg", 11599, 11622, "return T.__max_value();"), deferredCoreSpan(t, f, "core/intrinsics.sg", 11606, 11621, "T.__max_value()"))
				rootClean(t, f, analysis)
			}},
		{name: "i3_erring_exit", digest: "44be6341044bb0908df659e8b02eb7330ef47ebcc343d0df0f7f94f38eb5768f",
			text: "fn probe(r: Erring<int, Error>) -> int {\n    return r.exit();\n}\n", spans: []originSpan{{52, 60, "r.exit()"}},
			prepare: func(t *testing.T, f originalGenericFixture) {
				uses := deferredUsesAt(f, "core/result.sg", deferredCoreSpan(t, f, "core/result.sg", 671, 680, "exit(err)"))
				logReturnOriginCallEvidence(t, map[string]any{"exit_uses": uses})
				if len(uses) != 1 || deferredCandidateName(f, uses[0].CalleeTemplate) != "exit" || uses[0].Caller == (sema.InstanceKey{}) ||
					deferredCandidateName(f, uses[0].CallerTemplate) != "exit" || len(uses[0].CallerTemplateArgs) != 2 ||
					uses[0].CallerTemplateArgs[0] != f.authority.TypeInterner.Builtins().Int {
					t.Fatal("PRECONDITION: exit lacks its one current use from Erring<int, Error>.exit")
				}
			},
			check: func(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
				deferredCleared(t, analysis, "core/result.sg", deferredCoreSpan(t, f, "core/result.sg", 671, 680, "exit(err)"))
				deferredCleared(t, analysis, f.unit.SourceKey, originSpan{52, 60, "r.exit()"})
			}},
		// The map's filter-only form would drop this loan; the recorded requirement refuses it at the use.
		{name: "i4_relay_loan", digest: "a3e6de53ab0beef475aa8f909a27a299932b2f513bc70854f3ee194f36a4c7da",
			text:  "@intrinsic fn stash<T>(v: T) -> nothing;\nfn relay<U>(v: U) -> nothing {\n    stash(v);\n}\nfn hide() -> nothing {\n    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];\n    relay::<uint64[]>(xs[[1..3]]);\n    return nothing;\n}\n",
			spans: []originSpan{{76, 84, "stash(v)"}, {181, 210, "relay::<uint64[]>(xs[[1..3]])"}},
			prepare: func(t *testing.T, f originalGenericFixture) {
				uses := deferredUsesAt(f, f.unit.SourceKey, originSpan{181, 210, "relay::<uint64[]>(xs[[1..3]])"})
				logReturnOriginCallEvidence(t, map[string]any{"relay_uses": uses})
				if len(uses) != 1 || uses[0].Caller != (sema.InstanceKey{}) || deferredCandidateName(f, uses[0].CalleeTemplate) != "relay" || len(uses[0].TemplateArgs) != 1 {
					t.Fatal("PRECONDITION: relay lacks its one root use with one template argument")
				}
			},
			check: func(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
				if analysis.Complete() || !originPendingAt(analysis, f.unit.SourceKey, originSpan{181, 210, "relay::<uint64[]>(xs[[1..3]])"}, genericConditionUnsupported) {
					t.Errorf("relay's use lost its requirement refusal: %+v", originPendingWithin(analysis, f.unit.SourceKey, 0, 1<<30))
				}
				deferredCleared(t, analysis, f.unit.SourceKey, originSpan{76, 84, "stash(v)"})
			}},
		// B1 is closed by P1p-G: the generic implementation's own use is routed, and only the
		// pre-existing root receiver-expression row stays.
		{name: "i5_generic_impl_len", digest: "82d9d5484d2375c9bbee4b3b3c66771b487b95745810f460b54148324c058b7c",
			text: "fn probe(xs: &int[]) -> uint {\n    return len(xs);\n}\n", spans: []originSpan{{42, 49, "len(xs)"}},
			prepare: func(t *testing.T, f originalGenericFixture) {
				calls := deferredMethodOutcomes(t, f, "core/base.sg", baseLen(t, f))
				if len(calls) != 1 || len(calls[0].CalleeTemplateArgs) == 0 {
					t.Fatal("PRECONDITION: len<int[]> lacks its one generic implementation outcome")
				}
			},
			check: func(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
				deferredCleared(t, analysis, "core/base.sg", baseLen(t, f))
				if root := originPendingWithin(analysis, f.unit.SourceKey, 0, 1<<30); len(root) != 1 ||
					!originPendingAt(analysis, f.unit.SourceKey, originSpan{42, 49, "len(xs)"}, "generic original method call lacks its receiver expression") {
					t.Errorf("root Pending is not exactly the receiver-expression row at len(xs): %+v", root)
				}
			}},
	} {
		t.Run(leaf.name, func(t *testing.T) {
			checkOriginSource(t, leaf.text, leaf.digest, leaf.spans...)
			f, analysis := analyzeOriginRoot(t, "deferred_method_instance_"+leaf.name, leaf.text, false, func(f originalGenericFixture) {
				leaf.prepare(t, f)
			})
			leaf.check(t, f, analysis)
		})
	}
}

func TestAnalyzeDeferredMethodEscapes(t *testing.T) {
	for _, leaf := range []struct {
		name, text, digest, owner string
		escape, note              originSpan
		cleared                   []originSpan
	}{
		{"x1_template_local", "contract Top<T> {\n    fn __top() -> T;\n}\nfn leak<T: Top<T>>() -> &T {\n    let v: T = T.__top();\n    return &v;\n}\n",
			"e2d10fcc2c99712d000715517a52491eb36c8f504cd4c56a557460d91b550429", "v",
			originSpan{100, 110, "return &v;"}, originSpan{74, 95, "let v: T = T.__top();"}, []originSpan{{62, 67, "-> &T"}, {85, 94, "T.__top()"}}},
		{"x2_relay_then_local", "@intrinsic fn stash<X>(v: X) -> nothing;\nfn relay_or_leak<T>(v: T) -> &uint {\n    stash(v);\n    let code: uint = 7:uint;\n    return &code;\n}\n",
			"76bd007743b36959c78d716b1a7cc6d34f900869637b2b0960c8fec449da0f49", "code",
			originSpan{125, 138, "return &code;"}, originSpan{96, 120, "let code: uint = 7:uint;"}, []originSpan{{67, 75, "-> &uint"}, {82, 90, "stash(v)"}}},
	} {
		t.Run(leaf.name, func(t *testing.T) {
			checkOriginSource(t, leaf.text, leaf.digest, append([]originSpan{leaf.escape, leaf.note}, leaf.cleared...)...)
			f, analysis := analyzeOriginRoot(t, "deferred_method_escape_"+leaf.name, leaf.text, true, nil)
			requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, leaf.escape, leaf.owner)
			var root []diag.Diagnostic
			for _, d := range analysis.Diagnostics {
				if d.Primary.File == f.owner.File.ID {
					root = append(root, d)
				}
			}
			note := source.Span{File: f.owner.File.ID, Start: uint32(leaf.note.start), End: uint32(leaf.note.end)}
			if len(root) != 1 || !slices.ContainsFunc(root[0].Notes, func(n diag.Note) bool { return n.Span == note }) {
				t.Errorf("root diagnostics are not exactly one SEM3139 noting %q: %+v", leaf.note.snippet, root)
			}
			// With the deferred result proven, the returned borrow is a local escape diagnostic, not a Pending.
			if pending := originPendingWithin(analysis, f.unit.SourceKey, 0, 1<<30); len(pending) != 0 {
				t.Errorf("root Pending is not empty: %+v", pending)
			}
			deferredCleared(t, analysis, f.unit.SourceKey, leaf.cleared...)
		})
	}
	// B-1 on a real instance: Holder.__view returns a view of its own field.
	t.Run("i6_view_leak", func(t *testing.T) {
		const text = "contract Viewer<T> {\n    fn __view(self: &T) -> uint64[];\n}\ntype Holder = { buf: uint64[4] };\nextern<Holder> {\n    fn __view(self: &Holder) -> uint64[] {\n        return self.buf[[0..2]];\n    }\n}\nfn get<T: Viewer<T>>(x: &T) -> uint64[] {\n    return x.__view();\n}\nfn leak() -> uint64[] {\n    let h: Holder = Holder { buf = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };\n    return get(&h);\n}\n"
		view := originSpan{248, 258, "x.__view()"}
		checkOriginSource(t, text, "70e72d720c78f0fd76276756e81822c730685b1e732d57cb50de3a0ef654e268", view, originSpan{376, 383, "get(&h)"})
		f, analysis := analyzeOriginRoot(t, "deferred_method_escape_i6_view_leak", text, true, func(f originalGenericFixture) {
			calls := deferredMethodOutcomes(t, f, f.unit.SourceKey, view)
			if len(calls) != 1 || calls[0].Outcome != sema.DeferredCallableResolved || len(calls[0].CalleeParamTypes) != 1 ||
				!deferredReferenceTo(f, calls[0].CalleeParamTypes[0], types.NoTypeID) {
				t.Fatal("PRECONDITION: x.__view() lacks its one resolved &Holder method outcome")
			}
		})
		var views []sema.ReturnOriginSummary
		for _, summary := range analysis.Summaries {
			if summary.Name == "__view" {
				views = append(views, summary)
			}
		}
		logReturnOriginCallEvidence(t, map[string]any{"view_summaries": views, "diagnostics": analysis.Diagnostics})
		if analysis.Complete() || !originPendingAt(analysis, f.unit.SourceKey, view, genericConditionUnsupported) || originPendingAt(analysis, f.unit.SourceKey, view, originCallRefusal) {
			t.Errorf("returned view lost its classification refusal: %+v", originPendingWithin(analysis, f.unit.SourceKey, 0, 1<<30))
		}
	})
}

// L1 at its public edge: a body-less result of a loan-carrying or borrowed-view
// type is never classified borrow-free.
func TestReturnOriginLoanCarrierClassification(t *testing.T) {
	t.Run("carriers", func(t *testing.T) {
		const text = "pragma module::dep;\n@intrinsic fn mk_dyn() -> uint64[];\n@intrinsic fn mk_fixed() -> uint64[4];\n@intrinsic fn mk_range() -> Range<int>;\n@intrinsic fn mk_view() -> BytesView;\n@intrinsic fn mk_count() -> uint;\n"
		want := []originRefusal{{originSpan{34, 40, "mk_dyn"}, genericConditionUnsupported}, {originSpan{70, 78, "mk_fixed"}, genericConditionUnsupported},
			{originSpan{109, 117, "mk_range"}, genericConditionUnsupported}, {originSpan{149, 156, "mk_view"}, deferredTypeRefused}}
		control := originSpan{187, 195, "mk_count"}
		spans := []originSpan{control}
		for _, refusal := range want {
			spans = append(spans, refusal.span)
		}
		checkOriginSource(t, text, "f1f00be1957470d1b64d4c5b9fb9d454d2ac26357c5d192e27df9cc2fde0c978", spans...)
		f, analysis := analyzeOriginDependency(t, "loan_carrier_classification", text, nil)
		checkDeferredMethodLeaf(t, analysis, f, deferredMethodLeaf{name: "carriers", stays: want, only: true, gone: []originRefusal{{control, ""}}})
	})
}
