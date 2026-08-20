package llvm

import (
	"fmt"
	"sort"

	"surge/internal/mir"
)

func (e *Emitter) emitGlobals() error {
	if e.mod == nil {
		return nil
	}
	if len(e.mod.Globals) == 0 {
		return nil
	}
	for i, g := range e.mod.Globals {
		gid, err := safeGlobalID(i)
		if err != nil {
			return err
		}
		name := e.globalNames[gid]
		llvmTy, err := e.llvmValueType(g.Type)
		if err != nil {
			return err
		}
		align, err := naturalAlign(llvmTy)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.buf, "@%s = global %s zeroinitializer, align %d\n", name, llvmTy, align)
	}
	e.buf.WriteString("\n")
	return nil
}

func (e *Emitter) emitFunctions() error {
	if e.mod == nil {
		return nil
	}
	reachable := e.reachableFuncs()
	funcs := make([]*mir.Func, 0, len(e.mod.Funcs))
	for _, __id := range e.mod.SortedFuncIDs() {
		f := e.mod.Funcs[__id]
		if f != nil {
			if _, ok := reachable[f.ID]; !ok {
				continue
			}
			funcs = append(funcs, f)
		}
	}
	sort.Slice(funcs, func(i, j int) bool {
		return funcs[i].ID < funcs[j].ID
	})
	for _, f := range funcs {
		if err := e.emitFunction(f); err != nil {
			return err
		}
	}
	return nil
}

func (e *Emitter) reachableFuncs() map[mir.FuncID]struct{} {
	reachable := make(map[mir.FuncID]struct{}, len(e.mod.Funcs))
	if e.mod == nil {
		return reachable
	}
	var roots []mir.FuncID
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if isPollFunc(f) {
			roots = append(roots, id)
		}
	}
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if f != nil && f.Name == "__surge_start" {
			roots = append(roots, id)
		}
	}
	// A value descriptor binds a type's clone, and binding is a reference: the
	// runtime may call it for a value no source line clones. Without this the
	// descriptor would name a function the module never defines, which links
	// only by luck — a defect the descriptor agreement test caught the first
	// time a clonable type reached this path.
	roots = append(roots, e.descriptorReferencedFuncs()...)
	if len(roots) == 0 {
		for _, id := range e.mod.SortedFuncIDs() {
			reachable[id] = struct{}{}
		}
		return reachable
	}
	queue := make([]mir.FuncID, 0, len(roots))
	queue = append(queue, roots...)
	for id := range e.fnRefs {
		queue = append(queue, id)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := reachable[id]; ok {
			continue
		}
		reachable[id] = struct{}{}
		f := e.mod.Funcs[id]
		if f == nil {
			continue
		}
		for i := range f.Blocks {
			bb := &f.Blocks[i]
			for j := range bb.Instrs {
				ins := &bb.Instrs[j]
				if ins.Kind != mir.InstrCall {
					continue
				}
				call := &ins.Call
				switch call.Callee.Kind {
				case mir.CalleeSym:
					if nextID, ok := e.resolveFuncIDForCall(f, call); ok {
						if _, seen := reachable[nextID]; !seen {
							queue = append(queue, nextID)
						}
					}
				case mir.CalleeValue:
					if call.Callee.Value.Kind == mir.OperandConst && call.Callee.Value.Const.Kind == mir.ConstFn && call.Callee.Value.Const.Sym.IsValid() {
						if nextID, ok := e.mod.FuncBySym[call.Callee.Value.Const.Sym]; ok {
							if _, seen := reachable[nextID]; !seen {
								queue = append(queue, nextID)
							}
						}
						continue
					}
					if nextID, ok := e.resolveFuncIDForCall(f, call); ok {
						if _, seen := reachable[nextID]; !seen {
							queue = append(queue, nextID)
						}
					}
				}
			}
		}
	}
	return reachable
}
