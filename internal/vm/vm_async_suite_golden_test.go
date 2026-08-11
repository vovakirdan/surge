package vm_test

import "testing"

func TestVMAsyncSuiteGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_async_suite")
}
