package vm

import "fmt"

type rawAlloc struct {
	data  []byte
	align int
	freed bool
}

type rawMemory struct {
	next   Handle
	allocs map[Handle]*rawAlloc
}

func newRawMemory() *rawMemory {
	return &rawMemory{
		next:   1,
		allocs: make(map[Handle]*rawAlloc, 64),
	}
}

func (vm *VM) ensureRawMemory() {
	if vm.rawMem == nil {
		vm.rawMem = newRawMemory()
	}
}

func (vm *VM) rawAlloc(size, align int) (Handle, *VMError) {
	if size < 0 {
		return 0, vm.eb.invalidNumericConversion("alloc size out of range")
	}
	if align <= 0 {
		align = 1
	}
	vm.ensureRawMemory()
	h := vm.rawMem.next
	vm.rawMem.next++
	vm.rawMem.allocs[h] = &rawAlloc{
		data:  make([]byte, size),
		align: align,
	}
	vm.heapCounters.allocCount++
	return h, nil
}

func (vm *VM) rawGet(handle Handle) (*rawAlloc, *VMError) {
	if handle == 0 {
		return nil, vm.eb.makeError(PanicInvalidHandle, "invalid raw handle 0")
	}
	vm.ensureRawMemory()
	alloc, ok := vm.rawMem.allocs[handle]
	if !ok || alloc == nil {
		return nil, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid raw handle %d", handle))
	}
	if alloc.freed {
		return nil, vm.eb.makeError(PanicUseAfterFree, fmt.Sprintf("use-after-free: raw handle %d", handle))
	}
	return alloc, nil
}

func (vm *VM) rawFree(handle Handle, size, align int) *VMError {
	if handle == 0 {
		return vm.eb.makeError(PanicInvalidHandle, "invalid raw handle 0")
	}
	vm.ensureRawMemory()
	alloc, ok := vm.rawMem.allocs[handle]
	if !ok || alloc == nil {
		return vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid raw handle %d", handle))
	}
	if alloc.freed {
		return vm.eb.makeError(PanicDoubleFree, fmt.Sprintf("double free: raw handle %d", handle))
	}
	if size >= 0 && len(alloc.data) != size {
		return vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid raw free size: got %d want %d", size, len(alloc.data)))
	}
	if align > 0 && alloc.align != align {
		return vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid raw free align: got %d want %d", align, alloc.align))
	}
	vm.heapCounters.freeCount++
	alloc.freed = true
	alloc.data = nil
	return nil
}

func (vm *VM) rawRealloc(handle Handle, oldSize, newSize, align int) (Handle, *VMError) {
	if handle == 0 {
		return vm.rawAlloc(newSize, align)
	}
	alloc, vmErr := vm.rawGet(handle)
	if vmErr != nil {
		return 0, vmErr
	}
	if oldSize >= 0 && len(alloc.data) != oldSize {
		return 0, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid raw realloc size: got %d want %d", oldSize, len(alloc.data)))
	}
	if align > 0 && alloc.align != align {
		return 0, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid raw realloc align: got %d want %d", align, alloc.align))
	}
	newHandle, vmErr := vm.rawAlloc(newSize, align)
	if vmErr != nil {
		return 0, vmErr
	}
	newAlloc, vmErr := vm.rawGet(newHandle)
	if vmErr != nil {
		return 0, vmErr
	}
	copy(newAlloc.data, alloc.data)
	vm.heapCounters.freeCount++
	alloc.freed = true
	alloc.data = nil
	return newHandle, nil
}
