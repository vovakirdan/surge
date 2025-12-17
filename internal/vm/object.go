package vm

import "surge/internal/types"

// Handle is a stable, monotonically increasing reference to a heap object.
// Handle(0) is always invalid.
type Handle uint32

// ObjectKind identifies the kind of heap object.
type ObjectKind uint8

const (
	OKString ObjectKind = iota
	OKArray
	OKStruct
)

// Object is a typed heap object.
type Object struct {
	Kind    ObjectKind
	TypeID  types.TypeID
	Alive   bool
	AllocID uint64

	Str    string
	Arr    []Value
	Fields []Value
}
