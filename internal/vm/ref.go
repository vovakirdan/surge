package vm

import "fmt"

type LocKind uint8

const (
	LKLocal LocKind = iota
	LKStructField
	LKArrayElem
)

type Location struct {
	Kind  LocKind
	Frame int

	Local  int
	Handle Handle
	Index  int

	IsMut bool
}

func (l Location) String() string {
	switch l.Kind {
	case LKLocal:
		return fmt.Sprintf("L%d", l.Local)
	case LKStructField:
		return fmt.Sprintf("struct#%d.field[%d]", l.Handle, l.Index)
	case LKArrayElem:
		return fmt.Sprintf("array#%d[%d]", l.Handle, l.Index)
	default:
		return "<invalid-loc>"
	}
}
