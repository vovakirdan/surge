package mir

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

type InstrKind uint8

const (
	InstrAssign InstrKind = iota
	InstrCall
	InstrDrop
	InstrEndBorrow
	InstrAwait
	InstrSpawn
	InstrPoll
	InstrNop
)

type Instr struct {
	Kind InstrKind

	Assign    AssignInstr
	Call      CallInstr
	Drop      DropInstr
	EndBorrow EndBorrowInstr
	Await     AwaitInstr
	Spawn     SpawnInstr
	Poll      PollInstr
}

type AssignInstr struct {
	Dst Place
	Src RValue
}

type CalleeKind uint8

const (
	CalleeSym CalleeKind = iota
	CalleeValue
)

type Callee struct {
	Kind  CalleeKind
	Sym   symbols.SymbolID
	Name  string
	Value Operand
}

type CallInstr struct {
	HasDst bool
	Dst    Place
	Callee Callee
	Args   []Operand
}

type DropInstr struct {
	Place Place
}

type EndBorrowInstr struct {
	Place Place
}

type AwaitInstr struct {
	Dst  Place
	Task Operand
}

type SpawnInstr struct {
	Dst   Place
	Value Operand
}

type PollInstr struct {
	Dst     Place
	Task    Operand
	ReadyBB BlockID
	PendBB  BlockID
}

type OperandKind uint8

const (
	OperandConst OperandKind = iota
	OperandCopy
	OperandMove
	OperandAddrOf
	OperandAddrOfMut
)

type Operand struct {
	Kind OperandKind
	Type types.TypeID

	Const Const
	Place Place
}

type ConstKind uint8

const (
	ConstInt ConstKind = iota
	ConstUint
	ConstFloat
	ConstBool
	ConstString
	ConstNothing
)

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
}

type RValueKind uint8

const (
	RValueUse RValueKind = iota
	RValueUnaryOp
	RValueBinaryOp
	RValueCast
	RValueStructLit
	RValueArrayLit
	RValueTupleLit
	RValueField
	RValueIndex
	RValueTagTest
	RValueTagPayload
	RValueIterInit
	RValueIterNext
	RValueTypeTest
	RValueHeirTest
)

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

type UnaryOp struct {
	Op      ast.ExprUnaryOp
	Operand Operand
}

type BinaryOp struct {
	Op    ast.ExprBinaryOp
	Left  Operand
	Right Operand
}

type CastOp struct {
	Value    Operand
	TargetTy types.TypeID
}

type StructLitField struct {
	Name  string
	Value Operand
}

type StructLit struct {
	TypeID types.TypeID
	Fields []StructLitField
}

type ArrayLit struct {
	Elems []Operand
}

type TupleLit struct {
	Elems []Operand
}

type FieldAccess struct {
	Object    Operand
	FieldName string
	FieldIdx  int
}

type IndexAccess struct {
	Object Operand
	Index  Operand
}

type TagTest struct {
	Value   Operand
	TagName string
}

type TagPayload struct {
	Value   Operand
	TagName string
	Index   int
}

type TypeTest struct {
	Value    Operand
	TargetTy types.TypeID
}

type HeirTest struct {
	Value    Operand
	TargetTy types.TypeID
}

type IterInit struct {
	Iterable Operand
}

type IterNext struct {
	Iter Operand
}
