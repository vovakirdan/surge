package vm

const monotonicNsPerMs int64 = 1_000_000

func (vm *VM) monotonicNowMs() uint64 {
	if vm == nil || vm.RT == nil {
		return 0
	}
	ns := vm.RT.MonotonicNow()
	if ns <= 0 {
		return 0
	}
	return uint64(ns / monotonicNsPerMs)
}
