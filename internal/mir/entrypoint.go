package mir

import (
	"fmt"

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
