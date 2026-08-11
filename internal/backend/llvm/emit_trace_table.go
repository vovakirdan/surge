package llvm

import (
	"fmt"

	"surge/internal/source"
)

// Where a native panic gets its backtrace from.
//
// The VM can name its frames because it HAS frames: an interpreter stack it can
// walk. A compiled binary has a machine stack instead, and nothing on it says
// which Surge function an address belongs to or which line it is executing.
//
// The choice made here is to recover that at panic time rather than maintain it
// as the program runs, because the second costs something on every call and the
// first costs nothing until something has already gone wrong. Two facts make it
// work, both measured rather than assumed. LLVM already emits `.eh_frame` for
// these functions, so the stack can be walked with no help from us and no frame
// pointer — which matters, because clang does NOT keep a frame pointer here even
// at -O0. And the assembler will build an address table for us: a label in the
// instruction stream, and a row in a section of its own that records where that
// label landed.
//
// So each marker below emits a LABEL and two words of data in another section.
// No instruction, no register, nothing on the live path — verified by
// disassembly, not by reasoning about it. The rows are self-relative
// (`.long 1b - .`) so they need no relocation and work in a PIE; storing an
// absolute address instead produced a DT_TEXTREL, which is how the shape of
// this was settled.
//
// Two tables, read together at panic time:
//
//	surge_fn_map    one row per function entry:      address -> function name
//	surge_line_map  one row per CHANGE of location:  address -> file:line:col
//
// The line map is a line table in the ordinary sense, and asking it "what is the
// last row at or before this address" answers for any address, not only a call
// site. That is what lets a panic raised INSIDE the C runtime still name a Surge
// location: the walk climbs to the innermost Surge frame and asks there.
//
// One ordering rule the lookup depends on: a marker is emitted at the TOP of the
// MIR instruction whose location it announces, so it precedes every machine
// instruction that instruction lowers to — including its call. A return address
// therefore always falls after its own row and before the next one.
const (
	traceFuncSection = "surge_fn_map"
	traceLineSection = "surge_line_map"
)

// traceStringIndex interns a string for the trace tables and returns its index
// in the table emitted by emitTraceStrings. The location table is reused rather
// than given a twin: a function name and a rendered location are both just
// interned bytes, and one table means one place for the count to come from.
func (e *Emitter) traceStringIndex(text string) (int, bool) {
	if e == nil || text == "" {
		return 0, false
	}
	sc := e.spanConst(text)
	for i, candidate := range e.spanConstOrder {
		if candidate == sc {
			return i, true
		}
	}
	return 0, false
}

// emitTraceRow writes the label and its row. The label is a numeric local
// (`1:` / `1b`), so it cannot collide with anything else in the function however
// many rows a body ends up carrying.
func (fe *funcEmitter) emitTraceRow(section string, index int) {
	fmt.Fprintf(&fe.emitter.buf,
		"  call void asm sideeffect \"1:\\0A\\09.pushsection %s,\\22a\\22\\0A\\09.long 1b - .\\0A\\09.long %d\\0A\\09.popsection\", \"~{memory}\"()\n",
		section, index)
}

// emitTraceFuncMarker records where a function begins and what it is called.
//
// The name is the one the program wrote, not the emitted symbol: functions reach
// the IR as `fn.<ID>`, while `mir.Func.Name` still holds `append_fmt_arg` or
// `inner$poll`. A backtrace that printed the symbol would be readable only by
// someone holding the same build.
func (fe *funcEmitter) emitTraceFuncMarker(name string) {
	if fe == nil || name == "" {
		return
	}
	index, ok := fe.emitter.traceStringIndex(name)
	if !ok {
		return
	}
	fe.emitTraceRow(traceFuncSection, index)
	// A function entry also RESETS the line map. Without it the last row of the
	// previous function would answer for the first instructions of this one,
	// which is the one wrong answer a sorted table gives for free.
	fe.lastTraceSpan = source.Span{}
	fe.lastTraceSpanSet = false
}

// emitTraceLineMarker records a change of source location.
//
// Only a CHANGE: a straight run of instructions from one statement shares a row,
// which is what keeps the table proportional to the source rather than to the
// generated code.
func (fe *funcEmitter) emitTraceLineMarker(span source.Span) {
	if fe == nil || fe.emitter == nil || fe.emitter.files == nil {
		return
	}
	if fe.lastTraceSpanSet && fe.lastTraceSpan == span {
		return
	}
	fe.lastTraceSpan = span
	fe.lastTraceSpanSet = true
	text := fe.emitter.spanText(span)
	if text == "" {
		return
	}
	index, ok := fe.emitter.traceStringIndex(text)
	if !ok {
		return
	}
	fe.emitTraceRow(traceLineSection, index)
}

// emitTraceStrings writes the table both maps index into.
//
// External linkage and a fixed name, because the runtime reads it by symbol.
// Emitted even when empty: a module with nothing to say still has to satisfy the
// runtime's declaration, and an absent symbol is a link error rather than an
// empty answer.
func (e *Emitter) emitTraceStrings() {
	e.buf.WriteString("%surge.trace.str = type { ptr, i64 }\n")
	count := len(e.spanConstOrder)
	if count == 0 {
		e.buf.WriteString("@surge_trace_strings = constant [0 x %surge.trace.str] zeroinitializer\n")
		e.buf.WriteString("@surge_trace_string_count = constant i64 0\n\n")
		return
	}
	fmt.Fprintf(&e.buf, "@surge_trace_strings = constant [%d x %%surge.trace.str] [", count)
	for i, sc := range e.spanConstOrder {
		if i > 0 {
			e.buf.WriteString(", ")
		}
		fmt.Fprintf(&e.buf,
			"%%surge.trace.str { ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d }",
			sc.arrayLen, sc.globalName, sc.dataLen)
	}
	e.buf.WriteString("]\n")
	fmt.Fprintf(&e.buf, "@surge_trace_string_count = constant i64 %d\n\n", count)
}

// traceBoundaryIndex is the row value that means "this is not a function the
// program wrote". The dispatchers below are runtime glue: real Surge code on
// both sides of them, but not a frame the VM has, because the VM's dispatcher
// is not a MIR frame either. Marking them stops the walk exactly where the VM's
// stack stops — at a task's poll entry — instead of printing the trampoline and
// then whatever ran before it on that worker.
const traceBoundaryIndex = 0xFFFFFFFF

// emitTraceBoundaryMarker marks an emitted function as glue rather than program.
func (e *Emitter) emitTraceBoundaryMarker() {
	fmt.Fprintf(&e.buf,
		"  call void asm sideeffect \"1:\\0A\\09.pushsection %s,\\22a\\22\\0A\\09.long 1b - .\\0A\\09.long %d\\0A\\09.popsection\", \"~{memory}\"()\n",
		traceFuncSection, traceBoundaryIndex)
}

// emitTraceTextEnd marks where this module's code stops.
//
// The function map records where each function BEGINS and nothing says where
// the last one ends, so an address past it — every address in the C runtime,
// which links after this object — would find the last Surge function and be
// reported as running inside it. Three frames of the runtime's own panic path
// appeared under a Surge name before this existed.
//
// An empty function emitted after all the others gives the bound its own
// address. External linkage so nothing drops it for being uncalled, and it is
// uncalled by construction: the runtime takes its address and never its body.
func (e *Emitter) emitTraceTextEnd() {
	e.buf.WriteString("define void @__surge_trace_text_end() {\nentry:\n  ret void\n}\n\n")
}
