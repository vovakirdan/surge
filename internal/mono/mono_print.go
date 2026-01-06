package mono

import (
	"fmt"
	"io"
	"slices"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// MonoDumpOptions configures the monomorphized module dump.
type MonoDumpOptions struct {
	// If true, prints only function/type headers.
	HeadersOnly bool
}

// DumpMonoModule writes a text representation of a MonoModule to the provided writer.
func DumpMonoModule(w io.Writer, mm *MonoModule, opts MonoDumpOptions) error {
	if w == nil || mm == nil {
		return nil
	}
	var (
		syms    *symbols.Result
		strs    *source.Interner
		typesIn *types.Interner
	)
	if mm.Source != nil {
		syms = mm.Source.Symbols
		typesIn = mm.Source.TypeInterner
	}
	if syms != nil && syms.Table != nil {
		strs = syms.Table.Strings
	}

	funcs := make([]*MonoFunc, 0, len(mm.Funcs))
	for _, f := range mm.Funcs {
		if f != nil {
			funcs = append(funcs, f)
		}
	}
	slices.SortStableFunc(funcs, func(a, b *MonoFunc) int {
		if a.OrigSym != b.OrigSym {
			if a.OrigSym < b.OrigSym {
				return -1
			}
			return 1
		}
		return cmpArgsKey(a.Key.ArgsKey, b.Key.ArgsKey)
	})

	typesList := make([]*MonoType, 0, len(mm.Types))
	for _, t := range mm.Types {
		if t != nil {
			typesList = append(typesList, t)
		}
	}
	slices.SortStableFunc(typesList, func(a, b *MonoType) int {
		if a.OrigSym != b.OrigSym {
			if a.OrigSym < b.OrigSym {
				return -1
			}
			return 1
		}
		return cmpArgsKey(a.Key.ArgsKey, b.Key.ArgsKey)
	})

	fmt.Fprintf(w, "funcs=%d types=%d\n", len(funcs), len(typesList))

	p := hir.NewPrinter(w, typesIn)
	for _, mf := range funcs {
		if mf == nil {
			continue
		}
		if mf.Func != nil && !opts.HeadersOnly {
			if err := p.PrintFunc(mf.Func); err != nil {
				return err
			}
			continue
		}

		name := "sym#_"
		if mf.OrigSym.IsValid() {
			name = symbolName(syms, strs, mf.OrigSym) + formatTypeArgs(typesIn, strs, mf.TypeArgs)
		}
		fmt.Fprintf(w, "fn %s (sym=%d)\n", name, mf.InstanceSym)
	}

	if len(typesList) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\ntypes:\n")
	for _, mt := range typesList {
		if mt == nil {
			continue
		}
		name := "type#_"
		if mt.OrigSym.IsValid() {
			name = symbolName(syms, strs, mt.OrigSym) + formatTypeArgs(typesIn, strs, mt.TypeArgs)
		}
		fmt.Fprintf(w, "  type %s = type#%d\n", name, mt.TypeID)
	}
	return nil
}

func cmpArgsKey(a, b ArgsKey) int {
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}
