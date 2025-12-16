// Package vm implements a direct MIR interpreter for Surge programs.
package vm

import (
	"fmt"

	"surge/internal/types"
)

// ValueKind identifies the runtime type of a Value.
type ValueKind uint8

const (
	VKInvalid     ValueKind = iota
	VKInt                   // signed integer
	VKBool                  // boolean
	VKNothing               // nothing/unit value
	VKStringConst           // string literal constant
	VKStringSlice           // slice of strings (for argv)
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
	case VKStringConst:
		return "string"
	case VKStringSlice:
		return "[]string"
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
	Str    string       // For VKStringConst
	Strs   []string     // For VKStringSlice (argv)
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
	case VKStringConst:
		return fmt.Sprintf("%q", v.Str)
	case VKStringSlice:
		return fmt.Sprintf("%v", v.Strs)
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

// MakeString creates a string constant value.
func MakeString(s string, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKStringConst,
		Str:    s,
	}
}

// MakeStringSlice creates a string slice value (for argv).
func MakeStringSlice(strs []string, typeID types.TypeID) Value {
	return Value{
		TypeID: typeID,
		Kind:   VKStringSlice,
		Strs:   strs,
	}
}
