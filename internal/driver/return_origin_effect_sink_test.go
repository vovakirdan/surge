package driver

import "testing"

// A `&mut` formal whose referent reads reference-free can still receive a storage loan.
// P1c-E refuses such a write wherever no body answers for it: a body-less callee, a
// function value, a generic body-less use and a body-less deferred implementation. The
// certified core identities and the three core byte sinks stay exempt.

const effectSinkIndirect = "indirect call may change reference-bearing or callable contents"

const effectSinkDeferred = "deferred method may change reference-bearing or callable contents"

// effectSinkLeaf is one t.Run leaf: its body checks, then Pending rows of the source unit
// that must be present, or absent, at exact spans.
type effectSinkLeaf struct {
	name            string
	checks          []backingCheck
	present, absent []backingPending
}

type effectSinkSource struct {
	name, text, digest string
	root               bool
	spans              []originSpan
	leaves             []effectSinkLeaf
}

const effectSinkE1 = `pragma module::dep;
@intrinsic fn stash(dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing;
fn keep(dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    stash(dst, src);
    return nothing;
}
@intrinsic fn tally(dst: &mut uint64, src: &uint64[]) -> nothing;
fn count(dst: &mut uint64, src: &uint64[]) -> nothing {
    tally(dst, src);
    return nothing;
}
`

const effectSinkE1Digest = "624cf857eb7e75bfe903000abcba9e19e0e4ef222266b680932936e9f10def2d"

const effectSinkE2 = `pragma module::dep;
@intrinsic fn swap_in(dst: &mut uint64[], src: &uint64[4]) -> nothing;
fn refill(dst: &mut uint64[], src: &uint64[4]) -> nothing {
    swap_in(dst, src);
    return nothing;
}
`

const effectSinkE2Digest = "ccb16d224eedf2b893ce21c97c83ebc53d4ab09133d85ad84e4a892db18c0195"

const effectSinkE3 = `pragma module::dep;
fn append(dst: &mut byte[], src: &byte[]) -> nothing {
    rt_byte_array_append_range(dst, src, 0:uint64, 1:uint64);
    rt_byte_array_drop_prefix(dst, 1:uint64);
    return nothing;
}
`

const effectSinkE3Digest = "5ffb834e4a0f2b40a809ccdf94870016ec31015e84fc4218ce22a1f2ebd75726"

const effectSinkE4 = `pragma module::dep, no_std;
@intrinsic fn rt_byte_array_drop_prefix(a: &mut uint8[], count: uint64) -> nothing;
fn trim(a: &mut uint8[]) -> nothing {
    rt_byte_array_drop_prefix(a, 1:uint64);
    return nothing;
}
`

const effectSinkE4Digest = "f4b42908e00c540d677df97a52db68048d77cf11f7576d954304c6b5d6336189"

const effectSinkE6 = `pragma module::dep;
fn put(dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    return nothing;
}
fn keep_value(dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    let f = put;
    f(dst, src);
    return nothing;
}
`

const effectSinkE6Digest = "fed12bb12bec7edcc19ad5f80c625645636788e9026dc72464d2da759670af2e"

const effectSinkE7 = `@intrinsic fn put_opt<T>(dst: &mut Option<T>, src: &T) -> nothing;
fn fill_view(dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    put_opt::<uint64[]>(dst, src);
    return nothing;
}
`

const effectSinkE7Digest = "8dc334424fb713ed30be7f6df154d9869b5749053b2a32e30f322e7564ac62f3"

const effectSinkE8 = `contract Fill<T> {
    fn __fill(self: &T, dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing;
}
type Filler = { n: uint };
extern<Filler> {
    @intrinsic fn __fill(self: &Filler, dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing;
}
fn fill_via<T: Fill<T>>(x: &T, dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    x.__fill(dst, src);
    return nothing;
}
fn use_fill(f: &Filler, dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    fill_via::<Filler>(f, dst, src);
    return nothing;
}
type Keeper = { n: uint };
extern<Keeper> {
    fn __fill(self: &Keeper, dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
        return nothing;
    }
}
fn keep_via<T: Fill<T>>(x: &T, dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    x.__fill(dst, src);
    return nothing;
}
fn use_keep(k: &Keeper, dst: &mut Option<uint64[]>, src: &uint64[]) -> nothing {
    keep_via::<Keeper>(k, dst, src);
    return nothing;
}
`

const effectSinkE8Digest = "0837b62fa911ca301263ac86b70d93f9a2c3ff39b02a84adad6e6f359351b113"

func effectSinkSources() []effectSinkSource {
	return []effectSinkSource{
		{name: "e1_contents_sink", text: effectSinkE1, digest: effectSinkE1Digest, spans: []originSpan{{165, 180, "stash(dst, src)"}}, leaves: []effectSinkLeaf{
			{name: "keep", checks: []backingCheck{arrayPopOnly("keep", backingPending{165, 180, rangeNextMutableEffect}, backingPending{165, 180, rangeNextOpaqueEffect})}},
			{name: "count", checks: []backingCheck{arrayPopClean("count")}},
		}},
		{name: "e2_whole_sink", text: effectSinkE2, digest: effectSinkE2Digest, spans: []originSpan{{155, 172, "swap_in(dst, src)"}}, leaves: []effectSinkLeaf{
			{name: "refill", checks: []backingCheck{arrayPopOnly("refill", backingPending{155, 172, rangeNextMutableEffect}, backingPending{155, 172, rangeNextOpaqueEffect})}},
		}},
		{name: "e3_certified_bytes", text: effectSinkE3, digest: effectSinkE3Digest, spans: []originSpan{{79, 135, "rt_byte_array_append_range(dst, src, 0:uint64, 1:uint64)"}, {141, 181, "rt_byte_array_drop_prefix(dst, 1:uint64)"}}, leaves: []effectSinkLeaf{
			{name: "append", checks: []backingCheck{arrayPopClean("append")}},
		}},
		{name: "e4_bytes_name_control", text: effectSinkE4, digest: effectSinkE4Digest, spans: []originSpan{{154, 192, "rt_byte_array_drop_prefix(a, 1:uint64)"}}, leaves: []effectSinkLeaf{
			{name: "trim", checks: []backingCheck{arrayPopOnly("trim", backingPending{154, 192, rangeNextMutableEffect}, backingPending{154, 192, rangeNextOpaqueEffect})}},
		}},
		{name: "e6_indirect_sink", text: effectSinkE6, digest: effectSinkE6Digest, spans: []originSpan{{198, 209, "f(dst, src)"}}, leaves: []effectSinkLeaf{
			{name: "keep_value", checks: []backingCheck{{function: "keep_value", pending: []backingPending{{198, 209, rangeNextMutableEffect}, {198, 209, effectSinkIndirect}}}}},
			{name: "put", checks: []backingCheck{arrayPopClean("put")}},
		}},
		{name: "e7_generic_sink_use", root: true, text: effectSinkE7, digest: effectSinkE7Digest, spans: []originSpan{{141, 170, "put_opt::<uint64[]>(dst, src)"}}, leaves: []effectSinkLeaf{
			{name: "fill_view", checks: []backingCheck{{function: "fill_view", pending: []backingPending{{141, 170, rangeNextMutableEffect}, {141, 170, rangeNextOpaqueEffect}, {141, 170, originGenericOpaqueUse}}}}},
		}},
		{name: "e8_deferred_sink", root: true, text: effectSinkE8, digest: effectSinkE8Digest, spans: []originSpan{{335, 353, "x.__fill(dst, src)"}, {771, 789, "x.__fill(dst, src)"}}, leaves: []effectSinkLeaf{
			{name: "fill_via", checks: []backingCheck{{function: "fill_via", anyDiagnostics: true, pending: []backingPending{{335, 353, effectSinkDeferred}}}}},
			{name: "keep_via", checks: []backingCheck{{function: "keep_via", anyDiagnostics: true}}, absent: []backingPending{{771, 789, effectSinkDeferred}}},
		}},
	}
}

// 1 parent + 7 sources + 10 leaves = 18 RUN.
func TestAnalyzeEffectSinkOrigins(t *testing.T) {
	sources, leaves := effectSinkSources(), 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 7 || leaves != 10 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			g := analyzeEffectSinkSource(t, src)
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) {
					for _, check := range leaf.checks {
						checkBackingFunction(t, g, check)
					}
					checkEffectSinkRows(t, g, leaf.present, true)
					checkEffectSinkRows(t, g, leaf.absent, false)
				})
			}
		})
	}
}

// analyzeEffectSinkSource analyzes a dependency source, or a root program: only a root's
// finalized closure carries the instance use sites and deferred outcomes of its own bodies.
func analyzeEffectSinkSource(t *testing.T, src effectSinkSource) backingFixture {
	t.Helper()
	if !src.root {
		_, g := analyzeArrayPopSource(t, "effect_sink", arrayPopSource{name: src.name, text: src.text, digest: src.digest, spans: src.spans}, nil)
		return g
	}
	checkOriginSource(t, src.text, src.digest, src.spans...)
	f, analysis := analyzeOriginRoot(t, "effect_sink", src.text, false, nil)
	return backingFixture{res: f.owner, units: f.inputs.units, root: f.unit, text: src.text, analysis: analysis}
}

// checkEffectSinkRows requires each row to be present, or absent, in the source unit.
func checkEffectSinkRows(t *testing.T, g backingFixture, rows []backingPending, want bool) {
	t.Helper()
	for _, row := range rows {
		if got := originPendingAt(g.analysis, g.root.SourceKey, originSpan{row.start, row.end, ""}, row.reason); got != want {
			t.Errorf("Pending %q at %d:%d %q present=%v, want %v", row.reason, row.start, row.end, g.text[row.start:row.end], got, want)
		}
	}
}
