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
	fe.addrOfTargets = fe.collectAddrOfTargets()

	fmt.Fprint(&e.buf, "entry:\n")
	if err := fe.emitAllocas(); err != nil {
		return fmt.Errorf("llvm emit %s allocas: %w", f.Name, err)
	}
	if err := fe.emitParamStores(); err != nil {
		return fmt.Errorf("llvm emit %s param stores: %w", f.Name, err)
	}
	fmt.Fprintf(&e.buf, "  br label %%bb%d\n", f.Entry)

	order := fe.blockOrder()
	for _, bb := range order {
		if bb == nil {
			continue
		}
		fmt.Fprintf(&e.buf, "bb%d:\n", bb.ID)
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
		llvmTy, err := llvmLocalValueType(fe.emitter.types, local)
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
	boxAsyncRefs := fe.shouldBoxAsyncRefParams()
	for i, localID := range fe.paramLocals {
		local := fe.f.Locals[localID]
		llvmTy, err := llvmLocalValueType(fe.emitter.types, local)
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%%p%d", i)
		if boxAsyncRefs && isRefType(fe.emitter.types, local.Type) && !isMutableRefType(fe.emitter.types, local.Type) {
			boxed, err := fe.emitAsyncRefParamBox(value, local.Type)
			if err != nil {
				return err
			}
			value = boxed
			llvmTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %%%s\n", llvmTy, value, fe.localAlloca[localID])
	}
	return nil
}

func (fe *funcEmitter) shouldBoxAsyncRefParams() bool {
	if fe == nil || fe.f == nil || fe.emitter == nil || fe.emitter.mod == nil {
		return false
	}
	if isPollFunc(fe.f) || !isTaskType(fe.emitter.types, fe.f.Result) {
		return false
	}
	pollName := fe.f.Name + "$poll"
	for _, fn := range fe.emitter.mod.Funcs {
		if fn != nil && fn.Name == pollName {
			return true
		}
	}
	return false
}

func (fe *funcEmitter) emitAsyncRefParamBox(paramValue string, refType types.TypeID) (string, error) {
	valueType, ok := derefType(fe.emitter.types, refType)
	if !ok {
		return "", fmt.Errorf("async ref parameter has non-reference type")
	}
	valueLLVM, err := llvmValueType(fe.emitter.types, valueType)
	if err != nil {
		return "", err
	}
	size, align, err := llvmTypeSizeAlign(valueLLVM)
	if err != nil {
		return "", err
	}
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}
	box := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", box, size, align)
	isNull := fe.nextTemp()
	trapBB := fe.nextInlineBlock()
	okBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq ptr %s, null\n", isNull, box)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isNull, trapBB, okBB)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", trapBB)
	fmt.Fprintf(&fe.emitter.buf, "  call void @llvm.trap()\n")
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
	value := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", value, valueLLVM, paramValue)
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valueLLVM, value, box)
	return box, nil
}
