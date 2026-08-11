package vm_test

import "testing"

func TestVMAsyncGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_async")
}
