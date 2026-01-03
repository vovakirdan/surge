package llvm

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/layout"
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
	localAlloca map[mir.LocalID]string
	paramLocals []mir.LocalID
}

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
	if err := e.collectStringConsts(); err != nil {
		return "", err
	}
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
	e.emitGlobals()
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
		e.globalNames[mir.GlobalID(id)] = fmt.Sprintf("g%d", id)
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
			llvmTy, err := llvmValueType(e.types, f.Locals[localID].Type)
			if err != nil {
				return err
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
						params[i] = mir.LocalID(i)
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
					params[i] = mir.LocalID(i)
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
				params[i] = mir.LocalID(i)
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
			params = append(params, mir.LocalID(idx))
		}
	}
	return params, nil
}

func (e *Emitter) collectStringConsts() error {
	if e.mod == nil {
		return nil
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
	return nil
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

func (e *Emitter) emitGlobals() {
	if e.mod == nil {
		return
	}
	if len(e.mod.Globals) == 0 {
		return
	}
	for i, g := range e.mod.Globals {
		name := e.globalNames[mir.GlobalID(i)]
		llvmTy, err := llvmValueType(e.types, g.Type)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(&e.buf, "@%s = global %s zeroinitializer\n", name, llvmTy)
	}
	e.buf.WriteString("\n")
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
		localID := mir.LocalID(i)
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
			if err := fe.emitParamStores(sig); err != nil {
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
		fmt.Fprintf(&fe.emitter.buf, "  %%%s = alloca %s\n", fe.localAlloca[mir.LocalID(i)], llvmTy)
	}
	return nil
}

func (fe *funcEmitter) emitParamStores(sig funcSig) error {
	for i, localID := range fe.paramLocals {
		llvmTy, err := llvmValueType(fe.emitter.types, fe.f.Locals[localID].Type)
		if err != nil {
			return err
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %%p%d, ptr %%%s\n", llvmTy, i, fe.localAlloca[localID])
	}
	return nil
}

func (fe *funcEmitter) emitInstr(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	switch ins.Kind {
	case mir.InstrAssign:
		return fe.emitAssign(ins)
	case mir.InstrCall:
		return fe.emitCall(ins)
	case mir.InstrDrop, mir.InstrEndBorrow, mir.InstrNop:
		return nil
	default:
		return fmt.Errorf("unsupported instruction kind %v", ins.Kind)
	}
}

func (fe *funcEmitter) emitAssign(ins *mir.Instr) error {
	val, ty, err := fe.emitRValue(&ins.Assign.Src)
	if err != nil {
		return err
	}
	ptr, dstTy, err := fe.emitPlacePtr(ins.Assign.Dst)
	if err != nil {
		return err
	}
	if dstTy != ty {
		ty = dstTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
	return nil
}

func (fe *funcEmitter) emitRValue(rv *mir.RValue) (string, string, error) {
	if rv == nil {
		return "", "", fmt.Errorf("nil rvalue")
	}
	switch rv.Kind {
	case mir.RValueUse:
		return fe.emitOperand(&rv.Use)
	case mir.RValueUnaryOp:
		return fe.emitUnary(&rv.Unary)
	case mir.RValueBinaryOp:
		return fe.emitBinary(&rv.Binary)
	case mir.RValueCast:
		return fe.emitCast(&rv.Cast)
	case mir.RValueTagTest:
		return fe.emitTagTest(&rv.TagTest)
	case mir.RValueTagPayload:
		return fe.emitTagPayload(&rv.TagPayload)
	default:
		return "", "", fmt.Errorf("unsupported rvalue kind %v", rv.Kind)
	}
}

func (fe *funcEmitter) emitCast(c *mir.CastOp) (string, string, error) {
	if c == nil {
		return "", "", fmt.Errorf("nil cast")
	}
	val, srcTy, err := fe.emitOperand(&c.Value)
	if err != nil {
		return "", "", err
	}
	dstTy, err := llvmValueType(fe.emitter.types, c.TargetTy)
	if err != nil {
		return "", "", err
	}
	if srcTy == dstTy {
		return val, dstTy, nil
	}
	srcInfo, srcOK := intInfo(fe.emitter.types, c.Value.Type)
	dstInfo, dstOK := intInfo(fe.emitter.types, c.TargetTy)
	if srcOK && dstOK {
		if srcInfo.bits < dstInfo.bits {
			op := "zext"
			if srcInfo.signed {
				op = "sext"
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to %s\n", tmp, op, srcTy, val, dstTy)
			return tmp, dstTy, nil
		}
		if srcInfo.bits > dstInfo.bits {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = trunc %s %s to %s\n", tmp, srcTy, val, dstTy)
			return tmp, dstTy, nil
		}
		return val, dstTy, nil
	}
	return "", "", fmt.Errorf("unsupported cast to %s", dstTy)
}

func (fe *funcEmitter) emitUnary(op *mir.UnaryOp) (string, string, error) {
	if op == nil {
		return "", "", fmt.Errorf("nil unary op")
	}
	switch op.Op {
	case ast.ExprUnaryPlus:
		return fe.emitValueOperand(&op.Operand)
	case ast.ExprUnaryMinus:
		val, ty, err := fe.emitValueOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		info, ok := intInfo(fe.emitter.types, op.Operand.Type)
		if !ok || !info.signed {
			return "", "", fmt.Errorf("unsupported unary minus type")
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = sub %s 0, %s\n", tmp, ty, val)
		return tmp, ty, nil
	case ast.ExprUnaryNot:
		val, ty, err := fe.emitValueOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		if ty != "i1" {
			return "", "", fmt.Errorf("unary not requires i1, got %s", ty)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, val)
		return tmp, "i1", nil
	case ast.ExprUnaryDeref:
		ptrVal, _, err := fe.emitOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		elemType, ok := derefType(fe.emitter.types, op.Operand.Type)
		if !ok {
			return "", "", fmt.Errorf("unsupported deref type")
		}
		elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, elemLLVM, ptrVal)
		return tmp, elemLLVM, nil
	default:
		return "", "", fmt.Errorf("unsupported unary op %v", op.Op)
	}
}

func (fe *funcEmitter) emitBinary(op *mir.BinaryOp) (string, string, error) {
	if op == nil {
		return "", "", fmt.Errorf("nil binary op")
	}
	if isStringLike(fe.emitter.types, op.Left.Type) || isStringLike(fe.emitter.types, op.Right.Type) {
		if !isStringLike(fe.emitter.types, op.Left.Type) || !isStringLike(fe.emitter.types, op.Right.Type) {
			return "", "", fmt.Errorf("mixed string and non-string operands")
		}
		return fe.emitStringBinary(op)
	}
	leftVal, leftTy, err := fe.emitValueOperand(&op.Left)
	if err != nil {
		return "", "", err
	}
	rightVal, rightTy, err := fe.emitValueOperand(&op.Right)
	if err != nil {
		return "", "", err
	}
	if leftTy != rightTy {
		return "", "", fmt.Errorf("binary operand type mismatch: %s vs %s", leftTy, rightTy)
	}

	switch op.Op {
	case ast.ExprBinaryLogicalAnd:
		if leftTy != "i1" {
			return "", "", fmt.Errorf("logical and requires i1, got %s", leftTy)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", tmp, leftVal, rightVal)
		return tmp, "i1", nil
	case ast.ExprBinaryLogicalOr:
		if leftTy != "i1" {
			return "", "", fmt.Errorf("logical or requires i1, got %s", leftTy)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", tmp, leftVal, rightVal)
		return tmp, "i1", nil
	case ast.ExprBinaryAdd, ast.ExprBinarySub, ast.ExprBinaryMul, ast.ExprBinaryDiv, ast.ExprBinaryMod,
		ast.ExprBinaryBitAnd, ast.ExprBinaryBitOr, ast.ExprBinaryBitXor, ast.ExprBinaryShiftLeft, ast.ExprBinaryShiftRight:
		info, ok := intInfo(fe.emitter.types, op.Left.Type)
		if !ok {
			return "", "", fmt.Errorf("unsupported numeric op on type")
		}
		var opcode string
		switch op.Op {
		case ast.ExprBinaryAdd:
			opcode = "add"
		case ast.ExprBinarySub:
			opcode = "sub"
		case ast.ExprBinaryMul:
			opcode = "mul"
		case ast.ExprBinaryDiv:
			if info.signed {
				opcode = "sdiv"
			} else {
				opcode = "udiv"
			}
		case ast.ExprBinaryMod:
			if info.signed {
				opcode = "srem"
			} else {
				opcode = "urem"
			}
		case ast.ExprBinaryBitAnd:
			opcode = "and"
		case ast.ExprBinaryBitOr:
			opcode = "or"
		case ast.ExprBinaryBitXor:
			opcode = "xor"
		case ast.ExprBinaryShiftLeft:
			opcode = "shl"
		case ast.ExprBinaryShiftRight:
			if info.signed {
				opcode = "ashr"
			} else {
				opcode = "lshr"
			}
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
		return tmp, leftTy, nil
	case ast.ExprBinaryEq, ast.ExprBinaryNotEq, ast.ExprBinaryLess, ast.ExprBinaryLessEq, ast.ExprBinaryGreater, ast.ExprBinaryGreaterEq:
		return fe.emitCompare(op, leftVal, rightVal, leftTy)
	default:
		return "", "", fmt.Errorf("unsupported binary op %v", op.Op)
	}
}

func (fe *funcEmitter) emitCompare(op *mir.BinaryOp, leftVal, rightVal, leftTy string) (string, string, error) {
	info, ok := intInfo(fe.emitter.types, op.Left.Type)
	if !ok && leftTy != "ptr" {
		return "", "", fmt.Errorf("unsupported compare type")
	}
	if leftTy == "ptr" {
		pred := "eq"
		if op.Op == ast.ExprBinaryNotEq {
			pred = "ne"
		} else if op.Op != ast.ExprBinaryEq {
			return "", "", fmt.Errorf("unsupported pointer comparison %v", op.Op)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s ptr %s, %s\n", tmp, pred, leftVal, rightVal)
		return tmp, "i1", nil
	}
	pred := ""
	switch op.Op {
	case ast.ExprBinaryEq:
		pred = "eq"
	case ast.ExprBinaryNotEq:
		pred = "ne"
	case ast.ExprBinaryLess:
		if info.signed {
			pred = "slt"
		} else {
			pred = "ult"
		}
	case ast.ExprBinaryLessEq:
		if info.signed {
			pred = "sle"
		} else {
			pred = "ule"
		}
	case ast.ExprBinaryGreater:
		if info.signed {
			pred = "sgt"
		} else {
			pred = "ugt"
		}
	case ast.ExprBinaryGreaterEq:
		if info.signed {
			pred = "sge"
		} else {
			pred = "uge"
		}
	default:
		return "", "", fmt.Errorf("unsupported compare op %v", op.Op)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitStringBinary(op *mir.BinaryOp) (string, string, error) {
	leftPtr, _, err := fe.emitOperandAddr(&op.Left)
	if err != nil {
		return "", "", err
	}
	rightPtr, _, err := fe.emitOperandAddr(&op.Right)
	if err != nil {
		return "", "", err
	}
	switch op.Op {
	case ast.ExprBinaryAdd:
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_concat(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
		return tmp, "ptr", nil
	case ast.ExprBinaryEq:
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
		return tmp, "i1", nil
	case ast.ExprBinaryNotEq:
		eqTmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", eqTmp, leftPtr, rightPtr)
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, eqTmp)
		return tmp, "i1", nil
	default:
		return "", "", fmt.Errorf("unsupported string op %v", op.Op)
	}
}

func (fe *funcEmitter) emitTagTest(tt *mir.TagTest) (string, string, error) {
	if tt == nil {
		return "", "", fmt.Errorf("nil tag test")
	}
	tagVal, err := fe.emitTagDiscriminant(&tt.Value)
	if err != nil {
		return "", "", err
	}
	idx, err := fe.emitter.tagCaseIndex(tt.Value.Type, tt.TagName, symbols.NoSymbolID)
	if err != nil {
		return "", "", err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", tmp, tagVal, idx)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitTagPayload(tp *mir.TagPayload) (string, string, error) {
	if tp == nil {
		return "", "", fmt.Errorf("nil tag payload")
	}
	_, meta, err := fe.emitter.tagCaseMeta(tp.Value.Type, tp.TagName, symbols.NoSymbolID)
	if err != nil {
		return "", "", err
	}
	if tp.Index < 0 || tp.Index >= len(meta.PayloadTypes) {
		return "", "", fmt.Errorf("tag payload index out of range")
	}
	layoutInfo, err := fe.emitter.layoutOf(tp.Value.Type)
	if err != nil {
		return "", "", err
	}
	payloadOffsets, err := fe.emitter.payloadOffsets(meta.PayloadTypes)
	if err != nil {
		return "", "", err
	}
	offset := layoutInfo.PayloadOffset + payloadOffsets[tp.Index]
	basePtr, baseTy, err := fe.emitValueOperand(&tp.Value)
	if err != nil {
		return "", "", err
	}
	if baseTy != "ptr" {
		return "", "", fmt.Errorf("tag payload requires ptr base, got %s", baseTy)
	}
	payloadType := meta.PayloadTypes[tp.Index]
	payloadLLVM, err := llvmValueType(fe.emitter.types, payloadType)
	if err != nil {
		return "", "", err
	}
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, basePtr, offset)
	val := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", val, payloadLLVM, bytePtr)
	return val, payloadLLVM, nil
}

func (fe *funcEmitter) emitCall(ins *mir.Instr) error {
	call := &ins.Call
	if call == nil {
		return nil
	}
	if handled, err := fe.emitTagConstructor(call); handled {
		return err
	}
	if handled, err := fe.emitLenIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitMagicIntrinsic(call); handled {
		return err
	}
	callee, sig, err := fe.resolveCallee(call)
	if err != nil {
		return err
	}
	args := make([]string, 0, len(call.Args))
	for i := range call.Args {
		val, ty, err := fe.emitOperand(&call.Args[i])
		if err != nil {
			return err
		}
		args = append(args, fmt.Sprintf("%s %s", ty, val))
	}
	callStmt := fmt.Sprintf("call %s @%s(%s)", sig.ret, callee, strings.Join(args, ", "))
	if call.HasDst {
		if sig.ret == "void" {
			return fmt.Errorf("call has destination but returns void: %s", callee)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s\n", tmp, callStmt)
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != sig.ret {
			dstTy = sig.ret
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  %s\n", callStmt)
	return nil
}

func (fe *funcEmitter) emitTagConstructor(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym || !call.Callee.Sym.IsValid() {
		return false, nil
	}
	sym := fe.emitter.symFor(call.Callee.Sym)
	if sym == nil || sym.Kind != symbols.SymbolTag {
		return false, nil
	}
	if !call.HasDst {
		return true, fmt.Errorf("tag constructor requires a destination")
	}
	dstType := fe.f.Locals[call.Dst.Local].Type
	tagName := call.Callee.Name
	if tagName == "" {
		tagName = fe.symbolName(call.Callee.Sym)
	}
	ptrVal, err := fe.emitTagValue(dstType, tagName, call.Callee.Sym, call.Args)
	if err != nil {
		return true, err
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, ptrVal, ptr)
	return true, nil
}

func (fe *funcEmitter) emitLenIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "__len" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("__len requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	if !isStringLike(fe.emitter.types, call.Args[0].Type) {
		return true, fmt.Errorf("unsupported __len target")
	}
	argVal, _, err := fe.emitOperand(&call.Args[0])
	if err != nil {
		return true, err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_string_len(ptr %s)\n", tmp, argVal)
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != "i64" {
		dstTy = "i64"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return true, nil
}

func (fe *funcEmitter) emitMagicIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod",
		"__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr",
		"__eq", "__ne", "__lt", "__le", "__gt", "__ge":
		return true, fe.emitMagicBinaryIntrinsic(call, name)
	case "__pos", "__neg", "__not":
		return true, fe.emitMagicUnaryIntrinsic(call, name)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitMagicBinaryIntrinsic(call *mir.CallInstr, name string) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("%s requires 2 arguments", name)
	}
	if !call.HasDst {
		return nil
	}
	leftType := operandValueType(fe.emitter.types, &call.Args[0])
	rightType := operandValueType(fe.emitter.types, &call.Args[1])
	if isStringLike(fe.emitter.types, leftType) || isStringLike(fe.emitter.types, rightType) {
		if !isStringLike(fe.emitter.types, leftType) || !isStringLike(fe.emitter.types, rightType) {
			return fmt.Errorf("mixed string and non-string operands")
		}
		leftPtr, _, err := fe.emitOperandAddr(&call.Args[0])
		if err != nil {
			return err
		}
		rightPtr, _, err := fe.emitOperandAddr(&call.Args[1])
		if err != nil {
			return err
		}
		tmp := fe.nextTemp()
		resultTy := ""
		switch name {
		case "__add":
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_concat(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
			resultTy = "ptr"
		case "__eq":
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
			resultTy = "i1"
		case "__ne":
			eqTmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", eqTmp, leftPtr, rightPtr)
			fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, eqTmp)
			resultTy = "i1"
		default:
			return fmt.Errorf("unsupported string op %s", name)
		}
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != resultTy {
			dstTy = resultTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}

	leftVal, leftTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	rightVal, rightTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if leftTy != rightTy {
		return fmt.Errorf("binary operand type mismatch: %s vs %s", leftTy, rightTy)
	}
	info, ok := intInfo(fe.emitter.types, leftType)
	if !ok && leftTy != "ptr" {
		return fmt.Errorf("unsupported numeric op on type")
	}
	resultTy := leftTy
	tmp := fe.nextTemp()
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod", "__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr":
		if !ok {
			return fmt.Errorf("unsupported numeric op on type")
		}
		opcode := ""
		switch name {
		case "__add":
			opcode = "add"
		case "__sub":
			opcode = "sub"
		case "__mul":
			opcode = "mul"
		case "__div":
			if info.signed {
				opcode = "sdiv"
			} else {
				opcode = "udiv"
			}
		case "__mod":
			if info.signed {
				opcode = "srem"
			} else {
				opcode = "urem"
			}
		case "__bit_and":
			opcode = "and"
		case "__bit_or":
			opcode = "or"
		case "__bit_xor":
			opcode = "xor"
		case "__shl":
			opcode = "shl"
		case "__shr":
			if info.signed {
				opcode = "ashr"
			} else {
				opcode = "lshr"
			}
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
	case "__eq", "__ne", "__lt", "__le", "__gt", "__ge":
		pred := ""
		switch name {
		case "__eq":
			pred = "eq"
		case "__ne":
			pred = "ne"
		case "__lt":
			if info.signed {
				pred = "slt"
			} else {
				pred = "ult"
			}
		case "__le":
			if info.signed {
				pred = "sle"
			} else {
				pred = "ule"
			}
		case "__gt":
			if info.signed {
				pred = "sgt"
			} else {
				pred = "ugt"
			}
		case "__ge":
			if info.signed {
				pred = "sge"
			} else {
				pred = "uge"
			}
		}
		if leftTy == "ptr" {
			if name != "__eq" && name != "__ne" {
				return fmt.Errorf("unsupported pointer comparison %s", name)
			}
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s ptr %s, %s\n", tmp, pred, leftVal, rightVal)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
		}
		resultTy = "i1"
	default:
		return fmt.Errorf("unsupported magic binary op %s", name)
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != resultTy {
		dstTy = resultTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func (fe *funcEmitter) emitMagicUnaryIntrinsic(call *mir.CallInstr, name string) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("%s requires 1 argument", name)
	}
	if !call.HasDst {
		return nil
	}
	val, ty, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	switch name {
	case "__pos":
		tmp = val
	case "__neg":
		info, ok := intInfo(fe.emitter.types, operandValueType(fe.emitter.types, &call.Args[0]))
		if !ok || !info.signed {
			return fmt.Errorf("unsupported unary minus type")
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = sub %s 0, %s\n", tmp, ty, val)
	case "__not":
		if ty != "i1" {
			return fmt.Errorf("unary not requires i1, got %s", ty)
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, val)
	default:
		return fmt.Errorf("unsupported magic unary op %s", name)
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != ty {
		dstTy = ty
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func (fe *funcEmitter) resolveCallee(call *mir.CallInstr) (string, funcSig, error) {
	if call == nil {
		return "", funcSig{}, fmt.Errorf("nil call")
	}
	if call.Callee.Kind == mir.CalleeSym {
		if call.Callee.Sym.IsValid() {
			if fe.emitter.mod != nil {
				if id, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
					name := fe.emitter.funcNames[id]
					sig := fe.emitter.funcSigs[id]
					return name, sig, nil
				}
			}
		}
		name := call.Callee.Name
		if name == "" {
			name = fe.symbolName(call.Callee.Sym)
		}
		if sig, ok := fe.emitter.runtimeSigs[name]; ok {
			return name, sig, nil
		}
		return "", funcSig{}, fmt.Errorf("unknown external function %q", name)
	}
	return "", funcSig{}, fmt.Errorf("unsupported callee kind %v", call.Callee.Kind)
}

func (fe *funcEmitter) symbolName(symID symbols.SymbolID) string {
	if !symID.IsValid() || fe.emitter.syms == nil || fe.emitter.syms.Symbols == nil || fe.emitter.syms.Strings == nil {
		return ""
	}
	sym := fe.emitter.syms.Symbols.Get(symID)
	if sym == nil {
		return ""
	}
	return fe.emitter.syms.Strings.MustLookup(sym.Name)
}

func stripGenericSuffix(name string) string {
	if idx := strings.Index(name, "::<"); idx >= 0 {
		return name[:idx]
	}
	return name
}

func (fe *funcEmitter) emitTerminator(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	switch term.Kind {
	case mir.TermReturn:
		if term.Return.HasValue {
			val, ty, err := fe.emitOperand(&term.Return.Value)
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  ret %s %s\n", ty, val)
			return nil
		}
		fmt.Fprintf(&fe.emitter.buf, "  ret void\n")
		return nil
	case mir.TermGoto:
		fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", term.Goto.Target)
		return nil
	case mir.TermIf:
		condVal, condTy, err := fe.emitOperand(&term.If.Cond)
		if err != nil {
			return err
		}
		if condTy != "i1" {
			return fmt.Errorf("if condition must be i1, got %s", condTy)
		}
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb%d\n", condVal, term.If.Then, term.If.Else)
		return nil
	case mir.TermSwitchTag:
		return fe.emitSwitchTag(&term.SwitchTag)
	case mir.TermUnreachable:
		fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
		return nil
	default:
		return fmt.Errorf("unsupported terminator kind %v", term.Kind)
	}
}

func (fe *funcEmitter) emitSwitchTag(term *mir.SwitchTagTerm) error {
	if term == nil {
		return nil
	}
	tagVal, err := fe.emitTagDiscriminant(&term.Value)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%bb%d [\n", tagVal, term.Default)
	for _, c := range term.Cases {
		idx, err := fe.emitter.tagCaseIndex(term.Value.Type, c.TagName, symbols.NoSymbolID)
		if err != nil {
			return err
		}
		fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%bb%d\n", idx, c.Target)
	}
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	return nil
}

func (fe *funcEmitter) emitOperand(op *mir.Operand) (string, string, error) {
	if op == nil {
		return "", "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandConst:
		return fe.emitConst(&op.Const)
	case mir.OperandCopy, mir.OperandMove:
		ptr, ty, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, ty, ptr)
		return tmp, ty, nil
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		return ptr, "ptr", nil
	default:
		return "", "", fmt.Errorf("unsupported operand kind %v", op.Kind)
	}
}

func (fe *funcEmitter) emitValueOperand(op *mir.Operand) (string, string, error) {
	if op == nil {
		return "", "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		elemType, ok := derefType(fe.emitter.types, op.Type)
		if !ok {
			return "", "", fmt.Errorf("unsupported address-of operand type")
		}
		llvmTy, err := llvmValueType(fe.emitter.types, elemType)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, llvmTy, ptr)
		return tmp, llvmTy, nil
	default:
		return fe.emitOperand(op)
	}
}

func (fe *funcEmitter) emitOperandAddr(op *mir.Operand) (string, string, error) {
	if op == nil {
		return "", "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut, mir.OperandCopy, mir.OperandMove:
		ptr, ty, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		return ptr, ty, nil
	case mir.OperandConst:
		val, ty, err := fe.emitConst(&op.Const)
		if err != nil {
			return "", "", err
		}
		ptr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca %s\n", ptr, ty)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
		return ptr, ty, nil
	default:
		return "", "", fmt.Errorf("unsupported operand kind %v", op.Kind)
	}
}

func (fe *funcEmitter) emitConst(c *mir.Const) (string, string, error) {
	if c == nil {
		return "", "", fmt.Errorf("nil const")
	}
	switch c.Kind {
	case mir.ConstInt:
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%d", c.IntValue), ty, nil
	case mir.ConstUint:
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%d", c.UintValue), ty, nil
	case mir.ConstBool:
		return boolValue(c.BoolValue), "i1", nil
	case mir.ConstNothing:
		if fe.emitter.hasTagLayout(c.Type) {
			ptr, err := fe.emitTagValue(c.Type, "nothing", symbols.NoSymbolID, nil)
			if err != nil {
				return "", "", err
			}
			return ptr, "ptr", nil
		}
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		return "0", ty, nil
	case mir.ConstString:
		return fe.emitStringConst(c.StringValue)
	default:
		return "", "", fmt.Errorf("unsupported const kind %v", c.Kind)
	}
}

func (fe *funcEmitter) emitStringConst(raw string) (string, string, error) {
	sc, ok := fe.emitter.stringConsts[raw]
	if !ok {
		return "", "", fmt.Errorf("missing string const %q", raw)
	}
	ptrTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i8], ptr @%s, i64 0, i64 0\n", ptrTmp, sc.arrayLen, sc.globalName)
	handleTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_bytes(ptr %s, i64 %d)\n", handleTmp, ptrTmp, sc.dataLen)
	return handleTmp, "ptr", nil
}

func (fe *funcEmitter) emitPlacePtr(place mir.Place) (string, string, error) {
	if fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	var curPtr string
	var curType types.TypeID
	switch place.Kind {
	case mir.PlaceLocal:
		name, ok := fe.localAlloca[place.Local]
		if !ok {
			return "", "", fmt.Errorf("unknown local %d", place.Local)
		}
		curPtr = fmt.Sprintf("%%%s", name)
		curType = fe.f.Locals[place.Local].Type
	case mir.PlaceGlobal:
		name := fe.emitter.globalNames[place.Global]
		if name == "" {
			return "", "", fmt.Errorf("unknown global %d", place.Global)
		}
		curPtr = fmt.Sprintf("@%s", name)
		curType = fe.emitter.mod.Globals[place.Global].Type
	default:
		return "", "", fmt.Errorf("unsupported place kind %v", place.Kind)
	}
	curLLVMType, err := llvmValueType(fe.emitter.types, curType)
	if err != nil {
		return "", "", err
	}

	for _, proj := range place.Proj {
		switch proj.Kind {
		case mir.PlaceProjDeref:
			if curLLVMType != "ptr" {
				return "", "", fmt.Errorf("deref requires pointer type, got %s", curLLVMType)
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, curPtr)
			nextType, ok := derefType(fe.emitter.types, curType)
			if !ok {
				return "", "", fmt.Errorf("unsupported deref type")
			}
			curPtr = tmp
			curType = nextType
			curLLVMType, err = llvmValueType(fe.emitter.types, curType)
			if err != nil {
				return "", "", err
			}
		case mir.PlaceProjField:
			fieldIdx, fieldType, err := fe.structFieldInfo(curType, proj)
			if err != nil {
				return "", "", err
			}
			layout, err := fe.emitter.layoutOf(curType)
			if err != nil {
				return "", "", err
			}
			if fieldIdx < 0 || fieldIdx >= len(layout.FieldOffsets) {
				return "", "", fmt.Errorf("field index %d out of range", fieldIdx)
			}
			off := layout.FieldOffsets[fieldIdx]
			base := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", base, curLLVMType, curPtr)
			bytePtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, base, off)
			fieldLLVMType, err := llvmValueType(fe.emitter.types, fieldType)
			if err != nil {
				return "", "", err
			}
			curPtr = bytePtr
			curType = fieldType
			curLLVMType = fieldLLVMType
		case mir.PlaceProjIndex:
			return "", "", fmt.Errorf("index projections are not supported")
		default:
			return "", "", fmt.Errorf("unsupported place projection kind %v", proj.Kind)
		}
	}

	return curPtr, curLLVMType, nil
}

func (fe *funcEmitter) nextTemp() string {
	fe.tmpID++
	return fmt.Sprintf("%%t%d", fe.tmpID)
}

func boolValue(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

type intMeta struct {
	bits   int
	signed bool
}

func intInfo(typesIn *types.Interner, id types.TypeID) (intMeta, bool) {
	if typesIn == nil {
		return intMeta{}, false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return intMeta{}, false
	}
	switch tt.Kind {
	case types.KindBool:
		return intMeta{bits: 1, signed: false}, true
	case types.KindInt:
		return intMeta{bits: widthBits(tt.Width), signed: true}, true
	case types.KindUint:
		return intMeta{bits: widthBits(tt.Width), signed: false}, true
	default:
		return intMeta{}, false
	}
}

func widthBits(width types.Width) int {
	if width == types.WidthAny {
		return 64
	}
	return int(width)
}

func formatLLVMBytes(data []byte, arrayLen int) string {
	var sb strings.Builder
	sb.WriteString("c\"")
	for i := 0; i < arrayLen; i++ {
		b := byte(0)
		if i < len(data) {
			b = data[i]
		}
		fmt.Fprintf(&sb, "\\%02X", b)
	}
	sb.WriteString("\"")
	return sb.String()
}

func decodeStringLiteral(raw string) []byte {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch != '\\' {
			out = append(out, ch)
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		i++
		switch raw[i] {
		case '\\':
			out = append(out, '\\')
		case '"':
			out = append(out, '"')
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		default:
			out = append(out, raw[i])
		}
	}
	return out
}

func (e *Emitter) symFor(symID symbols.SymbolID) *symbols.Symbol {
	if e == nil || e.syms == nil || e.syms.Symbols == nil {
		return nil
	}
	if !symID.IsValid() {
		return nil
	}
	return e.syms.Symbols.Get(symID)
}

func (e *Emitter) layoutOf(id types.TypeID) (layout.TypeLayout, error) {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.Layout == nil {
		return layout.TypeLayout{}, fmt.Errorf("missing layout engine")
	}
	return e.mod.Meta.Layout.LayoutOf(id)
}

func (e *Emitter) hasTagLayout(id types.TypeID) bool {
	if e == nil || e.mod == nil || e.mod.Meta == nil || len(e.mod.Meta.TagLayouts) == 0 {
		return false
	}
	id = resolveAliasAndOwn(e.types, id)
	_, ok := e.mod.Meta.TagLayouts[id]
	return ok
}

func (e *Emitter) tagCases(id types.TypeID) ([]mir.TagCaseMeta, error) {
	if e == nil || e.mod == nil || e.mod.Meta == nil || len(e.mod.Meta.TagLayouts) == 0 {
		return nil, fmt.Errorf("missing tag layout metadata")
	}
	id = resolveAliasAndOwn(e.types, id)
	cases, ok := e.mod.Meta.TagLayouts[id]
	if !ok {
		return nil, fmt.Errorf("missing tag layout for type#%d", id)
	}
	return cases, nil
}

func (e *Emitter) canonicalTagSym(sym symbols.SymbolID) symbols.SymbolID {
	if !sym.IsValid() || e == nil || e.mod == nil || e.mod.Meta == nil || len(e.mod.Meta.TagAliases) == 0 {
		return sym
	}
	if orig, ok := e.mod.Meta.TagAliases[sym]; ok && orig.IsValid() {
		return orig
	}
	return sym
}

func (e *Emitter) tagCaseMeta(id types.TypeID, tagName string, tagSym symbols.SymbolID) (int, mir.TagCaseMeta, error) {
	cases, err := e.tagCases(id)
	if err != nil {
		return -1, mir.TagCaseMeta{}, err
	}
	tagSym = e.canonicalTagSym(tagSym)
	if tagSym.IsValid() {
		for i, c := range cases {
			if c.TagSym == tagSym {
				return i, c, nil
			}
		}
	}
	if tagName != "" {
		for i, c := range cases {
			if c.TagName == tagName {
				return i, c, nil
			}
		}
	}
	return -1, mir.TagCaseMeta{}, fmt.Errorf("unknown tag %q", tagName)
}

func (e *Emitter) tagCaseIndex(id types.TypeID, tagName string, tagSym symbols.SymbolID) (int, error) {
	idx, _, err := e.tagCaseMeta(id, tagName, tagSym)
	return idx, err
}

func (e *Emitter) payloadOffsets(payload []types.TypeID) ([]int, error) {
	offsets := make([]int, len(payload))
	size := 0
	for i, t := range payload {
		layoutInfo, err := e.layoutOf(t)
		if err != nil {
			return nil, err
		}
		align := layoutInfo.Align
		if align <= 0 {
			align = 1
		}
		size = roundUpInt(size, align)
		offsets[i] = size
		size += layoutInfo.Size
	}
	return offsets, nil
}

func (fe *funcEmitter) emitTagDiscriminant(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	layoutInfo, err := fe.emitter.layoutOf(op.Type)
	if err != nil {
		return "", err
	}
	if layoutInfo.TagSize != 4 {
		return "", fmt.Errorf("unsupported tag size %d", layoutInfo.TagSize)
	}
	val, valTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	if valTy != "ptr" {
		return "", fmt.Errorf("tag value must be ptr, got %s", valTy)
	}
	tagVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s\n", tagVal, val)
	return tagVal, nil
}

func (fe *funcEmitter) emitTagValue(typeID types.TypeID, tagName string, tagSym symbols.SymbolID, args []mir.Operand) (string, error) {
	if typeID == types.NoTypeID {
		return "", fmt.Errorf("missing tag type")
	}
	caseIdx, meta, err := fe.emitter.tagCaseMeta(typeID, tagName, tagSym)
	if err != nil {
		return "", err
	}
	if len(args) != len(meta.PayloadTypes) {
		return "", fmt.Errorf("tag %q expects %d payload value(s), got %d", meta.TagName, len(meta.PayloadTypes), len(args))
	}
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", err
	}
	if layoutInfo.TagSize != 4 {
		return "", fmt.Errorf("unsupported tag size %d", layoutInfo.TagSize)
	}
	size := layoutInfo.Size
	align := layoutInfo.Align
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s\n", caseIdx, mem)

	if len(meta.PayloadTypes) > 0 {
		offsets, err := fe.emitter.payloadOffsets(meta.PayloadTypes)
		if err != nil {
			return "", err
		}
		for i, arg := range args {
			val, valTy, err := fe.emitValueOperand(&arg)
			if err != nil {
				return "", err
			}
			payloadTy := meta.PayloadTypes[i]
			payloadLLVM, err := llvmValueType(fe.emitter.types, payloadTy)
			if err != nil {
				return "", err
			}
			if valTy != payloadLLVM {
				valTy = payloadLLVM
			}
			off := layoutInfo.PayloadOffset + offsets[i]
			bytePtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
		}
	}
	return mem, nil
}

func (fe *funcEmitter) structFieldInfo(typeID types.TypeID, proj mir.PlaceProj) (int, types.TypeID, error) {
	if fe.emitter.types == nil {
		return -1, types.NoTypeID, fmt.Errorf("missing type interner")
	}
	typeID = resolveAliasAndOwn(fe.emitter.types, typeID)
	info, ok := fe.emitter.types.StructInfo(typeID)
	if !ok || info == nil {
		return -1, types.NoTypeID, fmt.Errorf("missing struct info")
	}
	fieldIdx := proj.FieldIdx
	if fieldIdx < 0 && proj.FieldName != "" && fe.emitter.types.Strings != nil {
		for i, field := range info.Fields {
			if fe.emitter.types.Strings.MustLookup(field.Name) == proj.FieldName {
				fieldIdx = i
				break
			}
		}
	}
	if fieldIdx < 0 || fieldIdx >= len(info.Fields) {
		return -1, types.NoTypeID, fmt.Errorf("unknown field %q", proj.FieldName)
	}
	return fieldIdx, info.Fields[fieldIdx].Type, nil
}

func resolveValueType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for i := 0; i < 32 && id != types.NoTypeID; i++ {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func isStringLike(typesIn *types.Interner, id types.TypeID) bool {
	id = resolveValueType(typesIn, id)
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindString
}

func operandValueType(typesIn *types.Interner, op *mir.Operand) types.TypeID {
	if op == nil {
		return types.NoTypeID
	}
	if op.Kind == mir.OperandAddrOf || op.Kind == mir.OperandAddrOfMut {
		if next, ok := derefType(typesIn, op.Type); ok {
			return next
		}
	}
	return op.Type
}

func isIntLike(typesIn *types.Interner, id types.TypeID) bool {
	id = resolveValueType(typesIn, id)
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := typesIn.Lookup(id)
	return ok && (tt.Kind == types.KindInt || tt.Kind == types.KindUint)
}

func derefType(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool) {
	if typesIn == nil || id == types.NoTypeID {
		return types.NoTypeID, false
	}
	for i := 0; i < 32 && id != types.NoTypeID; i++ {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return types.NoTypeID, false
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return types.NoTypeID, false
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			return tt.Elem, true
		default:
			return types.NoTypeID, false
		}
	}
	return types.NoTypeID, false
}

func roundUpInt(n, align int) int {
	if align <= 1 {
		return n
	}
	rem := n % align
	if rem == 0 {
		return n
	}
	return n + (align - rem)
}
