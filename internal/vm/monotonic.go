package vm

import "fortio.org/safecast"

const monotonicNsPerMs int64 = 1_000_000

func (vm *VM) monotonicNowMs() uint64 {
	if vm == nil || vm.RT == nil {
		return 0
	}
	ns := vm.RT.MonotonicNow()
	if ns <= 0 {
		return 0
	}
	ms := ns / monotonicNsPerMs
	if ms <= 0 {
		return 0
	}
	out, err := safecast.Conv[uint64](ms)
	if err != nil {
		return 0
	}
	return out
}
