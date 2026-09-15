package gatecheck

import "testing"

// An independent roster prevents narrowing -run and --expect together from
// silently removing a numeric lifecycle proof. Every proof has both an owned
// aggregate home and a place in the mandatory carrier closeout sweep.
func TestNumericLifecycleGateCoverageIsRequired(t *testing.T) {
	makefile := makefileText(t)
	for _, group := range []struct {
		home  string
		names []string
	}{
		{"runtime-v2-owned-storage-check", []string{
			"TestRefCountedNumericScalarKindsAndQualifiers",
			"TestEmitNumericLifecycleHeapGuardsDominate",
			"TestEmitNumericLifecyclePreservesFloatIR",
			"TestEmitNumericLifecycleAliasesAndNonOwningControls",
			"TestEmitNumericBoundsStepSkipsOnlyIntegerOneRelease",
			"TestEmitNumericLifecycleNestedControlFlow",
			"TestRuntimeV2NumericEmittedLifecycle",
			"TestRuntimeV2NumericEmittedLifecycleUnderAddressAndUndefinedSanitizers",
		}},
		{"runtime-v2-heap-check", []string{
			"TestRuntimeV2NumericEmittedLifecycleValgrindZero",
			"TestRuntimeV2NumericEmittedLifecycleNegativeControls",
			"TestRuntimeV2NumericCopyOwnershipValgrindZero",
		}},
	} {
		for _, target := range []string{group.home, carrierSanitizerTarget} {
			covered := make(map[string]int)
			for _, line := range recipeLines(makefile, target) {
				for _, name := range expectedTests(line) {
					covered[name]++
				}
			}
			for _, name := range group.names {
				if covered[name] != 1 {
					t.Errorf("%s declares numeric lifecycle proof %s %d times, want once",
						target, name, covered[name])
				}
			}
		}
	}
}
