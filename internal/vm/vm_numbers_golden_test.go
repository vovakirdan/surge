package vm_test

import "testing"

func TestVMNumbersGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_numbers", behaviourCorpusOptions{skipSuffixes: []string{"_panics.sg"}})
}
