package vm

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Inspector struct {
	vm  *VM
	out io.Writer
	fmt *Tracer
}

func NewInspector(vm *VM, out io.Writer) *Inspector {
	if vm == nil {
		return nil
	}
	return &Inspector{
		vm:  vm,
		out: out,
		fmt: NewFormatter(vm, vm.Files),
	}
}

func (i *Inspector) Locals() {
	if i == nil || i.out == nil {
		return
	}
	fmt.Fprintln(i.out, "locals:")

	if i.vm == nil || len(i.vm.Stack) == 0 {
		return
	}
	frame := &i.vm.Stack[len(i.vm.Stack)-1]
	for id := range frame.Locals {
		slot := &frame.Locals[id]
		name := slot.Name
		if name == "" {
			name = "?"
		}
		fmt.Fprintf(i.out, "  L%d(%s): %s\n", id, name, i.localValueString(slot))
	}
}

func (i *Inspector) Stack() {
	if i == nil || i.out == nil {
		return
	}
	fmt.Fprintln(i.out, "stack:")
	if i.vm == nil || i.vm.Files == nil {
		return
	}
	for depth := range len(i.vm.Stack) {
		frame := &i.vm.Stack[len(i.vm.Stack)-1-depth]
		name := "<nil>"
		span := "<no-span>"
		if frame.Func != nil {
			name = frame.Func.Name
		}
		if i.fmt != nil {
			span = i.fmt.formatSpan(frame.Span)
		}
		fmt.Fprintf(i.out, "  %d: %s @ %s\n", depth, name, span)
	}
}

func (i *Inspector) Heap() {
	if i == nil || i.out == nil {
		return
	}
	fmt.Fprintln(i.out, "heap:")
	if i.vm == nil || i.vm.Heap == nil {
		return
	}
	for h := Handle(1); h < i.vm.Heap.next; h++ {
		obj, ok := i.vm.Heap.lookup(h)
		if !ok || obj == nil || obj.Freed || obj.RefCount == 0 {
			continue
		}
		switch obj.Kind {
		case OKString:
			byteLen := 0
			if i.vm != nil {
				byteLen = i.vm.stringByteLen(obj)
			}
			fmt.Fprintf(i.out, "  string#%d len=%d\n", h, byteLen)
		case OKArray:
			fmt.Fprintf(i.out, "  array#%d len=%d\n", h, len(obj.Arr))
		case OKStruct:
			fmt.Fprintf(i.out, "  struct#%d type=type#%d\n", h, obj.TypeID)
		case OKTag:
			fmt.Fprintf(i.out, "  tag#%d type=type#%d tag=%s\n", h, obj.TypeID, i.tagName(obj))
		case OKRange:
			fmt.Fprintf(i.out, "  range#%d\n", h)
		default:
			fmt.Fprintf(i.out, "  object#%d type=type#%d\n", h, obj.TypeID)
		}
	}
}

func (i *Inspector) PrintLocal(spec string) bool {
	if i == nil || i.out == nil {
		return false
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	if i.vm == nil || len(i.vm.Stack) == 0 {
		fmt.Fprintf(i.out, "error: unknown local '%s'\n", spec)
		return false
	}

	frame := &i.vm.Stack[len(i.vm.Stack)-1]
	slot, label, ok := i.findLocal(frame, spec)
	if !ok {
		fmt.Fprintf(i.out, "error: unknown local '%s'\n", spec)
		return false
	}

	fmt.Fprintf(i.out, "%s = %s\n", label, i.localValueString(slot))
	return true
}

func (i *Inspector) findLocal(frame *Frame, spec string) (slot *LocalSlot, label string, ok bool) {
	if frame == nil {
		return nil, "", false
	}
	if strings.HasPrefix(spec, "L") && len(spec) > 1 {
		if n, err := strconv.Atoi(spec[1:]); err == nil && n >= 0 && n < len(frame.Locals) {
			return &frame.Locals[n], fmt.Sprintf("L%d", n), true
		}
	}

	for id := range frame.Locals {
		if frame.Locals[id].Name == spec {
			return &frame.Locals[id], spec, true
		}
	}
	return nil, "", false
}

func (i *Inspector) localValueString(slot *LocalSlot) string {
	if slot == nil {
		return "<uninit>"
	}
	if slot.IsMoved {
		return "<moved>"
	}
	if !slot.IsInit {
		return "<uninit>"
	}
	if i.fmt != nil {
		return i.fmt.formatValue(slot.V)
	}
	return slot.V.String()
}

func (i *Inspector) tagName(obj *Object) string {
	if obj == nil || obj.Kind != OKTag || i.vm == nil || i.vm.tagLayouts == nil {
		return "<unknown>"
	}
	tagName := "<unknown>"
	if layout, ok := i.vm.tagLayouts.Layout(i.vm.valueType(obj.TypeID)); ok && layout != nil {
		if tc, ok := layout.CaseBySym(obj.Tag.TagSym); ok && tc.TagName != "" {
			tagName = tc.TagName
		}
	}
	if tagName == "<unknown>" && obj.Tag.TagSym.IsValid() {
		if name, ok := i.vm.tagLayouts.AnyTagName(obj.Tag.TagSym); ok && name != "" {
			tagName = name
		}
	}
	return tagName
}
