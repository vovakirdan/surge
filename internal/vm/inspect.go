package vm

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Inspector provides helpers for examining VM state.
type Inspector struct {
	vm  *VM
	out io.Writer
	fmt *Tracer
}

// NewInspector creates a new inspector for the given VM.
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

// writef writes one line to the inspector's sink. A debugger that cannot reach
// its own output has nothing left to answer through, so a failed write ends the
// process rather than leaving a session that silently reports nothing.
func (i *Inspector) writef(format string, args ...any) {
	if _, err := fmt.Fprintf(i.out, format, args...); err != nil {
		panic(err)
	}
}

// Locals prints the values of all locals in the current frame.
func (i *Inspector) Locals() {
	if i == nil || i.out == nil {
		return
	}
	i.writef("locals:\n")
	if i.vm == nil || len(i.vm.Stack) == 0 {
		return
	}
	frame := i.vm.Stack[len(i.vm.Stack)-1]
	for id := range frame.Locals {
		slot := &frame.Locals[id]
		name := slot.Name
		if name == "" {
			name = "?"
		}
		i.writef("  L%d(%s): %s\n", id, name, i.localValueString(slot))
	}
}

// Stack prints the current call stack.
func (i *Inspector) Stack() {
	if i == nil || i.out == nil {
		return
	}
	i.writef("stack:\n")
	if i.vm == nil || i.vm.Files == nil {
		return
	}
	for depth := range len(i.vm.Stack) {
		frame := i.vm.Stack[len(i.vm.Stack)-1-depth]
		name := "<nil>"
		span := "<no-span>"
		if frame.Func != nil {
			name = frame.Func.Name
		}
		if i.fmt != nil {
			span = i.fmt.formatSpan(frame.Span)
		}
		i.writef("  %d: %s @ %s\n", depth, name, span)
	}
}

// Heap prints all active objects on the heap.
func (i *Inspector) Heap() {
	if i == nil || i.out == nil {
		return
	}
	i.writef("heap:\n")
	if i.vm == nil || i.vm.Heap == nil {
		return
	}
	for h := Handle(1); h < i.vm.Heap.next; h++ {
		obj, ok := i.vm.Heap.lookup(h)
		if !ok || obj == nil || obj.Freed || obj.RefCount == 0 {
			continue
		}
		i.writef("  %s\n", i.vm.objectSummary(obj))
	}
}

// PrintLocal prints the value of a specific local by name or ID.
func (i *Inspector) PrintLocal(spec string) bool {

	var printErr error

	if i == nil || i.out == nil {
		return false
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	if i.vm == nil || len(i.vm.Stack) == 0 {
		_, printErr = fmt.Fprintf(i.out, "error: unknown local '%s'\n", spec)
		if printErr != nil {
			panic(printErr)
		}
		return false
	}

	frame := i.vm.Stack[len(i.vm.Stack)-1]
	slot, label, ok := i.findLocal(frame, spec)
	if !ok {
		_, printErr = fmt.Fprintf(i.out, "error: unknown local '%s'\n", spec)
		if printErr != nil {
			panic(printErr)
		}
		return false
	}

	_, printErr = fmt.Fprintf(i.out, "%s = %s\n", label, i.localValueString(slot))
	if printErr != nil {
		panic(printErr)
	}
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
