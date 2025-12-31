package mir

import (
	"fmt"
	"slices"

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
func BuildSurgeStart(mm *mono.MonoModule, semaRes *sema.Result, typesIn *types.Interner, nextID FuncID, globals []Global, symToGlobal map[symbols.SymbolID]GlobalID, staticStringGlobals map[string]GlobalID, staticStringInits map[GlobalID]string) (*Func, error) {
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
	return buildSurgeStartFunc(entryMF, mode, typesIn, mm, nextID, semaRes, globals, symToGlobal, staticStringGlobals, staticStringInits)
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
func buildSurgeStartFunc(entryMF *mono.MonoFunc, mode symbols.EntrypointMode, typesIn *types.Interner, mm *mono.MonoModule, nextID FuncID, semaRes *sema.Result, globals []Global, symToGlobal map[symbols.SymbolID]GlobalID, staticStringGlobals map[string]GlobalID, staticStringInits map[GlobalID]string) (*Func, error) {
	b := &surgeStartBuilder{
		entryMF:             entryMF,
		mode:                mode,
		typesIn:             typesIn,
		mm:                  mm,
		sema:                semaRes,
		globals:             globals,
		symToGlobal:         symToGlobal,
		staticStringGlobals: staticStringGlobals,
		staticStringInits:   staticStringInits,
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
	entryMF             *mono.MonoFunc
	mode                symbols.EntrypointMode
	typesIn             *types.Interner
	mm                  *mono.MonoModule // for __to method lookup via mm.Source.Symbols
	sema                *sema.Result
	globals             []Global
	symToGlobal         map[symbols.SymbolID]GlobalID
	staticStringGlobals map[string]GlobalID
	staticStringInits   map[GlobalID]string
	f                   *Func

	// Current block being built
	cur BlockID

	paramLocals map[symbols.SymbolID]LocalID
}

func (b *surgeStartBuilder) build() error {
	// Entry block
	b.f.Entry = b.newBlock()
	b.cur = b.f.Entry

	if err := b.emitGlobalInits(); err != nil {
		return err
	}

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

func (b *surgeStartBuilder) emitGlobalInits() error {
	if b == nil {
		return nil
	}

	var consts map[symbols.SymbolID]*hir.ConstDecl
	if b.mm != nil {
		consts = buildConstMap(b.mm.Source)
	}
	fl := &funcLowerer{
		out:                 &Module{Globals: b.globals},
		sema:                b.sema,
		types:               b.typesIn,
		f:                   b.f,
		cur:                 b.cur,
		symToLocal:          make(map[symbols.SymbolID]LocalID),
		symToGlobal:         b.symToGlobal,
		nextTemp:            1,
		scopeLocal:          NoLocalID,
		consts:              consts,
		staticStringGlobals: b.staticStringGlobals,
		staticStringInits:   b.staticStringInits,
	}

	if b.mm != nil && b.mm.Source != nil && len(b.mm.Source.Globals) != 0 {
		for i := range b.mm.Source.Globals {
			decl := &b.mm.Source.Globals[i]
			if !decl.SymbolID.IsValid() {
				if decl.Value != nil {
					if _, err := fl.lowerExpr(decl.Value, false); err != nil {
						return err
					}
				}
				continue
			}
			globalID, ok := b.symToGlobal[decl.SymbolID]
			if !ok {
				return fmt.Errorf("mir: global %q has no id", decl.Name)
			}
			if decl.Value == nil {
				continue
			}
			op, err := fl.lowerExpr(decl.Value, true)
			if err != nil {
				return err
			}
			if op.Type == types.NoTypeID && decl.Type != types.NoTypeID {
				op.Type = decl.Type
			}
			fl.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Kind: PlaceGlobal, Global: globalID},
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
		}
	}

	if len(b.staticStringInits) != 0 {
		ids := make([]GlobalID, 0, len(b.staticStringInits))
		for id := range b.staticStringInits {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			raw := b.staticStringInits[id]
			fl.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Kind: PlaceGlobal, Global: id},
					Src: RValue{Kind: RValueUse, Use: Operand{
						Kind: OperandConst,
						Type: fl.types.Builtins().String,
						Const: Const{
							Kind:        ConstString,
							Type:        fl.types.Builtins().String,
							StringValue: raw,
						},
					}},
				},
			})
		}
	}

	b.cur = fl.cur
	return nil
}
