package vm

// Storage a heap container owns, for the composite values it holds.
//
// A composite is its bytes. An activation's arena retires when the activation
// leaves, and a container does not leave with it — an array built inside a
// function and returned outlives every frame that touched it — so an element
// that named a frame's bytes would name storage that has been given away. With
// the boxed representation gone there is nothing else an element could be, so
// the container gets an arena and the value is copied into it.
//
// The discipline is deliberately simpler than an activation's. There is no
// rewind to a mark, because a container has no instruction boundaries: space is
// reclaimed only when the object is freed, and an element removed before then
// releases what it held and leaves its extent behind. That trades bytes for a
// reclamation rule with no way to free one extent twice, which is the same
// trade scratch makes and the right one while the representation is settling.

// releaseContainerStorage drops everything a container's arena still holds and
// invalidates every reference into it.
//
// It is the arena and not the element list that owns the composites, which is
// what keeps the release single: walking the elements as well would release
// each of them twice. The generation bump is the load-bearing half — a
// reference into a freed container is refused rather than resolved against
// whatever occupies those bytes next.
func (vm *VM) releaseContainerStorage(obj *Object) {
	if vm == nil || obj == nil || obj.storage == nil {
		return
	}
	// A drop that cannot complete is not raised: the object is being freed
	// either way, and what could not be released stays on the heap where the
	// leak check sees it with the whole heap in front of it.
	_ = vm.rewindScratch(obj.storage, scratchMark{}) //nolint:errcheck // see above
	obj.storage = nil
}
