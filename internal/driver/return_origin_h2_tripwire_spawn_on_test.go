package driver

import "testing"

// RV2-DEBT-365 tripwire, the `spawn on` runner (N-TASK-27S). With an origin transfer, a far body is a runner: it can
// join the leaked task (son_await), spawn and join it (son_spawn), or hand it out as the far result (son_ret). The
// first line: each leaking program is refused by the task check alone, IN THE LEAKER, which no runner reaches. The
// second line: the same programs over a task that borrows nothing build; TestH2TripwireSpawnOnRunnerRuns (vm) runs
// them. One t.Run per program, so a counterfactual can redden one.

func TestH2TripwireTaskCheckRefusesSpawnOnLeaks(t *testing.T) {
	// son_ret hands the leaked task out as the far result, a task whose result is a task:
	// SEM3223 (owner ruling 2026-09-26) refuses its type and its body besides the task check.
	for _, row := range []struct {
		name, text, digest string
		beside             []string
	}{
		{"son_await", spawnOnLeakSonAwaitSource, spawnOnLeakSonAwaitSourceDigest, nil},
		{"son_spawn", spawnOnLeakSonSpawnSource, spawnOnLeakSonSpawnSourceDigest, nil},
		{"son_ret", spawnOnLeakSonRetSource, spawnOnLeakSonRetSourceDigest,
			[]string{"SEM3223@Task<Task<int>>", "SEM3223@spawn on distributed {\n        ret leak();\n    }"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.text, row.digest, "SEM3139", "t", row.beside...); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}

func TestH2TripwireSpawnOnRunnerBuilds(t *testing.T) {
	for _, row := range []struct{ name, text, digest string }{
		{"son_await", spawnOnTwinSonAwaitSource, spawnOnTwinSonAwaitSourceDigest},
		{"son_spawn", spawnOnTwinSonSpawnSource, spawnOnTwinSonSpawnSourceDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			if own, core, built := h2TripwirePending(t, row.text, row.digest); !built {
				t.Errorf("the spawn on runner does not build: own rows %+v, core rows %d", own, len(core))
			}
		})
	}
}
