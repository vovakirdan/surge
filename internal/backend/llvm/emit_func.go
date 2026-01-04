package llvm

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/mir"
)

func (e *Emitter) emitFunction(f *mir.Func) error {
	if f == nil {
		return nil
	}
	name := e.funcNames[f.ID]
	sig, ok := e.funcSigs[f.ID]
	if !ok {
		return fmt.Errorf("missing function signature for %s", f.Name)
	}
	paramLocals, err := e.paramLocals(f)
	if err != nil {
		return err
	}
	paramNames := make([]string, 0, len(paramLocals))
	for i, localID := range paramLocals {
		if int(localID) < 0 || int(localID) >= len(f.Locals) {
			return fmt.Errorf("invalid param local %d", localID)
		}
		paramNames = append(paramNames, fmt.Sprintf("%s %%%s", sig.params[i], fmt.Sprintf("p%d", i)))
	}
	fmt.Fprintf(&e.buf, "define %s @%s(%s) {\n", sig.ret, name, strings.Join(paramNames, ", "))

	fe := &funcEmitter{
		emitter:     e,
		f:           f,
		localAlloca: make(map[mir.LocalID]string, len(f.Locals)),
		paramLocals: paramLocals,
	}
	for i := range f.Locals {
		localID, err := safeLocalID(i)
		if err != nil {
			return err
		}
		fe.localAlloca[localID] = fmt.Sprintf("l%d", i)
	}

	order := fe.blockOrder()
	for _, bb := range order {
		if bb == nil {
			continue
		}
		fmt.Fprintf(&e.buf, "bb%d:\n", bb.ID)
		if bb.ID == f.Entry {
			if err := fe.emitAllocas(); err != nil {
				return err
			}
			if err := fe.emitParamStores(); err != nil {
				return err
			}
		}
		for i := range bb.Instrs {
			if err := fe.emitInstr(&bb.Instrs[i]); err != nil {
				return err
			}
		}
		if err := fe.emitTerminator(&bb.Term); err != nil {
			return err
		}
	}
	fmt.Fprint(&e.buf, "}\n\n")
	return nil
}

func (fe *funcEmitter) blockOrder() []*mir.Block {
	if fe.f == nil {
		return nil
	}
	blocks := make([]*mir.Block, 0, len(fe.f.Blocks))
	for i := range fe.f.Blocks {
		bb := &fe.f.Blocks[i]
		blocks = append(blocks, bb)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID < blocks[j].ID })
	if len(blocks) == 0 {
		return blocks
	}
	if blocks[0].ID == fe.f.Entry {
		return blocks
	}
	ordered := make([]*mir.Block, 0, len(blocks))
	for _, bb := range blocks {
		if bb.ID == fe.f.Entry {
			ordered = append(ordered, bb)
			break
		}
	}
	for _, bb := range blocks {
		if bb.ID == fe.f.Entry {
			continue
		}
		ordered = append(ordered, bb)
	}
	return ordered
}

func (fe *funcEmitter) emitAllocas() error {
	for i, local := range fe.f.Locals {
		llvmTy, err := llvmValueType(fe.emitter.types, local.Type)
		if err != nil {
			return err
		}
		localID, err := safeLocalID(i)
		if err != nil {
			return err
		}
		fmt.Fprintf(&fe.emitter.buf, "  %%%s = alloca %s\n", fe.localAlloca[localID], llvmTy)
	}
	return nil
}

func (fe *funcEmitter) emitParamStores() error {
	for i, localID := range fe.paramLocals {
		llvmTy, err := llvmValueType(fe.emitter.types, fe.f.Locals[localID].Type)
		if err != nil {
			return err
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %%p%d, ptr %%%s\n", llvmTy, i, fe.localAlloca[localID])
	}
	return nil
}
