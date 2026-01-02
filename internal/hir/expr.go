package hir

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ExprKind enumerates HIR expression kinds.
// These map closely to AST expression kinds with minimal desugaring.
type ExprKind uint8

const (
	// ExprLiteral represents literals (int, float, bool, string, nothing).
	ExprLiteral ExprKind = iota
	// ExprVarRef represents a variable reference.
	ExprVarRef
	// ExprUnaryOp represents unary operators (-, !, +, *, &, &mut, own).
	ExprUnaryOp
	// ExprBinaryOp represents binary operators (+, -, *, /, ==, etc.).
	ExprBinaryOp
	// ExprCall represents function or method calls.
	ExprCall
	// ExprFieldAccess represents field access (expr.field).
	ExprFieldAccess
	// ExprIndex represents indexing (expr[index]).
	ExprIndex
	// ExprStructLit represents struct literals (Type { field = value, ... }).
	ExprStructLit
	// ExprArrayLit represents array literals ([a, b, c]).
	ExprArrayLit
	// ExprTupleLit represents tuple literals ((a, b, c)).
	ExprTupleLit
	// ExprCompare represents pattern matching (compare expr { ... }).
	// Preserved as-is, desugaring happens in later stages.
	ExprCompare
	// ExprSelect represents select expression over awaitables.
	ExprSelect
	// ExprRace represents race expression over awaitables.
	ExprRace
	// ExprTagTest checks whether a union value matches a tag or the `nothing` variant.
	ExprTagTest
	// ExprTagPayload extracts a payload component from a tagged union value.
	ExprTagPayload
	// ExprIterInit lowers `for x in xs` iteration initialization into an intrinsic iterator value.
	ExprIterInit
	// ExprIterNext lowers iterator advancement into an intrinsic next operation yielding Option<T>.
	ExprIterNext
	// ExprIf represents conditional expression (ternary or if-expression).
	ExprIf
	// ExprAwait represents .await() on a Task<T>.
	ExprAwait
	// ExprTask represents task expression.
	ExprTask
	// ExprSpawn represents reserved spawn expression.
	ExprSpawn
	// ExprAsync represents async { ... } block expression.
	ExprAsync
	// ExprCast represents type cast (expr to Type or expr: Type).
	ExprCast
	// ExprBlock represents a block expression { ... }.
	ExprBlock
)

// String returns a human-readable name for the expression kind.
func (k ExprKind) String() string {
	switch k {
	case ExprLiteral:
		return "Literal"
	case ExprVarRef:
		return "VarRef"
	case ExprUnaryOp:
		return "UnaryOp"
	case ExprBinaryOp:
		return "BinaryOp"
	case ExprCall:
		return "Call"
	case ExprFieldAccess:
		return "FieldAccess"
	case ExprIndex:
		return "Index"
	case ExprStructLit:
		return "StructLit"
	case ExprArrayLit:
		return "ArrayLit"
	case ExprTupleLit:
		return "TupleLit"
	case ExprCompare:
		return "Compare"
	case ExprSelect:
		return "Select"
	case ExprRace:
		return "Race"
	case ExprTagTest:
		return "TagTest"
	case ExprTagPayload:
		return "TagPayload"
	case ExprIterInit:
		return "IterInit"
	case ExprIterNext:
		return "IterNext"
	case ExprIf:
		return "If"
	case ExprAwait:
		return "Await"
	case ExprTask:
		return "Task"
	case ExprSpawn:
		return "Spawn"
	case ExprAsync:
		return "Async"
	case ExprCast:
		return "Cast"
	case ExprBlock:
		return "Block"
	default:
		return "Unknown"
	}
}

// Expr represents an HIR expression with type information.
type Expr struct {
	Kind ExprKind
	Type types.TypeID // Always filled from sema.ExprTypes
	Span source.Span  // Source location for diagnostics
	Data ExprData     // Kind-specific payload
}

// ExprData is the interface for expression-specific data.
type ExprData interface {
	exprData()
}

// LiteralKind enumerates literal value kinds.
type LiteralKind uint8

const (
	LiteralInt LiteralKind = iota
	LiteralFloat
	LiteralBool
	LiteralString
	LiteralNothing
)

// LiteralData holds data for ExprLiteral.
type LiteralData struct {
	Kind        LiteralKind
	Text        string // Raw literal text for numeric literals (int/float).
	IntValue    int64
	FloatValue  float64
	BoolValue   bool
	StringValue string
}

func (LiteralData) exprData() {}

// VarRefData holds data for ExprVarRef.
type VarRefData struct {
	Name     string
	SymbolID symbols.SymbolID
}

func (VarRefData) exprData() {}

// UnaryOpData holds data for ExprUnaryOp.
type UnaryOpData struct {
	Op      ast.ExprUnaryOp
	Operand *Expr
}

func (UnaryOpData) exprData() {}

// BinaryOpData holds data for ExprBinaryOp.
type BinaryOpData struct {
	Op        ast.ExprBinaryOp
	Left      *Expr
	Right     *Expr
	TypeLeft  types.TypeID
	TypeRight types.TypeID
}

func (BinaryOpData) exprData() {}

// CallData holds data for ExprCall.
type CallData struct {
	Callee   *Expr   // The function/method being called
	Args     []*Expr // Arguments
	SymbolID symbols.SymbolID
}

func (CallData) exprData() {}

// FieldAccessData holds data for ExprFieldAccess.
type FieldAccessData struct {
	Object    *Expr
	FieldName string
	FieldIdx  int // Struct field index, -1 if unknown
}

func (FieldAccessData) exprData() {}

// IndexData holds data for ExprIndex.
type IndexData struct {
	Object *Expr
	Index  *Expr
}

func (IndexData) exprData() {}

// StructLitData holds data for ExprStructLit.
type StructLitData struct {
	TypeName string
	TypeID   types.TypeID
	Fields   []StructFieldInit
}

func (StructLitData) exprData() {}

// StructFieldInit represents a field initializer in a struct literal.
type StructFieldInit struct {
	Name  string
	Value *Expr
	Span  source.Span
}

// ArrayLitData holds data for ExprArrayLit.
type ArrayLitData struct {
	Elements []*Expr
}

func (ArrayLitData) exprData() {}

// TupleLitData holds data for ExprTupleLit.
type TupleLitData struct {
	Elements []*Expr
}

func (TupleLitData) exprData() {}

// CompareArm represents one arm in a compare expression.
type CompareArm struct {
	Pattern   *Expr       // Pattern to match against
	Guard     *Expr       // Optional guard condition (nil if none)
	Result    *Expr       // Result expression
	IsFinally bool        // true if this is a 'finally' clause
	Span      source.Span // Source location
}

// CompareData holds data for ExprCompare.
type CompareData struct {
	Value *Expr        // Expression being matched
	Arms  []CompareArm // Pattern match arms
}

func (CompareData) exprData() {}

// SelectArm represents one arm in select/race expressions.
type SelectArm struct {
	Await     *Expr
	Result    *Expr
	IsDefault bool
	Span      source.Span
}

// SelectData holds data for ExprSelect/ExprRace.
type SelectData struct {
	Arms []SelectArm
}

func (SelectData) exprData() {}

// TagTestData holds data for ExprTagTest.
type TagTestData struct {
	Value   *Expr
	TagName string // e.g. "Some" or "nothing"
}

func (TagTestData) exprData() {}

// TagPayloadData holds data for ExprTagPayload.
type TagPayloadData struct {
	Value   *Expr
	TagName string // e.g. "Some"
	Index   int    // payload slot
}

func (TagPayloadData) exprData() {}

// IterInitData holds data for ExprIterInit.
type IterInitData struct {
	Iterable *Expr
}

func (IterInitData) exprData() {}

// IterNextData holds data for ExprIterNext.
type IterNextData struct {
	Iter *Expr
}

func (IterNextData) exprData() {}

// IfData holds data for ExprIf (conditional expression).
type IfData struct {
	Cond *Expr
	Then *Expr
	Else *Expr // nil if no else branch
}

func (IfData) exprData() {}

// AwaitData holds data for ExprAwait.
type AwaitData struct {
	Value *Expr
}

func (AwaitData) exprData() {}

// TaskData holds data for ExprTask.
type TaskData struct {
	Value *Expr
}

func (TaskData) exprData() {}

// SpawnData holds data for ExprSpawn.
type SpawnData struct {
	Value *Expr
}

func (SpawnData) exprData() {}

// AsyncData holds data for ExprAsync.
type AsyncData struct {
	Body     *Block
	Failfast bool
}

func (AsyncData) exprData() {}

// CastData holds data for ExprCast.
type CastData struct {
	Value    *Expr
	TargetTy types.TypeID
}

func (CastData) exprData() {}

// BlockExprData holds data for ExprBlock.
type BlockExprData struct {
	Block *Block
}

func (BlockExprData) exprData() {}
