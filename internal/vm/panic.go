package vm

import (
	"fmt"
	"strings"

	"surge/internal/source"
)

// PanicCode identifies the type of VM panic.
type PanicCode int

// Stable panic codes - do not change values.
const (
	PanicUseBeforeInit        PanicCode = 1001 // VM1001: use before initialization
	PanicUseAfterMove         PanicCode = 1002 // VM1002: use after move
	PanicTypeMismatch         PanicCode = 1003 // VM1003: type mismatch
	PanicOutOfBounds          PanicCode = 1004 // VM1004: out of bounds
	PanicUnsupportedIntrinsic PanicCode = 1005 // VM1005: unsupported intrinsic
	PanicUnsupportedParseType PanicCode = 1006 // VM1006: unsupported parse type
	PanicUnimplemented        PanicCode = 1999 // VM1999: unimplemented opcode/terminator
)

// String returns the code as "VM1001" format.
func (c PanicCode) String() string {
	return fmt.Sprintf("VM%d", c)
}

// BacktraceFrame represents one frame in the panic backtrace.
type BacktraceFrame struct {
	FuncName string
	Span     source.Span
}

// VMError represents a runtime panic in the VM.
type VMError struct {
	Code      PanicCode
	Message   string
	Span      source.Span      // Location where panic occurred
	Backtrace []BacktraceFrame // Stack frames from top to bottom
}

// Error implements the error interface.
func (p *VMError) Error() string {
	return fmt.Sprintf("panic %s: %s", p.Code, p.Message)
}

// FormatWithFiles formats the panic with resolved file:line:col information.
func (p *VMError) FormatWithFiles(files *source.FileSet) string {
	var sb strings.Builder

	// Header: panic VM1004: <message>
	sb.WriteString(fmt.Sprintf("panic %s: %s\n", p.Code, p.Message))

	// Location: at <file>:<line>:<col>
	sb.WriteString("at ")
	sb.WriteString(formatSpan(p.Span, files))
	sb.WriteString("\n")

	// Backtrace
	if len(p.Backtrace) > 0 {
		sb.WriteString("backtrace:\n")
		for i, frame := range p.Backtrace {
			sb.WriteString(fmt.Sprintf("  %d: %s at %s\n", i, frame.FuncName, formatSpan(frame.Span, files)))
		}
	}

	return sb.String()
}

// formatSpan formats a span as "file:line:col" or "<no-span>" if empty.
func formatSpan(span source.Span, files *source.FileSet) string {
	if files == nil || (span.Start == 0 && span.End == 0) {
		return "<no-span>"
	}

	file := files.Get(span.File)
	if file == nil {
		return "<no-span>"
	}

	start, _ := files.Resolve(span)
	return fmt.Sprintf("%s:%d:%d", file.Path, start.Line, start.Col)
}

// errorBuilder helps construct VMError values.
type errorBuilder struct {
	vm *VM
}

func (eb *errorBuilder) makeError(code PanicCode, msg string) *VMError {
	e := &VMError{
		Code:    code,
		Message: msg,
	}

	// Get current span from top frame
	if len(eb.vm.Stack) > 0 {
		frame := &eb.vm.Stack[len(eb.vm.Stack)-1]
		e.Span = frame.Span
	}

	// Build backtrace from stack (top to bottom)
	e.Backtrace = make([]BacktraceFrame, len(eb.vm.Stack))
	for i := len(eb.vm.Stack) - 1; i >= 0; i-- {
		frame := &eb.vm.Stack[i]
		e.Backtrace[len(eb.vm.Stack)-1-i] = BacktraceFrame{
			FuncName: frame.Func.Name,
			Span:     frame.Span,
		}
	}

	return e
}

func (eb *errorBuilder) useBeforeInit(localName string) *VMError {
	return eb.makeError(PanicUseBeforeInit, fmt.Sprintf("local %q used before initialization", localName))
}

func (eb *errorBuilder) useAfterMove(localName string) *VMError {
	return eb.makeError(PanicUseAfterMove, fmt.Sprintf("local %q used after move", localName))
}

func (eb *errorBuilder) typeMismatch(expected, got string) *VMError {
	return eb.makeError(PanicTypeMismatch, fmt.Sprintf("expected %s, got %s", expected, got))
}

func (eb *errorBuilder) outOfBounds(index, length int) *VMError {
	return eb.makeError(PanicOutOfBounds, fmt.Sprintf("index %d out of bounds for length %d", index, length))
}

func (eb *errorBuilder) unsupportedIntrinsic(name string) *VMError {
	return eb.makeError(PanicUnsupportedIntrinsic, fmt.Sprintf("unsupported intrinsic: %s", name))
}

func (eb *errorBuilder) unsupportedParseType(typeName string) *VMError {
	return eb.makeError(PanicUnsupportedParseType, fmt.Sprintf("rt_parse_arg only supports int, got %s", typeName))
}

func (eb *errorBuilder) unimplemented(what string) *VMError {
	return eb.makeError(PanicUnimplemented, fmt.Sprintf("unimplemented: %s", what))
}
