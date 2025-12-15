package mir

import (
	"fmt"

	"fortio.org/safecast"

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
	return buildSurgeStartFunc(entryMF, mode, typesIn, nextID)
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
func buildSurgeStartFunc(entryMF *mono.MonoFunc, mode symbols.EntrypointMode, typesIn *types.Interner, nextID FuncID) (*Func, error) {
	b := &surgeStartBuilder{
		entryMF: entryMF,
		mode:    mode,
		typesIn: typesIn,
		f: &Func{
			ID:     nextID,
			Sym:    symbols.NoSymbolID, // synthetic function
			Name:   "__surge_start",
			Result: types.NoTypeID, // returns nothing (via rt_exit)
		},
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
	f       *Func

	// Current block being built
	cur BlockID
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
				Const: Const{
					Kind:     ConstInt,
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
		// TODO: implement proper __to call lookup, for now use 0
		b.emitAssign(codeLocal, &RValue{
			Kind: RValueUse,
			Use: Operand{
				Kind: OperandConst,
				Const: Const{
					Kind:     ConstInt,
					IntValue: 0,
				},
			},
		})
	}

	// Call rt_exit(code)
	b.emitExitCall(codeLocal)

	// Terminate with return (never reached, but required)
	b.setTerm(&Terminator{Kind: TermReturn})

	return nil
}

func (b *surgeStartBuilder) prepareArgsNone() []Operand {
	// For mode none, all params must have defaults (checked by sema)
	// We don't pass any args - the runtime will use defaults
	// Actually, for now just return empty args
	return nil
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

	// For each param, extract from argv and parse
	args := make([]Operand, 0, len(params))
	for i, param := range params {
		if param.HasDefault {
			// Skip params with defaults for now
			// TODO: check if argv has enough elements
			continue
		}

		// Extract argument string from argv at index i
		argStrLocal := b.newLocal(fmt.Sprintf("arg_str%d", i), b.stringType(), LocalFlags(0))
		b.emitIndex(argStrLocal, argvLocal, i)

		// Parse argument value from string
		// TODO: implement proper T.from_str call
		// For now, emit a placeholder call to rt_parse_arg intrinsic
		argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))
		b.emitCallIntrinsic(argLocal, "rt_parse_arg", []Operand{
			{Kind: OperandCopy, Place: Place{Local: argStrLocal}},
		})

		args = append(args, Operand{Kind: OperandMove, Place: Place{Local: argLocal}})
	}

	return args
}

func (b *surgeStartBuilder) prepareArgsStdin() []Operand {
	params := b.entryMF.Func.Params
	if len(params) == 0 {
		return nil
	}

	// L_stdin = call rt_stdin_read_all()
	stdinLocal := b.newLocal("stdin", b.stringType(), LocalFlags(0))
	b.emitCallIntrinsic(stdinLocal, "rt_stdin_read_all", nil)

	// For stdin mode, parse each param from stdin
	// TODO: implement proper stdin parsing (lines, JSON, etc.)
	args := make([]Operand, 0, len(params))
	for i, param := range params {
		if param.HasDefault {
			continue
		}

		argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))

		if param.Type == b.stringType() {
			// For string, just copy stdin content
			b.emitAssign(argLocal, &RValue{
				Kind: RValueUse,
				Use:  Operand{Kind: OperandCopy, Place: Place{Local: stdinLocal}},
			})
		} else {
			// Parse from stdin using placeholder intrinsic
			// TODO: implement proper T.from_str call
			b.emitCallIntrinsic(argLocal, "rt_parse_arg", []Operand{
				{Kind: OperandCopy, Place: Place{Local: stdinLocal}},
			})
		}

		args = append(args, Operand{Kind: OperandMove, Place: Place{Local: argLocal}})
		// Only use stdin for first param for now
		// TODO: properly split stdin for multiple params
		_ = i
	}

	return args
}

// Helper methods

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
				Const: Const{
					Kind:     ConstInt,
					IntValue: int64(idx),
				},
			},
		},
	})
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
	// Dynamic array of strings (count=0 means dynamic)
	return b.typesIn.Intern(types.MakeArray(b.stringType(), 0))
}
