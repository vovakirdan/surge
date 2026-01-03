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

func (e *Emitter) paramLocals(f *mir.Func) ([]mir.LocalID, error) {
	if f == nil {
		return nil, nil
	}
	if e.syms == nil || e.syms.Symbols == nil || e.syms.Strings == nil {
		return nil, fmt.Errorf("missing symbol table")
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
	case mir.RValueCast:
		return fe.emitCast(&rv.Cast)
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

func (fe *funcEmitter) emitCall(ins *mir.Instr) error {
	call := &ins.Call
	if call == nil {
		return nil
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
	case mir.TermUnreachable:
		fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
		return nil
	default:
		return fmt.Errorf("unsupported terminator kind %v", term.Kind)
	}
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
	if len(place.Proj) != 0 {
		return "", "", fmt.Errorf("place projections are not supported")
	}
	switch place.Kind {
	case mir.PlaceLocal:
		if name, ok := fe.localAlloca[place.Local]; ok {
			llvmTy, err := llvmValueType(fe.emitter.types, fe.f.Locals[place.Local].Type)
			if err != nil {
				return "", "", err
			}
			return fmt.Sprintf("%%%s", name), llvmTy, nil
		}
		return "", "", fmt.Errorf("unknown local %d", place.Local)
	case mir.PlaceGlobal:
		name := fe.emitter.globalNames[place.Global]
		if name == "" {
			return "", "", fmt.Errorf("unknown global %d", place.Global)
		}
		g := fe.emitter.mod.Globals[place.Global]
		llvmTy, err := llvmValueType(fe.emitter.types, g.Type)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("@%s", name), llvmTy, nil
	default:
		return "", "", fmt.Errorf("unsupported place kind %v", place.Kind)
	}
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
