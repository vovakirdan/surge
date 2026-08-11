package vm_test

import "testing"

// Every fixture in vm_strings runs, panics included.
//
// The directory was swept with `skipSuffixes: []string{"_panics.sg"}` until the
// panic-site gate went looking for what reaches each raise. Five fixtures sat
// under that suffix - the four format reports and a string index out of bounds
// - recorded with their .out and .code and never executed by anything. All
// five agree on both backends; the suffix was never standing in for a red one.
// A blanket suffix is the wrong shape for excluding a fixture: it takes every
// future fixture with it silently. A fixture that genuinely must not run says
// so for itself, with the leading underscore the corpus already understands.
func TestVMStringsGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_strings")
}
