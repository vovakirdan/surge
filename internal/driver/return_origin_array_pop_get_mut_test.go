package driver

import (
	"testing"

	"surge/internal/sema"
	"surge/internal/types"
)

// Array pop and get_mut become closed core-array identities: a certified call is
// answered from the caller's own proven target set instead of leaving the
// body-less generic obligations behind. P and G judge the two identities, L2 the
// legacy receivers Part L substitutes, H the holes F2/F2b close, and M the loan
// elements M-1R reads out of a container that keeps storage loans. P's and G's
// DIRECT intrinsic bodies use a non-reference element because inferring a
// reference type argument at an rt_array_* call has no precedent (MEASURE.md 5).

const arrayPopLoanElement = rangeNextLoanElement

const arrayPopLegacyTransfer = "container-content result lacks its checked backing call transfer"

const arrayPopMissingPost = "container call lacks its callee's complete post-state"

// arrayPopSource is one frozen dependency source of the pop/get_mut suites.
type arrayPopSource struct {
	name   string
	text   string
	digest string
	spans  []originSpan
	leaves []backingLeaf
}

// analyzeArrayPopSource freezes one dependency source and analyzes it with core.
func analyzeArrayPopSource(t *testing.T, stage string, src arrayPopSource,
	prepare func(originalGenericFixture),
) (originalGenericFixture, backingFixture) {
	t.Helper()
	checkOriginSource(t, src.text, src.digest, src.spans...)
	f := originalGenericSignatureFixture(t, src.text, false, true)
	if prepare != nil {
		prepare(f)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": stage, "source": src.name, "source_key": f.unit.SourceKey,
		"pending": originPendingWithin(analysis, f.unit.SourceKey, 0, len(src.text)), "diagnostics": analysis.Diagnostics,
		"core_pending": originPendingWithin(analysis, "core/array.sg", 0, 1<<30), "summaries": analysis.Summaries})
	return f, backingFixture{res: f.owner, units: f.inputs.units, root: f.unit, text: src.text, analysis: analysis}
}

func arrayPopClean(function string, slots ...uint32) backingCheck {
	return backingCheck{function: function, slots: slots, summary: true, clean: true}
}

// arrayPopQuiet asserts no obligation is left without pinning the summary slots.
func arrayPopQuiet(function string) backingCheck {
	return backingCheck{function: function, clean: true}
}

func arrayPopOnly(function string, rows ...backingPending) backingCheck {
	return backingCheck{function: function, only: true, pending: rows}
}

func arrayPopEscape(function string, at backingEscape) backingCheck {
	return backingCheck{function: function, clean: true, escapes: []backingEscape{at}}
}

func arrayPopLeaf(name string, checks ...backingCheck) backingLeaf {
	return backingLeaf{name: name, checks: checks}
}

const arrayPopSourceP = `pragma module::dep;
fn wrap<T>(x: T) -> Array<T> {
    let mut out: Array<T> = [];
    out.push(x);
    return out;
}
fn last(xs: &mut Array<&string>) -> Option<&string> {
    return xs.pop();
}
fn last_rt(xs: &mut uint64[]) -> Option<uint64> {
    return rt_array_pop(xs);
}
fn keep(first: &string) -> Option<&string> {
    let mut a = wrap::<&string>(first);
    return a.pop();
}
fn both(first: &string, second: &string) -> Option<&string> {
    let mut a = wrap::<&string>(first);
    a.push(second);
    return a.pop();
}
fn leak() -> Option<&string> {
    let s: string = "local";
    let mut b = wrap::<&string>(&s);
    return b.pop();
}
`

const arrayPopSourcePDigest = "6af4b1176f2d9bd654cec0a5a7e75179d9652000b0f09ede6d5efdbf22619555"

const arrayPopSourceG = `pragma module::dep;
fn at(xs: &mut Array<&string>) -> &mut &string {
    return xs.get_mut(0);
}
fn at_rt(xs: &mut uint64[]) -> &mut uint64 {
    return rt_array_get_mut(xs, 0);
}
fn at_fixed(xs: &mut uint64[4]) -> &mut uint64 {
    return xs.get_mut(1);
}
fn leak_local() -> Option<&mut uint64> {
    let mut xs: uint64[] = [1:uint64];
    return Some::<&mut uint64>(xs.get_mut(0));
}
fn leak_fixed() -> Option<&mut uint64> {
    let mut ys: uint64[2] = [1:uint64, 2:uint64];
    return Some::<&mut uint64>(ys.get_mut(1));
}
`

const arrayPopSourceGDigest = "c9ea6f15f0c27a61dc99c5e3e4faaf6d2c8f32e2b217b70bfc83721cd0f6c4e4"

const arrayPopSourceL2 = `pragma module::dep;
type Bytes = { buf: byte[] };
type Rows = { rows: uint64[] };
type Held<T> = { items: Array<T> };
extern<Bytes> {
    pub fn drop_last(self: &mut Bytes) -> nothing {
        let _ = self.buf.pop();
        return nothing;
    }
}
fn first_byte(h: &mut Bytes) -> Option<byte> {
    return h.buf.pop();
}
fn sub(xs: &uint64[]) -> uint64[] {
    return xs[[1..3]];
}
fn view_field(h: &Rows) -> uint64[] {
    return sub(h.rows);
}
fn take<T>(h: &mut Held<T>) -> Option<T> {
    return h.items.pop();
}
`

const arrayPopSourceL2Digest = "9ff28ec0a1e0367b8df0917594db1f96fefd6bca716c0a39fac5f75a74b9d3dd"

const arrayPopSourceH = `pragma module::dep;
type Nest = { items: Array<uint64[]> };
fn f<T>(xs: &mut Array<T>, x: T) -> Option<T> {
    xs.push(x);
    return xs.pop();
}
fn stash<T>(xs: &mut Array<T>, x: T) -> nothing {
    xs.push(x);
    return nothing;
}
fn nested_view() -> Option<uint64[]> {
    let a: uint64[3] = [1:uint64, 2:uint64, 3:uint64];
    let mut h = Nest { items = [] };
    return f::<uint64[]>(&mut h.items, a[[0..2]]);
}
fn stash_view_into_field() -> Nest {
    let a: uint64[3] = [1:uint64, 2:uint64, 3:uint64];
    let mut h = Nest { items = [] };
    stash::<uint64[]>(&mut h.items, a[[0..1]]);
    return h;
}
fn push_view_into_field() -> Nest {
    let a: uint64[3] = [1:uint64, 2:uint64, 3:uint64];
    let mut h = Nest { items = [] };
    rt_array_push(&mut h.items, a[[0..1]]);
    return h;
}
fn pop_views(h: &mut Nest) -> Option<uint64[]> {
    return h.items.pop();
}
`

const arrayPopSourceHDigest = "fb7a8a1e1d130afe913e082a37e0996395d0f9ba6bddb167d15535acd00ed7f1"

const arrayPopSourceM = `pragma module::dep;
type Nest = { items: Array<uint64[]> };
fn wrap<T>(x: T) -> Array<T> {
    let mut out: Array<T> = [];
    out.push(x);
    return out;
}
fn pop_view() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: Array<uint64[]> = wrap::<uint64[]>(xs[[1..3]]);
    return views.pop();
}
fn pop_view_rt() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut views: Array<uint64[]> = wrap::<uint64[]>(xs[[1..3]]);
    return rt_array_pop(&mut views);
}
fn reserve_views(views: &mut Array<uint64[]>) -> nothing {
    views.reserve(4:uint);
    return nothing;
}
fn reserve_field(h: &mut Nest) -> nothing {
    h.items.reserve(4:uint);
    return nothing;
}
`

const arrayPopSourceMDigest = "b3472eeb00534fdd6d95d10ae728b0c94330c7c3ea3c0a8717568ffd4ae24987"

func arrayPopGetMutSources() []arrayPopSource {
	return []arrayPopSource{
		{name: "p_array_pop_contents", text: arrayPopSourceP, digest: arrayPopSourcePDigest,
			spans: []originSpan{{256, 272, "rt_array_pop(xs)"}, {628, 643, "return b.pop();"},
				{562, 586, `let s: string = "local";`}},
			leaves: []backingLeaf{
				arrayPopLeaf("last", arrayPopClean("last", 0)),
				// Subject change: the direct form pops a uint64, so its summary is the
				// empty set, not [0]; it still witnesses the identity by staying clean.
				arrayPopLeaf("last_rt", arrayPopClean("last_rt")),
				arrayPopLeaf("keep", arrayPopClean("keep", 0)),
				arrayPopLeaf("both", arrayPopClean("both", 0, 1)),
				arrayPopLeaf("leak", arrayPopEscape("leak", backingEscape{628, 643, "s", 562, 586})),
			}},
		{name: "g_array_get_mut_slot", text: arrayPopSourceG, digest: arrayPopSourceGDigest,
			spans: []originSpan{{153, 176, "rt_array_get_mut(xs, 0)"}, {341, 383, "return Some::<&mut uint64>(xs.get_mut(0));"},
				{302, 336, "let mut xs: uint64[] = [1:uint64];"}, {481, 523, "return Some::<&mut uint64>(ys.get_mut(1));"},
				{431, 476, "let mut ys: uint64[2] = [1:uint64, 2:uint64];"}},
			leaves: []backingLeaf{
				arrayPopLeaf("at", arrayPopClean("at", 0)),
				// get_mut borrows slot 0 whatever the element is, so [0] is intact.
				arrayPopLeaf("at_rt", arrayPopClean("at_rt", 0)),
				arrayPopLeaf("at_fixed", arrayPopClean("at_fixed", 0)),
				arrayPopLeaf("leak_local", arrayPopEscape("leak_local", backingEscape{341, 383, "xs", 302, 336})),
				arrayPopLeaf("leak_fixed", arrayPopEscape("leak_fixed", backingEscape{481, 523, "ys", 431, 476})),
			}},
		{name: "l2_legacy_pop", text: arrayPopSourceL2, digest: arrayPopSourceL2Digest,
			spans: []originSpan{{202, 216, "self.buf.pop()"}, {308, 319, "h.buf.pop()"}, {433, 444, "sub(h.rows)"},
				{502, 515, "h.items.pop()"}, {202, 210, "self.buf"}, {308, 313, "h.buf"}, {437, 443, "h.rows"},
				{502, 509, "h.items"}, {279, 294, "-> Option<byte>"}, {301, 320, "return h.buf.pop();"},
				{408, 419, "-> uint64[]"}, {426, 445, "return sub(h.rows);"}, {476, 488, "-> Option<T>"},
				{495, 516, "return h.items.pop();"}},
			leaves: []backingLeaf{
				// BEFORE-equality: the member-projection family is pre-existing and
				// measured identical on e77ac017. Only the legacy-transfer rows at
				// 433:444 and 502:515 are this packet's; drop_last and first_byte gain
				// nothing, so for them AFTER is exactly BEFORE.
				arrayPopLeaf("drop_last", arrayPopOnly("drop_last",
					backingPending{202, 210, originProjectionRefusal})),
				// Not BEFORE-equality: the certificate answers a payload-free `byte`
				// element, so the result becomes proven and the two derived rows go
				// away. Only the member projection itself is beyond this packet.
				arrayPopLeaf("first_byte", arrayPopOnly("first_byte",
					backingPending{308, 313, originProjectionRefusal})),
				arrayPopLeaf("sub", arrayPopClean("sub", 0)),
				arrayPopLeaf("view_field", arrayPopOnly("view_field",
					backingPending{408, 419, originResultRefusal},
					backingPending{426, 445, originOutgoingRefusal},
					backingPending{437, 443, originProjectionRefusal},
					backingPending{433, 444, arrayPopLegacyTransfer})),
				// take<T>'s element is a template parameter, so elementsFree is false and
				// refuseLegacyBackingSummary's post guard raises the mutable-argument row
				// itself (§5.3 L-6(a), §7.10). It is this packet's row, not a leftover.
				arrayPopLeaf("take", arrayPopOnly("take",
					backingPending{476, 488, originResultRefusal},
					backingPending{495, 516, originOutgoingRefusal},
					backingPending{502, 509, originProjectionRefusal},
					backingPending{502, 515, arrayPopLegacyTransfer},
					backingPending{502, 515, rangeNextMutableEffect})),
			}},
		{name: "h_legacy_holes", text: arrayPopSourceH, digest: arrayPopSourceHDigest,
			spans: []originSpan{{377, 415, "f::<uint64[]>(&mut h.items, a[[0..2]])"}, {552, 594, "stash::<uint64[]>(&mut h.items, a[[0..1]])"},
				{744, 782, "rt_array_push(&mut h.items, a[[0..1]])"}, {860, 873, "h.items.pop()"}, {860, 867, "h.items"}},
			leaves: []backingLeaf{
				arrayPopLeaf("f", arrayPopClean("f", 0, 1)),
				arrayPopLeaf("stash", arrayPopClean("stash")),
				// The outgoing-provenance row at 370:416 is allowed; no SEM3139 may remain.
				arrayPopLeaf("nested_view", backingCheck{function: "nested_view", pending: []backingPending{
					{377, 415, backingLoanDiscard}, {377, 415, arrayPopLoanElement}}}),
				arrayPopLeaf("stash_view_into_field", arrayPopOnly("stash_view_into_field",
					backingPending{552, 594, backingLoanDiscard}, backingPending{552, 594, arrayPopLoanElement})),
				arrayPopLeaf("push_view_into_field", arrayPopOnly("push_view_into_field",
					backingPending{744, 782, backingLoanDiscard})),
				// BEFORE-equality: three rows are the pre-existing member-projection
				// family, measured identical before and after; only 860:873 is P1n's.
				arrayPopLeaf("pop_views", arrayPopOnly("pop_views",
					backingPending{827, 846, originResultRefusal},
					backingPending{853, 874, originOutgoingRefusal},
					backingPending{860, 867, originProjectionRefusal},
					backingPending{860, 873, arrayPopLoanElement})),
			}},
		{name: "m_loan_element_pop", text: arrayPopSourceM, digest: arrayPopSourceMDigest,
			spans: []originSpan{{338, 349, "views.pop()"}, {536, 560, "rt_array_pop(&mut views)"},
				{627, 648, "views.reserve(4:uint)"}, {720, 743, "h.items.reserve(4:uint)"}, {720, 727, "h.items"},
				{198, 259, "let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];"},
				{396, 457, "let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];"}},
			leaves: []backingLeaf{
				arrayPopLeaf("wrap", arrayPopClean("wrap", 0)),
				arrayPopLeaf("pop_view", arrayPopLeakingRows("pop_view",
					[]backingPending{{338, 349, backingLoanDiscard}}, backingEscape{331, 350, "xs", 198, 259})),
				arrayPopLeaf("pop_view_rt", arrayPopLeakingRows("pop_view_rt",
					[]backingPending{{536, 560, backingLoanDiscard}}, backingEscape{529, 561, "xs", 396, 457})),
				arrayPopLeaf("reserve_views", arrayPopClean("reserve_views")),
				// BEFORE-equality, as pop_views: the projection row is pre-existing.
				arrayPopLeaf("reserve_field", arrayPopOnly("reserve_field",
					backingPending{720, 727, originProjectionRefusal})),
			}},
	}
}

// 1 parent + 5 sources + 26 leaves = 32 RUN.
func TestAnalyzeArrayPopGetMut(t *testing.T) {
	sources, leaves := arrayPopGetMutSources(), 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 5 || leaves != 26 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			_, g := analyzeArrayPopSource(t, "array_pop_get_mut", src, nil)
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) {
					for _, check := range leaf.checks {
						checkBackingFunction(t, g, check)
					}
				})
			}
		})
	}
}

// arrayPopNamedSource is the one frozen source of that name.
func arrayPopNamedSource(t *testing.T, name string) arrayPopSource {
	t.Helper()
	for _, src := range arrayPopGetMutSources() {
		if src.name == name {
			return src
		}
	}
	t.Fatalf("PRECONDITION: no frozen source %q", name)
	return arrayPopSource{}
}

// arrayPopShadowModule breaks one core candidate's module identity (M1/M2).
func arrayPopShadowModule(t *testing.T, f originalGenericFixture, name string, arity int) {
	t.Helper()
	matched := 0
	for i := range f.authority.CallableCandidates {
		c := &f.authority.CallableCandidates[i]
		if c.Name == name && c.SourceKey == "builtin" && c.ModulePath == "core/intrinsics" && len(c.TemplateParams) == arity {
			c.ModulePath = "core/intrinsics_shadow"
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("PRECONDITION: core %s candidate of arity %d is not unique: %d", name, arity, matched)
	}
}

// arrayPopMutatePromise rewrites a core declaration's promise in both places that
// carry it, so candidate and interned signature still agree. After hunk 3 relaxes
// the AllInputs guard by name, that promise is all that pins get_mut.
func arrayPopMutatePromise(t *testing.T, f originalGenericFixture, name string, want types.ReturnSources) {
	t.Helper()
	matched := 0
	for i := range f.authority.CallableCandidates {
		c := &f.authority.CallableCandidates[i]
		if c.Name != name || c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" {
			continue
		}
		for _, unit := range f.inputs.units {
			if unit.SourceKey != "core/intrinsics.sg" {
				continue
			}
			sym := unit.Symbols.Table.Symbols.Get(originalGenericSignatureLocal(t, unit, *c))
			info, ok := f.authority.TypeInterner.FnInfo(sym.Type)
			if sym == nil || !ok || info == nil {
				t.Fatal("PRECONDITION: core declaration lost its typed signature")
			}
			sym.Type = f.authority.TypeInterner.RegisterFnWithReturnSources(info.Params, info.Result, want)
		}
		c.ReturnSources = want
		matched++
	}
	if matched == 0 {
		t.Fatalf("PRECONDITION: core %s candidate is missing", name)
	}
}

// Breaking the identity a certificate rests on must bring the refusals back.
// 1 parent + 3 leaves = 4 RUN.
func TestReturnOriginArrayPopGetMutIdentityMutation(t *testing.T) {
	getSite := originSpan{153, 176, "rt_array_get_mut(xs, 0)"}
	core := arrayPopCoreUseSites()
	for _, row := range []struct {
		name    string
		source  string
		site    originSpan
		present []originSpan
		absent  []originSpan
		mutate  func(t *testing.T, f originalGenericFixture)
	}{
		// M1/M2 assert the CORE spans only. The source-site half was withdrawn as a
		// subject change: the two flow reasons arise only when the container element
		// is reference-bearing (returnOriginCallHasUnprovedEffects,
		// return_origin_publication.go:104-124), and these direct-call sites use a
		// payload-free element, whose effect is proved, so no taint can ever fire
		// there. The certificate is still pinned, where the census measures it.
		{name: "m1_array_pop_module", source: "p_array_pop_contents",
			present: []originSpan{core[0]}, absent: []originSpan{core[1], core[2]},
			mutate: func(t *testing.T, f originalGenericFixture) { arrayPopShadowModule(t, f, "rt_array_pop", 1) }},
		{name: "m2_get_mut_module", source: "g_array_get_mut_slot",
			present: []originSpan{core[1]}, absent: []originSpan{core[2]},
			mutate: func(t *testing.T, f originalGenericFixture) { arrayPopShadowModule(t, f, "rt_array_get_mut", 1) }},
		// M3 mutates the promise itself. Asserted by presence, not by reason: an
		// inconsistent promise trips the selection's agreement gate before hunk 4's
		// Slots() test, so the reason is logged for Stage A (MEASURE.md 5).
		{name: "m3_get_mut_promise", source: "g_array_get_mut_slot", site: getSite,
			present: []originSpan{core[1], core[2]},
			mutate: func(t *testing.T, f originalGenericFixture) {
				arrayPopMutatePromise(t, f, "rt_array_get_mut", types.ExplicitReturnSources(1))
			}},
	} {
		t.Run(row.name, func(t *testing.T) {
			src := arrayPopNamedSource(t, row.source)
			f, g := analyzeArrayPopSource(t, "array_pop_identity_mutation", src,
				func(f originalGenericFixture) { row.mutate(t, f) })
			logReturnOriginCallEvidence(t, map[string]any{"stage": "array_pop_identity_mutation_rows", "mutation": row.name,
				"site": originPendingWithin(g.analysis, f.unit.SourceKey, row.site.start, row.site.end),
				"core": originPendingWithin(g.analysis, "core/array.sg", 0, 1<<30)})
			if row.site.snippet != "" && !originPendingAt(g.analysis, f.unit.SourceKey, row.site, "") {
				t.Errorf("%s: %q kept its certificate", row.name, row.site.snippet)
			}
			for _, site := range row.present {
				if !originPendingAt(g.analysis, "core/array.sg", site, "") {
					t.Errorf("%s: core %q stayed certified", row.name, site.snippet)
				}
			}
			for _, site := range row.absent {
				if originPendingAt(g.analysis, "core/array.sg", site, "") {
					t.Errorf("%s: core %q lost an unrelated certificate", row.name, site.snippet)
				}
			}
		})
	}
}
