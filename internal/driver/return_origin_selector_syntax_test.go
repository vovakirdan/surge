package driver

import (
	"testing"
)

// An enum declared in the prelude with an alias base: the variant's target is the enum's
// type name, which the checker never types. A root program, so the analysis that runs is
// the one a build runs. The harness and helpers are return_origin_core_range_test.go's.
const selectorPreludeEnumSource = `fn probe(a: &string) -> &string {
    let w: SeekWhence = SeekWhences::Current;
    let _ = w;
    return a;
}
`

// 2 RUN: 1 parent, 1 leaf.
func TestAnalyzeEnumVariantSelector(t *testing.T) {
	t.Run("prelude_enum_alias_base", func(t *testing.T) {
		checkOriginSource(t, selectorPreludeEnumSource, "7916dc591cfdeb5b8324ad8832880a1b133d776a2d6778d65303e76d6b5f8b21", originSpan{58, 78, "SeekWhences::Current"})
		f, analysis := analyzeOriginRoot(t, "selector_prelude_enum", selectorPreludeEnumSource, false, nil)
		fn := coreRangeFn(t, selectorPreludeEnumSource, "probe", 0, len(selectorPreludeEnumSource)-1)
		originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
		originNoEscape(t, analysis, f.owner.File.ID, fn)
		requireOriginSummary(t, analysis, f.owner.File.ID, "probe", false, []uint32{0})
	})
}
