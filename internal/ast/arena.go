package ast

type Arena[T any] struct {
	data []T
}

func NewArena[T any](capHint uint) *Arena[T] {
	return &Arena[T]{
		data: make([]T, 0, capHint),
	}
}

// Возвращает индекс нового элемента
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
