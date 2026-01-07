package llvm

import (
	"fmt"
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

// Emitter generates LLVM IR from MIR.
type Emitter struct {
	mod          *mir.Module
	types        *types.Interner
	syms         *symbols.Table
	buf          strings.Builder
	stringConsts map[string]*stringConst
	fnRefs       map[mir.FuncID]struct{}
	funcNames    map[mir.FuncID]string
	funcSigs     map[mir.FuncID]funcSig
	globalNames  map[mir.GlobalID]string
	runtimeSigs  map[string]funcSig
	paramCounts  map[mir.FuncID]int
}

type funcEmitter struct {
	emitter         *Emitter
	f               *mir.Func
	tmpID           int
	inlineBlock     int
	localAlloca     map[mir.LocalID]string
	paramLocals     []mir.LocalID
	blockTerminated bool
}

const (
	arrayHeaderSize  = 24
	arrayHeaderAlign = 8
	arrayLenOffset   = 0
	arrayCapOffset   = 8
	arrayDataOffset  = 16
)

// EmitModule converts a MIR module into an LLVM IR string.
func EmitModule(mod *mir.Module, typesIn *types.Interner, symTable *symbols.Table) (string, error) {
	e := &Emitter{
		mod:          mod,
		types:        typesIn,
		syms:         symTable,
		stringConsts: make(map[string]*stringConst),
		fnRefs:       make(map[mir.FuncID]struct{}),
		funcNames:    make(map[mir.FuncID]string),
		funcSigs:     make(map[mir.FuncID]funcSig),
		globalNames:  make(map[mir.GlobalID]string),
		runtimeSigs:  runtimeSigMap(),
	}
	if mod == nil {
		return "", nil
	}
	e.collectStringConsts()
	e.ensureStringConst("parse error")
	e.ensureStringConst("failed to parse \\\"")
	e.ensureStringConst("\\\" as int: invalid numeric format: \\\"")
	e.ensureStringConst("\\\" as uint: invalid numeric format: \\\"")
	e.ensureStringConst("\\\" as float: invalid numeric format: \\\"")
	e.ensureStringConst("\\\"")
	e.ensureStringConst("\n")
	e.ensureStringConst("true")
	e.ensureStringConst("false")
	e.ensureStringConst("")
	e.ensureStringConst("integer overflow")
	e.ensureStringConst("unsigned overflow")
	e.ensureStringConst("float overflow")
	e.ensureStringConst("cannot convert negative int to uint")
	e.ensureStringConst("array capacity out of range")
	e.ensureStringConst("exit code out of range")
	e.ensureStringConst("bytes view length out of range")
	e.ensureStringConst("string repeat count out of range")
	e.ensureStringConst("sleep duration out of range")
	e.ensureStringConst("timeout duration out of range")
	e.ensureStringConst("channel capacity out of range")
	e.ensureStringConst("alloc size out of range")
	e.ensureStringConst("alloc align out of range")
	e.ensureStringConst("free size out of range")
	e.ensureStringConst("free align out of range")
	e.ensureStringConst("old size out of range")
	e.ensureStringConst("new size out of range")
	e.ensureStringConst("realloc align out of range")
	e.ensureStringConst("memcpy length out of range")
	e.ensureStringConst("memmove length out of range")
	e.ensureStringConst("stdout write length out of range")
	e.ensureStringConst("stderr write length out of range")
	e.ensureStringConst("fs open flags out of range")
	e.ensureStringConst("fs read cap out of range")
	e.ensureStringConst("fs write length out of range")
	e.ensureStringConst("fs seek offset out of range")
	e.ensureStringConst("fs seek whence out of range")
	e.ensureStringConst("panic message length out of range")
	e.ensureStringConst("string length out of range")
	e.ensureStringConst("panic bounds kind out of range")
	e.ensureStringConst("panic bounds index out of range")
	e.ensureStringConst("panic bounds length out of range")
	e.ensureStringConst("missing poll function")
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
	if err := e.emitPollDispatch(); err != nil {
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
