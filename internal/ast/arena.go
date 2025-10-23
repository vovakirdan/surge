package ast

type Arena[T any] struct {
	data []T
}

// NewArena creates and returns an *Arena[T] whose internal slice is allocated with a capacity of capHint.
// capHint is a hint for the initial capacity of the arena's underlying storage; zero is allowed.
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