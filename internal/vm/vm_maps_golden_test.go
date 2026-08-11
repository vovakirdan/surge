package vm_test

import "testing"

func TestVMMapsGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_maps")
}
