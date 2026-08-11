package vm_test

import "testing"

// As in vm_compare, the `_panics.sg` sweep here matched no fixture. It is
// removed rather than kept as a precaution, because a precaution that silently
// drops a future fixture costs more than it saves.
func TestVMTuplesGolden(t *testing.T) {
	runBehaviourCorpus(t, "vm_tuples")
}
