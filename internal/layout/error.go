package layout

import (
	"fmt"
	"strings"

	"surge/internal/types"
)

// ErrorKind identifies a fail-closed physical-layout failure.
type ErrorKind uint8

const (
	// ErrInvalidTarget reports an invalid target ABI description.
	ErrInvalidTarget ErrorKind = iota + 1
	// ErrUnknownType reports a missing type or type metadata entry.
	ErrUnknownType
	// ErrUnsupportedKind reports a type kind without a physical ABI model.
	ErrUnsupportedKind
	// ErrRecursiveUnsized reports a by-value type cycle with infinite size.
	ErrRecursiveUnsized
	// ErrOverflow reports checked target-address-space arithmetic overflow.
	ErrOverflow
	// ErrUnsupportedAlignment reports an alignment rejected by the target ABI.
	ErrUnsupportedAlignment
	// ErrInvalidQuery reports a query that cannot yield the requested fact.
	ErrInvalidQuery
	// ErrDeferred reports a layout that still depends on generic information.
	ErrDeferred
)

// PathKind identifies one declaration-order edge from a root type.
type PathKind uint8

const (
	// PathAliasTarget traverses from an alias to its target.
	PathAliasTarget PathKind = iota + 1
	// PathOwnValue traverses from own T to T.
	PathOwnValue
	// PathArrayElement traverses to an array element.
	PathArrayElement
	// PathStructField traverses to a struct field.
	PathStructField
	// PathTupleElement traverses to a tuple element.
	PathTupleElement
	// PathUnionCase traverses to a union case.
	PathUnionCase
	// PathUnionPayload traverses to a tagged-union payload field.
	PathUnionPayload
	// PathEnumBase traverses to an enum base representation.
	PathEnumBase
)

// PathElement is immutable once stored in a LayoutError.
type PathElement struct {
	Kind  PathKind
	Index uint32
	Name  string
}

func (p PathElement) String() string {
	if p.Name != "" {
		return p.Name
	}
	switch p.Kind {
	case PathAliasTarget:
		return "alias target"
	case PathOwnValue:
		return "owned value"
	case PathArrayElement:
		return "array element"
	case PathStructField:
		return fmt.Sprintf("field[%d]", p.Index)
	case PathTupleElement:
		return fmt.Sprintf("tuple[%d]", p.Index)
	case PathUnionCase:
		return fmt.Sprintf("union case[%d]", p.Index)
	case PathUnionPayload:
		return fmt.Sprintf("payload[%d]", p.Index)
	case PathEnumBase:
		return "enum base"
	default:
		return fmt.Sprintf("path[%d]", p.Index)
	}
}

// LayoutError is an owned, immutable description of a physical-layout failure.
// Path is always relative to Type, the root whose result was requested.
type LayoutError struct {
	Kind      ErrorKind
	Type      types.TypeID
	Operation string
	Value     uint64
	Limit     uint64

	path  []PathElement
	cycle []types.TypeID
}

// Path returns a copy so cached errors cannot be mutated by callers.
func (e *LayoutError) Path() []PathElement {
	if e == nil {
		return nil
	}
	return append([]PathElement(nil), e.path...)
}

// Cycle returns a copy of the by-value recursion cycle.
func (e *LayoutError) Cycle() []types.TypeID {
	if e == nil {
		return nil
	}
	return append([]types.TypeID(nil), e.cycle...)
}

func (e *LayoutError) clone() *LayoutError {
	if e == nil {
		return nil
	}
	out := *e
	out.path = e.Path()
	out.cycle = e.Cycle()
	return &out
}

func (e *LayoutError) withRoot(root types.TypeID) *LayoutError {
	out := e.clone()
	if out != nil {
		out.Type = root
	}
	return out
}

func (e *LayoutError) prepend(root types.TypeID, elem PathElement) *LayoutError {
	out := e.clone()
	if out == nil {
		return nil
	}
	out.Type = root
	out.path = append([]PathElement{elem}, out.path...)
	return out
}

func (e *LayoutError) prependPath(root types.TypeID, prefix []PathElement) *LayoutError {
	out := e.clone()
	for i := len(prefix) - 1; i >= 0; i-- {
		out = out.prepend(root, prefix[i])
	}
	if out != nil {
		out.Type = root
	}
	return out
}

func (e *LayoutError) Error() string {
	if e == nil {
		return "<nil>"
	}
	base := "physical layout failed"
	switch e.Kind {
	case ErrInvalidTarget:
		base = "invalid layout target"
	case ErrUnknownType:
		base = "unknown type in physical layout"
	case ErrUnsupportedKind:
		base = "unsupported type in physical layout"
	case ErrRecursiveUnsized:
		base = "recursive value type has infinite size"
	case ErrOverflow:
		base = "physical layout exceeds target address space"
	case ErrUnsupportedAlignment:
		base = "alignment exceeds target ABI limit"
	case ErrInvalidQuery:
		base = "invalid physical-layout query"
	case ErrDeferred:
		base = "physical layout is deferred"
	}
	parts := []string{fmt.Sprintf("%s (type#%d)", base, e.Type)}
	if e.Operation != "" {
		parts = append(parts, "operation="+e.Operation)
	}
	if e.Kind == ErrOverflow || e.Kind == ErrUnsupportedAlignment || e.Kind == ErrInvalidTarget {
		parts = append(parts, fmt.Sprintf("value=%d limit=%d", e.Value, e.Limit))
	}
	if len(e.path) != 0 {
		path := make([]string, len(e.path))
		for i := range e.path {
			path[i] = e.path[i].String()
		}
		parts = append(parts, "path="+strings.Join(path, " -> "))
	}
	if len(e.cycle) != 0 {
		cycle := make([]string, len(e.cycle))
		for i, id := range e.cycle {
			cycle[i] = fmt.Sprintf("type#%d", id)
		}
		parts = append(parts, "cycle="+strings.Join(cycle, " -> "))
	}
	return strings.Join(parts, "; ")
}
