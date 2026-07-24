package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

type funcSig struct {
	ret        string
	params     []string
	paramTypes []types.TypeID
}

type addrOfTarget struct {
	place mir.Place
	ty    types.TypeID
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
	// Recursive drop glue generated on demand (emit_drop_glue.go).
	dropGlueNeeded     map[types.TypeID]struct{}
	dropElemGlueNeeded map[types.TypeID]struct{}
	// Crossing states that ship with a drop obligation: the crossing
	// body's FuncID doubles as the drop-fn id, and the dispatch routes
	// it to the state struct's recursive glue (emit_async.go).
	crossingDropStates map[mir.FuncID]types.TypeID
	// Remote-body RESULT payload types that ship over the reply edge with
	// an owner-side drop obligation (RV2-DEBT-053a). Keyed by the result
	// payload TypeID (its own id space, distinct from state's FuncID
	// space) because a result may be a heap leaf — string or dynamic
	// array — needing full value-drop, not just struct-box glue. The
	// dispatch (__surge_drop_result_call) routes the id to the value's
	// drop wrapper (emit_async.go).
	crossingDropResults  map[types.TypeID]struct{}
	dropResultGlueNeeded map[types.TypeID]struct{}
	// Suspend-point/scope-join state boxes a cancellation may abandon with
	// nothing left to unpack them (the abandoned-suspend-state fix). Keyed
	// by the state struct's own TypeID (its own id space, like results, not
	// state's FuncID space): every yield/return-cancelled site in every
	// function can build a differently-typed state, so there is no single
	// natural FuncID to key on the way a crossing body has. Unlike a
	// result, this box always exists (built fresh at every suspend, never
	// inert/Copy), so every registration always needs the box-freeing
	// struct glue (dropGlueName), not the value-drop wrapper results use.
	// The dispatch (__surge_drop_abandoned_state_call) routes the id to
	// that struct glue (emit_async.go).
	abandonedStateDrops map[types.TypeID]struct{}
}

type funcEmitter struct {
	emitter         *Emitter
	f               *mir.Func
	tmpID           int
	inlineBlock     int
	localAlloca     map[mir.LocalID]string
	addrOfTargets   map[mir.LocalID]addrOfTarget
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
	e.ensureStringConst("invalid UTF-8")
	e.ensureStringConst("\n")
	e.ensureStringConst("true")
	e.ensureStringConst("false")
	e.ensureStringConst("")
	e.ensureStringConst("integer overflow")
	e.ensureStringConst("unsigned overflow")
	e.ensureStringConst("float overflow")
	e.ensureStringConst("cannot convert negative int to uint")
	e.ensureStringConst("array capacity out of range")
	e.ensureStringConst("array view is not resizable")
	e.ensureStringConst("byte range start out of range")
	e.ensureStringConst("byte range length out of range")
	e.ensureStringConst("byte drop count out of range")
	e.ensureStringConst("exit code out of range")
	e.ensureStringConst("bytes view length out of range")
	e.ensureStringConst("string repeat count out of range")
	e.ensureStringConst("sleep duration out of range")
	e.ensureStringConst("timeout duration out of range")
	e.ensureStringConst("channel capacity out of range")
	e.ensureStringConst("placement shard id out of range")
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
	e.ensureStringConst("entropy length out of range")
	e.ensureStringConst("fs open flags out of range")
	e.ensureStringConst("fs read cap out of range")
	e.ensureStringConst("fs write length out of range")
	e.ensureStringConst("fs seek offset out of range")
	e.ensureStringConst("fs seek whence out of range")
	e.ensureStringConst("net listen port out of range")
	e.ensureStringConst("net connect port out of range")
	e.ensureStringConst("net read cap out of range")
	e.ensureStringConst("net write length out of range")
	e.ensureStringConst("panic message length out of range")
	e.ensureStringConst("string length out of range")
	e.ensureStringConst("panic bounds kind out of range")
	e.ensureStringConst("panic bounds index out of range")
	e.ensureStringConst("panic bounds length out of range")
	e.ensureStringConst("missing poll function")
	e.ensureStringConst("missing drop function")
	e.ensureStringConst("missing blocking function")
	e.ensureStringConst("spawn on remote publish requires an async task context")
	e.ensureStringConst("spawn on destination is shut down")
	e.ensureStringConst("spawn on remote publish queue is full")
	e.ensureStringConst("spawn on remote publish was refused")
	e.ensureStringConst("spawn on placement is not supported by this backend")
	e.ensureStringConst("spawn on placement is invalid")
	e.ensureStringConst("spawn on remote publish failed")
	e.ensureStringConst("far Task.await requires an async task context")
	e.ensureStringConst("far Task.await owner shard is shut down")
	e.ensureStringConst("far Task.await transport queue is full")
	e.ensureStringConst("far Task.await remote task request was refused")
	e.ensureStringConst("far Task.await handle is stale")
	e.ensureStringConst("far Task.await handle was already consumed")
	e.ensureStringConst("far Task.await remote task request failed")
	e.ensureStringConst("far Task.cancel requires an async task context")
	e.ensureStringConst("far Task.cancel owner shard is shut down")
	e.ensureStringConst("far Task.cancel transport queue is full")
	e.ensureStringConst("far Task.cancel remote task request was refused")
	e.ensureStringConst("far Task.cancel handle is stale")
	e.ensureStringConst("far Task.cancel handle was already consumed")
	e.ensureStringConst("far Task.cancel remote task request failed")
	e.ensureStringConst("on crossing requires an async task context")
	e.ensureStringConst("on destination shard is shut down")
	e.ensureStringConst("on transport queue is full")
	e.ensureStringConst("on execute request was refused")
	e.ensureStringConst("on execute token is stale")
	e.ensureStringConst("on placement is not supported by this backend")
	e.ensureStringConst("on execute request failed")
	e.ensureStringConst("channel_on placement is invalid")
	e.ensureStringConst("channel_on destination shard is shut down")
	e.ensureStringConst("channel_on transport queue is full")
	e.ensureStringConst("channel_on create request was refused")
	e.ensureStringConst("channel_on placement is not supported by this backend")
	e.ensureStringConst("channel_on create request failed")
	e.ensureStringConst("channel_on capacity out of range")
	e.ensureStringConst("anchored on requires an async task context and a channel anchor")
	e.ensureStringConst("anchored on destination shard is shut down")
	e.ensureStringConst("anchored on transport queue is full")
	e.ensureStringConst("anchored on execute request was refused")
	e.ensureStringConst("anchored channel is gone: the far channel was released before the block could run")
	e.ensureStringConst("anchored on execute request failed")
	e.ensureStringConst("share requires an async task context and a channel handle")
	e.ensureStringConst("share destination shard is shut down")
	e.ensureStringConst("share transport queue is full")
	e.ensureStringConst("share request was refused")
	e.ensureStringConst("share source lease was already released; a released holder cannot propagate access")
	e.ensureStringConst("share request failed")
	e.ensureStringConst("remote select arms must be far channels sharing one owner shard; split into one select per owner")
	e.ensureStringConst("remote select destination shard is shut down")
	e.ensureStringConst("remote select transport queue is full")
	e.ensureStringConst("remote select request was refused")
	e.ensureStringConst("remote select arm lease was already released; a released holder cannot select")
	e.ensureStringConst("remote select request failed")
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
	if err := e.emitDropGlue(); err != nil {
		return "", err
	}
	if err := e.emitPollDispatch(); err != nil {
		return "", err
	}
	if err := e.emitBlockingDispatch(); err != nil {
		return "", err
	}
	return e.buf.String(), nil
}

func (e *Emitter) emitPreamble() {
	e.buf.WriteString("target triple = \"x86_64-linux-gnu\"\n\n")
	// Landing pad for a retain on the NULL float — the canonical zero, which
	// has no block behind it. Retaining is branchless (see emitRetainOperand):
	// the NULL case is redirected onto this word instead of jumping over the
	// bump, so duplicating a float stays straight-line code. Thread-local
	// because shards are OS threads, and the counter is written by whichever
	// shard runs the copy; nothing ever reads it back.
	fmt.Fprintf(&e.buf, "@%s = thread_local global i32 0\n\n", retainScratchGlobal)
}

// retainScratchGlobal is the module-level sink a NULL retain bumps instead.
const retainScratchGlobal = "__surge_rc_scratch"

func (e *Emitter) emitRuntimeDecls() {
	for _, decl := range runtimeDecls() {
		fmt.Fprintf(&e.buf, "declare %s @%s(%s)\n", decl.ret, decl.name, strings.Join(decl.params, ", "))
	}
	e.buf.WriteString("\n")
}
