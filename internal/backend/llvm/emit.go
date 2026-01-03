package llvm

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

type funcSig struct {
	ret    string
	params []string
}

type stringConst struct {
	raw        string
	bytes      []byte
	dataLen    int
	arrayLen   int
	globalName string
}

type Emitter struct {
	mod          *mir.Module
	types        *types.Interner
	syms         *symbols.Table
	buf          strings.Builder
	stringConsts map[string]*stringConst
	funcNames    map[mir.FuncID]string
	funcSigs     map[mir.FuncID]funcSig
	globalNames  map[mir.GlobalID]string
	runtimeSigs  map[string]funcSig
	paramCounts  map[mir.FuncID]int
}

type funcEmitter struct {
	emitter     *Emitter
	f           *mir.Func
	tmpID       int
	inlineBlock int
	localAlloca map[mir.LocalID]string
	paramLocals []mir.LocalID
}

const (
	arrayHeaderSize  = 24
	arrayHeaderAlign = 8
	arrayLenOffset   = 0
	arrayCapOffset   = 8
	arrayDataOffset  = 16
)

func EmitModule(mod *mir.Module, typesIn *types.Interner, symTable *symbols.Table) (string, error) {
	e := &Emitter{
		mod:          mod,
		types:        typesIn,
		syms:         symTable,
		stringConsts: make(map[string]*stringConst),
		funcNames:    make(map[mir.FuncID]string),
		funcSigs:     make(map[mir.FuncID]funcSig),
		globalNames:  make(map[mir.GlobalID]string),
		runtimeSigs:  runtimeSigMap(),
	}
	if mod == nil {
		return "", nil
	}
	e.collectStringConsts()
	if err := e.prepareGlobals(); err != nil {
		return "", err
	}
	if err := e.collectParamCounts(); err != nil {
		return "", err
	}
	if err := e.prepareFunctions(); err != nil {
		return "", err
	}
	e.emitPreamble()
	e.emitRuntimeDecls()
	e.emitStringConsts()
	if err := e.emitGlobals(); err != nil {
		return "", err
	}
	if err := e.emitFunctions(); err != nil {
		return "", err
	}
	return e.buf.String(), nil
}

func (e *Emitter) emitPreamble() {
	e.buf.WriteString("target triple = \"x86_64-linux-gnu\"\n\n")
}

func (e *Emitter) emitRuntimeDecls() {
	for _, decl := range runtimeDecls() {
		fmt.Fprintf(&e.buf, "declare %s @%s(%s)\n", decl.ret, decl.name, strings.Join(decl.params, ", "))
	}
	e.buf.WriteString("\n")
}

func (e *Emitter) prepareGlobals() error {
	if e.mod == nil {
		return nil
	}
	for id := range e.mod.Globals {
		gid, err := safeGlobalID(id)
		if err != nil {
			return err
		}
		e.globalNames[gid] = fmt.Sprintf("g%d", id)
	}
	return nil
}

func (e *Emitter) prepareFunctions() error {
	if e.mod == nil {
		return nil
	}
	funcs := make([]*mir.Func, 0, len(e.mod.Funcs))
	for _, f := range e.mod.Funcs {
		if f != nil {
			funcs = append(funcs, f)
		}
	}
	for _, f := range funcs {
		name := fmt.Sprintf("fn.%d", f.ID)
		if f.Name == "__surge_start" {
			name = f.Name
		}
		e.funcNames[f.ID] = name
		paramLocals, err := e.paramLocals(f)
		if err != nil {
			return err
		}
		params := make([]string, 0, len(paramLocals))
		for _, localID := range paramLocals {
			if int(localID) < 0 || int(localID) >= len(f.Locals) {
				return fmt.Errorf("invalid param local %d", localID)
			}
			llvmTy, llvmErr := llvmValueType(e.types, f.Locals[localID].Type)
			if llvmErr != nil {
				return llvmErr
			}
			params = append(params, llvmTy)
		}
		ret, err := llvmType(e.types, f.Result)
		if err != nil {
			return err
		}
		e.funcSigs[f.ID] = funcSig{ret: ret, params: params}
	}
	return nil
}

func (e *Emitter) collectParamCounts() error {
	if e.mod == nil {
		return nil
	}
	counts := make(map[mir.FuncID]int)
	nameToID := make(map[string]mir.FuncID, len(e.mod.Funcs))
	for id, f := range e.mod.Funcs {
		if f != nil && f.Name != "" {
			nameToID[f.Name] = mir.FuncID(id)
		}
	}
	for _, f := range e.mod.Funcs {
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
				if call.Callee.Kind != mir.CalleeSym {
					continue
				}
				targetID := mir.NoFuncID
				if call.Callee.Sym.IsValid() {
					if id, ok := e.mod.FuncBySym[call.Callee.Sym]; ok {
						targetID = id
					}
				} else if call.Callee.Name != "" {
					if id, ok := nameToID[call.Callee.Name]; ok {
						targetID = id
					}
				}
				if targetID == mir.NoFuncID {
					continue
				}
				argCount := len(call.Args)
				if prev, ok := counts[targetID]; ok && prev != argCount {
					targetName := e.mod.Funcs[targetID].Name
					if targetName == "" {
						targetName = fmt.Sprintf("fn.%d", targetID)
					}
					return fmt.Errorf("function %s called with %d and %d args", targetName, prev, argCount)
				}
				counts[targetID] = argCount
			}
		}
	}
	e.paramCounts = counts
	return nil
}

func (e *Emitter) paramLocals(f *mir.Func) ([]mir.LocalID, error) {
	if f == nil {
		return nil, nil
	}
	if e.syms == nil || e.syms.Symbols == nil || e.syms.Strings == nil {
		return nil, fmt.Errorf("missing symbol table")
	}
	if f.Sym.IsValid() {
		sym := e.syms.Symbols.Get(f.Sym)
		if sym != nil {
			if e.types != nil && sym.Type != types.NoTypeID {
				if info, ok := e.types.FnInfo(sym.Type); ok {
					count := len(info.Params)
					if count > len(f.Locals) {
						return nil, fmt.Errorf("function %q has %d params but only %d locals", f.Name, count, len(f.Locals))
					}
					params := make([]mir.LocalID, count)
					for i := range params {
						localID, err := safeLocalID(i)
						if err != nil {
							return nil, err
						}
						params[i] = localID
					}
					return params, nil
				}
			}
			if sym.Signature != nil {
				count := len(sym.Signature.Params)
				if count > len(f.Locals) {
					return nil, fmt.Errorf("function %q has %d params but only %d locals", f.Name, count, len(f.Locals))
				}
				params := make([]mir.LocalID, count)
				for i := range params {
					localID, err := safeLocalID(i)
					if err != nil {
						return nil, err
					}
					params[i] = localID
				}
				return params, nil
			}
		}
	}
	if e.paramCounts != nil {
		if count, ok := e.paramCounts[f.ID]; ok {
			if count > len(f.Locals) {
				return nil, fmt.Errorf("function %q has %d params but only %d locals", f.Name, count, len(f.Locals))
			}
			params := make([]mir.LocalID, count)
			for i := range params {
				localID, err := safeLocalID(i)
				if err != nil {
					return nil, err
				}
				params[i] = localID
			}
			return params, nil
		}
	}
	params := make([]mir.LocalID, 0, len(f.Locals))
	for idx, local := range f.Locals {
		if !local.Sym.IsValid() {
			continue
		}
		sym := e.syms.Symbols.Get(local.Sym)
		if sym == nil {
			continue
		}
		if sym.Kind == symbols.SymbolParam {
			localID, err := safeLocalID(idx)
			if err != nil {
				return nil, err
			}
			params = append(params, localID)
		}
	}
	return params, nil
}

func (e *Emitter) collectStringConsts() {
	if e.mod == nil {
		return
	}
	for _, f := range e.mod.Funcs {
		if f == nil {
			continue
		}
		for i := range f.Blocks {
			bb := &f.Blocks[i]
			for j := range bb.Instrs {
				ins := &bb.Instrs[j]
				switch ins.Kind {
				case mir.InstrAssign:
					e.collectRValue(&ins.Assign.Src)
				case mir.InstrCall:
					for k := range ins.Call.Args {
						e.collectOperand(&ins.Call.Args[k])
					}
				}
			}
			e.collectTerminator(&bb.Term)
		}
	}
}

func (e *Emitter) collectRValue(rv *mir.RValue) {
	if rv == nil {
		return
	}
	switch rv.Kind {
	case mir.RValueUse:
		e.collectOperand(&rv.Use)
	case mir.RValueStructLit:
		for i := range rv.StructLit.Fields {
			e.collectOperand(&rv.StructLit.Fields[i].Value)
		}
	case mir.RValueArrayLit:
		for i := range rv.ArrayLit.Elems {
			e.collectOperand(&rv.ArrayLit.Elems[i])
		}
	case mir.RValueTupleLit:
		for i := range rv.TupleLit.Elems {
			e.collectOperand(&rv.TupleLit.Elems[i])
		}
	case mir.RValueUnaryOp:
		e.collectOperand(&rv.Unary.Operand)
	case mir.RValueBinaryOp:
		e.collectOperand(&rv.Binary.Left)
		e.collectOperand(&rv.Binary.Right)
	case mir.RValueCast:
		e.collectOperand(&rv.Cast.Value)
	case mir.RValueField:
		e.collectOperand(&rv.Field.Object)
	case mir.RValueIndex:
		e.collectOperand(&rv.Index.Object)
		e.collectOperand(&rv.Index.Index)
	case mir.RValueTagTest:
		e.collectOperand(&rv.TagTest.Value)
	case mir.RValueTagPayload:
		e.collectOperand(&rv.TagPayload.Value)
	case mir.RValueIterInit:
		e.collectOperand(&rv.IterInit.Iterable)
	case mir.RValueIterNext:
		e.collectOperand(&rv.IterNext.Iter)
	case mir.RValueTypeTest:
		e.collectOperand(&rv.TypeTest.Value)
	case mir.RValueHeirTest:
		e.collectOperand(&rv.HeirTest.Value)
	}
}

func (e *Emitter) collectTerminator(term *mir.Terminator) {
	if term == nil {
		return
	}
	switch term.Kind {
	case mir.TermReturn:
		if term.Return.HasValue {
			e.collectOperand(&term.Return.Value)
		}
	case mir.TermIf:
		e.collectOperand(&term.If.Cond)
	case mir.TermSwitchTag:
		e.collectOperand(&term.SwitchTag.Value)
	}
}

func (e *Emitter) collectOperand(op *mir.Operand) {
	if op == nil {
		return
	}
	if op.Kind != mir.OperandConst {
		return
	}
	if op.Const.Kind != mir.ConstString {
		return
	}
	raw := op.Const.StringValue
	if _, ok := e.stringConsts[raw]; ok {
		return
	}
	bytes := decodeStringLiteral(raw)
	arrayLen := len(bytes)
	dataLen := len(bytes)
	if arrayLen == 0 {
		arrayLen = 1
	}
	e.stringConsts[raw] = &stringConst{
		raw:      raw,
		bytes:    bytes,
		dataLen:  dataLen,
		arrayLen: arrayLen,
	}
}

func (e *Emitter) emitStringConsts() {
	if len(e.stringConsts) == 0 {
		return
	}
	raws := make([]string, 0, len(e.stringConsts))
	for raw := range e.stringConsts {
		raws = append(raws, raw)
	}
	sort.Strings(raws)
	for idx, raw := range raws {
		sc := e.stringConsts[raw]
		name := fmt.Sprintf(".str.%d", idx)
		sc.globalName = name
		lit := formatLLVMBytes(sc.bytes, sc.arrayLen)
		fmt.Fprintf(&e.buf, "@%s = private unnamed_addr constant [%d x i8] %s\n", name, sc.arrayLen, lit)
	}
	e.buf.WriteString("\n")
}

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
		llvmTy, err := llvmValueType(e.types, g.Type)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.buf, "@%s = global %s zeroinitializer\n", name, llvmTy)
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
	for _, f := range e.mod.Funcs {
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
	for id, f := range e.mod.Funcs {
		if f != nil && f.Name == "__surge_start" {
			roots = append(roots, id)
		}
	}
	if len(roots) == 0 {
		for id := range e.mod.Funcs {
			reachable[id] = struct{}{}
		}
		return reachable
	}
	queue := make([]mir.FuncID, 0, len(roots))
	queue = append(queue, roots...)
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
				if call.Callee.Kind != mir.CalleeSym {
					continue
				}
				if !call.Callee.Sym.IsValid() {
					continue
				}
				if nextID, ok := e.mod.FuncBySym[call.Callee.Sym]; ok {
					if _, seen := reachable[nextID]; !seen {
						queue = append(queue, nextID)
					}
				}
			}
		}
	}
	return reachable
}

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
