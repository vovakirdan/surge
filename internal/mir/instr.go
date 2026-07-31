package mir

import (
	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// InstrKind enumerates instruction kinds in MIR.
type InstrKind uint8

const (
	// InstrAssign represents an assignment instruction.
	InstrAssign InstrKind = iota
	// InstrCall represents a call instruction.
	InstrCall
	// InstrDrop represents a drop instruction.
	InstrDrop
	// InstrEndBorrow represents an end borrow instruction.
	InstrEndBorrow
	// InstrAwait represents an await instruction.
	InstrAwait
	// InstrSpawn represents a spawn instruction.
	InstrSpawn
	// InstrCrossing represents a backend-neutral crossing operation.
	InstrCrossing
	// InstrBlocking represents a blocking task creation instruction.
	InstrBlocking
	// InstrPoll represents a poll instruction.
	InstrPoll
	// InstrJoinAll represents a join all instruction.
	InstrJoinAll
	// InstrChanSend represents a channel send instruction.
	InstrChanSend
	// InstrChanRecv represents a channel receive instruction.
	InstrChanRecv
	// InstrNetWait represents a network readiness wait instruction.
	InstrNetWait
	// InstrTimeout represents a timeout instruction.
	InstrTimeout
	// InstrSelect represents a select instruction.
	InstrSelect
	// InstrNop represents a no-op instruction.
	InstrNop
	// InstrEnvelopeRelease frees a heap box that never acquired a normal
	// scope-exit drop because it was synthesized after sema ran: a
	// for-loop's per-step Option<T> box, its iterator cursor, or a
	// `compare` expression's boxed-union scrutinee temp. See
	// EnvelopeReleaseInstr.
	InstrEnvelopeRelease
)

func (k InstrKind) String() string {
	switch k {
	case InstrAssign:
		return "Assign"
	case InstrCall:
		return "Call"
	case InstrDrop:
		return "Drop"
	case InstrEndBorrow:
		return "EndBorrow"
	case InstrAwait:
		return "Await"
	case InstrSpawn:
		return "Spawn"
	case InstrCrossing:
		return "Crossing"
	case InstrBlocking:
		return "Blocking"
	case InstrPoll:
		return "Poll"
	case InstrJoinAll:
		return "JoinAll"
	case InstrChanSend:
		return "ChanSend"
	case InstrChanRecv:
		return "ChanRecv"
	case InstrNetWait:
		return "NetWait"
	case InstrTimeout:
		return "Timeout"
	case InstrSelect:
		return "Select"
	case InstrNop:
		return "Nop"
	case InstrEnvelopeRelease:
		return "EnvelopeRelease"
	default:
		return "Unknown"
	}
}

// Instr represents a MIR instruction.
type Instr struct {
	Kind InstrKind

	Assign          AssignInstr
	Call            CallInstr
	Drop            DropInstr
	EndBorrow       EndBorrowInstr
	Await           AwaitInstr
	Spawn           SpawnInstr
	Crossing        CrossingInstr
	Blocking        BlockingInstr
	Poll            PollInstr
	JoinAll         JoinAllInstr
	ChanSend        ChanSendInstr
	ChanRecv        ChanRecvInstr
	NetWait         NetWaitInstr
	Timeout         TimeoutInstr
	Select          SelectInstr
	EnvelopeRelease EnvelopeReleaseInstr
}

// AssignInstr represents an assignment instruction.
type AssignInstr struct {
	Dst Place
	Src RValue
}

// CalleeKind distinguishes call target types.
type CalleeKind uint8

const (
	// CalleeSym represents a symbol call target.
	CalleeSym CalleeKind = iota
	// CalleeValue represents a value call target.
	CalleeValue
)

func (k CalleeKind) String() string {
	switch k {
	case CalleeSym:
		return "Sym"
	case CalleeValue:
		return "Value"
	default:
		return "Unknown"
	}
}

// Callee represents a call target.
type Callee struct {
	Kind  CalleeKind
	Sym   symbols.SymbolID
	Name  string
	Value Operand
}

// CallInstr represents a function call instruction.
type CallInstr struct {
	HasDst bool
	Dst    Place
	Callee Callee
	Args   []Operand
}

// DropInstr represents a drop instruction.
type DropInstr struct {
	Place Place
	// Shallow frees the place's own storage and leaves whatever it still
	// contains alone. It is what closes a RESIDUAL drop: a partially-moved
	// value has its live fields dropped one at a time first, and the container
	// box is then released without a field walk, because the only fields still
	// present are the ones that moved away and are no longer this value's to
	// release.
	//
	// Deliberately not "null the moved slot and deep-drop as usual". That works
	// while composites are boxed and stops working the moment a field is stored
	// INLINE, because an inline field has no null to write — which is exactly
	// the representation this epic is clearing the way for.
	Shallow bool
}

// EnvelopeReleaseInstr frees a synthesized-after-sema heap box: a for-loop
// iterator protocol envelope (step box or cursor) or a `compare`
// expression's boxed-union scrutinee. Unlike DropInstr, the for-loop
// envelope is emitted unconditionally (never gated on the place's declared
// type being Copy — it is always a heap box on the LLVM backend
// regardless of what the iteration element type is); the compare-scrutinee
// case is gated by the HIR normalize pass on ownership actually having
// transferred into the scrutinee (see compareScrutineeReleaseSafe).
// Cursor selects the fixed-shape free (see EnvelopeReleaseData).
type EnvelopeReleaseInstr struct {
	Place  Place
	Cursor bool
}

// EndBorrowInstr represents an end borrow instruction.
type EndBorrowInstr struct {
	Place Place
}

// AwaitInstr represents an await instruction.
type AwaitInstr struct {
	Dst  Place
	Task Operand
}

// SpawnInstr represents a spawn instruction.
type SpawnInstr struct {
	Dst   Place
	Value Operand
}

// CrossingDestination carries the MIR-side destination operand for `on` and
// `spawn on` crossings.
type CrossingDestination struct {
	Kind          sema.CrossingDestinationKind
	Value         Operand
	Type          types.TypeID
	AnchorSymbol  symbols.SymbolID
	OwnerAnchored bool
}

// CrossingCapture carries one sema-accepted capture operand.
type CrossingCapture struct {
	Symbol  symbols.SymbolID
	Value   Operand
	Type    types.TypeID
	Mode    sema.CrossingCaptureMode
	Verdict sema.CrossingCaptureVerdict
}

// CrossingRemoteOp carries one owner-anchored remote operation in an
// `on far_handle { ... }` body.
type CrossingRemoteOp struct {
	Method         string
	Receiver       Operand
	ReceiverSymbol symbols.SymbolID
	ReceiverType   types.TypeID
	// Value carries a remote-select send arm's payload; unset elsewhere.
	Value Operand
}

// CrossingInstr represents a crossing operation without backend execution
// semantics. ReadyBB/PendBB are populated by async lowering when the crossing is
// used as a suspend point inside an async function.
type CrossingInstr struct {
	Kind sema.CrossingLoweringKind
	Dst  Place

	Destination CrossingDestination
	Captures    []CrossingCapture
	RemoteOps   []CrossingRemoteOp

	// Spawn-on executable lowering uses BodyFuncID as the destination poll
	// function and State as the once-published payload. Pending is a persisted
	// rt_remote_spawn_pending* slot that survives async suspension.
	BodyFuncID     FuncID
	State          StructLit
	Pending        Place
	Handle         Place
	Receiver       Operand
	ReceiverSymbol symbols.SymbolID
	ReceiverType   types.TypeID
	ConsumesHandle bool

	PayloadType types.TypeID
	ResultType  types.TypeID
	HandleType  types.TypeID

	ReadyBB BlockID
	PendBB  BlockID
}

// BlockingInstr represents a blocking task creation instruction.
type BlockingInstr struct {
	Dst    Place
	FuncID FuncID
	State  StructLit
}

// PollInstr represents a poll instruction.
type PollInstr struct {
	Dst     Place
	Task    Operand
	ReadyBB BlockID
	PendBB  BlockID
}

// JoinAllInstr represents a join all instruction.
type JoinAllInstr struct {
	Dst     Place
	Scope   Operand
	ReadyBB BlockID
	PendBB  BlockID
}

// ChanSendInstr represents a channel send instruction.
type ChanSendInstr struct {
	Channel           Operand
	Value             Operand
	ReadyBB           BlockID
	PendBB            BlockID
	YieldAfterHandoff bool
}

// ChanRecvInstr represents a channel receive instruction.
type ChanRecvInstr struct {
	Dst     Place
	Channel Operand
	ReadyBB BlockID
	PendBB  BlockID
	// Anchored marks a receive inside an anchored crossing body: the runtime
	// helper reaches the channel through the current task's pending and parks
	// by re-entering the body, so Channel is unused and there are no
	// ready/pending suspend targets.
	Anchored bool
}

// NetWaitKind distinguishes network readiness waits.
type NetWaitKind uint8

const (
	// NetWaitAccept waits for listener accept readiness.
	NetWaitAccept NetWaitKind = iota
	// NetWaitRead waits for connection read readiness.
	NetWaitRead
	// NetWaitWrite waits for connection write readiness.
	NetWaitWrite
)

func (k NetWaitKind) String() string {
	switch k {
	case NetWaitAccept:
		return "accept"
	case NetWaitRead:
		return "read"
	case NetWaitWrite:
		return "write"
	default:
		return "unknown"
	}
}

// NetWaitInstr represents a network readiness wait instruction.
type NetWaitInstr struct {
	Kind    NetWaitKind
	Handle  Operand
	ReadyBB BlockID
	PendBB  BlockID
}

// TimeoutInstr represents a timeout instruction.
type TimeoutInstr struct {
	Dst     Place
	Task    Operand
	Ms      Operand
	ReadyBB BlockID
	PendBB  BlockID
}

// SelectArmKind distinguishes select arm types.
type SelectArmKind uint8

const (
	// SelectArmTask represents a task select arm.
	SelectArmTask SelectArmKind = iota
	// SelectArmChanRecv represents a channel receive select arm.
	SelectArmChanRecv
	// SelectArmChanSend represents a channel send select arm.
	SelectArmChanSend
	// SelectArmTimeout represents a timeout select arm.
	SelectArmTimeout
	// SelectArmDefault represents a default select arm.
	SelectArmDefault
)

// SelectArm represents a select arm.
type SelectArm struct {
	Kind    SelectArmKind
	Task    Operand
	Channel Operand
	Value   Operand
	Ms      Operand
}

// SelectInstr represents a select instruction.
type SelectInstr struct {
	Dst     Place
	Arms    []SelectArm
	ReadyBB BlockID
	PendBB  BlockID
}

// OperandKind distinguishes operand types.
type OperandKind uint8

const (
	// OperandConst represents a constant operand.
	OperandConst OperandKind = iota
	// OperandCopy represents a copy operand.
	OperandCopy
	// OperandMove represents a move operand.
	OperandMove
	// OperandAddrOf represents an address-of operand.
	OperandAddrOf
	// OperandAddrOfMut represents a mutable address-of operand.
	OperandAddrOfMut
	// OperandRetain reads a place whose value is a reference-counted scalar
	// and gives the destination its OWN reference to the same block.
	//
	// It exists because neither Copy nor Move can express this. Copy would
	// leave two owners sharing one reference, so whichever released first
	// would free the block under the other. Move would end the source's
	// ownership, which is exactly what these types must not do — they are
	// Copy at the surface, so `let b = a` has to leave `a` usable.
	//
	// Whether a read retains is a property of the USE SITE, not of the type:
	// storing `a` into `b` creates an owner, while handing `a` to an
	// arithmetic entry point that borrows it does not. No type predicate can
	// decide that, which is why this is a distinct operand kind rather than a
	// flag derived from the operand's type.
	OperandRetain
	// OperandCopyValue reads a place holding a VALUE COMPOSITE for consumption
	// and gives the destination an INDEPENDENT value — a struct, tuple, tagged
	// union or fixed array duplicated so that mutating one does not touch the
	// other.
	//
	// It exists for the same reason OperandRetain does, one type-category over:
	// whether a read duplicates is a property of the USE SITE, and OperandCopy
	// cannot carry that. A composite read for CONSUMPTION must produce a second
	// independent value; the same composite read for BORROWING must not, since
	// the reader is only looking through to storage someone else owns and has
	// to keep seeing later writes to it. Both are OperandCopy today, so the
	// backend sees one kind and cannot tell them apart — which is why copying a
	// composite copies its box pointer and `let mut q = p; q.a = 99` mutates
	// `p`.
	//
	// The distinction is permanent; how the backend PRODUCES the independent
	// value is not. It clones a heap box today and copies inline bits later,
	// behind an unchanged operand kind.
	OperandCopyValue
)

func (k OperandKind) String() string {
	switch k {
	case OperandConst:
		return "Const"
	case OperandCopy:
		return "Copy"
	case OperandMove:
		return "Move"
	case OperandAddrOf:
		return "AddrOf"
	case OperandAddrOfMut:
		return "AddrOfMut"
	case OperandRetain:
		return "Retain"
	case OperandCopyValue:
		return "CopyValue"
	default:
		return "Unknown"
	}
}

// Operand represents a MIR operand.
type Operand struct {
	Kind OperandKind
	Type types.TypeID

	Const Const
	Place Place
}

// ConstKind distinguishes constant kinds.
type ConstKind uint8

const (
	// ConstInt represents an integer constant.
	ConstInt ConstKind = iota
	// ConstUint represents an unsigned integer constant.
	ConstUint
	// ConstFloat represents a float constant.
	ConstFloat
	// ConstBool represents a boolean constant.
	ConstBool
	// ConstString represents a string constant.
	ConstString
	// ConstNothing represents a nothing constant.
	ConstNothing
	// ConstFn represents a function constant.
	ConstFn
)

// Const represents a MIR constant.
type Const struct {
	Kind ConstKind
	Type types.TypeID

	// Text preserves raw literal text for numeric constants when available.
	// For v1 VM, this is the source of truth for dynamic-sized numbers.
	Text string

	IntValue    int64
	UintValue   uint64
	FloatValue  float64
	BoolValue   bool
	StringValue string
	Sym         symbols.SymbolID
}

// RValueKind distinguishes right-hand value kinds.
type RValueKind uint8

const (
	// RValueUse represents a use of a value.
	RValueUse RValueKind = iota
	// RValueUnaryOp represents a unary operation.
	RValueUnaryOp
	// RValueBinaryOp represents a binary operation.
	RValueBinaryOp
	// RValueCast represents a cast operation.
	RValueCast
	// RValueStructLit represents a struct literal.
	RValueStructLit
	// RValueArrayLit represents an array literal.
	RValueArrayLit
	// RValueTupleLit represents a tuple literal.
	RValueTupleLit
	// RValueField represents a field access.
	RValueField
	// RValueIndex represents an index access.
	RValueIndex
	// RValueTagTest represents a tag test.
	RValueTagTest
	// RValueTagPayload represents a tag payload access.
	RValueTagPayload
	// RValueIterInit represents an iterator initialization.
	RValueIterInit
	// RValueIterNext represents an iterator next operation.
	RValueIterNext
	// RValueTypeTest represents a type test.
	RValueTypeTest
	// RValueHeirTest represents an heir test.
	RValueHeirTest
)

// RValue represents a right-hand value in MIR.
type RValue struct {
	Kind RValueKind

	Use        Operand
	Unary      UnaryOp
	Binary     BinaryOp
	Cast       CastOp
	StructLit  StructLit
	ArrayLit   ArrayLit
	TupleLit   TupleLit
	Field      FieldAccess
	Index      IndexAccess
	TagTest    TagTest
	TagPayload TagPayload
	IterInit   IterInit
	IterNext   IterNext
	TypeTest   TypeTest
	HeirTest   HeirTest
}

// UnaryOp represents a unary operation.
type UnaryOp struct {
	Op      ast.ExprUnaryOp
	Operand Operand
}

// BinaryOp represents a binary operation.
type BinaryOp struct {
	Op    ast.ExprBinaryOp
	Left  Operand
	Right Operand
}

// CastOp represents a cast operation.
type CastOp struct {
	Value    Operand
	TargetTy types.TypeID
}

// StructLitField represents a struct literal field.
type StructLitField struct {
	Name  string
	Value Operand
}

// StructLit represents a struct literal.
type StructLit struct {
	TypeID types.TypeID
	Fields []StructLitField
}

// ArrayLit represents an array literal.
type ArrayLit struct {
	Elems []Operand
}

// TupleLit represents a tuple literal.
type TupleLit struct {
	Elems []Operand
}

// FieldAccess represents a field access.
type FieldAccess struct {
	Object    Operand
	FieldName string
	FieldIdx  int
	// MoveOut takes the field's value out of the container instead of
	// producing a second holder of it. A plain read counts the new holder;
	// this one must not, because the container's drop has been narrowed to
	// the places that stayed and will never release what left.
	MoveOut bool
}

// IndexAccess represents an index access.
type IndexAccess struct {
	Object Operand
	Index  Operand
}

// TagTest represents a tag test.
type TagTest struct {
	Value   Operand
	TagName string
}

// TagPayload represents a tag payload access.
type TagPayload struct {
	Value   Operand
	TagName string
	Index   int
	// MoveOut takes the payload's value out of the union instead of producing
	// a second holder of it, mirroring FieldAccess.MoveOut exactly and for the
	// same reason: a plain read counts a new holder, and this one must not,
	// because the union's own release has already been narrowed around it.
	//
	// This is NOT the same question as "is the subject owned or borrowed" —
	// HIR's SubjectBorrowed (hir.TagPayloadData) is true for BOTH a borrowed
	// subject AND a `@copy` subject read through a deref (scrutineeDuplicated):
	// the duplicated envelope is a genuine, freshly minted clone, but it is
	// still deep-dropped as an ordinary composite afterward, so its payload
	// must still retain — only a MOVED subject's envelope gets the narrowed
	// shallow release that makes an unretained extraction safe. MoveOut is
	// exactly the inverse of SubjectBorrowed (set false for the same two cases
	// SubjectBorrowed is true for, true only for the moved case), carried one
	// field further into MIR instead of being consumed and discarded after
	// deciding whether to emit a trailing retain.
	MoveOut bool
}

// TypeTest represents a type test.
type TypeTest struct {
	Value    Operand
	TargetTy types.TypeID
}

// HeirTest represents an heir test.
type HeirTest struct {
	Value    Operand
	TargetTy types.TypeID
}

// IterInit represents an iterator initialization.
type IterInit struct {
	Iterable Operand
}

// IterNext represents an iterator next operation.
type IterNext struct {
	Iter Operand
}
