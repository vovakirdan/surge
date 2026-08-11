package vm_test

import "testing"

// The `_panics.sg` sweep this directory carried matched nothing: vm_compare has
// no panic fixture at all. A suffix exclusion that excludes nothing is not
// harmless - it is a trap set for whoever writes the first one, which is how
// three fixtures in another directory landed recorded and never executed.
func TestVMCompareGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_compare")
}
