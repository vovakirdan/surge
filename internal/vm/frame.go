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
		Func:   fn,
		BB:     fn.Entry,
		IP:     0,
		Locals: locals,
		Span:   fn.Span,
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
