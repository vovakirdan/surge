package ast

import (
	"fmt"

	"fortio.org/safecast"
)

// Arena is a generic typed arena for allocating elements.
type Arena[T any] struct {
	data []*T
}

// NewArena creates and returns an *Arena[T] whose internal slice is allocated with a capacity of capHint.
// capHint is a hint for the initial capacity of the arena's underlying storage; zero is allowed.
func NewArena[T any](capHint uint) *Arena[T] {
	return &Arena[T]{
		data: make([]*T, 0, capHint),
	}
}

// Allocate appends a value to the arena and returns its 1-based index.
func (a *Arena[T]) Allocate(value T) uint32 {
	elem := new(T)
	*elem = value
	a.data = append(a.data, elem)
	return a.Len()
}

// Get returns a pointer to the element at the given 1-based index, or nil if index is 0.
func (a *Arena[T]) Get(index uint32) *T {
	if index == 0 {
		return nil
	}
	return a.data[index-1]
}

// Slice returns a copy of the arena contents. `READONLY!`
func (a *Arena[T]) Slice() []T {
	result := make([]T, len(a.data))
	for i, ptr := range a.data {
		result[i] = *ptr
	}
	return result
}

// Len returns the number of elements in the arena.
func (a *Arena[T]) Len() uint32 {
	result, err := safecast.Conv[uint32](len(a.data))
	if err != nil {
		panic(fmt.Errorf("arena len overflow: %w", err))
	}
	return result
}
