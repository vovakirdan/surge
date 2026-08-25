package llvm

import (
	"fmt"
	"sort"

	"surge/internal/mir"
)

// The blocking dispatch: one switch from a body id to the compiled body, and
// the one place a blocking result is written into the storage the runtime sized
// for it. It lives apart from the async dispatch because it answers a different
// question -- what runs on a pool thread, rather than what runs on a lane.

func (e *Emitter) emitBlockingDispatch() error {
	if e == nil || e.mod == nil {
		return nil
	}
	blockIDs := make([]mir.FuncID, 0)
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if isBlockingFunc(f) {
			blockIDs = append(blockIDs, id)
		}
	}
	sort.Slice(blockIDs, func(i, j int) bool { return blockIDs[i] < blockIDs[j] })

	fmt.Fprintf(&e.buf, "define void @__surge_blocking_call(i64 %%id, ptr %%state, ptr %%out) {\n")
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  switch i64 %%id, label %%blocking_default [\n")
	for _, id := range blockIDs {
		fmt.Fprintf(&e.buf, "    i64 %d, label %%blocking.%d\n", id, id)
	}
	fmt.Fprintf(&e.buf, "  ]\n")

	fe := funcEmitter{emitter: e}
	for _, id := range blockIDs {
		f := e.mod.Funcs[id]
		if f == nil {
			continue
		}
		name := e.funcNames[id]
		sig, ok := e.funcSigs[id]
		if !ok {
			return fmt.Errorf("missing blocking function signature for %s", f.Name)
		}
		if len(sig.params) != 1 {
			return fmt.Errorf("blocking function %s must have 1 parameter", f.Name)
		}
		lowered, err := e.loweredSignature(&sig)
		if err != nil {
			return fmt.Errorf("call contract for %s: %w", f.Name, err)
		}
		fmt.Fprintf(&e.buf, "blocking.%d:\n", id)
		if lowered.sret {
			// The body already writes its result through a destination
			// pointer, so it is given the runtime's own storage directly --
			// no frame-local copy, and nothing to widen into a word.
			fmt.Fprintf(&e.buf, "  call void @%s(ptr sret(%s) align %d %%out, ptr %%state)\n",
				name, lowered.retStorage, lowered.retAlign)
			fmt.Fprintf(&e.buf, "  ret void\n")
			continue
		}
		if lowered.ret == "void" {
			fmt.Fprintf(&e.buf, "  call void @%s(ptr %%state)\n", name)
			fmt.Fprintf(&e.buf, "  ret void\n")
			continue
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&e.buf, "  %s = call %s @%s(ptr %%state)\n", tmp, lowered.ret, name)
		// A value the body RETURNS is stored into the runtime's storage at its
		// own type. The store is the whole conversion: the descriptor the
		// runtime bound sized that storage from the same type.
		//
		// A returned value's alignment comes from the TYPE, not from the sret
		// contract, which leaves retAlign at zero for anything it does not
		// carry indirectly.
		storeAlign := uint64(alignWord)
		if facts, alignErr := e.layoutOf(f.Result); alignErr == nil && facts.Align > 0 {
			storeAlign = facts.Align
		}
		fmt.Fprintf(&e.buf, "  store %s %s, ptr %%out, align %d\n", lowered.ret, tmp, storeAlign)
		fmt.Fprintf(&e.buf, "  ret void\n")
	}

	fmt.Fprintf(&e.buf, "blocking_default:\n")
	if sc, ok := e.stringConsts["missing blocking function"]; ok && sc.globalName != "" {
		fmt.Fprintf(&e.buf, "  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n", sc.arrayLen, sc.globalName, sc.dataLen)
	}
	fmt.Fprintf(&e.buf, "  unreachable\n")
	fmt.Fprintf(&e.buf, "}\n\n")
	return nil
}
