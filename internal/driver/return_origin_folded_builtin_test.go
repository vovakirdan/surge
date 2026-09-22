package driver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// A body-less builtin declaration spelled out again outside core is folded into the
// standard library's record of the same operation. The unit that spells it must not
// abort the analysis for want of its own record: it answers nothing, and the merged
// catalog keeps exactly the standard library's record.
const foldedBuiltinSource = `@intrinsic fn rt_panic_bounds(kind: uint, index: int, length: int) -> nothing;
fn probe() -> int {
    return 1;
}
`

// 3 RUN: 1 parent, 2 leaves.
func TestAnalyzeFoldedBuiltinDeclaration(t *testing.T) {
	t.Run("root_redeclares_core_intrinsic", func(t *testing.T) {
		checkOriginSource(t, foldedBuiltinSource, "4dcff1126e0dc1b0160e525e8808b3d684f63f25bb1f76eeb77d74e700743a0c")
		f, analysis, err := farSelectorAnalyze(t, foldedBuiltinSource, nil)
		if err != nil || analysis == nil {
			t.Fatalf("return-origin analysis did not run: %v", err)
		}
		survivors := 0
		for i := range f.authority.CallableCandidates {
			if c := &f.authority.CallableCandidates[i]; c.Builtin && c.Name == "rt_panic_bounds" {
				survivors++
				if c.ModulePath != "core/intrinsics" {
					t.Errorf("surviving rt_panic_bounds record comes from %q, want core/intrinsics", c.ModulePath)
				}
			}
		}
		if survivors != 1 {
			t.Errorf("merged catalog holds %d rt_panic_bounds records, want 1", survivors)
		}
		fn := coreRangeFn(t, foldedBuiltinSource, "probe", 79, 114)
		originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
		requireOriginSummary(t, analysis, f.owner.File.ID, "probe", false, nil)
	})
	// The golden copy of core, as the corpus census diagnoses it: every intrinsic it
	// spells out again is folded, and none may stop the analysis. The copy is not core,
	// so no identity certificate answers its bodies: it stays an unfinished program.
	t.Run("core_stdlib_mirror", func(t *testing.T) {
		repo := repoRootFromDriverTest(t)
		t.Setenv("SURGE_STDLIB", repo)
		_, err := DiagnoseWithOptions(context.Background(), filepath.Join(repo, "testdata", "golden", "core_stdlib", "array.sg"),
			&DiagnoseOptions{Stage: DiagnoseStageAll, MaxDiagnostics: 64})
		var unfinished *returnOriginUnfinishedError
		switch {
		case err == nil:
		case strings.Contains(err.Error(), "has no matching published authority"):
			t.Fatalf("a folded builtin declaration still stops the analysis: %v", err)
		case !errors.As(err, &unfinished):
			t.Fatalf("diagnose of the core copy stopped before return origins finished: %v", err)
		case len(unfinished.Pending) == 0:
			t.Errorf("unfinished analysis of the core copy names no row")
		}
	})
}
