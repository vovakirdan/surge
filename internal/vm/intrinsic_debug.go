package vm

import (
	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

type heapStatsLayoutInfo struct {
	layout      *StructLayout
	allocIdx    int
	freeIdx     int
	liveBlkIdx  int
	liveByteIdx int
	rcIncrIdx   int
	rcDecrIdx   int
	ok          bool
}

func (vm *VM) heapStatsLayout(typeID types.TypeID) (heapStatsLayoutInfo, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return heapStatsLayoutInfo{}, vmErr
	}
	info := heapStatsLayoutInfo{layout: layout}
	if len(layout.FieldNames) != 6 {
		return info, nil
	}
	allocIdx, okAlloc := layout.IndexByName["alloc_count"]
	freeIdx, okFree := layout.IndexByName["free_count"]
	liveBlkIdx, okLiveBlk := layout.IndexByName["live_blocks"]
	liveByteIdx, okLiveByte := layout.IndexByName["live_bytes"]
	rcIncrIdx, okRcIncr := layout.IndexByName["rc_increments"]
	rcDecrIdx, okRcDecr := layout.IndexByName["rc_decrements"]
	if !okAlloc || !okFree || !okLiveBlk || !okLiveByte || !okRcIncr || !okRcDecr {
		return info, nil
	}
	info.allocIdx = allocIdx
	info.freeIdx = freeIdx
	info.liveBlkIdx = liveBlkIdx
	info.liveByteIdx = liveByteIdx
	info.rcIncrIdx = rcIncrIdx
	info.rcDecrIdx = rcDecrIdx
	if vm.Types == nil {
		return info, nil
	}
	builtins := vm.Types.Builtins()
	fields := []int{allocIdx, freeIdx, liveBlkIdx, liveByteIdx, rcIncrIdx, rcDecrIdx}
	for _, idx := range fields {
		if vm.valueType(layout.FieldTypes[idx]) != builtins.Uint {
			return heapStatsLayoutInfo{layout: layout}, nil
		}
	}
	info.ok = true
	return info, nil
}

func (vm *VM) handleHeapStats(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_heap_stats requires 0 arguments")
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	info, vmErr := vm.heapStatsLayout(dstType)
	if vmErr != nil {
		return vmErr
	}
	if !info.ok {
		return vm.eb.makeError(PanicTypeMismatch, "invalid HeapStats layout")
	}
	snap := vm.heapStatsSnapshot()

	fields := make([]Value, len(info.layout.FieldNames))
	fields[info.allocIdx] = vm.makeBigUint(info.layout.FieldTypes[info.allocIdx], bignum.UintFromUint64(snap.allocCount))
	fields[info.freeIdx] = vm.makeBigUint(info.layout.FieldTypes[info.freeIdx], bignum.UintFromUint64(snap.freeCount))
	fields[info.liveBlkIdx] = vm.makeBigUint(info.layout.FieldTypes[info.liveBlkIdx], bignum.UintFromUint64(snap.liveBlocks))
	fields[info.liveByteIdx] = vm.makeBigUint(info.layout.FieldTypes[info.liveByteIdx], bignum.UintFromUint64(snap.liveBytes))
	fields[info.rcIncrIdx] = vm.makeBigUint(info.layout.FieldTypes[info.rcIncrIdx], bignum.UintFromUint64(snap.rcIncrCount))
	fields[info.rcDecrIdx] = vm.makeBigUint(info.layout.FieldTypes[info.rcDecrIdx], bignum.UintFromUint64(snap.rcDecrCount))

	h := vm.Heap.AllocStruct(info.layout.TypeID, fields)
	val := MakeHandleStruct(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

func (vm *VM) handleHeapDump(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_heap_dump requires 0 arguments")
	}
	dump := vm.heapDumpString()
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocString(dstType, dump)
	val := MakeHandleString(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}
