package gatecheck

import "testing"

// Keep this roster independent of the Makefile: narrowing both --expect and
// -run must not silently drop the heap-uint subtraction proof from closeout.
func TestBiguintSubSanitizerCoverageIsRequired(t *testing.T) {
	covered := make(map[string]bool)
	for _, line := range recipeLines(makefileText(t), carrierSanitizerTarget) {
		for _, name := range expectedTests(line) {
			covered[name] = true
		}
	}
	for _, name := range []string{
		"TestRuntimeV2BiguintSubZeroAndOrder",
		"TestRuntimeV2BiguintSubUnderAddressAndUndefinedSanitizers",
		"TestRuntimeV2BiguintSubEqualNegativeControl",
		"TestRuntimeV2BiguintSubValgrindZero",
	} {
		if !covered[name] {
			t.Errorf("%s no longer declares required heap-uint subtraction proof %s",
				carrierSanitizerTarget, name)
		}
	}
}
