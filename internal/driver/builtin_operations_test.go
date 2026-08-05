package driver

import (
	"context"
	"path/filepath"
	"testing"
)

// TestBuiltinOperationIsIngestedOnce pins the two ways one builtin `extern`
// block reaches the merged catalog more than once: the same file read under two
// module paths, and a second copy of it compiled from elsewhere in the tree.
// Either way `string` must end up with one `__clone` operation, because the
// compiler implements exactly one.
func TestBuiltinOperationIsIngestedOnce(t *testing.T) {
	repo := repoRootFromDriverTest(t)
	for _, source := range []string{
		"core/intrinsics.sg",
		filepath.Join("testdata", "golden", "core_stdlib", "string.sg"),
	} {
		t.Run(filepath.ToSlash(source), func(t *testing.T) {
			t.Setenv("SURGE_STDLIB", repo)
			result, err := DiagnoseWithOptions(
				context.Background(),
				filepath.Join(repo, source),
				&DiagnoseOptions{Stage: DiagnoseStageAll, MaxDiagnostics: 64},
			)
			if err != nil {
				t.Fatalf("diagnose %s: %v", source, err)
			}
			if result == nil || result.Sema == nil {
				t.Fatalf("diagnose %s produced no semantic authority", source)
			}
			modules := make([]string, 0, 2)
			for i := range result.Sema.CallableCandidates {
				candidate := &result.Sema.CallableCandidates[i]
				if candidate.Builtin && candidate.Name == "__clone" && candidate.ReceiverKey == "string" {
					modules = append(modules, candidate.ModulePath)
				}
			}
			if len(modules) != 1 {
				t.Fatalf("string __clone operations = %v, want exactly one record", modules)
			}
		})
	}
}
