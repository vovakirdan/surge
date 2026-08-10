package vm_test

import "testing"

func TestVMTuplesGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_tuples", behaviourCorpusOptions{skipSuffixes: []string{"_panics.sg"}})
}
