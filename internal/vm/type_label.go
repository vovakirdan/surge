package vm

import (
	"surge/internal/types"
)

func typeLabel(typesIn *types.Interner, id types.TypeID) string {
	return types.Label(typesIn, id)
}
