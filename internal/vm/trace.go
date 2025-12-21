package vm

import (
	"fmt"
	"io"
	"unicode/utf8"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/vm/bignum"
)

// Tracer outputs execution traces for debugging.
type Tracer struct {
	w     io.Writer
	files *source.FileSet
	vm    *VM
}

// NewTracer creates a new tracer that writes to w.
func NewTracer(w io.Writer, files *source.FileSet) *Tracer {
	return &Tracer{w: w, files: files}
}

// NewFormatter creates a tracer configured for deterministic formatting only.
// The returned tracer does not write any output (its writer is nil).
func NewFormatter(vm *VM, files *source.FileSet) *Tracer {
	return &Tracer{files: files, vm: vm}
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
		fmt.Fprintf(t.w, "    write L%d(%s) = %s\n", w.LocalID, w.Name, t.formatValue(w.Value))
	}
}

func (t *Tracer) TraceHeapAlloc(kind ObjectKind, h Handle, obj *Object) {
	if t == nil || t.w == nil {
		return
	}
	switch kind {
	case OKString:
		fmt.Fprintf(t.w, "[heap] alloc string#%d\n", h)
	case OKArray:
		fmt.Fprintf(t.w, "[heap] alloc array#%d\n", h)
	case OKStruct:
		fmt.Fprintf(t.w, "[heap] alloc struct#%d\n", h)
	case OKTag:
		fmt.Fprintf(t.w, "[heap] alloc tag#%d\n", h)
	case OKRange:
		fmt.Fprintf(t.w, "[heap] alloc range#%d\n", h)
	default:
		fmt.Fprintf(t.w, "[heap] alloc handle#%d\n", h)
	}
}

func (t *Tracer) TraceHeapRetain(kind ObjectKind, h Handle, rc uint32) {
	if t == nil || t.w == nil {
		return
	}
	fmt.Fprintf(t.w, "[heap] retain %s#%d rc=%d\n", kindLabel(kind), h, rc)
}

func (t *Tracer) TraceHeapRelease(kind ObjectKind, h Handle, rc uint32) {
	if t == nil || t.w == nil {
		return
	}
	fmt.Fprintf(t.w, "[heap] release %s#%d rc=%d\n", kindLabel(kind), h, rc)
}

func (t *Tracer) TraceHeapFree(h Handle) {
	if t == nil || t.w == nil {
		return
	}
	fmt.Fprintf(t.w, "[heap] free handle#%d\n", h)
}

func kindLabel(kind ObjectKind) string {
	switch kind {
	case OKString:
		return "string"
	case OKArray:
		return "array"
	case OKStruct:
		return "struct"
	case OKTag:
		return "tag"
	case OKRange:
		return "range"
	case OKBigInt:
		return "bigint"
	case OKBigUint:
		return "biguint"
	case OKBigFloat:
		return "bigfloat"
	default:
		return "handle"
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

func (t *Tracer) TraceSwitchTagDecision(tagName string, target mir.BlockID) {
	if t == nil || t.w == nil {
		return
	}
	if tagName == "default" {
		fmt.Fprintf(t.w, "    switch_tag -> default bb%d\n", target)
		return
	}
	fmt.Fprintf(t.w, "    switch_tag -> case %s bb%d\n", tagName, target)
}

func (t *Tracer) TraceStore(loc Location, v Value) {
	if t == nil || t.w == nil {
		return
	}
	fmt.Fprintf(t.w, "    store %s = %s\n", t.formatLocation(loc), t.formatValue(v))
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
		return fmt.Sprintf("%s = %s", t.formatPlace(instr.Assign.Dst), t.formatRValue(&instr.Assign.Src))
	case mir.InstrCall:
		call := &instr.Call
		if call.HasDst {
			return fmt.Sprintf("L%d = call %s(%s)", call.Dst.Local, call.Callee.Name, t.formatArgs(call.Args))
		}
		return fmt.Sprintf("call %s(%s)", call.Callee.Name, t.formatArgs(call.Args))
	case mir.InstrDrop:
		return fmt.Sprintf("drop %s", t.formatPlace(instr.Drop.Place))
	case mir.InstrEndBorrow:
		return fmt.Sprintf("end_borrow %s", t.formatPlace(instr.EndBorrow.Place))
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
	case mir.RValueStructLit:
		out := fmt.Sprintf("struct_lit type#%d {", rv.StructLit.TypeID)
		for i := range rv.StructLit.Fields {
			f := &rv.StructLit.Fields[i]
			if i > 0 {
				out += ", "
			}
			out += fmt.Sprintf("%s=%s", f.Name, t.formatOperand(&f.Value))
		}
		out += "}"
		return out
	case mir.RValueArrayLit:
		out := "array_lit ["
		for i := range rv.ArrayLit.Elems {
			el := &rv.ArrayLit.Elems[i]
			if i > 0 {
				out += ", "
			}
			out += t.formatOperand(el)
		}
		out += "]"
		return out
	case mir.RValueField:
		if rv.Field.FieldIdx >= 0 {
			return fmt.Sprintf("%s.#%d", t.formatOperand(&rv.Field.Object), rv.Field.FieldIdx)
		}
		return fmt.Sprintf("%s.%s", t.formatOperand(&rv.Field.Object), rv.Field.FieldName)
	case mir.RValueTagTest:
		return fmt.Sprintf("tag_test %s is %s", t.formatOperand(&rv.TagTest.Value), rv.TagTest.TagName)
	case mir.RValueTagPayload:
		return fmt.Sprintf("tag_payload %s.%s[%d]", t.formatOperand(&rv.TagPayload.Value), rv.TagPayload.TagName, rv.TagPayload.Index)
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
		return fmt.Sprintf("copy %s", t.formatPlace(op.Place))
	case mir.OperandMove:
		return fmt.Sprintf("move %s", t.formatPlace(op.Place))
	case mir.OperandAddrOf:
		return fmt.Sprintf("addr_of %s", t.formatPlace(op.Place))
	case mir.OperandAddrOfMut:
		return fmt.Sprintf("addr_of_mut %s", t.formatPlace(op.Place))
	default:
		return fmt.Sprintf("<?op:%d>", op.Kind)
	}
}

func (t *Tracer) formatPlace(p mir.Place) string {
	if !p.IsValid() {
		return "L?"
	}
	out := fmt.Sprintf("L%d", p.Local)
	for _, proj := range p.Proj {
		switch proj.Kind {
		case mir.PlaceProjDeref:
			out = fmt.Sprintf("(*%s)", out)
		case mir.PlaceProjField:
			if proj.FieldIdx >= 0 {
				out += fmt.Sprintf(".#%d", proj.FieldIdx)
				continue
			}
			if proj.FieldName != "" {
				out += "." + proj.FieldName
			} else {
				out += ".<?>"
			}
		case mir.PlaceProjIndex:
			if proj.IndexLocal != mir.NoLocalID {
				out += fmt.Sprintf("[L%d]", proj.IndexLocal)
			} else {
				out += "[?]"
			}
		default:
			out += ".<?>"
		}
	}
	return out
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

func (t *Tracer) formatValue(v Value) string {
	switch v.Kind {
	case VKRef:
		return "&" + t.formatLocation(v.Loc)

	case VKRefMut:
		return "&mut " + t.formatLocation(v.Loc)

	case VKHandleString:
		if v.H == 0 {
			return "string#0(<invalid>)"
		}
		obj := t.lookup(v.H)
		if obj == nil {
			return fmt.Sprintf("string#%d(<invalid>)", v.H)
		}
		if obj.Freed || obj.RefCount == 0 {
			return fmt.Sprintf("string#%d(rc=0,<freed>)", v.H)
		}
		preview := truncateRunes(obj.Str, 32)
		return fmt.Sprintf("string#%d(rc=%d,%q)", v.H, obj.RefCount, preview)

	case VKHandleArray:
		if v.H == 0 {
			return "array#0(<invalid>)"
		}
		obj := t.lookup(v.H)
		if obj == nil {
			return fmt.Sprintf("array#%d(<invalid>)", v.H)
		}
		if obj.Freed || obj.RefCount == 0 {
			return fmt.Sprintf("array#%d(rc=0,<freed>)", v.H)
		}
		return fmt.Sprintf("array#%d(rc=%d,len=%d)", v.H, obj.RefCount, len(obj.Arr))

	case VKHandleStruct:
		if v.H == 0 {
			return "struct#0(<invalid>)"
		}
		obj := t.lookup(v.H)
		if obj == nil {
			return fmt.Sprintf("struct#%d(<invalid>)", v.H)
		}
		if obj.Freed || obj.RefCount == 0 {
			return fmt.Sprintf("struct#%d(rc=0,<freed>)", v.H)
		}
		return fmt.Sprintf("struct#%d(rc=%d,type=type#%d)", v.H, obj.RefCount, obj.TypeID)

	case VKHandleTag:
		if v.H == 0 {
			return "tag#0(<invalid>)"
		}
		obj := t.lookup(v.H)
		if obj == nil {
			return fmt.Sprintf("tag#%d(<invalid>)", v.H)
		}
		if obj.Freed || obj.RefCount == 0 {
			return fmt.Sprintf("tag#%d(rc=0,<freed>)", v.H)
		}
		tagName := "<unknown>"
		if obj.Kind == OKTag && t.vm != nil && t.vm.tagLayouts != nil {
			if layout, ok := t.vm.tagLayouts.Layout(t.vm.valueType(obj.TypeID)); ok && layout != nil {
				if tc, ok := layout.CaseBySym(obj.Tag.TagSym); ok && tc.TagName != "" {
					tagName = tc.TagName
				}
			}
			if tagName == "<unknown>" && obj.Tag.TagSym.IsValid() {
				if name, ok := t.vm.tagLayouts.AnyTagName(obj.Tag.TagSym); ok {
					tagName = name
				}
			}
		}
		return fmt.Sprintf("tag#%d(rc=%d,type#%d,%s)", v.H, obj.RefCount, obj.TypeID, tagName)

	case VKHandleRange:
		if v.H == 0 {
			return "range#0(<invalid>)"
		}
		obj := t.lookup(v.H)
		if obj == nil {
			return fmt.Sprintf("range#%d(<invalid>)", v.H)
		}
		if obj.Freed || obj.RefCount == 0 {
			return fmt.Sprintf("range#%d(rc=0,<freed>)", v.H)
		}
		return fmt.Sprintf("range#%d(rc=%d)", v.H, obj.RefCount)

	case VKBigInt:
		if v.H == 0 {
			return "0"
		}
		obj := t.lookup(v.H)
		if obj == nil || obj.Freed || obj.RefCount == 0 || obj.Kind != OKBigInt {
			return fmt.Sprintf("bigint#%d(<invalid>)", v.H)
		}
		return fmt.Sprintf("bigint#%d(rc=%d,%s)", v.H, obj.RefCount, bignum.FormatInt(obj.BigInt))

	case VKBigUint:
		if v.H == 0 {
			return "0"
		}
		obj := t.lookup(v.H)
		if obj == nil || obj.Freed || obj.RefCount == 0 || obj.Kind != OKBigUint {
			return fmt.Sprintf("biguint#%d(<invalid>)", v.H)
		}
		return fmt.Sprintf("biguint#%d(rc=%d,%s)", v.H, obj.RefCount, bignum.FormatUint(obj.BigUint))

	case VKBigFloat:
		if v.H == 0 {
			return "0"
		}
		obj := t.lookup(v.H)
		if obj == nil || obj.Freed || obj.RefCount == 0 || obj.Kind != OKBigFloat {
			return fmt.Sprintf("bigfloat#%d(<invalid>)", v.H)
		}
		s, err := bignum.FormatFloat(obj.BigFloat)
		if err != nil {
			return fmt.Sprintf("bigfloat#%d(<%v>)", v.H, err)
		}
		return fmt.Sprintf("bigfloat#%d(rc=%d,%s)", v.H, obj.RefCount, s)

	default:
		return v.String()
	}
}

func (t *Tracer) formatLocation(loc Location) string {
	switch loc.Kind {
	case LKLocal:
		name := "?"
		if t.vm != nil {
			stackIdx := int(loc.Frame)
			if loc.Frame >= 0 && stackIdx >= 0 && stackIdx < len(t.vm.Stack) {
				frame := &t.vm.Stack[stackIdx]
				localIdx := int(loc.Local)
				if loc.Local >= 0 && localIdx >= 0 && localIdx < len(frame.Locals) && frame.Locals[localIdx].Name != "" {
					name = frame.Locals[localIdx].Name
				}
			}
		}
		return fmt.Sprintf("L%d(%s)", loc.Local, name)
	case LKStructField:
		return fmt.Sprintf("struct#%d.field[%d]", loc.Handle, loc.Index)
	case LKArrayElem:
		return fmt.Sprintf("array#%d[%d]", loc.Handle, loc.Index)
	case LKStringBytes:
		return fmt.Sprintf("string#%d.bytes+%d", loc.Handle, loc.ByteOffset)
	default:
		return "<invalid-loc>"
	}
}

func (t *Tracer) lookup(h Handle) *Object {
	if t == nil || t.vm == nil || t.vm.Heap == nil {
		return nil
	}
	obj, _ := t.vm.Heap.lookup(h)
	return obj
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	out := make([]rune, 0, limit)
	for _, r := range s {
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return string(out)
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
