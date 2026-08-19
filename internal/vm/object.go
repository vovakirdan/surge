package vm

import (
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// Handle is a stable, monotonically increasing reference to a heap object.
// Handle(0) is always invalid.
type Handle uint32

// ObjectKind identifies the kind of heap object.
type ObjectKind uint8

const (
	// OKString represents a string object.
	OKString ObjectKind = iota
	// OKArray represents an array object.
	OKArray
	// OKArraySlice represents an array slice object.
	OKArraySlice
	// OKMap represents a map object.
	OKMap
	// OKBigInt represents a big integer object.
	OKBigInt
	// OKBigUint represents a big unsigned integer object.
	OKBigUint
	// OKBigFloat represents a big float object.
	OKBigFloat
	// OKRange represents a range object.
	OKRange
	// OKResource represents a runtime-owned resource: a task, a channel or an
	// open file. It carries one opaque word and nothing else, because that word
	// is the whole value — the runtime owns what the word names, and no Surge
	// source can construct or read one.
	//
	// This is the settled shape for these types, not a step towards another
	// one. The wave that moves task, channel and select OWNERSHIP builds on
	// this carrier rather than replacing it: what that work changes is who
	// reclaims what the word names and when, which is a question about the
	// runtime's side of the word, not about how the language carries it.
	OKResource
)

// StringKind identifies the kind of string representation.
type StringKind uint8

const (
	// StringFlat represents a flat string.
	StringFlat StringKind = iota
	// StringConcat represents a concatenated string.
	StringConcat
	// StringSlice represents a string slice.
	StringSlice
)

// RangeKind identifies the kind of range object.
type RangeKind uint8

const (
	// RangeDescriptor represents a descriptor-based range.
	RangeDescriptor RangeKind = iota
	// RangeArrayIter represents an array iterator range.
	RangeArrayIter
)

// RangeObject represents a range object.
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

// HeapHeader contains metadata for a heap object.
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
	// A dynamic array's elements are ONE run of exact element slots in the
	// storage this object owns, described by these four fields together.
	// ArrElemType is the only source of stride, alignment and cell kind: no
	// caller passes an element type in, and no element is asked what it
	// happens to hold. Slots in [ArrLen, ArrCap) are dead — a pop moves the
	// bytes out — so a push initialises its slot and never replaces it.
	ArrElems      StorageRef
	ArrElemType   types.TypeID
	ArrLen        int
	ArrCap        int
	ArrSliceBase  Handle
	ArrSliceStart int
	ArrSliceLen   int
	ArrSliceCap   int
	// ArrSliceStorage names the whole fixed array a slice is cut from when its
	// elements live in an arena rather than in a base object. A fixed array is
	// an extent in its owner's storage, so there is no handle to point at, and
	// the two bases are told apart by which of these is set: ArrSliceBase for a
	// heap array, ArrSliceStorage for an arena one, never both.
	ArrSliceStorage StorageRef
	// A map's keys and values are TWO runs of exact slots in the storage this
	// object owns, indexed together: entry `i` is key slot `i` and value slot
	// `i`. MapIndex stays a lookup from a derived key to that position — it
	// carries no language value and owns nothing.
	MapIndex   map[mapKey]int
	MapKeys    StorageRef
	MapVals    StorageRef
	MapKeyType types.TypeID
	MapValType types.TypeID
	MapLen     int
	MapCap     int
	Range      RangeObject

	BigInt   bignum.BigInt
	BigUint  bignum.BigUint
	BigFloat bignum.BigFloat

	// Resource is the opaque word of an OKResource: a task id, a channel id, a
	// file handle, a socket handle or a nanosecond count. It is not a heap
	// handle and is never followed, which is why releasing a resource releases
	// nothing beneath it.
	Resource int64

	// storage is the arena the composite members of a container live in.
	//
	// A container OUTLIVES the activation that built what goes into it, so an
	// element cannot be a reference into a frame arena that is about to retire —
	// and with the boxed representation gone there is nothing else a composite
	// element could be. The container therefore owns storage of its own: a value
	// is COPIED into it on the way in and the whole arena is released when the
	// object is freed.
	storage *scratch
}
