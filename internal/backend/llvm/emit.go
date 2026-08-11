package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type funcSig struct {
	ret        string
	params     []string
	paramTypes []types.TypeID
	// resultType is the type `ret` spells. A composite result's storage facts
	// are read from it, so the hidden destination the caller allocates and the
	// one the callee writes state the same size and alignment.
	resultType types.TypeID
	// abi is the generated call contract for this signature: which result
	// travels through a hidden caller-owned destination, which arguments travel
	// by address, and which carry no payload at all. It is classified from the
	// callee's types alone (see emit_call_layout.go), so a definition and both
	// kinds of call site reach one answer for one callee.
	//
	// A hand-written runtime or foreign signature leaves it zero, which is the
	// unusable ParamGoverned/RetGoverned class: those callees are governed by
	// another authority and never take the hidden-destination or by-value
	// contract. `governedElsewhere` is how a lowering asks.
	abi mir.SurgeABI
}

// governedElsewhere reports whether another authority owns this signature's
// lowering — the hand-written runtime declaration table or foreign C lowering.
func (s *funcSig) governedElsewhere() bool {
	return s.abi.Ret.Class == mir.RetGoverned
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
	mod   *mir.Module
	types *types.Interner
	syms  *symbols.Table
	// files resolves a span into the "path:line:col" a panic prints. It is the
	// compiler's own file set: the native backend has to name a location while
	// it emits, because nothing at run time can look one up. Nil means the
	// caller did not supply one, and then no panic carries a location.
	files *source.FileSet
	// spanConsts interns the rendered locations panics report, keyed by the
	// rendered text so one file:line:col is one global. It is filled while
	// functions are emitted and written out after them; see emit_span.go for
	// why it is not the string-constant table above.
	spanConsts     map[string]*stringConst
	spanConstOrder []*stringConst
	buf            strings.Builder
	stringConsts   map[string]*stringConst
	fnRefs         map[mir.FuncID]struct{}
	funcNames      map[mir.FuncID]string
	funcSigs       map[mir.FuncID]funcSig
	globalNames    map[mir.GlobalID]string
	runtimeSigs    map[string]funcSig
	paramCounts    map[mir.FuncID]int
	// Recursive drop glue generated on demand (emit_drop_glue.go).
	dropGlueNeeded     map[types.TypeID]struct{}
	dropElemGlueNeeded map[types.TypeID]struct{}
	// Recursive clone glue, the copy-side twin of the drop glue above
	// (emit_clone_glue.go), and its per-element form for arrays
	// (emit_clone_array.go).
	cloneGlueNeeded     map[types.TypeID]struct{}
	cloneElemGlueNeeded map[types.TypeID]struct{}
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
	// result, this box always exists (an activation allocates one, never
	// inert/Copy), so every registration always needs an arm. The dispatch
	// (__surge_drop_abandoned_state_call) routes the id to the frame
	// release below (emit_async.go).
	abandonedStateDrops map[types.TypeID]struct{}
	// Values the runtime holds in an allocation of its own — an unadopted
	// transport payload, a crossing state that was never shipped — need one
	// entry point that releases what the value owns AND the allocation
	// carrying it. See emit_transport_allocation.go.
	runtimeOwnedReleaseNeeded map[types.TypeID]struct{}
	// Result payload types a cloned task handle must be served a copy of. See
	// emit_copy_result_glue.go.
	copyResultGlueNeeded map[types.TypeID]struct{}
	// An ABANDONED async frame is released rather than dropped: the storage
	// goes back to the allocator and nothing walks the resume payload, which
	// is a duplicate of what the resumed locals own (emit_drop_glue.go).
	suspensionFrameReleases map[types.TypeID]struct{}
}

type funcEmitter struct {
	emitter     *Emitter
	f           *mir.Func
	tmpID       int
	inlineBlock int
	localAlloca map[mir.LocalID]string
	// lowered is this function's own contract: whether its result travels
	// through the hidden destination named by sretParamName, and which of its
	// parameters occupy an argument position at all.
	lowered abiSignature
	// entryAllocas collects every reservation this function makes, wherever in
	// the body it was asked for, so they can all be placed in the entry block.
	// See emitAllocaAligned for why that is both necessary and sound.
	entryAllocas strings.Builder
	// lastTraceSpan is the location the backtrace line map last recorded for
	// this function, so a run of instructions from one statement emits one row
	// rather than one per instruction. Reset at the function entry marker; see
	// emit_trace_table.go.
	lastTraceSpan    source.Span
	lastTraceSpanSet bool
	addrOfTargets    map[mir.LocalID]addrOfTarget
	paramLocals      []mir.LocalID
	blockTerminated  bool
	// span is where the instruction currently being emitted came from, which is
	// the location any panic emitted underneath it will name. See emit_span.go.
	span spanCursor
}

// sretParamName is the hidden caller-owned destination a composite result is
// written into. It is named rather than numbered so that a `ret` cannot confuse
// it with a declared parameter.
const sretParamName = "surge.ret"

const (
	arrayHeaderSize  = 24
	arrayHeaderAlign = 8
	arrayLenOffset   = 0
	arrayCapOffset   = 8
	arrayDataOffset  = 16
)

// EmitModule converts a MIR module into an LLVM IR string. The file set is what
// lets an emitted panic name its source location; passing nil emits panics that
// report a message and no location.
func EmitModule(mod *mir.Module, typesIn *types.Interner, symTable *symbols.Table, files *source.FileSet) (string, error) {
	e := &Emitter{
		mod:          mod,
		types:        typesIn,
		syms:         symTable,
		files:        files,
		spanConsts:   make(map[string]*stringConst),
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
	e.ensureStringConst("division by zero")
	// The panic codes a condition names for itself. They are constants like
	// any other message, and they are here rather than beside their emit sites
	// for the same reason the messages are: a string used from inside a
	// function body has to exist before the body is written out.
	e.ensureStringConst("VM1003")
	e.ensureStringConst("VM1101")
	e.ensureStringConst("VM3203")
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
	e.ensureStringConst(uncopyableResultMessage)
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
	// The dispatches come before the glue passes. They emit no glue of their
	// own; they name it — an abandoned state or an undelivered result is
	// reclaimed through the glue of its type — so reaching them afterwards
	// would put those names on the worklists after the only pass that drains
	// them had already finished, and the module would call functions it never
	// defines. Where a function is DEFINED in the module is not what orders
	// this; what orders it is when the request to define it is made.
	if err := e.emitPollDispatch(); err != nil {
		return "", err
	}
	if err := e.emitBlockingDispatch(); err != nil {
		return "", err
	}
	if err := e.emitCopyResultGlue(); err != nil {
		return "", err
	}
	if err := e.emitCloneGlue(); err != nil {
		return "", err
	}
	if err := e.emitDropGlue(); err != nil {
		return "", err
	}
	e.emitTraceTextEnd()
	// Last, because every pass above may have named a source location and this
	// writes the ones that were actually named.
	e.emitSpanConsts()
	e.emitTraceStrings()
	return e.buf.String(), nil
}

func (e *Emitter) emitPreamble() {
	e.buf.WriteString("target triple = \"x86_64-linux-gnu\"\n\n")
	emitTypedCarrierABI(&e.buf)
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
		e.buf.WriteString(formatRuntimeDecl(&decl))
		e.buf.WriteByte('\n')
	}
	e.buf.WriteString("\n")
}
