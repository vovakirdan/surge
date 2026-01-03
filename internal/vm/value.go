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
	VKInvalid ValueKind = iota
	VKInt               // signed integer
	VKBool              // boolean
	VKNothing           // nothing/unit value
	VKFunc              // function value

	VKRef
	VKRefMut
	VKPtr

	VKHandleString
	VKHandleArray
	VKHandleMap
	VKHandleStruct
	VKHandleTag
	VKHandleRange

	VKBigInt
	VKBigUint
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

func MakeFunc(sym symbols.SymbolID, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKFunc,
		Sym:    sym,
	}
}

func MakeRef(loc Location, typeID types.TypeID) Value {
	loc.IsMut = false
	return Value{
		TypeID: typeID,
		Kind:   VKRef,
		Loc:    loc,
	}
}

func MakeRefMut(loc Location, typeID types.TypeID) Value {
	loc.IsMut = true
	return Value{
		TypeID: typeID,
		Kind:   VKRefMut,
		Loc:    loc,
	}
}

func MakePtr(loc Location, typeID types.TypeID) Value {
	loc.IsMut = false
	return Value{
		TypeID: typeID,
		Kind:   VKPtr,
		Loc:    loc,
	}
}

func MakeHandleString(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleString,
		H:      h,
	}
}

func MakeHandleArray(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleArray,
		H:      h,
	}
}

func MakeHandleMap(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleMap,
		H:      h,
	}
}

func MakeHandleStruct(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleStruct,
		H:      h,
	}
}

func MakeHandleTag(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleTag,
		H:      h,
	}
}

func MakeHandleRange(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleRange,
		H:      h,
	}
}

func MakeBigInt(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKBigInt,
		H:      h,
	}
}

func MakeBigUint(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKBigUint,
		H:      h,
	}
}

func MakeBigFloat(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKBigFloat,
		H:      h,
	}
}
