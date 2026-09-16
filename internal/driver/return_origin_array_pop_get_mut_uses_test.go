package driver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

// A finalized use of either identity is answered by the certificate, so the three
// core use sites lose the body-less generic use reason while the three root bodies
// keep their declared container source. The golden that pops nested arrays is the
// M-1R witness: its pops read literal-built locals, so the loan-element load
// answers an empty set and nothing new is refused.

const arrayPopUseRoot = `fn last(xs: &mut Array<&string>) -> Option<&string> {
    return xs.pop();
}
fn at(xs: &mut Array<&string>) -> &mut &string {
    return xs.get_mut(0);
}
fn at_fixed(xs: &mut uint64[4]) -> &mut uint64 {
    return xs.get_mut(1);
}
`

const arrayPopUseRootDigest = "a02adc1c9e9162fd3b60a1668a0dec59f95d4a83495d09d9a0803414357f1128"

const arrayPopGoldenPath = "../../testdata/golden/vm_compare/compare_arm_element_read.sg"

const arrayPopGoldenDigest = "9644f1368cc532d97653d5e0dc3f3cfbece84cb19bf2cc530583f66c424a9f7e"

// The three core spans whose enclosing bodies stop blocking the build.
func arrayPopCoreUseSites() []originSpan {
	return []originSpan{
		{339, 354, "rt_array_pop(a)"},
		{436, 462, "rt_array_get_mut(a, index)"},
		{6975, 7012, "rt_array_get_mut::<T, N>(self, index)"},
	}
}

// The four refusal texts this packet can introduce; none may reach a root here.
func arrayPopIntroducedReasons() []string {
	return []string{arrayPopLoanElement, backingLoanDiscard, arrayPopLegacyTransfer, arrayPopMissingPost}
}

func TestAnalyzeArrayPopGetMutUses(t *testing.T) {
	t.Run("u_root", func(t *testing.T) {
		checkOriginSource(t, arrayPopUseRoot, arrayPopUseRootDigest,
			originSpan{0, 76, "fn last(xs: &mut Array<&string>) -> Option<&string> {\n    return xs.pop();\n}"})
		f, analysis := analyzeOriginRoot(t, "array_pop_get_mut_uses", arrayPopUseRoot, false, nil)
		// S-U1 reads the closure the certificate answers; the assertion below is
		// the consequence, so the closure itself is evidence rather than a claim.
		logReturnOriginCallEvidence(t, map[string]any{"stage": "array_pop_use_sites",
			"closure": f.owner.Sema.InstantiationClosure, "core_sites": arrayPopCoreUseSites()})
		core := rangeNextUnitText(t, f, "core/array.sg", arrayPopUseRoot)
		for _, site := range arrayPopCoreUseSites() {
			if site.end > len(core) || core[site.start:site.end] != site.snippet {
				t.Fatalf("PRECONDITION: core span %d:%d is not %q", site.start, site.end, site.snippet)
			}
			if originPendingAt(analysis, "core/array.sg", site, "") {
				t.Errorf("core use site %q still refuses: %+v", site.snippet,
					originPendingWithin(analysis, "core/array.sg", site.start, site.end))
			}
		}
		for _, name := range []string{"last", "at", "at_fixed"} {
			requireOriginSummary(t, analysis, f.owner.File.ID, name, false, []uint32{0})
		}
		for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, 0, len(arrayPopUseRoot)) {
			t.Errorf("u_root left unfinished: %s at %d:%d", pending.Reason, pending.Span.Start, pending.Span.End)
		}
		for _, d := range analysis.Diagnostics {
			if d.Primary.File == f.owner.File.ID {
				t.Errorf("unexpected root diagnostic: %+v", d)
			}
		}
	})
	t.Run("golden_compare_arm_element_read", func(t *testing.T) {
		raw, err := os.ReadFile(arrayPopGoldenPath)
		if err != nil {
			t.Fatalf("PRECONDITION: the golden is unreadable: %v", err)
		}
		text := string(raw)
		checkOriginSource(t, text, arrayPopGoldenDigest,
			originSpan{855, 867, "values.pop()"},
			originSpan{1002, 1012, "more.pop()"},
			originSpan{1153, 1165, "nested.pop()"})
		f, analysis := analyzeOriginRoot(t, "array_pop_golden", text, false, nil)
		within := originPendingWithin(analysis, f.unit.SourceKey, 0, len(text))
		logReturnOriginCallEvidence(t, map[string]any{"stage": "array_pop_golden_rows",
			"golden_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "pending": within, "diagnostics": analysis.Diagnostics})
		for _, pop := range []originSpan{{855, 867, "values.pop()"}, {1002, 1012, "more.pop()"}, {1153, 1165, "nested.pop()"}} {
			if originPendingAt(analysis, f.unit.SourceKey, pop, "") {
				t.Errorf("golden pop %q gained a refusal: %+v", pop.snippet, within)
			}
		}
		for _, pending := range within {
			for _, reason := range arrayPopIntroducedReasons() {
				if pending.Reason == reason {
					t.Errorf("golden root gained %q at %d:%d", reason, pending.Span.Start, pending.Span.End)
				}
			}
		}
	})
}

// Moved here from the pop/get_mut suite: that file sits at its cap, and the
// coordinator's standing instruction is to relocate rather than raise it.
func arrayPopCoreDecl(decl, name string, slots ...uint32) backingCheck {
	return backingCheck{function: name, unit: "core/array.sg", decl: decl, slots: slots, summary: true, clean: true}
}

// The five retained core bodies stop blocking the build. 1 parent + 2 = 3 RUN.
func TestReturnOriginArrayPopGetMutCoreRows(t *testing.T) {
	for _, row := range []struct {
		name   string
		source string
		checks []backingCheck
	}{
		{"core_pop_bodies", "p_array_pop_contents", []backingCheck{
			arrayPopCoreDecl("pub fn array_pop<T>(a: &mut Array<T>) -> Option<T> {", "array_pop", 0),
			arrayPopCoreDecl("pub fn pop(self: &mut Array<T>) -> Option<T> {", "pop", 0)}},
		{"core_get_mut_bodies", "g_array_get_mut_slot", []backingCheck{
			arrayPopCoreDecl("pub fn array_get_mut<T>(a: &mut Array<T>, index: int) -> &mut T {", "array_get_mut", 0),
			arrayPopCoreDecl("pub fn get_mut(self: &mut Array<T>, index: int) -> &mut T {", "get_mut", 0),
			arrayPopCoreDecl("pub fn get_mut(self: &mut ArrayFixed<T, N>, index: int) -> &mut T {", "get_mut", 0)}},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, g := analyzeArrayPopSource(t, "array_pop_core_rows", arrayPopNamedSource(t, row.source), nil)
			for _, check := range row.checks {
				checkBackingFunction(t, g, check)
			}
		})
	}
}
