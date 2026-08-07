package vm

import (
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

// LocalSlot holds the runtime state of a local variable.
type LocalSlot struct {
	V         Value        // Current value
	IsInit    bool         // True if initialized (assigned at least once)
	IsMoved   bool         // True if value has been moved out
	IsDropped bool         // True if value has been dropped (@drop)
	PinCount  uint32       // Async task states borrowing this slot as backing storage
	Name      string       // Debug name from MIR
	TypeID    types.TypeID // Static type from MIR
}

// Frame represents a function activation record on the call stack.
type Frame struct {
	Func   *mir.Func   // The function being executed
	BB     mir.BlockID // Current basic block
	IP     int         // Instruction pointer within BB.Instrs
	Locals []LocalSlot // Local variable slots
	Span   source.Span // Current instruction span for error reporting

	// Result is how this activation's result reaches its caller, decided by
	// the caller at the call boundary. An activation entered other than by a
	// call — the program entry, or an async poll — leaves it unset, and its
	// result travels back the way it always did.
	Result resultProtocol

	// storage holds the bytes of this activation's composite slots, laid out
	// once per function by the layout registry. It is attached by the VM, so a
	// frame built without one has none.
	storage *Arena
	// scratch holds the composites this activation's instructions build before
	// they are stored anywhere, and scratchMark is the high-water point the
	// current instruction started from.
	scratch     *scratch
	scratchMark scratchMark
}

// NewFrame creates a new frame for executing the given function.
func NewFrame(fn *mir.Func) *Frame {
	locals := make([]LocalSlot, len(fn.Locals))
	for i, local := range fn.Locals {
		locals[i] = LocalSlot{
			Name:      local.Name,
			TypeID:    local.Type,
			IsInit:    false,
			IsMoved:   false,
			IsDropped: false,
		}
	}
	return &Frame{
		Func:    fn,
		BB:      fn.Entry,
		IP:      0,
		Locals:  locals,
		Span:    fn.Span,
		scratch: newScratch(),
	}
}

// CurrentBlock returns the current basic block being executed.
func (f *Frame) CurrentBlock() *mir.Block {
	if int(f.BB) < 0 || int(f.BB) >= len(f.Func.Blocks) {
		return nil
	}
	return &f.Func.Blocks[f.BB]
}

// CurrentInstr returns the current instruction, or nil if at terminator.
func (f *Frame) CurrentInstr() *mir.Instr {
	block := f.CurrentBlock()
	if block == nil || f.IP >= len(block.Instrs) {
		return nil
	}
	return &block.Instrs[f.IP]
}

// AtTerminator returns true if the IP is past all instructions (at terminator).
func (f *Frame) AtTerminator() bool {
	block := f.CurrentBlock()
	if block == nil {
		return true
	}
	return f.IP >= len(block.Instrs)
}
