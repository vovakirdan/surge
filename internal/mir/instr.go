package mir

import (
	"surge/internal/ast"
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
	// InstrPoll represents a poll instruction.
	InstrPoll
	// InstrJoinAll represents a join all instruction.
	InstrJoinAll
	// InstrChanSend represents a channel send instruction.
	InstrChanSend
	// InstrChanRecv represents a channel receive instruction.
	InstrChanRecv
	// InstrTimeout represents a timeout instruction.
	InstrTimeout
	// InstrSelect represents a select instruction.
	InstrSelect
	// InstrNop represents a no-op instruction.
	InstrNop
)

// Instr represents a MIR instruction.
type Instr struct {
	Kind InstrKind

	Assign    AssignInstr
	Call      CallInstr
	Drop      DropInstr
	EndBorrow EndBorrowInstr
	Await     AwaitInstr
	Spawn     SpawnInstr
	Poll      PollInstr
	JoinAll   JoinAllInstr
	ChanSend  ChanSendInstr
	ChanRecv  ChanRecvInstr
	Timeout   TimeoutInstr
	Select    SelectInstr
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
	Channel Operand
	Value   Operand
	ReadyBB BlockID
	PendBB  BlockID
}

// ChanRecvInstr represents a channel receive instruction.
type ChanRecvInstr struct {
	Dst     Place
	Channel Operand
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
)

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
