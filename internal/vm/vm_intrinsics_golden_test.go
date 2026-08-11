package vm_test

import "testing"

func TestVMIntrinsicsGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_intrinsics")
}
