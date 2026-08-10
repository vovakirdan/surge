package vm_test

import "testing"

func TestVMRCGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_rc", behaviourCorpusOptions{})
}
