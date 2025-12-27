package mono

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

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

func (b *monoBuilder) rewriteCloneCall(call *hir.Expr, data *hir.CallData, stack []MonoKey) (bool, error) {
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
	if b.types.IsCopy(resolveAlias(b.types, recvType)) {
		return false, nil
	}
	cloneSym, matchType := b.cloneSymbolForType(recvType)
	if !cloneSym.IsValid() {
		typeLabel := b.typeKeyForType(recvType)
		if typeLabel == "" {
			typeLabel = fmt.Sprintf("type#%d", recvType)
		}
		return true, fmt.Errorf("mono: clone for %s requires __clone method", typeLabel)
	}
	typeArgs := b.typeArgsForType(matchType)
	target, err := b.ensureFunc(cloneSym, typeArgs, stack)
	if err != nil {
		return true, err
	}
	if target != nil && target.InstanceSym.IsValid() {
		data.SymbolID = target.InstanceSym
	} else {
		data.SymbolID = cloneSym
	}
	name := b.monoName(cloneSym, typeArgs)
	data.Callee = &hir.Expr{
		Kind: hir.ExprVarRef,
		Type: types.NoTypeID,
		Span: call.Span,
		Data: hir.VarRefData{
			Name:     name,
			SymbolID: data.SymbolID,
		},
	}
	return true, nil
}

func (b *monoBuilder) cloneSymbolForType(recv types.TypeID) (symbols.SymbolID, types.TypeID) {
	if recv == types.NoTypeID {
		return symbols.NoSymbolID, types.NoTypeID
	}
	if sym := b.findCloneSymbol(recv); sym.IsValid() {
		return sym, recv
	}
	if b.types != nil {
		resolved := resolveAlias(b.types, recv)
		if resolved != recv {
			if sym := b.findCloneSymbol(resolved); sym.IsValid() {
				return sym, resolved
			}
		}
	}
	return symbols.NoSymbolID, types.NoTypeID
}

func (b *monoBuilder) findCloneSymbol(recv types.TypeID) symbols.SymbolID {
	typeKey := normalizeTypeKey(b.typeKeyForType(recv))
	if typeKey == "" || b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || b.mod.Symbols.Table.Strings == nil {
		return symbols.NoSymbolID
	}
	typeBase, typeArgs := parseTypeKeyShape(typeKey)
	var fallback symbols.SymbolID
	syms := b.mod.Symbols.Table.Symbols
	limit := syms.Len()
	for i := 1; i <= limit; i++ {
		rawID, err := safecast.Conv[uint32](i)
		if err != nil {
			break
		}
		symID := symbols.SymbolID(rawID)
		entry := syms.Get(symID)
		if entry == nil || entry.Kind != symbols.SymbolFunction || entry.ReceiverKey == "" {
			continue
		}
		name, ok := b.mod.Symbols.Table.Strings.Lookup(entry.Name)
		if !ok || name != "__clone" {
			continue
		}
		recvKey := normalizeTypeKey(string(entry.ReceiverKey))
		if recvKey == typeKey {
			return symID
		}
		if typeBase != "" {
			base, args := parseTypeKeyShape(recvKey)
			if base == typeBase && args == typeArgs {
				if !fallback.IsValid() {
					fallback = symID
				}
			}
		}
	}
	return fallback
}

func (b *monoBuilder) typeKeyForType(id types.TypeID) string {
	if b == nil || b.types == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Strings == nil {
		return ""
	}
	return formatType(b.types, b.mod.Symbols.Table.Strings, id, 0)
}

func (b *monoBuilder) typeArgsForType(id types.TypeID) []types.TypeID {
	if b == nil || b.types == nil || id == types.NoTypeID {
		return nil
	}
	resolved := resolveAlias(b.types, id)
	if info, ok := b.types.StructInfo(resolved); ok && info != nil {
		return cloneTypeArgs(info.TypeArgs)
	}
	if info, ok := b.types.AliasInfo(resolved); ok && info != nil {
		return cloneTypeArgs(info.TypeArgs)
	}
	if info, ok := b.types.UnionInfo(resolved); ok && info != nil {
		return cloneTypeArgs(info.TypeArgs)
	}
	return nil
}

func cloneTypeArgs(args []types.TypeID) []types.TypeID {
	if len(args) == 0 {
		return nil
	}
	out := make([]types.TypeID, len(args))
	copy(out, args)
	return out
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
