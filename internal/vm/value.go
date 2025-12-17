// Package vm implements a direct MIR interpreter for Surge programs.
package vm

import (
	"fmt"

	"surge/internal/types"
)

// ValueKind identifies the runtime type of a Value.
type ValueKind uint8

const (
	VKInvalid ValueKind = iota
	VKInt               // signed integer
	VKBool              // boolean
	VKNothing           // nothing/unit value

	VKHandleString
	VKHandleArray
	VKHandleStruct
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
	case VKHandleString:
		return "string"
	case VKHandleArray:
		return "array"
	case VKHandleStruct:
		return "struct"
	default:
		return fmt.Sprintf("ValueKind(%d)", k)
	}
}

// Value represents a runtime value in the VM.
type Value struct {
	TypeID types.TypeID // Static type from compiler
	Kind   ValueKind    // Runtime value kind
	Int    int64        // For VKInt
	Bool   bool         // For VKBool
	H      Handle       // For VKHandle*
}

// IsZero returns true if this is a zero/invalid value.
func (v Value) IsZero() bool {
	return v.Kind == VKInvalid
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
	case VKHandleString:
		return fmt.Sprintf("string#%d", v.H)
	case VKHandleArray:
		return fmt.Sprintf("array#%d", v.H)
	case VKHandleStruct:
		return fmt.Sprintf("struct#%d", v.H)
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

func MakeHandleStruct(h Handle, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKHandleStruct,
		H:      h,
	}
}
