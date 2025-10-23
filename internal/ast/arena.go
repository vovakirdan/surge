package ast

type Arena[T any] struct {
	data []T
}

// NewArena creates a new Arena[T] whose underlying slice has length 0 and capacity capHint.
// The capHint parameter is used as an initial capacity hint for the arena's backing slice.
func NewArena[T any](capHint uint) *Arena[T] {
	return &Arena[T]{
		data: make([]T, 0, capHint),
	}
}

// Возвращает индекс нового элемента (1-based).
func (a *Arena[T]) Allocate(value T) uint32 {
	a.data = append(a.data, value)
	return uint32(len(a.data))
}

func (a *Arena[T]) Get(index uint32) *T {
	if index == 0 {
		return nil
	}
	return &a.data[index-1]
}

// READONLY
func (a *Arena[T]) Slice() []T {
	return a.data
}

func (a *Arena[T]) Len() uint32 {
	return uint32(len(a.data))
}