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
	OKRange
)

type TagObject struct {
	TagSym symbols.SymbolID
	Fields []Value
}

type RangeObject struct {
	Start     Value
	End       Value
	HasStart  bool
	HasEnd    bool
	Inclusive bool
}

type HeapHeader struct {
	Kind     ObjectKind
	RefCount uint32
	Freed    bool
}

// Object is a typed heap object.
type Object struct {
	HeapHeader
	TypeID  types.TypeID
	AllocID uint64

	Str           string
	StrCPLen      int
	StrCPLenKnown bool
	Arr           []Value
	Fields        []Value
	Tag           TagObject
	Range         RangeObject

	BigInt   bignum.BigInt
	BigUint  bignum.BigUint
	BigFloat bignum.BigFloat
}
