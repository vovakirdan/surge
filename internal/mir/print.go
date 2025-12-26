package mir

import (
	"fmt"
	"io"
	"slices"

	"surge/internal/types"
)

type DumpOptions struct{}

func DumpModule(w io.Writer, m *Module, typesIn *types.Interner, _ DumpOptions) error {
	if w == nil || m == nil {
		return nil
	}

	funcs := make([]*Func, 0, len(m.Funcs))
	for _, f := range m.Funcs {
		if f != nil {
			funcs = append(funcs, f)
		}
	}
	slices.SortStableFunc(funcs, func(a, b *Func) int {
		if a.Name != b.Name {
			if a.Name < b.Name {
				return -1
			}
			return 1
		}
		if a.Sym != b.Sym {
			if a.Sym < b.Sym {
				return -1
			}
			return 1
		}
		return 0
	})

	fmt.Fprintf(w, "funcs=%d\n", len(funcs))
	for _, f := range funcs {
		if err := dumpFunc(w, f, typesIn); err != nil {
			return err
		}
	}
	return nil
}

func dumpFunc(w io.Writer, f *Func, typesIn *types.Interner) error {
	if w == nil || f == nil {
		return nil
	}
	fmt.Fprintf(w, "\nfn %s:\n", f.Name)

	fmt.Fprintf(w, "  locals:\n")
	for i := range f.Locals {
		l := f.Locals[i]
		flags := formatLocalFlags(l.Flags)
		name := l.Name
		if name == "" {
			name = "_"
		}
		if flags != "" {
			fmt.Fprintf(w, "    L%d: %s %s name=%s\n", i, typeStr(typesIn, l.Type), flags, name)
		} else {
			fmt.Fprintf(w, "    L%d: %s name=%s\n", i, typeStr(typesIn, l.Type), name)
		}
	}

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		fmt.Fprintf(w, "  bb%d:\n", bb.ID)
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			fmt.Fprintf(w, "    %s\n", formatInstr(typesIn, ins))
		}
		fmt.Fprintf(w, "    %s\n", formatTerm(&bb.Term))
	}

	return nil
}

func formatLocalFlags(f LocalFlags) string {
	if f == 0 {
		return ""
	}
	var parts []string
	if f&LocalFlagCopy != 0 {
		parts = append(parts, "copy")
	}
	if f&LocalFlagOwn != 0 {
		parts = append(parts, "own")
	}
	if f&LocalFlagRef != 0 {
		parts = append(parts, "ref")
	}
	if f&LocalFlagRefMut != 0 {
		parts = append(parts, "refmut")
	}
	if f&LocalFlagPtr != 0 {
		parts = append(parts, "ptr")
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + join(parts, ",") + "]"
}

func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += sep + parts[i]
	}
	return out
}

func formatInstr(typesIn *types.Interner, ins *Instr) string {
	if ins == nil {
		return "<instr?>"
	}
	switch ins.Kind {
	case InstrAssign:
		return fmt.Sprintf("%s = %s", formatPlace(ins.Assign.Dst), formatRValue(typesIn, &ins.Assign.Src))
	case InstrCall:
		dst := ""
		if ins.Call.HasDst {
			dst = formatPlace(ins.Call.Dst) + " = "
		}
		return fmt.Sprintf("%scall %s(%s)", dst, formatCallee(&ins.Call.Callee), formatOperands(ins.Call.Args))
	case InstrDrop:
		return fmt.Sprintf("drop %s", formatPlace(ins.Drop.Place))
	case InstrEndBorrow:
		return fmt.Sprintf("end_borrow %s", formatPlace(ins.EndBorrow.Place))
	case InstrAwait:
		return fmt.Sprintf("%s = await %s", formatPlace(ins.Await.Dst), formatOperand(&ins.Await.Task))
	case InstrSpawn:
		return fmt.Sprintf("%s = spawn %s", formatPlace(ins.Spawn.Dst), formatOperand(&ins.Spawn.Value))
	case InstrNop:
		return "nop"
	default:
		return "<instr?>"
	}
}

func formatTerm(term *Terminator) string {
	if term == nil {
		return "unreachable"
	}
	switch term.Kind {
	case TermNone:
		return "unreachable"
	case TermReturn:
		if !term.Return.HasValue {
			return "return"
		}
		return fmt.Sprintf("return %s", formatOperand(&term.Return.Value))
	case TermGoto:
		return fmt.Sprintf("goto bb%d", term.Goto.Target)
	case TermIf:
		return fmt.Sprintf("if %s then bb%d else bb%d", formatOperand(&term.If.Cond), term.If.Then, term.If.Else)
	case TermSwitchTag:
		out := fmt.Sprintf("switch_tag %s {", formatOperand(&term.SwitchTag.Value))
		for _, c := range term.SwitchTag.Cases {
			out += fmt.Sprintf(" %s -> bb%d;", c.TagName, c.Target)
		}
		out += fmt.Sprintf(" default -> bb%d; }", term.SwitchTag.Default)
		return out
	case TermUnreachable:
		return "unreachable"
	default:
		return "<term?>"
	}
}

func formatPlace(p Place) string {
	if !p.IsValid() {
		return "L?"
	}
	out := fmt.Sprintf("L%d", p.Local)
	for _, proj := range p.Proj {
		switch proj.Kind {
		case PlaceProjDeref:
			out = fmt.Sprintf("(*%s)", out)
		case PlaceProjField:
			if proj.FieldIdx >= 0 {
				out += fmt.Sprintf(".#%d", proj.FieldIdx)
				continue
			}
			if proj.FieldName != "" {
				out += "." + proj.FieldName
			} else {
				out += ".<?>"
			}
		case PlaceProjIndex:
			if proj.IndexLocal != NoLocalID {
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

func formatOperands(ops []Operand) string {
	if len(ops) == 0 {
		return ""
	}
	out := formatOperand(&ops[0])
	for i := 1; i < len(ops); i++ {
		out += ", " + formatOperand(&ops[i])
	}
	return out
}

func formatOperand(op *Operand) string {
	if op == nil {
		return "<op?>"
	}
	switch op.Kind {
	case OperandConst:
		return formatConst(&op.Const)
	case OperandCopy:
		return fmt.Sprintf("copy %s", formatPlace(op.Place))
	case OperandMove:
		return fmt.Sprintf("move %s", formatPlace(op.Place))
	case OperandAddrOf:
		return fmt.Sprintf("addr_of %s", formatPlace(op.Place))
	case OperandAddrOfMut:
		return fmt.Sprintf("addr_of_mut %s", formatPlace(op.Place))
	default:
		return "<op?>"
	}
}

func formatConst(c *Const) string {
	if c == nil {
		return "const ?"
	}
	switch c.Kind {
	case ConstInt:
		return fmt.Sprintf("const %d", c.IntValue)
	case ConstUint:
		return fmt.Sprintf("const %d:uint", c.UintValue)
	case ConstFloat:
		return fmt.Sprintf("const %g", c.FloatValue)
	case ConstBool:
		if c.BoolValue {
			return "const true"
		}
		return "const false"
	case ConstString:
		return fmt.Sprintf("const %q", c.StringValue)
	case ConstNothing:
		return "const nothing"
	default:
		return "const ?"
	}
}

func formatCallee(c *Callee) string {
	if c == nil {
		return "<callee?>"
	}
	switch c.Kind {
	case CalleeSym:
		if c.Name != "" {
			return c.Name
		}
		return fmt.Sprintf("sym#%d", c.Sym)
	case CalleeValue:
		return formatOperand(&c.Value)
	default:
		return "<callee?>"
	}
}

func formatRValue(typesIn *types.Interner, rv *RValue) string {
	if rv == nil {
		return "<rvalue?>"
	}
	switch rv.Kind {
	case RValueUse:
		return formatOperand(&rv.Use)
	case RValueUnaryOp:
		return fmt.Sprintf("(%v %s)", rv.Unary.Op, formatOperand(&rv.Unary.Operand))
	case RValueBinaryOp:
		return fmt.Sprintf("(%s %v %s)", formatOperand(&rv.Binary.Left), rv.Binary.Op, formatOperand(&rv.Binary.Right))
	case RValueCast:
		return fmt.Sprintf("cast %s to %s", formatOperand(&rv.Cast.Value), typeStr(typesIn, rv.Cast.TargetTy))
	case RValueStructLit:
		out := fmt.Sprintf("struct_lit %s {", typeStr(typesIn, rv.StructLit.TypeID))
		for i := range rv.StructLit.Fields {
			if i > 0 {
				out += ", "
			}
			f := &rv.StructLit.Fields[i]
			out += fmt.Sprintf("%s=%s", f.Name, formatOperand(&f.Value))
		}
		out += "}"
		return out
	case RValueArrayLit:
		out := "array_lit ["
		for i := range rv.ArrayLit.Elems {
			if i > 0 {
				out += ", "
			}
			out += formatOperand(&rv.ArrayLit.Elems[i])
		}
		out += "]"
		return out
	case RValueTupleLit:
		out := "tuple_lit ("
		for i := range rv.TupleLit.Elems {
			if i > 0 {
				out += ", "
			}
			out += formatOperand(&rv.TupleLit.Elems[i])
		}
		out += ")"
		return out
	case RValueField:
		if rv.Field.FieldName != "" {
			return fmt.Sprintf("field %s.%s", formatOperand(&rv.Field.Object), rv.Field.FieldName)
		}
		return fmt.Sprintf("field %s.%d", formatOperand(&rv.Field.Object), rv.Field.FieldIdx)
	case RValueIndex:
		return fmt.Sprintf("index %s[%s]", formatOperand(&rv.Index.Object), formatOperand(&rv.Index.Index))
	case RValueTagTest:
		return fmt.Sprintf("tag_test %s is %s", formatOperand(&rv.TagTest.Value), rv.TagTest.TagName)
	case RValueTagPayload:
		return fmt.Sprintf("tag_payload %s.%s[%d]", formatOperand(&rv.TagPayload.Value), rv.TagPayload.TagName, rv.TagPayload.Index)
	case RValueIterInit:
		return fmt.Sprintf("iter_init %s", formatOperand(&rv.IterInit.Iterable))
	case RValueIterNext:
		return fmt.Sprintf("iter_next %s", formatOperand(&rv.IterNext.Iter))
	case RValueTypeTest:
		return fmt.Sprintf("type_test %s is %s", formatOperand(&rv.TypeTest.Value), typeStr(typesIn, rv.TypeTest.TargetTy))
	case RValueHeirTest:
		return fmt.Sprintf("heir_test %s heir %s", typeStr(typesIn, rv.HeirTest.LeftTy), typeStr(typesIn, rv.HeirTest.RightTy))
	default:
		return "<rvalue?>"
	}
}

func typeStr(typesIn *types.Interner, id types.TypeID) string {
	if id == types.NoTypeID {
		return "?"
	}
	if typesIn == nil {
		return fmt.Sprintf("type#%d", id)
	}
	t, ok := typesIn.Lookup(id)
	if !ok {
		return fmt.Sprintf("type#%d", id)
	}
	switch t.Kind {
	case types.KindUnit:
		return "()"
	case types.KindNothing:
		return "nothing"
	case types.KindBool:
		return "bool"
	case types.KindString:
		return "string"
	case types.KindInt:
		return formatIntType(t.Width, true)
	case types.KindUint:
		return formatIntType(t.Width, false)
	case types.KindFloat:
		return formatFloatType(t.Width)
	case types.KindPointer:
		return fmt.Sprintf("*%s", typeStr(typesIn, t.Elem))
	case types.KindReference:
		if t.Mutable {
			return fmt.Sprintf("&mut %s", typeStr(typesIn, t.Elem))
		}
		return fmt.Sprintf("&%s", typeStr(typesIn, t.Elem))
	case types.KindOwn:
		return fmt.Sprintf("own %s", typeStr(typesIn, t.Elem))
	case types.KindArray:
		if t.Count == types.ArrayDynamicLength {
			return fmt.Sprintf("[%s]", typeStr(typesIn, t.Elem))
		}
		return fmt.Sprintf("[%s; %d]", typeStr(typesIn, t.Elem), t.Count)
	default:
		return fmt.Sprintf("type#%d", id)
	}
}

func formatIntType(width types.Width, signed bool) string {
	prefix := "int"
	if !signed {
		prefix = "uint"
	}
	switch width {
	case types.WidthAny:
		return prefix
	case types.Width8:
		return prefix + "8"
	case types.Width16:
		return prefix + "16"
	case types.Width32:
		return prefix + "32"
	case types.Width64:
		return prefix + "64"
	default:
		return prefix
	}
}

func formatFloatType(width types.Width) string {
	switch width {
	case types.WidthAny:
		return "float"
	case types.Width16:
		return "float16"
	case types.Width32:
		return "float32"
	case types.Width64:
		return "float64"
	default:
		return "float"
	}
}
