package vm

import (
	"fmt"
	"sort"
	"strings"
)

type heapDumpRecord struct {
	typeName  string
	kind      string
	size      uint64
	rc        uint32
	refs      int
	lenCP     int
	lenBytes  int
	arrLen    int
	arrCap    int
	arrStart  int
	repr      string
	tag       string
	rangeKind string
	align     int
	line      string
}

func (vm *VM) heapDumpString() string {
	if vm == nil {
		return ""
	}
	records := make([]heapDumpRecord, 0)
	if vm.Heap != nil {
		for h := Handle(1); h < vm.Heap.next; h++ {
			obj, ok := vm.Heap.lookup(h)
			if !ok || obj == nil || obj.Freed || obj.RefCount == 0 {
				continue
			}
			records = append(records, vm.heapDumpRecord(obj))
		}
	}
	if vm.rawMem != nil {
		for _, alloc := range vm.rawMem.allocs {
			if alloc == nil || alloc.freed {
				continue
			}
			records = append(records, heapDumpRecordForRaw(alloc))
		}
	}

	if len(records) == 0 {
		return ""
	}

	sort.Slice(records, func(i, j int) bool {
		a := records[i]
		b := records[j]
		if a.typeName != b.typeName {
			return a.typeName < b.typeName
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.size != b.size {
			return a.size < b.size
		}
		if a.lenCP != b.lenCP {
			return a.lenCP < b.lenCP
		}
		if a.lenBytes != b.lenBytes {
			return a.lenBytes < b.lenBytes
		}
		if a.arrLen != b.arrLen {
			return a.arrLen < b.arrLen
		}
		if a.arrCap != b.arrCap {
			return a.arrCap < b.arrCap
		}
		if a.arrStart != b.arrStart {
			return a.arrStart < b.arrStart
		}
		if a.rc != b.rc {
			return a.rc < b.rc
		}
		if a.refs != b.refs {
			return a.refs < b.refs
		}
		if a.repr != b.repr {
			return a.repr < b.repr
		}
		if a.rangeKind != b.rangeKind {
			return a.rangeKind < b.rangeKind
		}
		if a.tag != b.tag {
			return a.tag < b.tag
		}
		if a.align != b.align {
			return a.align < b.align
		}
		return a.line < b.line
	})

	var sb strings.Builder
	for i := 0; i < len(records); {
		line := records[i].line
		count := 1
		for j := i + 1; j < len(records); j++ {
			if records[j].line != line {
				break
			}
			count++
		}
		sb.WriteString(line)
		if count > 1 {
			sb.WriteString(fmt.Sprintf(" count=%d", count))
		}
		sb.WriteString("\n")
		i += count
	}
	return sb.String()
}

func (vm *VM) heapDumpRecord(obj *Object) heapDumpRecord {
	rec := heapDumpRecord{
		typeName: typeLabel(vm.Types, obj.TypeID),
		size:     vm.heapObjectBytes(obj),
		rc:       obj.RefCount,
		refs:     vm.objectRefCount(obj),
	}
	if rec.typeName == "?" || rec.typeName == "" {
		rec.typeName = vm.objectKindLabel(obj.Kind)
	}
	switch obj.Kind {
	case OKString:
		rec.kind = stringReprLabel(obj.StrKind)
		rec.lenCP = vm.stringCPLen(obj)
		rec.lenBytes = vm.stringByteLen(obj)
		rec.repr = rec.kind
	case OKArray:
		rec.kind = "array"
		rec.arrLen = len(obj.Arr)
		rec.arrCap = cap(obj.Arr)
	case OKArraySlice:
		rec.kind = "view"
		rec.arrLen = obj.ArrSliceLen
		rec.arrCap = obj.ArrSliceCap
		rec.arrStart = obj.ArrSliceStart
	case OKStruct:
		rec.kind = "struct"
	case OKTag:
		rec.kind = "tag"
		rec.tag = vm.tagName(obj)
	case OKRange:
		rec.kind = "range"
		rec.rangeKind = rangeKindLabel(obj.Range.Kind)
	case OKBigInt:
		rec.kind = "bigint"
	case OKBigUint:
		rec.kind = "biguint"
	case OKBigFloat:
		rec.kind = "bigfloat"
	default:
		rec.kind = "object"
	}
	rec.line = rec.formatLine()
	return rec
}

func heapDumpRecordForRaw(alloc *rawAlloc) heapDumpRecord {
	rec := heapDumpRecord{
		typeName: "raw",
		kind:     "raw",
		size:     uint64(len(alloc.data)),
		align:    alloc.align,
	}
	rec.line = rec.formatLine()
	return rec
}

func (rec heapDumpRecord) formatLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "OBJ type=%s size=%d rc=%d kind=%s refs=%d", rec.typeName, rec.size, rec.rc, rec.kind, rec.refs)
	if rec.repr != "" {
		fmt.Fprintf(&b, " len_cp=%d len_bytes=%d repr=%s", rec.lenCP, rec.lenBytes, rec.repr)
	}
	if rec.kind == "array" {
		fmt.Fprintf(&b, " len=%d cap=%d", rec.arrLen, rec.arrCap)
	}
	if rec.kind == "view" {
		fmt.Fprintf(&b, " len=%d cap=%d start=%d", rec.arrLen, rec.arrCap, rec.arrStart)
	}
	if rec.rangeKind != "" {
		fmt.Fprintf(&b, " range_kind=%s", rec.rangeKind)
	}
	if rec.tag != "" {
		fmt.Fprintf(&b, " tag=%s", rec.tag)
	}
	if rec.align > 0 {
		fmt.Fprintf(&b, " align=%d", rec.align)
	}
	return b.String()
}
