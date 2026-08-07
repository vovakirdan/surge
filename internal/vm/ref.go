package vm

import "fmt"

// LocKind identifies the kind of location.
type LocKind uint8

const (
	// LKLocal represents a local variable location.
	LKLocal LocKind = iota
	// LKGlobal represents a global variable location.
	LKGlobal
	// LKArrayElem represents an array element location.
	LKArrayElem
	// LKMapElem represents a map element location.
	LKMapElem
	// LKStringBytes represents a string bytes location.
	LKStringBytes
	// LKRawBytes represents a raw bytes location.
	LKRawBytes
	// LKStorage names bytes in an arena: the storage of a composite value, or
	// of one member projected out of it.
	//
	// It replaces naming a member by a container handle and an index. Those two
	// answered "which slot of which object", which is a question only a boxed
	// representation can answer; exact layout has no slots, it has offsets, and
	// the offset of a member is arithmetic on the offset of its owner. Carrying
	// the arena and the generation rather than a handle is also what lets a
	// projection outlive nothing: the reference goes stale with the storage it
	// came from instead of with an object that may be shared.
	LKStorage
)

// Location represents a memory location in the VM.
type Location struct {
	FrameRef *Frame
	Local    int32
	Global   int32
	Index    int32
	// ByteOffset is the ABI byte offset of the projected location within its base object.
	// It is used for layout-consistent addressing (even if the VM stores values differently).
	ByteOffset int32
	Handle     Handle
	Kind       LocKind

	// Storage is the extent an LKStorage location names. It is a StorageRef and
	// not an offset beside the others because an offset alone is not a
	// location: without the arena it belongs to and the generation it was
	// formed at, bytes that have been handed to a different value read back
	// perfectly well as the value that used to be there.
	Storage StorageRef

	IsMut bool
}

func (l Location) String() string {
	switch l.Kind {
	case LKLocal:
		return fmt.Sprintf("L%d", l.Local)
	case LKGlobal:
		return fmt.Sprintf("G%d", l.Global)
	case LKArrayElem:
		return fmt.Sprintf("array[%d]", l.Index)
	case LKMapElem:
		return fmt.Sprintf("map[%d]", l.Index)
	case LKStringBytes:
		return fmt.Sprintf("string.bytes+%d", l.ByteOffset)
	case LKRawBytes:
		return fmt.Sprintf("raw+%d", l.ByteOffset)
	case LKStorage:
		return fmt.Sprintf("storage+%d:type#%d", l.Storage.Offset, l.Storage.TypeID)
	default:
		return "<invalid-loc>"
	}
}
