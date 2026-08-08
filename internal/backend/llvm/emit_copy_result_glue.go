package llvm

import (
	"fmt"

	"surge/internal/types"
)

// Generated copy-result glue: how a task that may be asked for its result more
// than once builds each asker its own value.
//
// The runtime cannot answer this. It holds a result as bits and a size it never
// learned, and what makes a second value INDEPENDENT is entirely a property of
// the type: a string has to duplicate its bytes, a counted block only has to be
// counted again, and a composite travels in an allocation the copy must not
// share. So the runtime is handed one function per result type and calls it,
// the same way it is handed the drop of a result nobody took.
//
// The copy is made where the original is guaranteed to still exist: inside the
// take, while the asker's own handle still holds the task. Handing an asker the
// original and letting whoever arrives last reclaim it would be cheaper and
// wrong — two askers on two threads would have one of them reading storage the
// other had already freed.

func copyResultGlueName(id types.TypeID) string { return fmt.Sprintf("copy_result.type%d", id) }

// requireCopyResultGlue records that `id` needs the glue and returns its name.
func (e *Emitter) requireCopyResultGlue(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.copyResultGlueNeeded == nil {
		e.copyResultGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.copyResultGlueNeeded[id] = struct{}{}
	return copyResultGlueName(id)
}

// emitCopyResultGlue drains the requested bodies. It runs before the clone
// fixpoint, because a composite body asks for clone glue and nothing asks back.
func (e *Emitter) emitCopyResultGlue() error {
	done := make(map[types.TypeID]struct{})
	for {
		progressed := false
		for _, id := range takePendingGlue(e.copyResultGlueNeeded, done) {
			if err := e.emitCopyResultGlueBody(id); err != nil {
				return err
			}
			progressed = true
		}
		if !progressed {
			return nil
		}
	}
}

// emitCopyResultGlueBody emits `@copy_result.typeN(ptr %val) -> ptr`: the bits
// of a second value that owns everything the first one does.
//
// A composite is copied into a transport allocation of its own, because that is
// the shape the asker's take expects to adopt — the same shape the producer
// published. A value whose whole representation is one word answers with a
// different word.
func (e *Emitter) emitCopyResultGlueBody(id types.TypeID) error {
	id = resolveValueType(e.types, id)
	fmt.Fprintf(&e.buf, "define ptr @%s(ptr %%val) {\n", copyResultGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")

	switch {
	case !e.canDuplicateValue(id):
		return e.emitUncopyableResultBody()

	case e.hasInlineStorage(id):
		facts, err := e.storageFactsOf(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.buf, "  %%copy = call ptr @rt_alloc(i64 %d, i64 %d)\n",
			transportAllocationSize(facts.Size), facts.Align)
		fmt.Fprintf(&e.buf, "  call void @%s(ptr %%copy, ptr %%val)\n", e.requireCloneGlue(id))
		fmt.Fprintf(&e.buf, "  ret ptr %%copy\n}\n\n")
		return nil

	case isStringLike(e.types, id):
		fmt.Fprintf(&e.buf, "  %%copy = call ptr @rt_string_clone(ptr %%val)\n")
		fmt.Fprintf(&e.buf, "  ret ptr %%copy\n}\n\n")
		return nil

	case e.types.IsRefCountedScalar(id):
		// Immutable and counted: both askers may name the same block once each
		// of them is counted in it.
		g := &glueTmp{}
		e.emitGlueRetain(g, "%val")
		fmt.Fprintf(&e.buf, "  ret ptr %%val\n}\n\n")
		return nil
	}
	return fmt.Errorf("type#%d has no copy for a second asker", id)
}

// emitUncopyableResultBody closes a body for a result the backend cannot make a
// second owner of.
//
// Only a dynamic array reaches here: duplicating a buffer is not an operation
// the runtime offers, and answering with the same buffer would give two askers
// one thing to free. Saying so is reachable only from a cloned handle, which is
// the operation that took on the obligation.
func (e *Emitter) emitUncopyableResultBody() error {
	sc, ok := e.stringConsts[uncopyableResultMessage]
	if !ok || sc.globalName == "" {
		return fmt.Errorf("missing string const %q", uncopyableResultMessage)
	}
	fmt.Fprintf(&e.buf, "  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n",
		sc.arrayLen, sc.globalName, sc.dataLen)
	fmt.Fprintf(&e.buf, "  unreachable\n}\n\n")
	return nil
}

// uncopyableResultMessage names what a cloned handle cannot be served.
const uncopyableResultMessage = "a cloned task handle cannot be served a result holding a dynamic array"
