package vm_test

import "testing"

func TestVMABIGolden(t *testing.T) {
	runBehaviourCorpus(t, "abi", behaviourCorpusOptions{})
}
