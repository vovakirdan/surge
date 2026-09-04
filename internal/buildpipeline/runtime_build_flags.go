// The three environment hooks that change how a program's own runtime is
// compiled. All three are test-only and all three are fail-closed: an
// unexpected value is an error rather than a silent default, because each
// one changes what the built program MEASURES -- sync points change its
// scheduling, the bench counters change what it reports, and a negative
// control changes its behaviour on purpose.
package buildpipeline

import (
	"fmt"
	"os"
	"strings"
)

func runtimeCarrierBenchFlags() (flags []string, enabled bool, err error) {
	value := os.Getenv("SURGE_INTERNAL_CARRIER_BENCH_COUNTERS")
	switch value {
	case "":
		return nil, false, nil
	case "1":
		return []string{"-DRT_CARRIER_BENCH_ENABLED"}, true, nil
	default:
		return nil, false, fmt.Errorf(
			"SURGE_INTERNAL_CARRIER_BENCH_COUNTERS must be unset or exactly 1",
		)
	}
}

// runtimeNegativeControlFlags carries a Rule-13 mutant into a program's own
// runtime build. A defect that lives in the native runtime but is only
// observable through a compiled program -- an anchored body's state released
// on cancel, say -- has no other way to be shown red: the C stands cannot
// reach it, and rebuilding the runtime by hand beside the test would measure
// a different tree. So a test names the control here, exactly as
// SURGE_INTERNAL_TEST_SYNC_POINTS names the sync-point build, and the shape
// is refused rather than trusted: only RV2_*_NEGATIVE_CONTROL.
func runtimeNegativeControlFlags() ([]string, error) {
	value := os.Getenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL")
	if value == "" {
		return nil, nil
	}
	flags := make([]string, 0, 2)
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, "RV2_") || !strings.HasSuffix(name, "_NEGATIVE_CONTROL") {
			return nil, fmt.Errorf(
				"SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL names %q; every entry must be "+
					"RV2_*_NEGATIVE_CONTROL", name,
			)
		}
		flags = append(flags, "-D"+name)
	}
	return flags, nil
}

func runtimeTestSyncPointFlags() ([]string, error) {
	value := os.Getenv("SURGE_INTERNAL_TEST_SYNC_POINTS")
	switch value {
	case "":
		return nil, nil
	case "1":
		return []string{"-DRT_TEST_SYNC_POINTS"}, nil
	default:
		return nil, fmt.Errorf(
			"SURGE_INTERNAL_TEST_SYNC_POINTS must be unset or exactly 1",
		)
	}
}
