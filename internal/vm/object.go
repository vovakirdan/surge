package vm

import (
	"surge/internal/symbols"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// Handle is a stable, monotonically increasing reference to a heap object.
// Handle(0) is always invalid.
type Handle uint32

// ObjectKind identifies the kind of heap object.
type ObjectKind uint8

const (
	OKString ObjectKind = iota
	OKArray
	OKStruct
	OKTag
	OKBigInt
	OKBigUint
	OKBigFloat
)

type TagObject struct {
	TagSym symbols.SymbolID
	Fields []Value
}

// Object is a typed heap object.
type Object struct {
	Kind    ObjectKind
	TypeID  types.TypeID
	Alive   bool
	AllocID uint64

	Str    string
	Arr    []Value
	Fields []Value
	Tag    TagObject

	BigInt   bignum.BigInt
	BigUint  bignum.BigUint
	BigFloat bignum.BigFloat
}
