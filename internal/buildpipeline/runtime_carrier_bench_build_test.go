package buildpipeline

import "testing"

func TestRuntimeCarrierBenchBuildFlag(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_CARRIER_BENCH_COUNTERS", "")
	flags, enabled, err := runtimeCarrierBenchFlags()
	if err != nil || enabled || len(flags) != 0 {
		t.Fatalf("disabled carrier bench flags = %v, %v, %v", flags, enabled, err)
	}

	t.Setenv("SURGE_INTERNAL_CARRIER_BENCH_COUNTERS", "1")
	flags, enabled, err = runtimeCarrierBenchFlags()
	if err != nil || !enabled || len(flags) != 1 || flags[0] != "-DRT_CARRIER_BENCH_ENABLED" {
		t.Fatalf("enabled carrier bench flags = %v, %v, %v", flags, enabled, err)
	}

	t.Setenv("SURGE_INTERNAL_CARRIER_BENCH_COUNTERS", "true")
	if _, _, err = runtimeCarrierBenchFlags(); err == nil {
		t.Fatal("invalid carrier bench build flag accepted")
	}
}
