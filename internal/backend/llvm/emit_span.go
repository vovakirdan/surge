package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/source"
)

// A native panic has no frame to interrogate: by the time the reporter runs,
// the only thing it knows is what the emitter handed it. So the location a
// panic prints has to be decided here, while the call is being written.
//
// The location is decided the same way the VM decides it (internal/vm/vm_util.go,
// setSpanForInstr): a MIR instruction carries no span of its own, so the span of
// the local the instruction WRITES stands in for it. An instruction that writes
// no local leaves the previous instruction's span in place — in the VM because
// the frame's span field is simply not reassigned, and here because the cursor
// below is not advanced. Mirroring that choice, rather than inventing a second
// one, is what makes the two backends name the same line for the same panic.
//
// The one place the mirror is inexact: the VM's span persists along the path
// actually taken at run time, while this cursor persists along the block order
// the emitter writes in. They differ only for an instruction that writes no
// local AND panics, where the inherited span may name a different statement of
// the same function. Every panic emitted from an assignment or from a call with
// a destination — which is all of the indexing and conversion checks — names its
// own statement exactly.
type spanCursor struct {
	f    *mir.Func
	span source.Span
}

// advance moves the cursor onto the instruction about to be emitted.
func (c *spanCursor) advance(ins *mir.Instr) {
	if c == nil {
		return
	}
	if span, ok := instrSpan(c.f, ins); ok {
		c.span = span
	}
}

// instrSpan reports the span an instruction takes from the local it writes, and
// whether it writes one at all. The kinds listed here are exactly the kinds the
// VM's setSpanForInstr lists; a kind missing from both leaves the span alone.
func instrSpan(f *mir.Func, ins *mir.Instr) (source.Span, bool) {
	if f == nil || ins == nil {
		return source.Span{}, false
	}
	local, ok := instrSpanLocal(ins)
	if !ok || int(local) < 0 || int(local) >= len(f.Locals) {
		return source.Span{}, false
	}
	return f.Locals[local].Span, true
}

func instrSpanLocal(ins *mir.Instr) (mir.LocalID, bool) {
	switch ins.Kind {
	case mir.InstrAssign:
		return ins.Assign.Dst.Local, true
	case mir.InstrCall:
		if !ins.Call.HasDst {
			return 0, false
		}
		return ins.Call.Dst.Local, true
	case mir.InstrAwait:
		return ins.Await.Dst.Local, true
	case mir.InstrSpawn:
		return ins.Spawn.Dst.Local, true
	case mir.InstrPoll:
		return ins.Poll.Dst.Local, true
	case mir.InstrSelect:
		return ins.Select.Dst.Local, true
	default:
		return 0, false
	}
}

// spanText renders a span into the text a panic prints after "at ". An emitter
// that was handed no file set cannot resolve a span into a path, and says so by
// returning nothing rather than guessing: the reporter then prints no location
// line at all.
func (e *Emitter) spanText(span source.Span) string {
	if e == nil || e.files == nil {
		return ""
	}
	return source.FormatSpan(span, e.files)
}

// spanConst interns one rendered location, so a file:line:col that two panics
// name is one global.
//
// Locations live in their own table rather than the module's string-constant
// table because they are discovered while functions are being written, not
// before. The string table hands out names by index over its sorted contents,
// which forces it to be complete before the first function body mentions one of
// its globals; a location table filled the same way would have to be seeded with
// every local's span in the module, and a measurement of one ordinary program
// found seven eighths of those never reached a panic at all. This table pays for
// the locations that do.
func (e *Emitter) spanConst(text string) *stringConst {
	return e.lateConst("span", text, &e.spanConstOrder)
}

// messageConst interns one panic sentence built while a function was being
// emitted, which is what a per-type message is: the allocation guards report the
// type whose storage was refused, and no pass before the bodies knows which
// types those will be.
//
// It is deliberately NOT spanConst. spanConstOrder is also the trace string
// table — emitTraceStrings writes it out and the line/function maps index into
// it by position — so a message appended there would occupy a row the backtrace
// walker can reach and no row names.
func (e *Emitter) messageConst(text string) *stringConst {
	return e.lateConst("allocmsg", text, &e.messageConstOrder)
}

// lateConst interns one byte constant discovered while functions are being
// emitted, under a `kind` naming what it is and into the order its own table is
// written from. The interning map is shared and the key carries the kind, so a
// message whose text happened to read like a location cannot borrow its global.
func (e *Emitter) lateConst(kind, text string, order *[]*stringConst) *stringConst {
	key := kind + "\x00" + text
	if sc, ok := e.spanConsts[key]; ok {
		return sc
	}
	bytes := []byte(text)
	arrayLen := len(bytes)
	if arrayLen == 0 {
		arrayLen = 1
	}
	sc := &stringConst{
		raw:        text,
		bytes:      bytes,
		dataLen:    len(bytes),
		arrayLen:   arrayLen,
		globalName: fmt.Sprintf(".%s.%d", kind, len(*order)),
	}
	e.spanConsts[key] = sc
	*order = append(*order, sc)
	return sc
}

// emitSpanConsts writes the late tables. They run after the functions that
// reference them, which is what lets them be filled lazily: a global may be
// defined anywhere in a module, and a function body that mentions one earlier in
// the file is an ordinary forward reference.
func (e *Emitter) emitSpanConsts() {
	if len(e.spanConstOrder) == 0 && len(e.messageConstOrder) == 0 {
		return
	}
	// Written in the order the functions asked for them, which is the order the
	// functions themselves are emitted in — the same order for the same module,
	// so two emissions of one module produce the same text.
	for _, sc := range e.spanConstOrder {
		lit := formatLLVMBytes(sc.bytes, sc.arrayLen)
		fmt.Fprintf(&e.buf, "@%s = private unnamed_addr constant [%d x i8] %s\n", sc.globalName, sc.arrayLen, lit)
	}
	for _, sc := range e.messageConstOrder {
		lit := formatLLVMBytes(sc.bytes, sc.arrayLen)
		fmt.Fprintf(&e.buf, "@%s = private unnamed_addr constant [%d x i8] %s\n", sc.globalName, sc.arrayLen, lit)
	}
	e.buf.WriteString("\n")
}

// panicSpanArgs renders where the panic being written happened, formatted as the
// trailing arguments of the reporter call. A null pointer is how "no location"
// reaches the runtime, and the runtime prints nothing extra for it — which is
// also what the hand-written runtime call sites, which have no emitter-side span
// at all, pass.
//
// The pointer is a constant expression rather than an instruction, so the cold
// block a panic sits in gains no work at all.
func (fe *funcEmitter) panicSpanArgs() string {
	text := fe.emitter.spanText(fe.span.span)
	if text == "" {
		return "ptr null, i64 0"
	}
	sc := fe.emitter.spanConst(text)
	return fmt.Sprintf(
		"ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d",
		sc.arrayLen, sc.globalName, sc.dataLen,
	)
}
