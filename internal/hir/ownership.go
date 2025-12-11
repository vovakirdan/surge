package hir

// Ownership represents the ownership qualifier for a binding or value.
// This is derived from the type and borrow information during HIR lowering.
type Ownership uint8

const (
	// OwnershipOwn indicates owned value (own T).
	OwnershipOwn Ownership = iota
	// OwnershipRef indicates shared reference (&T).
	OwnershipRef
	// OwnershipRefMut indicates mutable reference (&mut T).
	OwnershipRefMut
	// OwnershipPtr indicates raw pointer (*T).
	OwnershipPtr
	// OwnershipCopy indicates a Copy type (value semantics, no move).
	OwnershipCopy
	// OwnershipNone indicates no ownership qualifier (e.g., for primitives).
	OwnershipNone
)

// String returns a human-readable representation of the ownership.
func (o Ownership) String() string {
	switch o {
	case OwnershipOwn:
		return "own"
	case OwnershipRef:
		return "&"
	case OwnershipRefMut:
		return "&mut"
	case OwnershipPtr:
		return "*"
	case OwnershipCopy:
		return "copy"
	case OwnershipNone:
		return ""
	default:
		return "?"
	}
}
