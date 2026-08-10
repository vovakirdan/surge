package vm_test

import "testing"

func TestVMCompareGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_compare", behaviourCorpusOptions{skipSuffixes: []string{"_panics.sg"}})
}
