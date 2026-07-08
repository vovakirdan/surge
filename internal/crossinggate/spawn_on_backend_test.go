package crossinggate

import (
	"path/filepath"
	"testing"
)

// TestSpawnOnBackendGuards validates the Epic 11 Block 3 backend-unavailable
// guards (FUT7015 / FUT7016 / FUT7017) directly against the staged block03
// fixtures, independent of the Block3Enabled gate. Epic 11 delivers the remote
// spawn surface compile-only: a `spawn on` remote spawn, `far Task<T>.await()`,
// or `far Task<T>.cancel()` that type-checks but reaches a backend must be
// reported deterministically rather than crash or silently drop. The await /
// cancel fixtures obtain the `far Task` via a parameter (no `spawn on` in the
// same file), proving those guards fire on their own.
func TestSpawnOnBackendGuards(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	base := filepath.Join(repoRoot(t), "testdata", "golden", "crossing", "block03", "invalid")

	cases := []struct {
		fixture string
		want    string
	}{
		{"_spawn_on_negative_backend_unavailable.sg", "FUT7015"},
		{"_spawn_on_negative_await_backend_unavailable.sg", "FUT7016"},
		{"_spawn_on_negative_cancel_backend_unavailable.sg", "FUT7017"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			path := filepath.Join(base, tc.fixture)
			diags := diagnoseBackend(t, path)
			for _, d := range diags {
				if d.Code.ID() == tc.want {
					return
				}
			}
			t.Errorf("expected %s from backend guard, got %s", tc.want, summarize(diags))
		})
	}
}
