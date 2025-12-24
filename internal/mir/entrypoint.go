package mir

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// BuildSurgeStart creates the synthetic __surge_start function if an entrypoint exists.
// Returns nil if no entrypoint in module.
//
// The __surge_start function:
// 1. Prepares args based on entrypoint mode (none/argv/stdin)
// 2. Calls the user entrypoint function
// 3. Converts return value to exit code (0 for nothing, direct for int, __to for other)
// 4. Calls rt_exit(code)
func BuildSurgeStart(mm *mono.MonoModule, semaRes *sema.Result, typesIn *types.Interner, nextID FuncID) (*Func, error) {
	if mm == nil {
		return nil, nil
	}

	// Find entrypoint function
	entryMF := findEntrypoint(mm)
	if entryMF == nil {
		return nil, nil
	}

	// Get entrypoint mode from symbol
	mode := getEntrypointMode(entryMF, mm)

	// Build the synthetic function
	return buildSurgeStartFunc(entryMF, mode, typesIn, mm, nextID)
}

// findEntrypoint finds the function marked with @entrypoint.
func findEntrypoint(mm *mono.MonoModule) *mono.MonoFunc {
	if mm == nil {
		return nil
	}
	for _, mf := range mm.Funcs {
		if mf == nil || mf.Func == nil {
			continue
		}
		if mf.Func.Flags.HasFlag(hir.FuncEntrypoint) {
			return mf
		}
	}
	return nil
}

// getEntrypointMode retrieves the EntrypointMode from the symbol.
func getEntrypointMode(mf *mono.MonoFunc, mm *mono.MonoModule) symbols.EntrypointMode {
	if mf == nil {
		return symbols.EntrypointModeNone
	}

	// Use OrigSym (original symbol before monomorphization) to look up EntrypointMode
	symID := mf.OrigSym
	if !symID.IsValid() && mf.Func != nil {
		symID = mf.Func.SymbolID
	}
	if !symID.IsValid() {
		return symbols.EntrypointModeNone
	}

	if mm.Source == nil || mm.Source.Symbols == nil || mm.Source.Symbols.Table == nil {
		return symbols.EntrypointModeNone
	}

	sym := mm.Source.Symbols.Table.Symbols.Get(symID)
	if sym == nil {
		return symbols.EntrypointModeNone
	}

	return sym.EntrypointMode
}

// buildSurgeStartFunc creates the MIR function for __surge_start.
func buildSurgeStartFunc(entryMF *mono.MonoFunc, mode symbols.EntrypointMode, typesIn *types.Interner, mm *mono.MonoModule, nextID FuncID) (*Func, error) {
	b := &surgeStartBuilder{
		entryMF: entryMF,
		mode:    mode,
		typesIn: typesIn,
		mm:      mm,
		f: &Func{
			ID:     nextID,
			Sym:    symbols.NoSymbolID, // synthetic function
			Name:   "__surge_start",
			Result: types.NoTypeID, // returns nothing (via rt_exit)
		},
		paramLocals: make(map[symbols.SymbolID]LocalID),
	}

	if err := b.build(); err != nil {
		return nil, err
	}

	return b.f, nil
}

type surgeStartBuilder struct {
	entryMF *mono.MonoFunc
	mode    symbols.EntrypointMode
	typesIn *types.Interner
	mm      *mono.MonoModule // for __to method lookup via mm.Source.Symbols
	f       *Func

	// Current block being built
	cur BlockID

	paramLocals map[symbols.SymbolID]LocalID
}

func (b *surgeStartBuilder) build() error {
	// Entry block
	b.f.Entry = b.newBlock()
	b.cur = b.f.Entry

	// Determine entrypoint return type
	entryReturnType := b.entryMF.Func.Result
	hasReturn := entryReturnType != types.NoTypeID && !b.isNothingType(entryReturnType)

	// Prepare args based on mode
	var argOperands []Operand
	switch b.mode {
	case symbols.EntrypointModeNone:
		argOperands = b.prepareArgsNone()
	case symbols.EntrypointModeArgv:
		argOperands = b.prepareArgsArgv()
	case symbols.EntrypointModeStdin:
		argOperands = b.prepareArgsStdin()
	default:
		return fmt.Errorf("unsupported entrypoint mode: %v", b.mode)
	}

	// Call entrypoint
	retLocal := NoLocalID
	if hasReturn {
		retLocal = b.newLocal("entry_ret", entryReturnType, LocalFlags(0))
	}

	b.emitCall(retLocal, b.entryMF.Func.SymbolID, b.entryMF.Func.Name, argOperands)

	// Convert return to exit code
	codeLocal := b.newLocal("code", b.intType(), LocalFlagCopy)

	switch {
	case !hasReturn:
		// nothing -> code = 0
		b.emitAssign(codeLocal, &RValue{
			Kind: RValueUse,
			Use: Operand{
				Kind: OperandConst,
				Type: b.intType(),
				Const: Const{
					Kind:     ConstInt,
					Type:     b.intType(),
					IntValue: 0,
				},
			},
		})
	case b.isIntType(entryReturnType):
		// int -> code = copy retLocal
		b.emitAssign(codeLocal, &RValue{
			Kind: RValueUse,
			Use: Operand{
				Kind:  OperandCopy,
				Place: Place{Local: retLocal},
			},
		})
	default:
		// other -> code = call __to(ret, int)
		toSymID := b.findToMethod(entryReturnType, b.intType())
		if toSymID.IsValid() {
			// Emit: code = call __to(move entry_ret)
			b.emitCall(codeLocal, toSymID, "__to", []Operand{
				{Kind: OperandMove, Place: Place{Local: retLocal}},
			})
		} else {
			// Fallback: no __to found, use 0
			b.emitAssign(codeLocal, &RValue{
				Kind: RValueUse,
				Use: Operand{
					Kind: OperandConst,
					Type: b.intType(),
					Const: Const{
						Kind:     ConstInt,
						Type:     b.intType(),
						IntValue: 0,
					},
				},
			})
		}
	}

	// Call rt_exit(code)
	b.emitExitCall(codeLocal)

	// Terminate with return (never reached, but required)
	b.setTerm(&Terminator{Kind: TermReturn})

	return nil
}

func (b *surgeStartBuilder) prepareArgsNone() []Operand {
	params := b.entryMF.Func.Params
	if len(params) == 0 {
		return nil
	}
	args := make([]Operand, 0, len(params))
	for i, param := range params {
		argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))
		b.registerParamLocal(param, argLocal)
		if !param.HasDefault || param.Default == nil {
			b.emitExitWithMessage(fmt.Sprintf("missing default for parameter %q", param.Name), 1)
			break
		}
		op, err := b.lowerDefaultExpr(param.Default)
		if err != nil {
			b.emitExitWithMessage(fmt.Sprintf("failed to lower default for parameter %q", param.Name), 1)
			break
		}
		b.emitAssign(argLocal, &RValue{Kind: RValueUse, Use: op})
		args = append(args, Operand{Kind: OperandMove, Place: Place{Local: argLocal}})
		if i < len(params)-1 && b.curBlock().Terminated() {
			break
		}
	}
	return args
}

func (b *surgeStartBuilder) prepareArgsArgv() []Operand {
	params := b.entryMF.Func.Params
	if len(params) == 0 {
		return nil
	}

	// L_argv = call rt_argv()
	argvType := b.stringArrayType()
	argvLocal := b.newLocal("argv", argvType, LocalFlags(0))
	b.emitCallIntrinsic(argvLocal, "rt_argv", nil)

	argvLenLocal := b.newLocal("argv_len", b.uintType(), LocalFlagCopy)
	b.emitCallIntrinsic(argvLenLocal, "__len", []Operand{
		{Kind: OperandCopy, Place: Place{Local: argvLocal}},
	})

	args := make([]Operand, 0, len(params))
	for i, param := range params {
		argIdx, err := safecast.Conv[uint64](i)
		if err != nil {
			b.emitExitWithMessage("argv index overflow", 1)
			break
		}

		argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))
		b.registerParamLocal(param, argLocal)

		erringType := b.erringType(param.Type)
		if erringType == types.NoTypeID {
			b.emitExitWithMessage("missing Erring type for entrypoint parsing", 1)
			break
		}

		condLocal := b.newLocal(fmt.Sprintf("has_arg%d", i), b.boolType(), LocalFlagCopy)
		b.emitAssign(condLocal, &RValue{
			Kind: RValueBinaryOp,
			Binary: BinaryOp{
				Op: ast.ExprBinaryLess,
				Left: Operand{
					Kind: OperandConst,
					Type: b.uintType(),
					Const: Const{
						Kind:      ConstUint,
						Type:      b.uintType(),
						UintValue: argIdx,
					},
				},
				Right: Operand{Kind: OperandCopy, Place: Place{Local: argvLenLocal}},
			},
		})

		hasArgBB := b.newBlock()
		noArgBB := b.newBlock()
		nextBB := b.newBlock()
		b.setTerm(&Terminator{
			Kind: TermIf,
			If: IfTerm{
				Cond: Operand{Kind: OperandCopy, Place: Place{Local: condLocal}},
				Then: hasArgBB,
				Else: noArgBB,
			},
		})

		b.startBlock(hasArgBB)
		argStrLocal := b.newLocal(fmt.Sprintf("arg_str%d", i), b.stringType(), LocalFlags(0))
		b.emitIndex(argStrLocal, argvLocal, i)
		parseLocal := b.newLocal(fmt.Sprintf("arg_parsed%d", i), erringType, LocalFlags(0))
		b.emitFromStrCall(parseLocal, argStrLocal, param.Type)

		okLocal := b.newLocal(fmt.Sprintf("arg_ok%d", i), b.boolType(), LocalFlagCopy)
		b.emitTagTest(okLocal, parseLocal, "Success")

		okBB := b.newBlock()
		errBB := b.newBlock()
		b.setTerm(&Terminator{
			Kind: TermIf,
			If: IfTerm{
				Cond: Operand{Kind: OperandCopy, Place: Place{Local: okLocal}},
				Then: okBB,
				Else: errBB,
			},
		})

		b.startBlock(okBB)
		b.emitTagPayload(argLocal, parseLocal, "Success", 0)
		b.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: nextBB}})

		b.startBlock(errBB)
		b.emitCallIntrinsic(NoLocalID, "exit", []Operand{{Kind: OperandMove, Place: Place{Local: parseLocal}}})
		b.setTerm(&Terminator{Kind: TermReturn})

		b.startBlock(noArgBB)
		if param.HasDefault && param.Default != nil {
			op, err := b.lowerDefaultExpr(param.Default)
			if err != nil {
				b.emitExitWithMessage(fmt.Sprintf("failed to lower default for parameter %q", param.Name), 1)
			} else {
				b.emitAssign(argLocal, &RValue{Kind: RValueUse, Use: op})
			}
		} else {
			b.emitExitWithMessage(fmt.Sprintf("missing argv argument %q", param.Name), 1)
		}
		if !b.curBlock().Terminated() {
			b.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: nextBB}})
		}

		b.startBlock(nextBB)
		args = append(args, Operand{Kind: OperandMove, Place: Place{Local: argLocal}})
	}

	return args
}

func (b *surgeStartBuilder) prepareArgsStdin() []Operand {
	params := b.entryMF.Func.Params
	if len(params) == 0 {
		return nil
	}
	if len(params) > 1 {
		b.emitExitWithMessage("multiple stdin parameters are not supported yet", 7001)
		return nil
	}

	// L_stdin = call rt_stdin_read_all()
	stdinLocal := b.newLocal("stdin", b.stringType(), LocalFlags(0))
	b.emitCallIntrinsic(stdinLocal, "rt_stdin_read_all", nil)

	param := params[0]
	argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))
	b.registerParamLocal(param, argLocal)

	erringType := b.erringType(param.Type)
	if erringType == types.NoTypeID {
		b.emitExitWithMessage("missing Erring type for entrypoint parsing", 1)
		return nil
	}

	parseLocal := b.newLocal("stdin_parsed", erringType, LocalFlags(0))
	b.emitFromStrCall(parseLocal, stdinLocal, param.Type)

	okLocal := b.newLocal("stdin_ok", b.boolType(), LocalFlagCopy)
	b.emitTagTest(okLocal, parseLocal, "Success")

	okBB := b.newBlock()
	errBB := b.newBlock()
	nextBB := b.newBlock()
	b.setTerm(&Terminator{
		Kind: TermIf,
		If: IfTerm{
			Cond: Operand{Kind: OperandCopy, Place: Place{Local: okLocal}},
			Then: okBB,
			Else: errBB,
		},
	})

	b.startBlock(okBB)
	b.emitTagPayload(argLocal, parseLocal, "Success", 0)
	b.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: nextBB}})

	b.startBlock(errBB)
	b.emitCallIntrinsic(NoLocalID, "exit", []Operand{{Kind: OperandMove, Place: Place{Local: parseLocal}}})
	b.setTerm(&Terminator{Kind: TermReturn})

	b.startBlock(nextBB)
	return []Operand{{Kind: OperandMove, Place: Place{Local: argLocal}}}
}

// Helper methods

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
	arg := Operand{Kind: OperandMove, Place: Place{Local: strLocal}}
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

func (b *surgeStartBuilder) lowerDefaultExpr(expr *hir.Expr) (Operand, error) {
	if expr == nil {
		return Operand{}, fmt.Errorf("nil default expression")
	}
	lowerer := &funcLowerer{
		f:          b.f,
		types:      b.typesIn,
		symToLocal: make(map[symbols.SymbolID]LocalID, len(b.paramLocals)),
		nextTemp:   1,
		cur:        b.cur,
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

func (b *surgeStartBuilder) erringType(elemType types.TypeID) types.TypeID {
	if b.typesIn == nil || b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil || b.mm.Source.Symbols.Table == nil {
		return types.NoTypeID
	}
	stringTable := b.mm.Source.Symbols.Table.Strings
	if stringTable == nil {
		return types.NoTypeID
	}
	errType := b.errorType()
	if errType == types.NoTypeID {
		return types.NoTypeID
	}
	erringName := stringTable.Intern("Erring")
	if id, ok := b.typesIn.FindUnionInstance(erringName, []types.TypeID{elemType, errType}); ok {
		return id
	}
	return types.NoTypeID
}

func (b *surgeStartBuilder) errorType() types.TypeID {
	if b.typesIn == nil || b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil || b.mm.Source.Symbols.Table == nil {
		return types.NoTypeID
	}
	stringTable := b.mm.Source.Symbols.Table.Strings
	if stringTable == nil {
		return types.NoTypeID
	}
	errorName := stringTable.Intern("Error")
	if id, ok := b.typesIn.FindStructInstance(errorName, nil); ok {
		return id
	}
	return types.NoTypeID
}

func (b *surgeStartBuilder) isBuiltinFromStrType(typeID types.TypeID) bool {
	if b.typesIn == nil {
		return false
	}
	tt, ok := b.typesIn.Lookup(b.resolveAlias(typeID))
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool, types.KindString:
		return true
	default:
		return false
	}
}

func (b *surgeStartBuilder) resolveAlias(id types.TypeID) types.TypeID {
	if b.typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		seen++
		tt, ok := b.typesIn.Lookup(id)
		if !ok {
			return id
		}
		if tt.Kind != types.KindAlias {
			return id
		}
		target, ok := b.typesIn.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
	}
	return id
}

func (b *surgeStartBuilder) findFromStrMethod(typeID types.TypeID) symbols.SymbolID {
	if b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil || b.mm.Source.Symbols.Table == nil {
		return symbols.NoSymbolID
	}
	typeKey := b.typeKeyForType(typeID)
	if typeKey == "" {
		return symbols.NoSymbolID
	}
	table := b.mm.Source.Symbols.Table
	for i := range table.Symbols.Len() {
		raw, err := safecast.Conv[uint32](i + 1)
		if err != nil {
			continue
		}
		symID := symbols.SymbolID(raw)
		sym := table.Symbols.Get(symID)
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			continue
		}
		name, ok := table.Strings.Lookup(sym.Name)
		if !ok || name != "from_str" {
			continue
		}
		if sym.ReceiverKey != typeKey {
			continue
		}
		return symID
	}
	return symbols.NoSymbolID
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

func (b *surgeStartBuilder) localFlags(ty types.TypeID) LocalFlags {
	var out LocalFlags
	if b.isCopyType(ty) {
		out |= LocalFlagCopy
	}
	return out
}

func (b *surgeStartBuilder) isCopyType(ty types.TypeID) bool {
	if b.typesIn == nil || ty == types.NoTypeID {
		return false
	}
	return b.typesIn.IsCopy(ty)
}

func (b *surgeStartBuilder) isNothingType(ty types.TypeID) bool {
	if b.typesIn == nil || ty == types.NoTypeID {
		return false
	}
	tt, ok := b.typesIn.Lookup(ty)
	return ok && tt.Kind == types.KindNothing
}

func (b *surgeStartBuilder) isIntType(ty types.TypeID) bool {
	if b.typesIn == nil || ty == types.NoTypeID {
		return false
	}
	builtins := b.typesIn.Builtins()
	return ty == builtins.Int
}

func (b *surgeStartBuilder) intType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().Int
}

func (b *surgeStartBuilder) uintType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().Uint
}

func (b *surgeStartBuilder) boolType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().Bool
}

func (b *surgeStartBuilder) stringType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	return b.typesIn.Builtins().String
}

func (b *surgeStartBuilder) stringArrayType() types.TypeID {
	if b.typesIn == nil {
		return types.NoTypeID
	}
	// Dynamic array of strings (ArrayDynamicLength for slice/dynamic array)
	return b.typesIn.Intern(types.MakeArray(b.stringType(), types.ArrayDynamicLength))
}

// findToMethod looks up a __to method that converts srcType to targetType.
// Returns NoSymbolID if not found.
func (b *surgeStartBuilder) findToMethod(srcType, targetType types.TypeID) symbols.SymbolID {
	if b.mm == nil || b.mm.Source == nil || b.mm.Source.Symbols == nil {
		return symbols.NoSymbolID
	}

	table := b.mm.Source.Symbols.Table
	if table == nil || table.Symbols == nil {
		return symbols.NoSymbolID
	}

	// Get source type key for matching receiver
	srcTypeKey := b.typeKeyForType(srcType)
	if srcTypeKey == "" {
		return symbols.NoSymbolID
	}

	// Search for __to method with matching signature
	for i := range table.Symbols.Len() {
		raw, err := safecast.Conv[uint32](i + 1) // +1 because SymbolID 0 is NoSymbolID
		if err != nil {
			continue
		}
		symID := symbols.SymbolID(raw)
		sym := table.Symbols.Get(symID)
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			continue
		}

		// Check name is "__to"
		name, ok := table.Strings.Lookup(sym.Name)
		if !ok || name != "__to" {
			continue
		}

		// Check receiver matches source type
		if sym.ReceiverKey != srcTypeKey {
			continue
		}

		// Check signature: (self, target) -> target
		sig := sym.Signature
		if sig == nil || len(sig.Params) != 2 {
			continue
		}

		// Params[1] should be the target type, Result should equal target
		targetTypeKey := b.typeKeyForType(targetType)
		if sig.Params[1] == targetTypeKey && sig.Result == targetTypeKey {
			return symID
		}
	}

	return symbols.NoSymbolID
}

// typeKeyForType returns the TypeKey string for a given TypeID.
func (b *surgeStartBuilder) typeKeyForType(id types.TypeID) symbols.TypeKey {
	if b.typesIn == nil || id == types.NoTypeID {
		return ""
	}
	id = b.resolveAlias(id)
	if elem, length, ok := b.typesIn.ArrayFixedInfo(id); ok {
		inner := b.typeKeyForType(elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		return symbols.TypeKey("[" + string(inner) + "; " + fmt.Sprintf("%d", length) + "]")
	}
	if elem, ok := b.typesIn.ArrayInfo(id); ok {
		inner := b.typeKeyForType(elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		return symbols.TypeKey("[" + string(inner) + "]")
	}
	tt, ok := b.typesIn.Lookup(id)
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindInt:
		switch tt.Width {
		case types.Width8:
			return "int8"
		case types.Width16:
			return "int16"
		case types.Width32:
			return "int32"
		case types.Width64:
			return "int64"
		default:
			return "int"
		}
	case types.KindUint:
		switch tt.Width {
		case types.Width8:
			return "uint8"
		case types.Width16:
			return "uint16"
		case types.Width32:
			return "uint32"
		case types.Width64:
			return "uint64"
		default:
			return "uint"
		}
	case types.KindFloat:
		switch tt.Width {
		case types.Width16:
			return "float16"
		case types.Width32:
			return "float32"
		case types.Width64:
			return "float64"
		default:
			return "float"
		}
	case types.KindBool:
		return "bool"
	case types.KindString:
		return "string"
	case types.KindStruct:
		info, ok := b.typesIn.StructInfo(id)
		if ok && info != nil && b.mm != nil && b.mm.Source != nil && b.mm.Source.Symbols != nil && b.mm.Source.Symbols.Table != nil {
			if name, ok := b.mm.Source.Symbols.Table.Strings.Lookup(info.Name); ok && name != "" {
				return b.typeKeyWithArgs(name, info.TypeArgs)
			}
		}
	case types.KindAlias:
		if info, ok := b.typesIn.AliasInfo(id); ok && info != nil && b.mm != nil && b.mm.Source != nil && b.mm.Source.Symbols != nil && b.mm.Source.Symbols.Table != nil {
			if name, ok := b.mm.Source.Symbols.Table.Strings.Lookup(info.Name); ok && name != "" {
				return b.typeKeyWithArgs(name, info.TypeArgs)
			}
		}
	case types.KindUnion:
		if info, ok := b.typesIn.UnionInfo(id); ok && info != nil && b.mm != nil && b.mm.Source != nil && b.mm.Source.Symbols != nil && b.mm.Source.Symbols.Table != nil {
			if name, ok := b.mm.Source.Symbols.Table.Strings.Lookup(info.Name); ok && name != "" {
				return b.typeKeyWithArgs(name, info.TypeArgs)
			}
		}
	}
	return ""
}

func (b *surgeStartBuilder) typeKeyWithArgs(name string, args []types.TypeID) symbols.TypeKey {
	if len(args) == 0 {
		return symbols.TypeKey(name)
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if key := b.typeKeyForType(arg); key != "" {
			parts = append(parts, string(key))
		}
	}
	if len(parts) == 0 {
		return symbols.TypeKey(name)
	}
	return symbols.TypeKey(name + "<" + strings.Join(parts, ",") + ">")
}
