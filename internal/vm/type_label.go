package vm

import (
	"surge/internal/source"
	"surge/internal/types"
)

func typeLabel(typesIn *types.Interner, id types.TypeID) string {
	return types.Label(typesIn, id)
}

func lookupName(stringsIn *source.Interner, id source.StringID) (string, bool) {
	if stringsIn == nil {
		return "", false
	}
	name, ok := stringsIn.Lookup(id)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}
