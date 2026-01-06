package ast

import (
	"surge/internal/source"
)

// Exprs manages allocation of expressions.
type Exprs struct {
	Arena        *Arena[Expr]
	Idents       *Arena[ExprIdentData]
	Literals     *Arena[ExprLiteralData]
	Binaries     *Arena[ExprBinaryData]
	Unaries      *Arena[ExprUnaryData]
	Casts        *Arena[ExprCastData]
	Calls        *Arena[ExprCallData]
	Indices      *Arena[ExprIndexData]
	Members      *Arena[ExprMemberData]
	TupleIndices *Arena[ExprTupleIndexData]
	Awaits       *Arena[ExprAwaitData]
	Ternaries    *Arena[ExprTernaryData]
	Groups       *Arena[ExprGroupData]
	Tuples       *Arena[ExprTupleData]
	Arrays       *Arena[ExprArrayData]
	Maps         *Arena[ExprMapData]
	RangeLits    *Arena[ExprRangeLitData]
	Spreads      *Arena[ExprSpreadData]
	Tasks        *Arena[ExprTaskData]
	Spawns       *Arena[ExprSpawnData]
	Parallels    *Arena[ExprParallelData]
	Compares     *Arena[ExprCompareData]
	Selects      *Arena[ExprSelectData]
	Races        *Arena[ExprSelectData]
	Structs      *Arena[ExprStructData]
	Asyncs       *Arena[ExprAsyncData]
	Blocks       *Arena[ExprBlockData]
}

// NewExprs creates a new Exprs with per-kind arenas preallocated using capHint as the initial capacity.
// If capHint is 0, a default capacity of 1<<8 is used; all expression arenas (Expr, Idents, Literals, Binaries, Unaries, Casts, Calls, Indices, Members, Groups, Tuples, Arrays, Spreads, Compares) are initialized.
func NewExprs(capHint uint) *Exprs {
	if capHint == 0 {
		capHint = 1 << 8
	}
	return &Exprs{
		Arena:        NewArena[Expr](capHint),
		Idents:       NewArena[ExprIdentData](capHint),
		Literals:     NewArena[ExprLiteralData](capHint),
		Binaries:     NewArena[ExprBinaryData](capHint),
		Unaries:      NewArena[ExprUnaryData](capHint),
		Casts:        NewArena[ExprCastData](capHint),
		Calls:        NewArena[ExprCallData](capHint),
		Indices:      NewArena[ExprIndexData](capHint),
		Members:      NewArena[ExprMemberData](capHint),
		TupleIndices: NewArena[ExprTupleIndexData](capHint),
		Awaits:       NewArena[ExprAwaitData](capHint),
		Ternaries:    NewArena[ExprTernaryData](capHint),
		Groups:       NewArena[ExprGroupData](capHint),
		Tuples:       NewArena[ExprTupleData](capHint),
		Arrays:       NewArena[ExprArrayData](capHint),
		Maps:         NewArena[ExprMapData](capHint),
		RangeLits:    NewArena[ExprRangeLitData](capHint),
		Spreads:      NewArena[ExprSpreadData](capHint),
		Tasks:        NewArena[ExprTaskData](capHint),
		Spawns:       NewArena[ExprSpawnData](capHint),
		Parallels:    NewArena[ExprParallelData](capHint),
		Compares:     NewArena[ExprCompareData](capHint),
		Selects:      NewArena[ExprSelectData](capHint),
		Races:        NewArena[ExprSelectData](capHint),
		Structs:      NewArena[ExprStructData](capHint),
		Asyncs:       NewArena[ExprAsyncData](capHint),
		Blocks:       NewArena[ExprBlockData](capHint),
	}
}

func (e *Exprs) new(kind ExprKind, span source.Span, payload PayloadID) ExprID {
	return ExprID(e.Arena.Allocate(Expr{
		Kind:    kind,
		Span:    span,
		Payload: payload,
	}))
}

// Get returns the expression with the given ID.
func (e *Exprs) Get(id ExprID) *Expr {
	return e.Arena.Get(uint32(id))
}

// NewIdent creates a new identifier expression.
func (e *Exprs) NewIdent(span source.Span, name source.StringID) ExprID {
	payload := e.Idents.Allocate(ExprIdentData{Name: name})
	return e.new(ExprIdent, span, PayloadID(payload))
}

// Ident returns the identifier data for the given expression ID.
func (e *Exprs) Ident(id ExprID) (*ExprIdentData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprIdent {
		return nil, false
	}
	return e.Idents.Get(uint32(expr.Payload)), true
}

// NewLiteral creates a new literal expression.
func (e *Exprs) NewLiteral(span source.Span, kind ExprLitKind, value source.StringID) ExprID {
	payload := e.Literals.Allocate(ExprLiteralData{Kind: kind, Value: value})
	return e.new(ExprLit, span, PayloadID(payload))
}

// Literal returns the literal data for the given expression ID.
func (e *Exprs) Literal(id ExprID) (*ExprLiteralData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprLit {
		return nil, false
	}
	return e.Literals.Get(uint32(expr.Payload)), true
}

// NewBinary creates a new binary expression.
func (e *Exprs) NewBinary(span source.Span, op ExprBinaryOp, left, right ExprID) ExprID {
	payload := e.Binaries.Allocate(ExprBinaryData{Op: op, Left: left, Right: right})
	return e.new(ExprBinary, span, PayloadID(payload))
}

// Binary returns the binary data for the given expression ID.
func (e *Exprs) Binary(id ExprID) (*ExprBinaryData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprBinary {
		return nil, false
	}
	return e.Binaries.Get(uint32(expr.Payload)), true
}

// NewUnary creates a new unary expression.
func (e *Exprs) NewUnary(span source.Span, op ExprUnaryOp, operand ExprID) ExprID {
	payload := e.Unaries.Allocate(ExprUnaryData{Op: op, Operand: operand})
	return e.new(ExprUnary, span, PayloadID(payload))
}

// Unary returns the unary data for the given expression ID.
func (e *Exprs) Unary(id ExprID) (*ExprUnaryData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprUnary {
		return nil, false
	}
	return e.Unaries.Get(uint32(expr.Payload)), true
}

// NewCast creates a new cast expression.
func (e *Exprs) NewCast(span source.Span, value ExprID, typ TypeID, rawType ExprID) ExprID {
	payload := e.Casts.Allocate(ExprCastData{Value: value, Type: typ, RawType: rawType})
	return e.new(ExprCast, span, PayloadID(payload))
}

// Cast returns the cast data for the given expression ID.
func (e *Exprs) Cast(id ExprID) (*ExprCastData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprCast {
		return nil, false
	}
	return e.Casts.Get(uint32(expr.Payload)), true
}

// NewCall creates a new function call expression.
func (e *Exprs) NewCall(span source.Span, target ExprID, args []CallArg, typeArgs []TypeID, argCommas []source.Span, trailing bool) ExprID {
	payload := e.Calls.Allocate(ExprCallData{
		Target:           target,
		Args:             append([]CallArg(nil), args...),
		TypeArgs:         append([]TypeID(nil), typeArgs...),
		ArgCommas:        append([]source.Span(nil), argCommas...),
		HasTrailingComma: trailing,
	})
	return e.new(ExprCall, span, PayloadID(payload))
}

// Call returns the call data for the given expression ID.
func (e *Exprs) Call(id ExprID) (*ExprCallData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprCall {
		return nil, false
	}
	return e.Calls.Get(uint32(expr.Payload)), true
}

// NewIndex creates a new index expression.
func (e *Exprs) NewIndex(span source.Span, target, index ExprID) ExprID {
	payload := e.Indices.Allocate(ExprIndexData{Target: target, Index: index})
	return e.new(ExprIndex, span, PayloadID(payload))
}

// Index returns the index data for the given expression ID.
func (e *Exprs) Index(id ExprID) (*ExprIndexData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprIndex {
		return nil, false
	}
	return e.Indices.Get(uint32(expr.Payload)), true
}

// NewMember creates a new member access expression.
func (e *Exprs) NewMember(span source.Span, target ExprID, field source.StringID) ExprID {
	payload := e.Members.Allocate(ExprMemberData{Target: target, Field: field})
	return e.new(ExprMember, span, PayloadID(payload))
}

// Member returns the member data for the given expression ID.
func (e *Exprs) Member(id ExprID) (*ExprMemberData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprMember {
		return nil, false
	}
	return e.Members.Get(uint32(expr.Payload)), true
}

// NewTupleIndex creates a new tuple index expression.
func (e *Exprs) NewTupleIndex(span source.Span, target ExprID, index uint32) ExprID {
	payload := e.TupleIndices.Allocate(ExprTupleIndexData{Target: target, Index: index})
	return e.new(ExprTupleIndex, span, PayloadID(payload))
}

// TupleIndex returns the tuple index data for the given expression ID.
func (e *Exprs) TupleIndex(id ExprID) (*ExprTupleIndexData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprTupleIndex {
		return nil, false
	}
	return e.TupleIndices.Get(uint32(expr.Payload)), true
}

// NewAwait creates a new await expression.
func (e *Exprs) NewAwait(span source.Span, value ExprID) ExprID {
	payload := e.Awaits.Allocate(ExprAwaitData{Value: value})
	return e.new(ExprAwait, span, PayloadID(payload))
}

// Await returns the await data for the given expression ID.
func (e *Exprs) Await(id ExprID) (*ExprAwaitData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprAwait {
		return nil, false
	}
	return e.Awaits.Get(uint32(expr.Payload)), true
}

// NewTernary creates a new ternary expression.
func (e *Exprs) NewTernary(span source.Span, cond, trueExpr, falseExpr ExprID) ExprID {
	payload := e.Ternaries.Allocate(ExprTernaryData{
		Cond:      cond,
		TrueExpr:  trueExpr,
		FalseExpr: falseExpr,
	})
	return e.new(ExprTernary, span, PayloadID(payload))
}

// Ternary returns the ternary data for the given expression ID.
func (e *Exprs) Ternary(id ExprID) (*ExprTernaryData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprTernary {
		return nil, false
	}
	return e.Ternaries.Get(uint32(expr.Payload)), true
}

// NewStruct creates a new struct literal expression.
func (e *Exprs) NewStruct(span source.Span, typ TypeID, fields []ExprStructField, commas []source.Span, trailing, positional bool) ExprID {
	payload := e.Structs.Allocate(ExprStructData{
		Type:             typ,
		Fields:           append([]ExprStructField(nil), fields...),
		FieldCommas:      append([]source.Span(nil), commas...),
		HasTrailingComma: trailing,
		Positional:       positional,
	})
	return e.new(ExprStruct, span, PayloadID(payload))
}

// Struct returns the struct data for the given expression ID.
func (e *Exprs) Struct(id ExprID) (*ExprStructData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprStruct {
		return nil, false
	}
	return e.Structs.Get(uint32(expr.Payload)), true
}

// NewGroup creates a new parenthesized group expression.
func (e *Exprs) NewGroup(span source.Span, inner ExprID) ExprID {
	payload := e.Groups.Allocate(ExprGroupData{Inner: inner})
	return e.new(ExprGroup, span, PayloadID(payload))
}

// Group returns the group data for the given expression ID.
func (e *Exprs) Group(id ExprID) (*ExprGroupData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprGroup {
		return nil, false
	}
	return e.Groups.Get(uint32(expr.Payload)), true
}

// NewTuple creates a new tuple literal expression.
func (e *Exprs) NewTuple(span source.Span, elements []ExprID, commas []source.Span, trailing bool) ExprID {
	payload := e.Tuples.Allocate(ExprTupleData{
		Elements:         append([]ExprID(nil), elements...),
		ElementCommas:    append([]source.Span(nil), commas...),
		HasTrailingComma: trailing,
	})
	return e.new(ExprTuple, span, PayloadID(payload))
}

// Tuple returns the tuple data for the given expression ID.
func (e *Exprs) Tuple(id ExprID) (*ExprTupleData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprTuple {
		return nil, false
	}
	return e.Tuples.Get(uint32(expr.Payload)), true
}

// NewArray creates a new array literal expression.
func (e *Exprs) NewArray(span source.Span, elements []ExprID, commas []source.Span, trailing bool) ExprID {
	payload := e.Arrays.Allocate(ExprArrayData{
		Elements:         append([]ExprID(nil), elements...),
		ElementCommas:    append([]source.Span(nil), commas...),
		HasTrailingComma: trailing,
	})
	return e.new(ExprArray, span, PayloadID(payload))
}

// Array returns the array data for the given expression ID.
func (e *Exprs) Array(id ExprID) (*ExprArrayData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprArray {
		return nil, false
	}
	return e.Arrays.Get(uint32(expr.Payload)), true
}

// NewMap creates a new map literal expression.
func (e *Exprs) NewMap(span source.Span, entries []ExprMapEntry, commas []source.Span, trailing bool) ExprID {
	payload := e.Maps.Allocate(ExprMapData{
		Entries:          append([]ExprMapEntry(nil), entries...),
		EntryCommas:      append([]source.Span(nil), commas...),
		HasTrailingComma: trailing,
	})
	return e.new(ExprMap, span, PayloadID(payload))
}

// Map returns the map data for the given expression ID.
func (e *Exprs) Map(id ExprID) (*ExprMapData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprMap {
		return nil, false
	}
	return e.Maps.Get(uint32(expr.Payload)), true
}

// NewRangeLit creates a new range literal expression.
func (e *Exprs) NewRangeLit(span source.Span, start, end ExprID, inclusive bool) ExprID {
	payload := e.RangeLits.Allocate(ExprRangeLitData{
		Start:     start,
		End:       end,
		Inclusive: inclusive,
	})
	return e.new(ExprRangeLit, span, PayloadID(payload))
}

// RangeLit returns the range literal data for the given expression ID.
func (e *Exprs) RangeLit(id ExprID) (*ExprRangeLitData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprRangeLit {
		return nil, false
	}
	return e.RangeLits.Get(uint32(expr.Payload)), true
}

// NewSpread creates a new spread expression.
func (e *Exprs) NewSpread(span source.Span, value ExprID) ExprID {
	payload := e.Spreads.Allocate(ExprSpreadData{Value: value})
	return e.new(ExprSpread, span, PayloadID(payload))
}

// Spread returns the spread data for the given expression ID.
func (e *Exprs) Spread(id ExprID) (*ExprSpreadData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprSpread {
		return nil, false
	}
	return e.Spreads.Get(uint32(expr.Payload)), true
}

// NewTask creates a new task expression.
func (e *Exprs) NewTask(span source.Span, value ExprID) ExprID {
	payload := e.Tasks.Allocate(ExprTaskData{Value: value})
	return e.new(ExprTask, span, PayloadID(payload))
}

// Task returns the task data for the given expression ID.
func (e *Exprs) Task(id ExprID) (*ExprTaskData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprTask {
		return nil, false
	}
	return e.Tasks.Get(uint32(expr.Payload)), true
}

// NewSpawn creates a new spawn expression.
func (e *Exprs) NewSpawn(span source.Span, value ExprID) ExprID {
	payload := e.Spawns.Allocate(ExprSpawnData{Value: value})
	return e.new(ExprSpawn, span, PayloadID(payload))
}

// Spawn returns the spawn data for the given expression ID.
func (e *Exprs) Spawn(id ExprID) (*ExprSpawnData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprSpawn {
		return nil, false
	}
	return e.Spawns.Get(uint32(expr.Payload)), true
}

// NewAsync creates a new async expression.
func (e *Exprs) NewAsync(span source.Span, body StmtID, attrStart AttrID, attrCount uint32) ExprID {
	payload := e.Asyncs.Allocate(ExprAsyncData{
		Body:      body,
		AttrStart: attrStart,
		AttrCount: attrCount,
	})
	return e.new(ExprAsync, span, PayloadID(payload))
}

// Async returns the async data for the given expression ID.
func (e *Exprs) Async(id ExprID) (*ExprAsyncData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprAsync {
		return nil, false
	}
	return e.Asyncs.Get(uint32(expr.Payload)), true
}

// NewParallelMap creates a new parallel map expression.
func (e *Exprs) NewParallelMap(span source.Span, iterable ExprID, args []ExprID, body ExprID) ExprID {
	payload := e.Parallels.Allocate(ExprParallelData{
		Kind:     ExprParallelMap,
		Iterable: iterable,
		Init:     NoExprID,
		Args:     append([]ExprID(nil), args...),
		Body:     body,
	})
	return e.new(ExprParallel, span, PayloadID(payload))
}

// NewParallelReduce creates a new parallel reduce expression.
func (e *Exprs) NewParallelReduce(span source.Span, iterable, init ExprID, args []ExprID, body ExprID) ExprID {
	payload := e.Parallels.Allocate(ExprParallelData{
		Kind:     ExprParallelReduce,
		Iterable: iterable,
		Init:     init,
		Args:     append([]ExprID(nil), args...),
		Body:     body,
	})
	return e.new(ExprParallel, span, PayloadID(payload))
}

// Parallel returns the parallel data for the given expression ID.
func (e *Exprs) Parallel(id ExprID) (*ExprParallelData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprParallel {
		return nil, false
	}
	return e.Parallels.Get(uint32(expr.Payload)), true
}

// NewCompare creates a new compare expression.
func (e *Exprs) NewCompare(span source.Span, value ExprID, arms []ExprCompareArm) ExprID {
	payload := e.Compares.Allocate(ExprCompareData{
		Value: value,
		Arms:  append([]ExprCompareArm(nil), arms...),
	})
	return e.new(ExprCompare, span, PayloadID(payload))
}

// Compare returns the compare data for the given expression ID.
func (e *Exprs) Compare(id ExprID) (*ExprCompareData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprCompare || !expr.Payload.IsValid() {
		return nil, false
	}
	return e.Compares.Get(uint32(expr.Payload)), true
}

// NewSelect creates a new select expression.
func (e *Exprs) NewSelect(span source.Span, arms []ExprSelectArm) ExprID {
	payload := e.Selects.Allocate(ExprSelectData{
		Arms: append([]ExprSelectArm(nil), arms...),
	})
	return e.new(ExprSelect, span, PayloadID(payload))
}

// Select returns the select data for the given expression ID.
func (e *Exprs) Select(id ExprID) (*ExprSelectData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprSelect || !expr.Payload.IsValid() {
		return nil, false
	}
	return e.Selects.Get(uint32(expr.Payload)), true
}

// NewRace creates a new race expression.
func (e *Exprs) NewRace(span source.Span, arms []ExprSelectArm) ExprID {
	payload := e.Races.Allocate(ExprSelectData{
		Arms: append([]ExprSelectArm(nil), arms...),
	})
	return e.new(ExprRace, span, PayloadID(payload))
}

// Race returns the race data for the given expression ID.
func (e *Exprs) Race(id ExprID) (*ExprSelectData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprRace || !expr.Payload.IsValid() {
		return nil, false
	}
	return e.Races.Get(uint32(expr.Payload)), true
}

// NewBlock creates a new block expression.
func (e *Exprs) NewBlock(span source.Span, stmts []StmtID) ExprID {
	payload := e.Blocks.Allocate(ExprBlockData{
		Stmts: append([]StmtID(nil), stmts...),
	})
	return e.new(ExprBlock, span, PayloadID(payload))
}

// Block returns the block data for the given expression ID.
func (e *Exprs) Block(id ExprID) (*ExprBlockData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprBlock || !expr.Payload.IsValid() {
		return nil, false
	}
	return e.Blocks.Get(uint32(expr.Payload)), true
}
