package driver

import "testing"

// RV2-DEBT-365 tripwire, the `spawn on` runner (N-TASK-27S). With an origin transfer, a far body is a runner: it can
// join the leaked task (son_await), spawn and join it (son_spawn), or hand it out as the far result (son_ret). The
// first line: each leaking program is refused by the task check alone, IN THE LEAKER, which no runner reaches. The
// second line: the same programs over a task that borrows nothing build; TestH2TripwireSpawnOnRunnerRuns (vm) runs
// them. One t.Run per program, so a counterfactual can redden one.

func TestH2TripwireTaskCheckRefusesSpawnOnLeaks(t *testing.T) {
	for _, row := range []struct{ name, text, digest string }{
		{"son_await", spawnOnLeakSonAwaitSource, spawnOnLeakSonAwaitSourceDigest},
		{"son_spawn", spawnOnLeakSonSpawnSource, spawnOnLeakSonSpawnSourceDigest},
		{"son_ret", spawnOnLeakSonRetSource, spawnOnLeakSonRetSourceDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.text, row.digest, "SEM3139", "t"); verdict != "" {
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
