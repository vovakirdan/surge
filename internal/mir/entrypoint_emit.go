package mir

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *surgeStartBuilder) startBlock(id BlockID) {
	b.cur = id
}

func (b *surgeStartBuilder) registerParamLocal(param hir.Param, local LocalID) {
	if param.SymbolID.IsValid() {
		b.paramLocals[param.SymbolID] = local
	}
}

func (b *surgeStartBuilder) newBlock() BlockID {
	raw, err := safecast.Conv[int32](len(b.f.Blocks))
	if err != nil {
		panic(fmt.Errorf("mir: block id overflow: %w", err))
	}
	id := BlockID(raw)
	b.f.Blocks = append(b.f.Blocks, Block{ID: id, Term: Terminator{Kind: TermNone}})
	return id
}

func (b *surgeStartBuilder) curBlock() *Block {
	if int(b.cur) < 0 || int(b.cur) >= len(b.f.Blocks) {
		return nil
	}
	return &b.f.Blocks[b.cur]
}

func (b *surgeStartBuilder) newLocal(name string, ty types.TypeID, flags LocalFlags) LocalID {
	raw, err := safecast.Conv[int32](len(b.f.Locals))
	if err != nil {
		panic(fmt.Errorf("mir: local id overflow: %w", err))
	}
	id := LocalID(raw)
	b.f.Locals = append(b.f.Locals, Local{
		Sym:   symbols.NoSymbolID,
		Type:  ty,
		Flags: flags,
		Name:  name,
	})
	return id
}

func (b *surgeStartBuilder) emit(ins *Instr) {
	bb := b.curBlock()
	if bb == nil || bb.Terminated() {
		return
	}
	bb.Instrs = append(bb.Instrs, *ins)
}

func (b *surgeStartBuilder) setTerm(t *Terminator) {
	bb := b.curBlock()
	if bb == nil || bb.Terminated() {
		return
	}
	bb.Term = *t
}

func (b *surgeStartBuilder) emitAssign(dst LocalID, src *RValue) {
	b.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: dst},
			Src: *src,
		},
	})
}

func (b *surgeStartBuilder) emitCall(dst LocalID, sym symbols.SymbolID, name string, args []Operand) {
	hasDst := dst != NoLocalID
	b.emit(&Instr{
		Kind: InstrCall,
		Call: CallInstr{
			HasDst: hasDst,
			Dst:    Place{Local: dst},
			Callee: Callee{
				Kind: CalleeSym,
				Sym:  sym,
				Name: name,
			},
			Args: args,
		},
	})
}

func (b *surgeStartBuilder) emitCallIntrinsic(dst LocalID, name string, args []Operand) {
	hasDst := dst != NoLocalID
	b.emit(&Instr{
		Kind: InstrCall,
		Call: CallInstr{
			HasDst: hasDst,
			Dst:    Place{Local: dst},
			Callee: Callee{
				Kind: CalleeSym,
				Sym:  symbols.NoSymbolID, // intrinsics don't have symbols
				Name: name,
			},
			Args: args,
		},
	})
}

func (b *surgeStartBuilder) emitIndex(dst, arr LocalID, idx int) {
	b.emitAssign(dst, &RValue{
		Kind: RValueIndex,
		Index: IndexAccess{
			Object: Operand{Kind: OperandCopy, Place: Place{Local: arr}},
			Index: Operand{
				Kind: OperandConst,
				Type: b.intType(),
				Const: Const{
					Kind:     ConstInt,
					Type:     b.intType(),
					IntValue: int64(idx),
				},
			},
		},
	})
}

func (b *surgeStartBuilder) emitTagTest(dst, val LocalID, tag string) {
	b.emitAssign(dst, &RValue{
		Kind: RValueTagTest,
		TagTest: TagTest{
			Value:   Operand{Kind: OperandCopy, Place: Place{Local: val}},
			TagName: tag,
		},
	})
}

func (b *surgeStartBuilder) emitTagPayload(dst, val LocalID, tag string, index int) {
	b.emitAssign(dst, &RValue{
		Kind: RValueTagPayload,
		TagPayload: TagPayload{
			Value:   Operand{Kind: OperandCopy, Place: Place{Local: val}},
			TagName: tag,
			Index:   index,
		},
	})
}

func (b *surgeStartBuilder) emitFromStrCall(dst, strLocal LocalID, targetType types.TypeID) {
	arg := Operand{Kind: OperandAddrOf, Type: b.refType(b.stringType(), false), Place: Place{Local: strLocal}}
	if b.isBuiltinFromStrType(targetType) {
		b.emitCallIntrinsic(dst, "from_str", []Operand{arg})
		return
	}
	if sym := b.findFromStrMethod(targetType); sym.IsValid() {
		b.emitCall(dst, sym, "from_str", []Operand{arg})
		return
	}
	b.emitCallIntrinsic(dst, "from_str", []Operand{arg})
}

func (b *surgeStartBuilder) emitExitWithMessage(msg string, code uint64) {
	errType := b.errorType()
	if errType == types.NoTypeID {
		codeInt, err := safecast.Conv[int64](code)
		if err != nil {
			codeInt = 1
		}
		codeLocal := b.newLocal("exit_code", b.intType(), LocalFlagCopy)
		b.emitAssign(codeLocal, &RValue{
			Kind: RValueUse,
			Use: Operand{
				Kind: OperandConst,
				Type: b.intType(),
				Const: Const{
					Kind:     ConstInt,
					Type:     b.intType(),
					IntValue: codeInt,
				},
			},
		})
		b.emitExitCall(codeLocal)
		b.setTerm(&Terminator{Kind: TermReturn})
		return
	}

	errLocal := b.newLocal("entry_err", errType, LocalFlags(0))
	b.emitAssign(errLocal, &RValue{
		Kind: RValueStructLit,
		StructLit: StructLit{
			TypeID: errType,
			Fields: []StructLitField{
				{
					Name:  "message",
					Value: Operand{Kind: OperandConst, Type: b.stringType(), Const: Const{Kind: ConstString, Type: b.stringType(), StringValue: msg}},
				},
				{
					Name: "code",
					Value: Operand{Kind: OperandConst, Type: b.uintType(), Const: Const{
						Kind:      ConstUint,
						Type:      b.uintType(),
						UintValue: code,
					}},
				},
			},
		},
	})
	b.emitCallIntrinsic(NoLocalID, "exit", []Operand{{Kind: OperandMove, Place: Place{Local: errLocal}}})
	b.setTerm(&Terminator{Kind: TermReturn})
}

func (b *surgeStartBuilder) emitExitCall(codeLocal LocalID) {
	b.emit(&Instr{
		Kind: InstrCall,
		Call: CallInstr{
			HasDst: false,
			Callee: Callee{
				Kind: CalleeSym,
				Sym:  symbols.NoSymbolID,
				Name: "rt_exit",
			},
			Args: []Operand{
				{Kind: OperandCopy, Place: Place{Local: codeLocal}},
			},
		},
	})
}

func (b *surgeStartBuilder) lowerDefaultExpr(expr *hir.Expr) (Operand, error) {
	if expr == nil {
		return Operand{}, fmt.Errorf("nil default expression")
	}
	lowerer := &funcLowerer{
		f:                   b.f,
		types:               b.typesIn,
		symToLocal:          make(map[symbols.SymbolID]LocalID, len(b.paramLocals)),
		nextTemp:            1,
		scopeLocal:          NoLocalID,
		cur:                 b.cur,
		consts:              buildConstMap(b.mm.Source),
		staticStringGlobals: b.staticStringGlobals,
		staticStringInits:   b.staticStringInits,
	}
	for sym, local := range b.paramLocals {
		lowerer.symToLocal[sym] = local
	}
	op, err := lowerer.lowerExpr(expr, true)
	if err != nil {
		return Operand{}, err
	}
	b.cur = lowerer.cur
	return op, nil
}
