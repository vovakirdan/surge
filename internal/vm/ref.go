package vm

import "fmt"

// LocKind identifies the kind of location.
type LocKind uint8

const (
	// LKLocal represents a local variable location.
	LKLocal LocKind = iota
	// LKGlobal represents a global variable location.
	LKGlobal
	// LKStructField represents a struct field location.
	LKStructField
	// LKArrayElem represents an array element location.
	LKArrayElem
	// LKMapElem represents a map element location.
	LKMapElem
	// LKStringBytes represents a string bytes location.
	LKStringBytes
	// LKRawBytes represents a raw bytes location.
	LKRawBytes
	// LKTagField represents a tagged union payload location.
	LKTagField
)

// Location represents a memory location in the VM.
type Location struct {
	Frame int32

	Local  int32
	Global int32
	Index  int32
	// ByteOffset is the ABI byte offset of the projected location within its base object.
	// It is used for layout-consistent addressing (even if the VM stores values differently).
	ByteOffset int32
	Handle     Handle
	Kind       LocKind

	IsMut bool
}

func (l Location) String() string {
	switch l.Kind {
	case LKLocal:
		return fmt.Sprintf("L%d", l.Local)
	case LKGlobal:
		return fmt.Sprintf("G%d", l.Global)
	case LKStructField:
		return fmt.Sprintf("struct.field[%d]", l.Index)
	case LKArrayElem:
		return fmt.Sprintf("array[%d]", l.Index)
	case LKMapElem:
		return fmt.Sprintf("map[%d]", l.Index)
	case LKStringBytes:
		return fmt.Sprintf("string.bytes+%d", l.ByteOffset)
	case LKRawBytes:
		return fmt.Sprintf("raw+%d", l.ByteOffset)
	case LKTagField:
		return fmt.Sprintf("tag.field[%d]", l.Index)
	default:
		return "<invalid-loc>"
	}
}
