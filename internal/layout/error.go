package layout

import (
	"fmt"
	"strings"

	"surge/internal/types"
)

type LayoutErrorKind uint8

const (
	LayoutErrRecursiveUnsized LayoutErrorKind = iota + 1
	LayoutErrLengthConversion
	LayoutErrNegativeLength
)

type LayoutError struct {
	Kind  LayoutErrorKind
	Type  types.TypeID
	Cycle []types.TypeID
	// Для ошибок конвертации: вложенная ошибка
	Err error
	// Для ошибок с отрицательной длиной: значение, вызвавшее ошибку
	Value int64
}

func (e *LayoutError) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch e.Kind {
	case LayoutErrRecursiveUnsized:
		if len(e.Cycle) == 0 {
			return fmt.Sprintf("recursive value type has infinite size (type#%d)", e.Type)
		}
		parts := make([]string, 0, len(e.Cycle))
		for _, id := range e.Cycle {
			parts = append(parts, fmt.Sprintf("type#%d", id))
		}
		return fmt.Sprintf("recursive value type has infinite size (cycle: %s)", strings.Join(parts, " -> "))
	case LayoutErrLengthConversion:
		if e.Err != nil {
			return fmt.Sprintf("array length conversion error (type#%d): %v", e.Type, e.Err)
		}
		return fmt.Sprintf("array length conversion error (type#%d)", e.Type)
	case LayoutErrNegativeLength:
		return fmt.Sprintf("negative array length: %d (type#%d)", e.Value, e.Type)
	default:
		return fmt.Sprintf("layout error kind=%d type#%d", e.Kind, e.Type)
	}
}
