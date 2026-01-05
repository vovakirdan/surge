package llvm

import (
	"fmt"
	"sort"

	"surge/internal/mir"
)

func (e *Emitter) collectStringConsts() {
	if e.mod == nil {
		return
	}
	for _, f := range e.mod.Funcs {
		if f == nil {
			continue
		}
		for i := range f.Blocks {
			bb := &f.Blocks[i]
			for j := range bb.Instrs {
				ins := &bb.Instrs[j]
				switch ins.Kind {
				case mir.InstrAssign:
					e.collectRValue(&ins.Assign.Src)
				case mir.InstrCall:
					for k := range ins.Call.Args {
						e.collectOperand(&ins.Call.Args[k])
					}
				}
			}
			e.collectTerminator(&bb.Term)
		}
	}
}

func (e *Emitter) collectRValue(rv *mir.RValue) {
	if rv == nil {
		return
	}
	switch rv.Kind {
	case mir.RValueUse:
		e.collectOperand(&rv.Use)
	case mir.RValueStructLit:
		for i := range rv.StructLit.Fields {
			e.collectOperand(&rv.StructLit.Fields[i].Value)
		}
	case mir.RValueArrayLit:
		for i := range rv.ArrayLit.Elems {
			e.collectOperand(&rv.ArrayLit.Elems[i])
		}
	case mir.RValueTupleLit:
		for i := range rv.TupleLit.Elems {
			e.collectOperand(&rv.TupleLit.Elems[i])
		}
	case mir.RValueUnaryOp:
		e.collectOperand(&rv.Unary.Operand)
	case mir.RValueBinaryOp:
		e.collectOperand(&rv.Binary.Left)
		e.collectOperand(&rv.Binary.Right)
	case mir.RValueCast:
		e.collectOperand(&rv.Cast.Value)
	case mir.RValueField:
		e.collectOperand(&rv.Field.Object)
	case mir.RValueIndex:
		e.collectOperand(&rv.Index.Object)
		e.collectOperand(&rv.Index.Index)
	case mir.RValueTagTest:
		e.collectOperand(&rv.TagTest.Value)
	case mir.RValueTagPayload:
		e.collectOperand(&rv.TagPayload.Value)
	case mir.RValueIterInit:
		e.collectOperand(&rv.IterInit.Iterable)
	case mir.RValueIterNext:
		e.collectOperand(&rv.IterNext.Iter)
	case mir.RValueTypeTest:
		e.collectOperand(&rv.TypeTest.Value)
	case mir.RValueHeirTest:
		e.collectOperand(&rv.HeirTest.Value)
	}
}

func (e *Emitter) collectTerminator(term *mir.Terminator) {
	if term == nil {
		return
	}
	switch term.Kind {
	case mir.TermReturn:
		if term.Return.HasValue {
			e.collectOperand(&term.Return.Value)
		}
	case mir.TermIf:
		e.collectOperand(&term.If.Cond)
	case mir.TermSwitchTag:
		e.collectOperand(&term.SwitchTag.Value)
	case mir.TermAsyncYield:
		e.collectOperand(&term.AsyncYield.State)
	case mir.TermAsyncReturn:
		e.collectOperand(&term.AsyncReturn.State)
		if term.AsyncReturn.HasValue {
			e.collectOperand(&term.AsyncReturn.Value)
		}
	case mir.TermAsyncReturnCancelled:
		e.collectOperand(&term.AsyncReturnCancelled.State)
	}
}

func (e *Emitter) collectOperand(op *mir.Operand) {
	if op == nil {
		return
	}
	if op.Kind != mir.OperandConst {
		return
	}
	switch op.Const.Kind {
	case mir.ConstString:
		e.ensureStringConst(op.Const.StringValue)
	case mir.ConstInt:
		if op.Const.Text != "" {
			e.ensureStringConst(op.Const.Text)
		}
	case mir.ConstUint:
		if op.Const.Text != "" {
			e.ensureStringConst(op.Const.Text)
		}
	case mir.ConstFloat:
		if op.Const.Text != "" {
			e.ensureStringConst(op.Const.Text)
		}
	case mir.ConstFn:
		if e.mod == nil || !op.Const.Sym.IsValid() {
			return
		}
		if id, ok := e.mod.FuncBySym[op.Const.Sym]; ok {
			e.fnRefs[id] = struct{}{}
		}
	}
}

func (e *Emitter) ensureStringConst(raw string) {
	if e == nil {
		return
	}
	if _, ok := e.stringConsts[raw]; ok {
		return
	}
	bytes := decodeStringLiteral(raw)
	arrayLen := len(bytes)
	dataLen := len(bytes)
	if arrayLen == 0 {
		arrayLen = 1
	}
	e.stringConsts[raw] = &stringConst{
		raw:      raw,
		bytes:    bytes,
		dataLen:  dataLen,
		arrayLen: arrayLen,
	}
}

func (e *Emitter) emitStringConsts() {
	if len(e.stringConsts) == 0 {
		return
	}
	raws := make([]string, 0, len(e.stringConsts))
	for raw := range e.stringConsts {
		raws = append(raws, raw)
	}
	sort.Strings(raws)
	for idx, raw := range raws {
		sc := e.stringConsts[raw]
		name := fmt.Sprintf(".str.%d", idx)
		sc.globalName = name
		lit := formatLLVMBytes(sc.bytes, sc.arrayLen)
		fmt.Fprintf(&e.buf, "@%s = private unnamed_addr constant [%d x i8] %s\n", name, sc.arrayLen, lit)
	}
	e.buf.WriteString("\n")
}
