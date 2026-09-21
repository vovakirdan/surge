package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/sema"
)

// A Map is a backing kind, and its eight retained core declarations are certified by
// identity. Each leaf judges one body of a frozen source, or one fact of its analysis.

// coreMapRow is a Pending triple; an empty key is the source's own unit.
type coreMapRow struct {
	key        string
	start, end int
	reason     string
}

// coreMapLeaf judges one body: its checks, rows present or absent at exact spans, reasons
// forbidden anywhere inside a byte range, and an optional whole-analysis fact.
type coreMapLeaf struct {
	name                    string
	checks                  []backingCheck
	present, absent, forbid []coreMapRow
	extra                   func(t *testing.T, g backingFixture)
}

// coreMapSource is a dependency, a root (escape tolerated or not), or a landed backing source.
type coreMapSource struct {
	name, fixture, text, digest string
	spans                       []originSpan
	leaves                      []coreMapLeaf
}

func coreMapSources() []coreMapSource {
	return []coreMapSource{
		{name: "m1_map_post_escape", fixture: "root_escape", text: coreMapM1, digest: coreMapM1Digest, spans: []originSpan{{79, 99, "m.insert(\"k\", value)"}, {243, 267, "let s: string = \"local\";"}, {276, 292, "fill(&mut m, &s)"}}, leaves: []coreMapLeaf{
			{name: "fill", checks: []backingCheck{arrayPopClean("fill")}},
			{name: "outer", checks: []backingCheck{arrayPopEscape("outer", backingEscape{233, 299, "s", 243, 267})}},
		}},
		{name: "m2_map_remove_keeps", fixture: "dependency", text: coreMapM2, digest: coreMapM2Digest, spans: []originSpan{{129, 149, "m.insert(\"k\", value)"}}, leaves: []coreMapLeaf{
			{name: "after_remove", checks: []backingCheck{arrayPopClean("after_remove", 0, 2)}},
		}},
		{name: "m3_map_generic_keys", fixture: "dependency", text: coreMapM3, digest: coreMapM3Digest, spans: []originSpan{{77, 85, "m.keys()"}, {153, 172, "names::<&string>(m)"}}, leaves: []coreMapLeaf{
			{name: "names", checks: []backingCheck{arrayPopClean("names")}},
			{name: "use_names", checks: []backingCheck{arrayPopClean("use_names")}},
			{name: "closure_complete", extra: checkCoreMapClosure},
		}},
		{name: "m4_map_name_control", fixture: "dependency", text: coreMapM4, digest: coreMapM4Digest, spans: []originSpan{{138, 170, "rt_map_len::<string, &string>(m)"}}, leaves: []coreMapLeaf{
			{name: "size", checks: []backingCheck{{function: "size", pending: []backingPending{{138, 170, rangeNextOpaqueEffect}}}}},
		}},
		{name: "m5_map_loan_value", fixture: "root_escape", text: coreMapM5, digest: coreMapM5Digest, spans: []originSpan{{109, 138, "rt_map_insert(&mut m, \"k\", v)"}, {336, 364, "rt_map_remove(&mut mm, &\"k\")"}, {760, 788, "rt_map_remove(&mut mm, &\"k\")"}, {556, 571, "mm.remove(&\"k\")"}, {192, 253, "let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];"}, {412, 473, "let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];"}}, leaves: []coreMapLeaf{
			{name: "wrapm", checks: []backingCheck{{function: "wrapm", slots: []uint32{0}, summary: true, pending: []backingPending{{109, 138, arrayPopLoanElement}}}}},
			{name: "leak", checks: []backingCheck{{function: "leak", pending: []backingPending{{336, 364, backingLoanDiscard}, {336, 364, arrayPopLoanElement}}, escapes: []backingEscape{{329, 365, "xs", 192, 253}}}}},
			{name: "leak_checked", checks: []backingCheck{{function: "leak_checked", pending: []backingPending{{556, 571, backingLoanDiscard}}, escapes: []backingEscape{{549, 572, "xs", 412, 473}}}}, present: []coreMapRow{{"core/map.sg", 965, 989, arrayPopLoanElement}}},
			{name: "leak_let", checks: []backingCheck{{function: "leak_let", pending: []backingPending{{760, 788, backingLoanDiscard}, {760, 788, arrayPopLoanElement}}}}},
		}},
		{name: "m6_map_direct_template", fixture: "dependency", text: coreMapM6, digest: coreMapM6Digest, spans: []originSpan{{105, 129, "rt_map_insert(m, \"k\", v)"}, {288, 312, "rt_map_insert(m, \"k\", v)"}}, leaves: []coreMapLeaf{
			{name: "put_get", checks: []backingCheck{arrayPopClean("put_get", 0, 1)}},
			{name: "mix", checks: []backingCheck{arrayPopClean("mix", 1)}},
		}},
		{name: "m7_map_inner_insert", fixture: "dependency", text: coreMapM7, digest: coreMapM7Digest, spans: []originSpan{{128, 173, "rt_map_insert(&mut outer[0], \"k\", xs[[1..3]])"}}, leaves: []coreMapLeaf{
			{name: "put_inner", checks: []backingCheck{{function: "put_inner", pending: []backingPending{{128, 173, backingLoanDiscard}, {128, 173, arrayPopLoanElement}}}}},
		}},
		{name: "m8_map_remove_inner", fixture: "dependency", text: coreMapM8, digest: coreMapM8Digest, spans: []originSpan{{504, 538, "rt_map_remove(&mut outer[0], &\"k\")"}}, leaves: []coreMapLeaf{
			{name: "remove_inner", checks: []backingCheck{{function: "remove_inner", pending: []backingPending{{504, 538, arrayPopLoanElement}}}}, absent: []coreMapRow{{"", 504, 538, backingLoanDiscard}}},
		}},
		{name: "m9_map_inner_get_mut", fixture: "dependency", text: coreMapM9, digest: coreMapM9Digest, spans: []originSpan{{127, 161, "rt_map_get_mut(&mut outer[0], key)"}}, leaves: []coreMapLeaf{
			{name: "lend_inner", checks: []backingCheck{{function: "lend_inner"}}, absent: []coreMapRow{{"", 127, 161, rangeNextMutableEffect}, {"", 127, 161, rangeNextOpaqueEffect}}},
		}},
		{name: "m10_map_remove_option_inner", fixture: "dependency", text: coreMapM10, digest: coreMapM10Digest, spans: []originSpan{{138, 171, "rt_map_remove(&mut outer[0], key)"}}, leaves: []coreMapLeaf{
			{name: "remove_option", checks: []backingCheck{{function: "remove_option", pending: []backingPending{{138, 171, arrayPopLoanElement}}}}, absent: []coreMapRow{{"", 138, 171, rangeNextMutableEffect}, {"", 138, 171, rangeNextOpaqueEffect}, {"", 138, 171, originGenericOpaqueUse}}},
		}},
		{name: "p11_replace_effects", fixture: "backing", digest: "8ff2ffc0aa919dc996caeb44e31a3074e82ef8298a08500ab0bd9710d93e16e0", spans: []originSpan{{456, 470, "m.remove(&key)"}}, leaves: []coreMapLeaf{
			{name: "read_old", checks: []backingCheck{arrayPopClean("read_old", 0)}},
			{name: "read_new", checks: []backingCheck{arrayPopClean("read_new", 0, 2)}},
			{name: "safe_keys", checks: []backingCheck{arrayPopClean("safe_keys")}},
		}},
		{name: "container_loan_twin", fixture: "root", text: containerLoanTwinSource, digest: containerLoanTwinDigest, spans: []originSpan{{420, 446, "mm.insert(\"k\", xs[[1..3]])"}, {460, 477, "take_map(&mut mm)"}}, leaves: []coreMapLeaf{
			{name: "map_twin_loads", checks: []backingCheck{{function: "leak_take_map"}}, present: []coreMapRow{{"", 420, 446, backingLoanDiscard}}, absent: []coreMapRow{{"", 460, 477, backingLoanDiscard}, {"", 460, 477, arrayPopLoanElement}}, forbid: []coreMapRow{{"", 121, 229, backingLoanDiscard}, {"", 121, 229, arrayPopLoanElement}, {"", 121, 229, rangeNextMutableEffect}, {"", 121, 229, rangeNextOpaqueEffect}}},
		}},
	}
}

func analyzeCoreMapSource(t *testing.T, src coreMapSource) backingFixture {
	t.Helper()
	switch src.fixture {
	case "dependency":
		_, g := analyzeArrayPopSource(t, "core_map", arrayPopSource{name: src.name, text: src.text, digest: src.digest, spans: src.spans}, nil)
		return g
	case "backing":
		landed := backingSource{name: src.name, fixture: src.name, digest: src.digest}
		text := backingSourceText(t, landed)
		checkOriginSource(t, text, src.digest, src.spans...)
		return analyzeBackingSource(t, landed, text, nil)
	case "root", "root_escape":
		checkOriginSource(t, src.text, src.digest, src.spans...)
		f, analysis := analyzeOriginRoot(t, "core_map", src.text, src.fixture == "root_escape", nil)
		return backingFixture{res: f.owner, units: f.inputs.units, root: f.unit, text: src.text, analysis: analysis}
	}
	t.Fatalf("PRECONDITION: unknown fixture %q", src.fixture)
	return backingFixture{}
}

// 1 parent + 12 sources + 21 leaves = 34 RUN.
func TestAnalyzeCoreMapOrigins(t *testing.T) {
	sources, leaves := coreMapSources(), 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 12 || leaves != 21 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			g := analyzeCoreMapSource(t, src)
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) {
					checkCoreMapLeaf(t, g, leaf)
				})
			}
		})
	}
}

func checkCoreMapLeaf(t *testing.T, g backingFixture, leaf coreMapLeaf) {
	t.Helper()
	for _, check := range leaf.checks {
		checkBackingFunction(t, g, check)
	}
	key := func(row coreMapRow) string {
		if row.key == "" {
			return g.root.SourceKey
		}
		return row.key
	}
	for _, row := range leaf.present {
		if !originPendingAt(g.analysis, key(row), originSpan{row.start, row.end, ""}, row.reason) {
			t.Errorf("missing Pending %q at %s %d:%d", row.reason, key(row), row.start, row.end)
		}
	}
	for _, row := range leaf.absent {
		if originPendingAt(g.analysis, key(row), originSpan{row.start, row.end, ""}, row.reason) {
			t.Errorf("unexpected Pending %q at %s %d:%d", row.reason, key(row), row.start, row.end)
		}
	}
	for _, row := range leaf.forbid {
		for _, p := range originPendingWithin(g.analysis, key(row), row.start, row.end) {
			if p.Reason == row.reason {
				t.Errorf("forbidden Pending %q at %d:%d inside %d:%d", p.Reason, p.Span.Start, p.Span.End, row.start, row.end)
			}
		}
	}
	if leaf.extra != nil {
		leaf.extra(t, g)
	}
}

// coreMapCensus reports whether a Pending is one of these census triples.
func coreMapCensus(p sema.ReturnOriginPending, rows []coreMapCensusRow) bool {
	return slices.ContainsFunc(rows, func(row coreMapCensusRow) bool {
		return p.SourceKey == row.key && int(p.Span.Start) == row.start && int(p.Span.End) == row.end && p.Reason == row.reason
	})
}

// checkCoreMapClosure: none of the 23 rows is left, and nothing outside the 24 that remain.
// The fixture's own unit is judged by its body leaves, not here.
// CORE_MAP_CLOSURE_EXTRA lines are the Stage A log S-M3U reads.
func checkCoreMapClosure(t *testing.T, g backingFixture) {
	t.Helper()
	for _, p := range g.analysis.Pending {
		switch {
		case coreMapCensus(p, coreMapRemovedRows):
			t.Errorf("census row still Pending: %s %d:%d %q", p.SourceKey, p.Span.Start, p.Span.End, p.Reason)
		case p.SourceKey != g.root.SourceKey && !coreMapCensus(p, coreMapRemainingRows):
			t.Logf("CORE_MAP_CLOSURE_EXTRA %s %d:%d %q", p.SourceKey, p.Span.Start, p.Span.End, p.Reason)
			t.Errorf("Pending outside the remaining census rows: %s %d:%d %q", p.SourceKey, p.Span.Start, p.Span.End, p.Reason)
		}
	}
}

// 1 RUN: none of the 23 census rows P1c-1 removes is left in M3's closure.
func TestReturnOriginMapCoreRows(t *testing.T) {
	var m3 coreMapSource
	for _, src := range coreMapSources() {
		if src.name == "m3_map_generic_keys" {
			m3 = src
		}
	}
	if m3.text == "" {
		t.Fatal("PRECONDITION: the M3 source is missing")
	}
	g := analyzeCoreMapSource(t, m3)
	digests := map[string]string{"core/map.sg": coreMapUnitDigest, "core/intrinsics.sg": coreIntrinsicsUnitDigest}
	content := make(map[string][]byte)
	for _, unit := range g.units {
		if want, pinned := digests[unit.SourceKey]; pinned {
			file := g.res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
			if got := fmt.Sprintf("%x", sha256.Sum256(file.Content)); got != want {
				t.Fatalf("PRECONDITION: %s changed: %s", unit.SourceKey, got)
			}
			content[unit.SourceKey] = file.Content
		}
	}
	if len(content) != 2 || len(coreMapRemovedRows) != 23 || len(coreMapRemainingRows) != 24 {
		t.Fatalf("PRECONDITION: core units %d, removed %d, remaining %d", len(content), len(coreMapRemovedRows), len(coreMapRemainingRows))
	}
	for _, row := range coreMapRemovedRows {
		if text := string(content[row.key][row.start:row.end]); text != row.text {
			t.Fatalf("PRECONDITION: %s %d:%d is %q, want %q", row.key, row.start, row.end, text, row.text)
		}
		if originPendingAt(g.analysis, row.key, originSpan{row.start, row.end, ""}, row.reason) {
			t.Errorf("census row still Pending: %s %d:%d %q", row.key, row.start, row.end, row.reason)
		}
	}
	for _, p := range g.analysis.Pending {
		if strings.HasPrefix(p.SourceKey, "core/") {
			t.Logf("CORE_MAP_ROWS_PENDING %s %d:%d %q", p.SourceKey, p.Span.Start, p.Span.End, p.Reason)
		}
	}
}
