package vm

import (
	"fmt"
	"io"

	"surge/internal/mir"
	"surge/internal/source"
)

// Tracer outputs execution traces for debugging.
type Tracer struct {
	w     io.Writer
	files *source.FileSet
}

// NewTracer creates a new tracer that writes to w.
func NewTracer(w io.Writer, files *source.FileSet) *Tracer {
	return &Tracer{w: w, files: files}
}

// LocalWrite records a local variable modification.
type LocalWrite struct {
	LocalID mir.LocalID
	Name    string
	Value   Value
}

// TraceInstr traces execution of an instruction.
// Format: [depth=N] <func> bb<id>:ip<ip> <instr> @ <file>:<line>:<col>
func (t *Tracer) TraceInstr(depth int, fn *mir.Func, bb mir.BlockID, ip int, instr *mir.Instr, span source.Span, writes []LocalWrite) {
	if t == nil || t.w == nil {
		return
	}

	instrStr := t.formatInstr(instr)
	spanStr := t.formatSpan(span)

	fmt.Fprintf(t.w, "[depth=%d] %s bb%d:ip%d %s @ %s\n",
		depth, fn.Name, bb, ip, instrStr, spanStr)

	// Print local writes
	for _, w := range writes {
		fmt.Fprintf(t.w, "    write L%d(%s) = %s\n", w.LocalID, w.Name, w.Value.String())
	}
}

// TraceTerm traces execution of a terminator.
// Format: [depth=N] <func> bb<id>:term <terminator> @ <file>:<line>:<col>
func (t *Tracer) TraceTerm(depth int, fn *mir.Func, bb mir.BlockID, term *mir.Terminator, span source.Span) {
	if t == nil || t.w == nil {
		return
	}

	termStr := t.formatTerminator(term)
	spanStr := t.formatSpan(span)

	fmt.Fprintf(t.w, "[depth=%d] %s bb%d:term %s @ %s\n",
		depth, fn.Name, bb, termStr, spanStr)
}

// formatSpan formats a span as "file:line:col" or "<no-span>".
func (t *Tracer) formatSpan(span source.Span) string {
	if t.files == nil || (span.Start == 0 && span.End == 0) {
		return "<no-span>"
	}

	file := t.files.Get(span.File)
	if file == nil {
		return "<no-span>"
	}

	start, _ := t.files.Resolve(span)
	return fmt.Sprintf("%s:%d:%d", file.Path, start.Line, start.Col)
}

// formatInstr formats an instruction for tracing.
func (t *Tracer) formatInstr(instr *mir.Instr) string {
	switch instr.Kind {
	case mir.InstrAssign:
		return fmt.Sprintf("L%d = %s", instr.Assign.Dst.Local, t.formatRValue(&instr.Assign.Src))
	case mir.InstrCall:
		call := &instr.Call
		if call.HasDst {
			return fmt.Sprintf("L%d = call %s(%s)", call.Dst.Local, call.Callee.Name, t.formatArgs(call.Args))
		}
		return fmt.Sprintf("call %s(%s)", call.Callee.Name, t.formatArgs(call.Args))
	case mir.InstrDrop:
		return fmt.Sprintf("drop L%d", instr.Drop.Place.Local)
	case mir.InstrEndBorrow:
		return fmt.Sprintf("end_borrow L%d", instr.EndBorrow.Place.Local)
	case mir.InstrNop:
		return "nop"
	default:
		return fmt.Sprintf("<?instr:%d>", instr.Kind)
	}
}

// formatRValue formats an rvalue for tracing.
func (t *Tracer) formatRValue(rv *mir.RValue) string {
	switch rv.Kind {
	case mir.RValueUse:
		return t.formatOperand(&rv.Use)
	case mir.RValueBinaryOp:
		return fmt.Sprintf("(%s %s %s)",
			t.formatOperand(&rv.Binary.Left),
			rv.Binary.Op,
			t.formatOperand(&rv.Binary.Right))
	case mir.RValueUnaryOp:
		return fmt.Sprintf("(%s %s)", rv.Unary.Op, t.formatOperand(&rv.Unary.Operand))
	case mir.RValueIndex:
		return fmt.Sprintf("%s[%s]", t.formatOperand(&rv.Index.Object), t.formatOperand(&rv.Index.Index))
	default:
		return fmt.Sprintf("<?rvalue:%d>", rv.Kind)
	}
}

// formatOperand formats an operand for tracing.
func (t *Tracer) formatOperand(op *mir.Operand) string {
	switch op.Kind {
	case mir.OperandConst:
		return t.formatConst(&op.Const)
	case mir.OperandCopy:
		return fmt.Sprintf("copy L%d", op.Place.Local)
	case mir.OperandMove:
		return fmt.Sprintf("move L%d", op.Place.Local)
	case mir.OperandAddrOf:
		return fmt.Sprintf("&L%d", op.Place.Local)
	case mir.OperandAddrOfMut:
		return fmt.Sprintf("&mut L%d", op.Place.Local)
	default:
		return fmt.Sprintf("<?op:%d>", op.Kind)
	}
}

// formatConst formats a constant for tracing.
func (t *Tracer) formatConst(c *mir.Const) string {
	switch c.Kind {
	case mir.ConstInt:
		return fmt.Sprintf("const %d", c.IntValue)
	case mir.ConstUint:
		return fmt.Sprintf("const %du", c.UintValue)
	case mir.ConstFloat:
		return fmt.Sprintf("const %f", c.FloatValue)
	case mir.ConstBool:
		if c.BoolValue {
			return "const true"
		}
		return "const false"
	case mir.ConstString:
		return fmt.Sprintf("const %q", c.StringValue)
	case mir.ConstNothing:
		return "const nothing"
	default:
		return fmt.Sprintf("<?const:%d>", c.Kind)
	}
}

// formatArgs formats call arguments.
func (t *Tracer) formatArgs(args []mir.Operand) string {
	if len(args) == 0 {
		return ""
	}
	result := t.formatOperand(&args[0])
	for i := 1; i < len(args); i++ {
		result += ", " + t.formatOperand(&args[i])
	}
	return result
}

// formatTerminator formats a terminator for tracing.
func (t *Tracer) formatTerminator(term *mir.Terminator) string {
	switch term.Kind {
	case mir.TermReturn:
		if term.Return.HasValue {
			return fmt.Sprintf("return %s", t.formatOperand(&term.Return.Value))
		}
		return "return"
	case mir.TermGoto:
		return fmt.Sprintf("goto bb%d", term.Goto.Target)
	case mir.TermIf:
		return fmt.Sprintf("if %s then bb%d else bb%d",
			t.formatOperand(&term.If.Cond),
			term.If.Then, term.If.Else)
	case mir.TermSwitchTag:
		return fmt.Sprintf("switch_tag %s", t.formatOperand(&term.SwitchTag.Value))
	case mir.TermUnreachable:
		return "unreachable"
	default:
		return fmt.Sprintf("<?term:%d>", term.Kind)
	}
}
