package llvm

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
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
	lowered, err := e.loweredSignature(&sig)
	if err != nil {
		return fmt.Errorf("call contract for %s: %w", f.Name, err)
	}
	paramNames := make([]string, 0, len(paramLocals)+1)
	if lowered.sret {
		paramNames = append(paramNames, fmt.Sprintf(
			"ptr sret(%s) align %d %%%s", lowered.retStorage, lowered.retAlign, sretParamName))
	}
	for i, localID := range paramLocals {
		if int(localID) < 0 || int(localID) >= len(f.Locals) {
			return fmt.Errorf("invalid param local %d", localID)
		}
		if i >= len(lowered.params) || lowered.params[i].elided {
			continue
		}
		paramNames = append(paramNames, fmt.Sprintf("%s %%p%d", lowered.params[i].spelling, i))
	}
	fe := &funcEmitter{
		emitter:     e,
		f:           f,
		localAlloca: make(map[mir.LocalID]string, len(f.Locals)),
		paramLocals: paramLocals,
		lowered:     lowered,
		span:        spanCursor{f: f},
	}
	for i := range f.Locals {
		localID, idErr := safeLocalID(i)
		if idErr != nil {
			return idErr
		}
		fe.localAlloca[localID] = fmt.Sprintf("l%d", i)
	}
	fe.addrOfTargets = fe.collectAddrOfTargets()

	// The body is emitted aside so that every reservation it made along the way
	// can be placed in the entry block ahead of it. See emitAllocaAligned.
	body, err := e.captureBody(func() error { return fe.emitBody(f) })
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.buf, "define %s @%s(%s) {\n", lowered.ret, name, strings.Join(paramNames, ", "))
	fmt.Fprint(&e.buf, "entry:\n")
	fe.emitTraceFuncMarker(f.Name)
	e.buf.WriteString(fe.entryAllocas.String())
	e.buf.WriteString(body)
	fmt.Fprint(&e.buf, "}\n\n")
	return nil
}

// captureBody runs one emission against a buffer of its own and returns what it
// wrote, leaving the module buffer untouched.
func (e *Emitter) captureBody(emit func() error) (string, error) {
	outer := e.buf
	e.buf = strings.Builder{}
	defer func() { e.buf = outer }()
	if err := emit(); err != nil {
		return "", err
	}
	return e.buf.String(), nil
}

func (fe *funcEmitter) emitBody(f *mir.Func) error {
	if err := fe.emitAllocas(); err != nil {
		return fmt.Errorf("llvm emit %s allocas: %w", f.Name, err)
	}
	if err := fe.emitParamStores(); err != nil {
		return fmt.Errorf("llvm emit %s param stores: %w", f.Name, err)
	}
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", f.Entry)

	for _, bb := range fe.blockOrder() {
		if bb == nil {
			continue
		}
		fmt.Fprintf(&fe.emitter.buf, "bb%d:\n", bb.ID)
		fe.blockTerminated = false
		for i := range bb.Instrs {
			if err := fe.emitInstr(&bb.Instrs[i]); err != nil {
				return fmt.Errorf("llvm emit %s bb%d instr[%d] (%s): %w", f.Name, bb.ID, i, bb.Instrs[i].Kind, err)
			}
			if fe.blockTerminated {
				break
			}
		}
		if fe.blockTerminated {
			continue
		}
		if err := fe.emitTerminator(&bb.Term); err != nil {
			return fmt.Errorf("llvm emit %s bb%d term (%s): %w", f.Name, bb.ID, bb.Term.Kind, err)
		}
	}
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

// emitAllocas reserves one slot per local.
//
// A composite local's slot IS the value now — a byte run at the layout's size
// and alignment — where it used to be a pointer-sized slot holding the address
// of a box. That is the representation change stated once, in the one place
// every local comes from: the place walk, the field GEPs and the copies below
// all read this storage directly and none of them dereferences anything first.
func (fe *funcEmitter) emitAllocas() error {
	for i, local := range fe.f.Locals {
		llvmTy, err := fe.emitter.llvmLocalValueType(local)
		if err != nil {
			return err
		}
		localID, err := safeLocalID(i)
		if err != nil {
			return err
		}
		if err := fe.emitTypedAlloca("%"+fe.localAlloca[localID], localSlotType(local), llvmTy); err != nil {
			return err
		}
	}
	return nil
}

// localSlotType is the type whose layout governs a local's slot, or NoTypeID
// when the slot holds a borrow rather than the value: a `&T` local stores a
// pointer and takes a pointer's alignment, not T's.
func localSlotType(local mir.Local) types.TypeID {
	if local.Flags&(mir.LocalFlagRef|mir.LocalFlagRefMut|mir.LocalFlagPtr) != 0 {
		return types.NoTypeID
	}
	return local.Type
}

// emitParamStores copies each incoming parameter into its local slot.
//
// A composite parameter arrives as the address of storage the callee owns: the
// by-value contract makes the copy at the call, once, so the move here is from
// that storage into the slot the body addresses. Everything else arrives in a
// register and is stored.
//
// A reference parameter of an async constructor arrives as the pointer it is
// and is stored as that pointer. It used to be copied into a heap box here, a
// snapshot of the referent that nothing freed (RV2-DEBT-303), because the
// referent was a per-poll alloca in the caller and the pointer would dangle at
// the caller's next suspension. Two things made the pointer honest: the place
// a child borrows is promoted to the creator's frame, which does not move
// under it (StableActivationPlaces, the resident fields), and the child is
// pinned to the creator's carrier and joined before the place is written
// again (task_borrow_pin.go; __task_create_affine). A borrow is now a borrow.
func (fe *funcEmitter) emitParamStores() error {
	for i, localID := range fe.paramLocals {
		local := fe.f.Locals[localID]
		llvmTy, err := fe.emitter.llvmLocalValueType(local)
		if err != nil {
			return err
		}
		if i < len(fe.lowered.params) && fe.lowered.params[i].elided {
			// Nothing arrives: the slot is already the whole of a zero-sized
			// value, and there is no incoming byte to move into it.
			continue
		}
		slotType := localSlotType(local)
		value := fmt.Sprintf("%%p%d", i)
		align, err := fe.emitter.storageAlignOf(slotType, llvmTy)
		if err != nil {
			return err
		}
		fe.emitValueStore(llvmTy, value, "%"+fe.localAlloca[localID], align)
	}
	return nil
}
