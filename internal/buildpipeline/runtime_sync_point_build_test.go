package buildpipeline

import "testing"

func TestRuntimeTestSyncPointBuildFlag(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_TEST_SYNC_POINTS", "")
	flags, err := runtimeTestSyncPointFlags()
	if err != nil || len(flags) != 0 {
		t.Fatalf("default build must be hook-free: flags=%v err=%v", flags, err)
	}

	t.Setenv("SURGE_INTERNAL_TEST_SYNC_POINTS", "1")
	flags, err = runtimeTestSyncPointFlags()
	if err != nil || len(flags) != 1 || flags[0] != "-DRT_TEST_SYNC_POINTS" {
		t.Fatalf("armed build flag mismatch: flags=%v err=%v", flags, err)
	}

	t.Setenv("SURGE_INTERNAL_TEST_SYNC_POINTS", "true")
	if _, err = runtimeTestSyncPointFlags(); err == nil {
		t.Fatal("ambiguous test-sync-point value must fail closed")
	}
}
