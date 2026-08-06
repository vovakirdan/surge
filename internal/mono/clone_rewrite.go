package mono

import (
	"fmt"
	"strings"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *monoBuilder) isIntrinsicCloneSymbol(sym symbols.SymbolID) bool {
	if !sym.IsValid() || b == nil {
		return false
	}
	if fn := b.origFuncBySym[sym]; fn != nil {
		return fn.IsIntrinsic() && fn.Name == "clone"
	}
	if b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Strings == nil {
		return false
	}
	entry := b.mod.Symbols.Table.Symbols.Get(sym)
	if entry == nil || entry.Kind != symbols.SymbolFunction {
		return false
	}
	name, ok := b.mod.Symbols.Table.Strings.Lookup(entry.Name)
	return ok && name == "clone"
}

func (b *monoBuilder) rewriteCloneCall(call *hir.Expr, data *hir.CallData) (bool, error) {
	if call == nil || data == nil || b == nil || b.types == nil {
		return false, nil
	}
	if len(data.Args) != 1 {
		return false, nil
	}
	recvType := call.Type
	if recvType == types.NoTypeID && data.Args[0] != nil {
		recvType = valueType(b.types, data.Args[0].Type)
	}
	if recvType == types.NoTypeID {
		return false, nil
	}
	if b.types.IsCopy(resolveAlias(b.types, recvType)) || b.isTaskType(recvType) {
		return false, nil
	}
	// Which body clones a type is decided once, program-wide, before mono runs:
	// sema publishes the decision into CloneSymbols and the closure carries the
	// chosen callable. Reaching this rewrite means that decision never arrived,
	// and rediscovering a body by scanning the symbol table could pick a
	// different one than the rest of the program uses — so this refuses on
	// every builder, closure-backed or not.
	typeLabel := b.typeKeyForType(recvType)
	if typeLabel == "" {
		typeLabel = fmt.Sprintf("type#%d", recvType)
	}
	return true, fmt.Errorf("mono: clone of %s at %s has no authoritative implementation", typeLabel, call.Span)
}

func (b *monoBuilder) isTaskType(recv types.TypeID) bool {
	if b == nil || b.types == nil || recv == types.NoTypeID {
		return false
	}
	resolved := resolveAlias(b.types, recv)
	if tt, ok := b.types.Lookup(resolved); ok {
		switch tt.Kind {
		case types.KindOwn, types.KindReference, types.KindPointer:
			if tt.Elem != types.NoTypeID {
				resolved = resolveAlias(b.types, tt.Elem)
			}
		}
	}
	info, ok := b.types.StructInfo(resolved)
	if !ok || info == nil || b.types.Strings == nil {
		return false
	}
	name, ok := b.types.Strings.Lookup(info.Name)
	return ok && name == "Task"
}

func (b *monoBuilder) typeKeyForType(id types.TypeID) string {
	if b == nil || b.types == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Strings == nil {
		return ""
	}
	return formatType(b.types, b.mod.Symbols.Table.Strings, id, 0)
}

func valueType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil || id == types.NoTypeID {
		return id
	}
	if tt, ok := typesIn.Lookup(id); ok && tt.Kind == types.KindReference {
		return tt.Elem
	}
	return id
}

func normalizeTypeKey(key string) string {
	return strings.ReplaceAll(strings.TrimSpace(key), " ", "")
}

func parseTypeKeyShape(key string) (base string, argCount int) {
	key = normalizeTypeKey(key)
	if key == "" {
		return "", 0
	}
	key = stripTypePrefix(key)
	if key == "" {
		return "", 0
	}
	start := strings.IndexByte(key, '<')
	if start < 0 {
		return key, 0
	}
	end := matchAngle(key, start)
	if end < 0 {
		return key, 0
	}
	args := key[start+1 : end]
	base = key[:start]
	argCount = countTopLevelArgs(args)
	return base, argCount
}

func stripTypePrefix(key string) string {
	switch {
	case strings.HasPrefix(key, "&mut"):
		return strings.TrimPrefix(key, "&mut")
	case strings.HasPrefix(key, "&"):
		return strings.TrimPrefix(key, "&")
	case strings.HasPrefix(key, "own"):
		return strings.TrimPrefix(key, "own")
	case strings.HasPrefix(key, "*"):
		return strings.TrimPrefix(key, "*")
	default:
		return key
	}
}

func matchAngle(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func countTopLevelArgs(s string) int {
	if s == "" {
		return 0
	}
	count := 1
	depthAngle := 0
	depthParen := 0
	depthBracket := 0
	for i := range len(s) {
		switch s[i] {
		case '<':
			depthAngle++
		case '>':
			if depthAngle > 0 {
				depthAngle--
			}
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '[':
			depthBracket++
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
		case ',':
			if depthAngle == 0 && depthParen == 0 && depthBracket == 0 {
				count++
			}
		}
	}
	return count
}

func resolveAlias(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		tt, ok := typesIn.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := typesIn.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
		seen++
	}
	return id
}
