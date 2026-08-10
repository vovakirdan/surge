package vm_test

import "testing"

func TestVMStringsGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_strings", behaviourCorpusOptions{skipSuffixes: []string{"_panics.sg"}})
}
