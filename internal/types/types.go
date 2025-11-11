package types

import "fmt"

// TypeID uniquely identifies a type inside the interner.
type TypeID uint32

// NoTypeID marks the absence of a type.
const NoTypeID TypeID = 0

// Kind enumerates all supported kinds of types.
type Kind uint8

const (
	KindInvalid Kind = iota
	KindUnit
	KindNothing
	KindBool
	KindString
	KindInt
	KindUint
	KindFloat
	KindArray
	KindPointer
	KindReference
	KindOwn
)

func (k Kind) String() string {
	switch k {
	case KindInvalid:
		return "invalid"
	case KindUnit:
		return "unit"
	case KindNothing:
		return "nothing"
	case KindBool:
		return "bool"
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindUint:
		return "uint"
	case KindFloat:
		return "float"
	case KindArray:
		return "array"
	case KindPointer:
		return "pointer"
	case KindReference:
		return "reference"
	case KindOwn:
		return "own"
	default:
		return fmt.Sprintf("Kind(%d)", k)
	}
}

// Width captures the precision of integers/floats.
type Width uint8

const (
	WidthAny Width = 0
	Width8   Width = 8
	Width16  Width = 16
	Width32  Width = 32
	Width64  Width = 64
)

// ArrayDynamicLength marks slices with unknown compile-time length.
const ArrayDynamicLength = ^uint32(0)

// Type is a compact descriptor for any supported type.
type Type struct {
	Kind    Kind
	Elem    TypeID
	Count   uint32 // for arrays (ArrayDynamicLength means slice)
	Width   Width  // for numeric primitives
	Mutable bool   // for references
}

// Descriptor helpers ---------------------------------------------------------

// MakeInt describes a signed integer of the given width (WidthAny for "int").
func MakeInt(width Width) Type {
	return Type{Kind: KindInt, Width: width}
}

// MakeUint describes an unsigned integer type.
func MakeUint(width Width) Type {
	return Type{Kind: KindUint, Width: width}
}

// MakeFloat describes a floating-point type.
func MakeFloat(width Width) Type {
	return Type{Kind: KindFloat, Width: width}
}

// MakeArray describes an array/slice of element type. Use ArrayDynamicLength
// for open-ended slices (T[]).
func MakeArray(elem TypeID, count uint32) Type {
	return Type{Kind: KindArray, Elem: elem, Count: count}
}

// MakePointer describes a raw pointer.
func MakePointer(elem TypeID) Type {
	return Type{Kind: KindPointer, Elem: elem}
}

// MakeReference describes &T or &mut T depending on the mutable flag.
func MakeReference(elem TypeID, mutable bool) Type {
	return Type{Kind: KindReference, Elem: elem, Mutable: mutable}
}

// MakeOwn describes own T.
func MakeOwn(elem TypeID) Type {
	return Type{Kind: KindOwn, Elem: elem}
}
