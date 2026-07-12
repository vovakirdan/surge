package crossinggate

import (
	"path/filepath"
	"testing"
)

// TestSpawnOnBackendGuards validates the crossing guards directly against
// the staged block03 fixtures, independent of the Block3Enabled gate: a
// `spawn on` remote spawn, `far Task<T>.await()`, or `far Task<T>.cancel()`
// that type-checks but reaches a backend must be reported deterministically
// rather than crash or silently drop. The fixtures sit in synchronous
// functions, so the guard names the missing async context (FUT7019) — the
// same finding on every backend; the await / cancel fixtures obtain the
// `far Task` via a parameter (no `spawn on` in the same file), proving the
// guards fire on their own.
func TestSpawnOnBackendGuards(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	base := filepath.Join(repoRoot(t), "testdata", "golden", "crossing", "block03", "invalid")

	cases := []struct {
		fixture string
		want    string
	}{
		{"_spawn_on_negative_backend_unavailable.sg", "FUT7019"},
		{"_spawn_on_negative_await_backend_unavailable.sg", "FUT7019"},
		{"_spawn_on_negative_cancel_backend_unavailable.sg", "FUT7019"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			path := filepath.Join(base, tc.fixture)
			for _, backend := range backendStageBackends {
				t.Run(string(backend), func(t *testing.T) {
					diags := diagnoseBackend(t, path, backend)
					for _, d := range diags {
						if d.Code.ID() == tc.want {
							return
						}
					}
					t.Errorf("expected %s from backend guard, got %s", tc.want, summarize(diags))
				})
			}
		})
	}
}
