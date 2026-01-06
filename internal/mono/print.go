package mono

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// DumpOptions configures the instantiation map dump.
type DumpOptions struct {
	// PathMode matches source.File.FormatPath modes: "relative", "absolute", "basename", "auto".
	PathMode string
}

// Dump writes a text representation of the instantiation map to the provided writer.
func Dump(w io.Writer, m *InstantiationMap, fs *source.FileSet, syms *symbols.Result, strs *source.Interner, typesIn *types.Interner, opts DumpOptions) error {
	if w == nil || m == nil || len(m.Entries) == 0 {
		return nil
	}
	if opts.PathMode == "" {
		opts.PathMode = "relative"
	}

	entries := make([]*InstEntry, 0, len(m.Entries))
	for _, e := range m.Entries {
		if e != nil && len(e.UseSites) > 0 {
			entries = append(entries, e)
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Key.Sym != b.Key.Sym {
			return a.Key.Sym < b.Key.Sym
		}
		return typeArgsLess(a.TypeArgs, b.TypeArgs)
	})

	for _, e := range entries {
		if e == nil {
			continue
		}
		kindLabel := "fn"
		switch e.Kind {
		case InstType:
			kindLabel = "type"
		case InstTag:
			kindLabel = "tag"
		}

		_, printErr := fmt.Fprintf(w, "%s %s%s  uses=%d\n", kindLabel, symbolName(syms, strs, e.Key.Sym), formatTypeArgs(typesIn, strs, e.TypeArgs), len(e.UseSites))
		if printErr != nil {
			return printErr
		}

		useSites := slicesClone(e.UseSites)
		sort.SliceStable(useSites, func(i, j int) bool {
			ai, aj := useSites[i], useSites[j]
			if si, sj := formatSpanForSort(ai.Span), formatSpanForSort(aj.Span); si != sj {
				return si < sj
			}
			if ai.Caller != aj.Caller {
				return ai.Caller < aj.Caller
			}
			return ai.Note < aj.Note
		})
		for _, us := range useSites {
			at := formatSpan(fs, us.Span, opts.PathMode)
			caller := symbolName(syms, strs, us.Caller)
			if us.Caller == symbols.NoSymbolID {
				caller = "_"
			}
			note := us.Note
			if note == "" {
				note = "_"
			}
			_, printErr = fmt.Fprintf(w, "  - at %s caller=%s note=%s\n", at, caller, note)
			if printErr != nil {
				return printErr
			}
		}
	}
	return nil
}

func formatSpanForSort(sp source.Span) string {
	if sp == (source.Span{}) {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d", sp.File, sp.Start, sp.End)
}

func formatSpan(fs *source.FileSet, sp source.Span, pathMode string) string {
	if fs == nil || sp == (source.Span{}) {
		return "_:0:0"
	}
	file := fs.Get(sp.File)
	start, _ := fs.Resolve(sp)
	baseDir := fs.BaseDir()
	path := "_"
	if file != nil {
		path = filepath.ToSlash(file.FormatPath(pathMode, baseDir))
	}
	return fmt.Sprintf("%s:%d:%d", path, start.Line, start.Col)
}

func typeArgsLess(a, b []types.TypeID) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func slicesClone[T any](in []T) []T {
	if len(in) == 0 {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}

func symbolName(syms *symbols.Result, strs *source.Interner, id symbols.SymbolID) string {
	if !id.IsValid() {
		return "_"
	}
	if syms == nil || syms.Table == nil || syms.Table.Symbols == nil {
		return fmt.Sprintf("sym#%d", id)
	}
	sym := syms.Table.Symbols.Get(id)
	if sym == nil {
		return fmt.Sprintf("sym#%d", id)
	}
	if strs == nil {
		return fmt.Sprintf("sym#%d", id)
	}
	name, ok := strs.Lookup(sym.Name)
	if !ok || name == "" {
		return fmt.Sprintf("sym#%d", id)
	}
	return name
}

func formatTypeArgs(typesIn *types.Interner, strs *source.Interner, args []types.TypeID) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, formatType(typesIn, strs, a, 0))
	}
	return "::<" + strings.Join(parts, ", ") + ">"
}

func formatType(typesIn *types.Interner, strs *source.Interner, id types.TypeID, depth int) string {
	if id == types.NoTypeID {
		return "?"
	}
	if typesIn == nil {
		return fmt.Sprintf("type#%d", id)
	}
	if depth > 32 {
		return fmt.Sprintf("type#%d", id)
	}
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return fmt.Sprintf("type#%d", id)
	}

	switch tt.Kind {
	case types.KindUnit:
		return "()"
	case types.KindNothing:
		return "nothing"
	case types.KindBool:
		return "bool"
	case types.KindString:
		return "string"
	case types.KindInt:
		return formatIntType(tt.Width, true)
	case types.KindUint:
		return formatIntType(tt.Width, false)
	case types.KindFloat:
		return formatFloatType(tt.Width)
	case types.KindConst:
		return fmt.Sprintf("%d", tt.Count)
	case types.KindGenericParam:
		if info, ok := typesIn.TypeParamInfo(id); ok && info != nil && strs != nil {
			if name, ok := strs.Lookup(info.Name); ok && name != "" {
				return name
			}
		}
		return fmt.Sprintf("generic#%d", id)
	case types.KindPointer:
		return "*" + formatType(typesIn, strs, tt.Elem, depth+1)
	case types.KindReference:
		if tt.Mutable {
			return "&mut " + formatType(typesIn, strs, tt.Elem, depth+1)
		}
		return "&" + formatType(typesIn, strs, tt.Elem, depth+1)
	case types.KindOwn:
		return "own " + formatType(typesIn, strs, tt.Elem, depth+1)
	case types.KindArray:
		if tt.Count == types.ArrayDynamicLength {
			return "[" + formatType(typesIn, strs, tt.Elem, depth+1) + "]"
		}
		return fmt.Sprintf("[%s; %d]", formatType(typesIn, strs, tt.Elem, depth+1), tt.Count)
	case types.KindTuple:
		if info, ok := typesIn.TupleInfo(id); ok && info != nil {
			elems := make([]string, 0, len(info.Elems))
			for _, e := range info.Elems {
				elems = append(elems, formatType(typesIn, strs, e, depth+1))
			}
			return "(" + strings.Join(elems, ", ") + ")"
		}
		return "()"
	case types.KindFn:
		if info, ok := typesIn.FnInfo(id); ok && info != nil {
			params := make([]string, 0, len(info.Params))
			for _, p := range info.Params {
				params = append(params, formatType(typesIn, strs, p, depth+1))
			}
			return "fn(" + strings.Join(params, ", ") + ") -> " + formatType(typesIn, strs, info.Result, depth+1)
		}
		return fmt.Sprintf("type#%d", id)
	case types.KindStruct:
		if info, ok := typesIn.StructInfo(id); ok && info != nil {
			if s := formatNominal(strs, info.Name, info.TypeArgs, typesIn, depth); s != "" {
				return s
			}
			return fmt.Sprintf("type#%d", id)
		}
	case types.KindAlias:
		if info, ok := typesIn.AliasInfo(id); ok && info != nil {
			if s := formatNominal(strs, info.Name, info.TypeArgs, typesIn, depth); s != "" {
				return s
			}
			return fmt.Sprintf("type#%d", id)
		}
	case types.KindUnion:
		if info, ok := typesIn.UnionInfo(id); ok && info != nil {
			if s := formatNominal(strs, info.Name, info.TypeArgs, typesIn, depth); s != "" {
				return s
			}
			return fmt.Sprintf("type#%d", id)
		}
	case types.KindEnum:
		if info, ok := typesIn.EnumInfo(id); ok && info != nil {
			if strs != nil {
				if name, ok := strs.Lookup(info.Name); ok && name != "" {
					return name
				}
			}
			return fmt.Sprintf("type#%d", id)
		}
	}

	return fmt.Sprintf("type#%d", id)
}

func formatNominal(strs *source.Interner, nameID source.StringID, args []types.TypeID, typesIn *types.Interner, depth int) string {
	name := ""
	if strs != nil && nameID != source.NoStringID {
		if s, ok := strs.Lookup(nameID); ok {
			name = s
		}
	}
	if name == "" {
		return ""
	}
	if len(args) == 0 {
		return name
	}
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, formatType(typesIn, strs, a, depth+1))
	}
	return name + "<" + strings.Join(parts, ", ") + ">"
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
