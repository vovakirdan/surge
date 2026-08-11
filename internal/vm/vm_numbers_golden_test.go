package vm_test

import "testing"

// Every fixture in vm_numbers runs, panics included.
//
// The directory used to be swept with `skipSuffixes: []string{"_panics.sg"}`,
// which was a blanket exclusion standing in for one fixture: `div_by_zero_panics`
// had no recorded `.out` or `.code`, so it could not be compared against
// anything. A suffix is the wrong shape for that — it silently took every future
// `_panics` fixture with it, and three landed under it before anyone noticed
// they were recorded and never executed. The one fixture has its answer recorded
// now, and a fixture that genuinely must not run says so for itself, with the
// leading underscore the corpus already understands.
func TestVMNumbersGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_numbers")
}
