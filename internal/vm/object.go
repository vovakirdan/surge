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
	OKArraySlice
	OKMap
	OKStruct
	OKTag
	OKBigInt
	OKBigUint
	OKBigFloat
	OKRange
)

type StringKind uint8

const (
	StringFlat StringKind = iota
	StringConcat
	StringSlice
)

type TagObject struct {
	TagSym symbols.SymbolID
	Fields []Value
}

type RangeKind uint8

const (
	RangeDescriptor RangeKind = iota
	RangeArrayIter
)

type RangeObject struct {
	Kind      RangeKind
	Start     Value
	End       Value
	HasStart  bool
	HasEnd    bool
	Inclusive bool

	ArrayBase  Handle
	ArrayStart int
	ArrayLen   int
	ArrayIndex int
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
	StrKind       StringKind
	StrFlatKnown  bool
	StrByteLen    int
	StrCPLen      int
	StrCPLenKnown bool
	StrLeft       Handle
	StrRight      Handle
	StrSliceBase  Handle
	StrSliceStart int
	StrSliceLen   int
	Arr           []Value
	ArrSliceBase  Handle
	ArrSliceStart int
	ArrSliceLen   int
	ArrSliceCap   int
	MapIndex      map[mapKey]int
	MapEntries    []mapEntry
	Fields        []Value
	Tag           TagObject
	Range         RangeObject

	BigInt   bignum.BigInt
	BigUint  bignum.BigUint
	BigFloat bignum.BigFloat
}
