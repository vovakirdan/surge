// Package vm implements a direct MIR interpreter for Surge programs.
package vm

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
)

// ValueKind identifies the runtime type of a Value.
type ValueKind uint8

const (
	// VKInvalid represents an invalid value.
	VKInvalid ValueKind = iota
	// VKInt represents a signed integer value.
	VKInt // signed integer
	// VKBool represents a boolean value.
	VKBool // boolean
	// VKNothing represents a nothing/unit value.
	VKNothing // nothing/unit value
	// VKFunc represents a function value.
	VKFunc // function value
	// VKRef represents a reference value.
	VKRef
	// VKRefMut represents a mutable reference value.
	VKRefMut
	// VKPtr represents a pointer value.
	VKPtr

	// VKHandleString represents a string handle value.
	VKHandleString
	// VKHandleArray represents an array handle value.
	VKHandleArray
	// VKHandleMap represents a map handle value.
	VKHandleMap
	// VKHandleStruct represents a struct handle value.
	VKHandleStruct
	// VKHandleTag represents a tagged union handle value.
	VKHandleTag
	// VKHandleRange represents a range handle value.
	VKHandleRange

	// VKBigInt represents a big integer handle value.
	VKBigInt
	// VKBigUint represents a big unsigned integer handle value.
	VKBigUint
	// VKBigFloat represents a big float handle value.
	VKBigFloat
)

// String returns a human-readable name for the value kind.
func (k ValueKind) String() string {
	switch k {
	case VKInvalid:
		return "invalid"
	case VKInt:
		return "int"
	case VKBool:
		return "bool"
	case VKNothing:
		return "nothing"
	case VKFunc:
		return "func"
	case VKRef:
		return "ref"
	case VKRefMut:
		return "refmut"
	case VKPtr:
		return "ptr"
	case VKHandleString:
		return "string"
	case VKHandleArray:
		return "array"
	case VKHandleMap:
		return "map"
	case VKHandleStruct:
		return "struct"
	case VKHandleTag:
		return "tag"
	case VKHandleRange:
		return "range"
	case VKBigInt:
		return "bigint"
	case VKBigUint:
		return "biguint"
	case VKBigFloat:
		return "bigfloat"
	default:
		return fmt.Sprintf("ValueKind(%d)", k)
	}
}

// Value represents a runtime value in the VM.
type Value struct {
	TypeID types.TypeID     // Static type from compiler
	Kind   ValueKind        // Runtime value kind
	Int    int64            // For VKInt
	Bool   bool             // For VKBool
	H      Handle           // For VKHandle*
	Loc    Location         // For VKRef/VKRefMut/VKPtr
	Sym    symbols.SymbolID // For VKFunc
}

// IsZero returns true if this is a zero/invalid value.
func (v Value) IsZero() bool {
	return v.Kind == VKInvalid
}

// IsHeap reports whether the value is stored on the heap.
func (v Value) IsHeap() bool {
	switch v.Kind {
	case VKHandleString, VKHandleArray, VKHandleMap, VKHandleStruct, VKHandleTag, VKHandleRange, VKBigInt, VKBigUint, VKBigFloat:
		return true
	default:
		return false
	}
}

// String returns a human-readable representation of the value.
func (v Value) String() string {
	switch v.Kind {
	case VKInvalid:
		return "<invalid>"
	case VKInt:
		return fmt.Sprintf("%d", v.Int)
	case VKBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case VKNothing:
		return "nothing"
	case VKFunc:
		return fmt.Sprintf("fn#%d", v.Sym)
	case VKRef:
		return fmt.Sprintf("&%s", v.Loc)
	case VKRefMut:
		return fmt.Sprintf("&mut %s", v.Loc)
	case VKPtr:
		return fmt.Sprintf("*%s", v.Loc)
	case VKHandleString:
		return "string"
	case VKHandleArray:
		return "array"
	case VKHandleMap:
		return "map"
	case VKHandleStruct:
		return "struct"
	case VKHandleTag:
		return "tag"
	case VKHandleRange:
		return "range"
	case VKBigInt:
		return "bigint"
	case VKBigUint:
		return "biguint"
	case VKBigFloat:
		return "bigfloat"
	default:
		return fmt.Sprintf("<unknown:%d>", v.Kind)
	}
}

// MakeInt creates an integer value.
func MakeInt(n int64, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKInt,
		Int:    n,
	}
}

// MakeBool creates a boolean value.
func MakeBool(b bool, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKBool,
		Bool:   b,
	}
}

// MakeNothing creates a nothing/unit value.
func MakeNothing() Value {
	return Value{
		Kind: VKNothing,
	}
}

// MakeFunc creates a function value.
func MakeFunc(sym symbols.SymbolID, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKFunc,
		Sym:    sym,
	}
}

// MakeRef creates a reference value.
func MakeRef(loc Location, typeID types.TypeID) Value {
	loc.IsMut = false
	return Value{
		TypeID: typeID,
		Kind:   VKRef,
		Loc:    loc,
	}
}

// MakeRefMut creates a mutable reference value.
func MakeRefMut(loc Location, typeID types.TypeID) Value {
	loc.IsMut = true
	return Value{
		TypeID: typeID,
		Kind:   VKRefMut,
		Loc:    loc,
	}
}

// MakePtr creates a pointer value.
func MakePtr(loc Location, typeID types.TypeID) Value {
	loc.IsMut = false
	return Value{
		TypeID: typeID,
		Kind:   VKPtr,
		Loc:    loc,
	}
}

// MakeHandleString creates a string handle value.
func MakeHandleString(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleString,
		H:      h,
	}
}

// MakeHandleArray creates an array handle value.
func MakeHandleArray(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleArray,
		H:      h,
	}
}

// MakeHandleMap creates a map handle value.
func MakeHandleMap(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleMap,
		H:      h,
	}
}

// MakeHandleStruct creates a struct handle value.
func MakeHandleStruct(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleStruct,
		H:      h,
	}
}

// MakeHandleTag creates a tagged union handle value.
func MakeHandleTag(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleTag,
		H:      h,
	}
}

// MakeHandleRange creates a range handle value.
func MakeHandleRange(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleRange,
		H:      h,
	}
}

// MakeBigInt creates a big integer handle value.
func MakeBigInt(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKBigInt,
		H:      h,
	}
}

// MakeBigUint creates a big unsigned integer handle value.
func MakeBigUint(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKBigUint,
		H:      h,
	}
}

// MakeBigFloat creates a big float handle value.
func MakeBigFloat(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKBigFloat,
		H:      h,
	}
}
