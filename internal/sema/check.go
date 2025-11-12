package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Options configure a semantic pass over a file.
type Options struct {
	Reporter diag.Reporter
	Symbols  *symbols.Result
	Types    *types.Interner
	Exports  map[string]*symbols.ModuleExports
}

// Result stores semantic artefacts produced by the checker.
type Result struct {
	TypeInterner *types.Interner
	ExprTypes    map[ast.ExprID]types.TypeID
	ExprBorrows  map[ast.ExprID]BorrowID
	Borrows      []BorrowInfo
}

// Check performs semantic analysis (type inference, borrow checks, etc.).
// At this stage it handles literal typing and basic operator validation.
func Check(builder *ast.Builder, fileID ast.FileID, opts Options) Result {
	res := Result{
		ExprTypes:   make(map[ast.ExprID]types.TypeID),
		ExprBorrows: make(map[ast.ExprID]BorrowID),
	}
	if opts.Types != nil {
		res.TypeInterner = opts.Types
	} else {
		res.TypeInterner = types.NewInterner()
	}
	if builder == nil || fileID == ast.NoFileID {
		return res
	}

	checker := typeChecker{
		builder:  builder,
		fileID:   fileID,
		reporter: opts.Reporter,
		symbols:  opts.Symbols,
		result:   &res,
		types:    res.TypeInterner,
		exports:  opts.Exports,
	}
	checker.run()
	return res
}

type typeChecker struct {
	builder  *ast.Builder
	fileID   ast.FileID
	reporter diag.Reporter
	symbols  *symbols.Result
	result   *Result
	types    *types.Interner
	exports  map[string]*symbols.ModuleExports
	magic    map[symbols.TypeKey]map[string]*symbols.FunctionSignature
	borrow   *BorrowTable

	scopeStack    []symbols.ScopeID
	scopeByItem   map[ast.ItemID]symbols.ScopeID
	scopeByStmt   map[ast.StmtID]symbols.ScopeID
	stmtSymbols   map[ast.StmtID]symbols.SymbolID
	bindingBorrow map[symbols.SymbolID]BorrowID
}

func (tc *typeChecker) run() {
	if tc.builder == nil || tc.result == nil || tc.types == nil {
		return
	}
	tc.buildMagicIndex()
	tc.buildScopeIndex()
	tc.buildSymbolIndex()
	tc.borrow = NewBorrowTable()
	tc.bindingBorrow = make(map[symbols.SymbolID]BorrowID)
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	root := tc.fileScope()
	rootPushed := tc.pushScope(root)
	for _, itemID := range file.Items {
		tc.walkItem(itemID)
	}
	if rootPushed {
		tc.leaveScope()
	}
	tc.flushBorrowResults()
}

func (tc *typeChecker) walkItem(id ast.ItemID) {
	item := tc.builder.Items.Get(id)
	if item == nil {
		return
	}
	switch item.Kind {
	case ast.ItemLet:
		letItem, ok := tc.builder.Items.Let(id)
		if !ok || letItem == nil || !letItem.Value.IsValid() {
			return
		}
		tc.typeExpr(letItem.Value)
		tc.observeMove(letItem.Value, tc.exprSpan(letItem.Value))
		tc.updateItemBinding(id, letItem.Value)
	case ast.ItemFn:
		fnItem, ok := tc.builder.Items.Fn(id)
		if !ok || fnItem == nil || !fnItem.Body.IsValid() {
			return
		}
		scope := tc.scopeForItem(id)
		pushed := tc.pushScope(scope)
		tc.walkStmt(fnItem.Body)
		if pushed {
			tc.leaveScope()
		}
	default:
		// Other item kinds are currently ignored.
	}
}

func (tc *typeChecker) walkStmt(id ast.StmtID) {
	stmt := tc.builder.Stmts.Get(id)
	if stmt == nil {
		return
	}
	switch stmt.Kind {
	case ast.StmtBlock:
		if block := tc.builder.Stmts.Block(id); block != nil {
			scope := tc.scopeForStmt(id)
			pushed := tc.pushScope(scope)
			for _, child := range block.Stmts {
				tc.walkStmt(child)
			}
			if pushed {
				tc.leaveScope()
			}
		}
	case ast.StmtLet:
		if letStmt := tc.builder.Stmts.Let(id); letStmt != nil && letStmt.Value.IsValid() {
			tc.typeExpr(letStmt.Value)
			tc.observeMove(letStmt.Value, tc.exprSpan(letStmt.Value))
			tc.updateStmtBinding(id, letStmt.Value)
		}
	case ast.StmtExpr:
		if exprStmt := tc.builder.Stmts.Expr(id); exprStmt != nil {
			tc.typeExpr(exprStmt.Expr)
		}
	case ast.StmtReturn:
		if ret := tc.builder.Stmts.Return(id); ret != nil {
			tc.typeExpr(ret.Expr)
			tc.observeMove(ret.Expr, tc.exprSpan(ret.Expr))
		}
	case ast.StmtIf:
		if ifStmt := tc.builder.Stmts.If(id); ifStmt != nil {
			tc.typeExpr(ifStmt.Cond)
			tc.walkStmt(ifStmt.Then)
			if ifStmt.Else.IsValid() {
				tc.walkStmt(ifStmt.Else)
			}
		}
	case ast.StmtWhile:
		if whileStmt := tc.builder.Stmts.While(id); whileStmt != nil {
			tc.typeExpr(whileStmt.Cond)
			tc.walkStmt(whileStmt.Body)
		}
	case ast.StmtForClassic:
		if forStmt := tc.builder.Stmts.ForClassic(id); forStmt != nil {
			scope := tc.scopeForStmt(id)
			pushed := tc.pushScope(scope)
			if forStmt.Init.IsValid() {
				tc.walkStmt(forStmt.Init)
			}
			tc.typeExpr(forStmt.Cond)
			tc.typeExpr(forStmt.Post)
			tc.walkStmt(forStmt.Body)
			if pushed {
				tc.leaveScope()
			}
		}
	case ast.StmtForIn:
		if forIn := tc.builder.Stmts.ForIn(id); forIn != nil {
			scope := tc.scopeForStmt(id)
			pushed := tc.pushScope(scope)
			tc.typeExpr(forIn.Iterable)
			tc.walkStmt(forIn.Body)
			if pushed {
				tc.leaveScope()
			}
		}
	case ast.StmtSignal:
		if signal := tc.builder.Stmts.Signal(id); signal != nil {
			tc.typeExpr(signal.Value)
		}
	default:
		// StmtBreak / StmtContinue and others have no expressions to type.
	}
}

func (tc *typeChecker) typeExpr(id ast.ExprID) types.TypeID {
	if !id.IsValid() {
		return types.NoTypeID
	}
	if ty, ok := tc.result.ExprTypes[id]; ok {
		return ty
	}
	expr := tc.builder.Exprs.Get(id)
	if expr == nil {
		return types.NoTypeID
	}
	var ty types.TypeID
	switch expr.Kind {
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(id); ok && lit != nil {
			ty = tc.literalType(lit.Kind)
		}
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(id); ok && group != nil {
			ty = tc.typeExpr(group.Inner)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(id); ok && data != nil {
			ty = tc.typeUnary(id, expr.Span, data)
		}
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(id); ok && data != nil {
			ty = tc.typeBinary(id, expr.Span, data)
		}
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(id); ok && call != nil {
			tc.typeExpr(call.Target)
			for _, arg := range call.Args {
				tc.typeExpr(arg)
				tc.observeMove(arg, tc.exprSpan(arg))
			}
		}
	case ast.ExprArray:
		if arr, ok := tc.builder.Exprs.Array(id); ok && arr != nil {
			for _, elem := range arr.Elements {
				tc.typeExpr(elem)
			}
		}
	case ast.ExprTuple:
		if tuple, ok := tc.builder.Exprs.Tuple(id); ok && tuple != nil {
			for _, elem := range tuple.Elements {
				tc.typeExpr(elem)
			}
		}
	case ast.ExprIndex:
		if idx, ok := tc.builder.Exprs.Index(id); ok && idx != nil {
			tc.typeExpr(idx.Target)
			tc.typeExpr(idx.Index)
		}
	case ast.ExprMember:
		if member, ok := tc.builder.Exprs.Member(id); ok && member != nil {
			tc.typeExpr(member.Target)
		}
	case ast.ExprAwait:
		if awaitData, ok := tc.builder.Exprs.Await(id); ok && awaitData != nil {
			ty = tc.typeExpr(awaitData.Value)
		}
	case ast.ExprCast:
		if cast, ok := tc.builder.Exprs.Cast(id); ok && cast != nil {
			ty = tc.typeExpr(cast.Value)
		}
	case ast.ExprCompare:
		if cmp, ok := tc.builder.Exprs.Compare(id); ok && cmp != nil {
			tc.typeExpr(cmp.Value)
			for _, arm := range cmp.Arms {
				tc.typeExpr(arm.Pattern)
				tc.typeExpr(arm.Guard)
				tc.typeExpr(arm.Result)
			}
		}
	case ast.ExprParallel:
		if par, ok := tc.builder.Exprs.Parallel(id); ok && par != nil {
			tc.typeExpr(par.Iterable)
			tc.typeExpr(par.Init)
			for _, arg := range par.Args {
				tc.typeExpr(arg)
			}
			tc.typeExpr(par.Body)
		}
	case ast.ExprSpawn:
		if spawn, ok := tc.builder.Exprs.Spawn(id); ok && spawn != nil {
			ty = tc.typeExpr(spawn.Value)
			tc.observeMove(spawn.Value, tc.exprSpan(spawn.Value))
			tc.enforceSpawn(spawn.Value)
		}
	case ast.ExprSpread:
		if spread, ok := tc.builder.Exprs.Spread(id); ok && spread != nil {
			tc.typeExpr(spread.Value)
		}
	default:
		// ExprIdent and other unhandled kinds default to unknown.
	}
	tc.result.ExprTypes[id] = ty
	return ty
}

func (tc *typeChecker) buildScopeIndex() {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
		return
	}
	data := tc.symbols.Table.Scopes.Data()
	if len(data) == 0 {
		return
	}
	tc.scopeByItem = make(map[ast.ItemID]symbols.ScopeID)
	tc.scopeByStmt = make(map[ast.StmtID]symbols.ScopeID)
	for idx := range data {
		scope := data[idx]
		value, err := safecast.Conv[uint32](idx + 1)
		if err != nil {
			panic(fmt.Errorf("scope index overflow: %w", err))
		}
		id := symbols.ScopeID(value)
		owner := scope.Owner
		if owner.ASTFile.IsValid() && owner.ASTFile != tc.fileID {
			continue
		}
		switch owner.Kind {
		case symbols.ScopeOwnerItem:
			if owner.Item.IsValid() {
				tc.scopeByItem[owner.Item] = id
			}
		case symbols.ScopeOwnerStmt:
			if owner.Stmt.IsValid() {
				tc.scopeByStmt[owner.Stmt] = id
			}
		}
	}
}

func (tc *typeChecker) buildSymbolIndex() {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return
	}
	data := tc.symbols.Table.Symbols.Data()
	if len(data) == 0 {
		return
	}
	tc.stmtSymbols = make(map[ast.StmtID]symbols.SymbolID)
	for idx := range data {
		sym := data[idx]
		value, err := safecast.Conv[uint32](idx + 1)
		if err != nil {
			panic(fmt.Errorf("symbol index overflow: %w", err))
		}
		id := symbols.SymbolID(value)
		if sym.Decl.ASTFile.IsValid() && sym.Decl.ASTFile != tc.fileID {
			continue
		}
		if sym.Decl.Stmt.IsValid() {
			if existing, ok := tc.stmtSymbols[sym.Decl.Stmt]; ok && existing.IsValid() {
				continue
			}
			tc.stmtSymbols[sym.Decl.Stmt] = id
		}
	}
}

func (tc *typeChecker) fileScope() symbols.ScopeID {
	if tc.symbols == nil {
		return symbols.NoScopeID
	}
	return tc.symbols.FileScope
}

func (tc *typeChecker) pushScope(scope symbols.ScopeID) bool {
	if !scope.IsValid() {
		return false
	}
	tc.scopeStack = append(tc.scopeStack, scope)
	return true
}

func (tc *typeChecker) leaveScope() {
	if len(tc.scopeStack) == 0 {
		return
	}
	top := tc.scopeStack[len(tc.scopeStack)-1]
	tc.scopeStack = tc.scopeStack[:len(tc.scopeStack)-1]
	if tc.borrow != nil {
		tc.borrow.EndScope(top)
	}
}

func (tc *typeChecker) currentScope() symbols.ScopeID {
	if len(tc.scopeStack) == 0 {
		return symbols.NoScopeID
	}
	return tc.scopeStack[len(tc.scopeStack)-1]
}

func (tc *typeChecker) scopeForItem(id ast.ItemID) symbols.ScopeID {
	if tc.scopeByItem == nil {
		return symbols.NoScopeID
	}
	return tc.scopeByItem[id]
}

func (tc *typeChecker) scopeForStmt(id ast.StmtID) symbols.ScopeID {
	if tc.scopeByStmt == nil {
		return symbols.NoScopeID
	}
	return tc.scopeByStmt[id]
}

func (tc *typeChecker) flushBorrowResults() {
	if tc.result == nil || tc.borrow == nil {
		return
	}
	if snapshot := tc.borrow.ExprBorrowSnapshot(); len(snapshot) > 0 {
		tc.result.ExprBorrows = snapshot
	}
	if infos := tc.borrow.Infos(); len(infos) > 0 {
		tc.result.Borrows = infos
	}
}

func (tc *typeChecker) symbolForStmt(id ast.StmtID) symbols.SymbolID {
	if tc.stmtSymbols == nil {
		return symbols.NoSymbolID
	}
	return tc.stmtSymbols[id]
}

func (tc *typeChecker) updateStmtBinding(stmtID ast.StmtID, expr ast.ExprID) {
	if !expr.IsValid() {
		return
	}
	symID := tc.symbolForStmt(stmtID)
	tc.updateBindingValue(symID, expr)
}

func (tc *typeChecker) updateItemBinding(itemID ast.ItemID, expr ast.ExprID) {
	if tc.symbols == nil || tc.symbols.ItemSymbols == nil {
		return
	}
	syms := tc.symbols.ItemSymbols[itemID]
	if len(syms) == 0 {
		return
	}
	tc.updateBindingValue(syms[0], expr)
}

func (tc *typeChecker) updateBindingValue(symID symbols.SymbolID, expr ast.ExprID) {
	if !symID.IsValid() || tc.bindingBorrow == nil {
		return
	}
	if tc.borrow == nil {
		tc.bindingBorrow[symID] = NoBorrowID
		return
	}
	bid := tc.borrow.ExprBorrow(expr)
	tc.bindingBorrow[symID] = bid
}

func (tc *typeChecker) observeMove(expr ast.ExprID, span source.Span) {
	if !expr.IsValid() || tc.borrow == nil {
		return
	}
	place, ok := tc.resolvePlace(expr)
	if !ok {
		return
	}
	issue := tc.borrow.MoveAllowed(place)
	if issue.Kind != BorrowIssueNone {
		if span == (source.Span{}) {
			span = tc.exprSpan(expr)
		}
		tc.reportBorrowMove(place, span, issue)
	}
}

func (tc *typeChecker) exprSpan(id ast.ExprID) source.Span {
	if !id.IsValid() || tc.builder == nil || tc.builder.Exprs == nil {
		return source.Span{}
	}
	expr := tc.builder.Exprs.Get(id)
	if expr == nil {
		return source.Span{}
	}
	return expr.Span
}

func (tc *typeChecker) resolvePlace(expr ast.ExprID) (Place, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return Place{}, false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return Place{}, false
	}
	switch node.Kind {
	case ast.ExprIdent:
		symID := tc.symbolForExpr(expr)
		if !symID.IsValid() {
			return Place{}, false
		}
		sym := tc.symbolFromID(symID)
		if sym == nil {
			return Place{}, false
		}
		if sym.Kind != symbols.SymbolLet && sym.Kind != symbols.SymbolParam {
			return Place{}, false
		}
		return Place{Kind: PlaceLocal, Base: symID}, true
	default:
		return Place{}, false
	}
}

func (tc *typeChecker) symbolForExpr(id ast.ExprID) symbols.SymbolID {
	if tc.symbols == nil || tc.symbols.ExprSymbols == nil {
		return symbols.NoSymbolID
	}
	if sym, ok := tc.symbols.ExprSymbols[id]; ok {
		return sym
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) ensureMutablePlace(place Place, span source.Span) bool {
	if !place.IsValid() {
		return false
	}
	sym := tc.symbolFromID(place.Base)
	if sym == nil {
		return false
	}
	if sym.Flags&symbols.SymbolFlagMutable == 0 {
		tc.report(diag.SemaBorrowImmutable, span, "cannot take mutable borrow of %s", tc.placeLabel(place))
		return false
	}
	return true
}

func (tc *typeChecker) handleBorrow(exprID ast.ExprID, span source.Span, op ast.ExprUnaryOp, operand ast.ExprID) {
	if tc.borrow == nil {
		return
	}
	place, ok := tc.resolvePlace(operand)
	if !ok {
		tc.report(diag.SemaBorrowNonAddressable, span, "expression is not addressable")
		return
	}
	scope := tc.currentScope()
	if !scope.IsValid() {
		return
	}
	kind := BorrowShared
	if op == ast.ExprUnaryRefMut {
		if !tc.ensureMutablePlace(place, span) {
			return
		}
		kind = BorrowMut
	}
	if _, issue := tc.borrow.BeginBorrow(exprID, span, kind, place, scope); issue.Kind != BorrowIssueNone {
		tc.reportBorrowConflict(place, span, issue, kind)
	}
}

func (tc *typeChecker) handleAssignmentIfNeeded(op ast.ExprBinaryOp, left, right ast.ExprID, span source.Span, flags types.BinaryFlags) {
	if flags&types.BinaryFlagAssignment == 0 {
		return
	}
	tc.handleAssignment(op, left, right, span)
}

func (tc *typeChecker) handleAssignment(op ast.ExprBinaryOp, left, right ast.ExprID, span source.Span) {
	place, ok := tc.resolvePlace(left)
	if !ok {
		return
	}
	if tc.borrow != nil {
		if issue := tc.borrow.MutationAllowed(place); issue.Kind != BorrowIssueNone {
			tc.reportBorrowMutation(place, span, issue)
		}
	}
	if op == ast.ExprBinaryAssign {
		tc.observeMove(right, tc.exprSpan(right))
		tc.updateBindingValue(place.Base, right)
		return
	}
	if tc.bindingBorrow != nil {
		tc.bindingBorrow[place.Base] = NoBorrowID
	}
}

func (tc *typeChecker) enforceSpawn(expr ast.ExprID) {
	if len(tc.bindingBorrow) == 0 {
		return
	}
	seen := make(map[symbols.SymbolID]struct{})
	tc.scanSpawn(expr, seen)
}

func (tc *typeChecker) scanSpawn(expr ast.ExprID, seen map[symbols.SymbolID]struct{}) {
	if !expr.IsValid() || tc.builder == nil {
		return
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return
	}
	if node.Kind == ast.ExprIdent {
		symID := tc.symbolForExpr(expr)
		if !symID.IsValid() {
			return
		}
		if seen != nil {
			if _, ok := seen[symID]; ok {
				return
			}
		}
		bid := tc.bindingBorrow[symID]
		if bid != NoBorrowID {
			if seen != nil {
				seen[symID] = struct{}{}
			}
			tc.reportSpawnThreadEscape(symID, node.Span, bid)
		}
		return
	}
	switch node.Kind {
	case ast.ExprBinary:
		if data, _ := tc.builder.Exprs.Binary(expr); data != nil {
			tc.scanSpawn(data.Left, seen)
			tc.scanSpawn(data.Right, seen)
		}
	case ast.ExprUnary:
		if data, _ := tc.builder.Exprs.Unary(expr); data != nil {
			tc.scanSpawn(data.Operand, seen)
		}
	case ast.ExprGroup:
		if data, _ := tc.builder.Exprs.Group(expr); data != nil {
			tc.scanSpawn(data.Inner, seen)
		}
	case ast.ExprCall:
		if data, _ := tc.builder.Exprs.Call(expr); data != nil {
			tc.scanSpawn(data.Target, seen)
			for _, arg := range data.Args {
				tc.scanSpawn(arg, seen)
			}
		}
	case ast.ExprTuple:
		if data, _ := tc.builder.Exprs.Tuple(expr); data != nil {
			for _, elem := range data.Elements {
				tc.scanSpawn(elem, seen)
			}
		}
	case ast.ExprArray:
		if data, _ := tc.builder.Exprs.Array(expr); data != nil {
			for _, elem := range data.Elements {
				tc.scanSpawn(elem, seen)
			}
		}
	case ast.ExprIndex:
		if data, _ := tc.builder.Exprs.Index(expr); data != nil {
			tc.scanSpawn(data.Target, seen)
			tc.scanSpawn(data.Index, seen)
		}
	case ast.ExprMember:
		if data, _ := tc.builder.Exprs.Member(expr); data != nil {
			tc.scanSpawn(data.Target, seen)
		}
	case ast.ExprAwait:
		if data, _ := tc.builder.Exprs.Await(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.ExprSpread:
		if data, _ := tc.builder.Exprs.Spread(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	case ast.ExprParallel:
		if data, _ := tc.builder.Exprs.Parallel(expr); data != nil {
			tc.scanSpawn(data.Iterable, seen)
			tc.scanSpawn(data.Init, seen)
			for _, arg := range data.Args {
				tc.scanSpawn(arg, seen)
			}
			tc.scanSpawn(data.Body, seen)
		}
	case ast.ExprCompare:
		if data, _ := tc.builder.Exprs.Compare(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
			for _, arm := range data.Arms {
				tc.scanSpawn(arm.Pattern, seen)
				tc.scanSpawn(arm.Guard, seen)
				tc.scanSpawn(arm.Result, seen)
			}
		}
	case ast.ExprSpawn:
		if data, _ := tc.builder.Exprs.Spawn(expr); data != nil {
			tc.scanSpawn(data.Value, seen)
		}
	}
}

func (tc *typeChecker) symbolFromID(id symbols.SymbolID) *symbols.Symbol {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return nil
	}
	return tc.symbols.Table.Symbols.Get(id)
}

func (tc *typeChecker) lookupName(id source.StringID) string {
	if id == source.NoStringID {
		return ""
	}
	if tc.builder != nil && tc.builder.StringsInterner != nil {
		return tc.builder.StringsInterner.MustLookup(id)
	}
	if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Strings != nil {
		return tc.symbols.Table.Strings.MustLookup(id)
	}
	return ""
}

func (tc *typeChecker) literalType(kind ast.ExprLitKind) types.TypeID {
	b := tc.types.Builtins()
	switch kind {
	case ast.ExprLitInt:
		return b.Int
	case ast.ExprLitUint:
		return b.Uint
	case ast.ExprLitFloat:
		return b.Float
	case ast.ExprLitString:
		return b.String
	case ast.ExprLitTrue, ast.ExprLitFalse:
		return b.Bool
	case ast.ExprLitNothing:
		return b.Nothing
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeUnary(exprID ast.ExprID, span source.Span, data *ast.ExprUnaryData) types.TypeID {
	operandType := tc.typeExpr(data.Operand)
	spec, ok := types.UnarySpecFor(data.Op)
	if !ok {
		return operandType
	}
	if operandType == types.NoTypeID && spec.Result != types.UnaryResultReference {
		return types.NoTypeID
	}
	if magic := tc.magicResultForUnary(operandType, data.Op); magic != types.NoTypeID {
		return magic
	}
	family := tc.familyOf(operandType)
	if spec.Result != types.UnaryResultReference && !tc.familyMatches(family, spec.Operand) {
		tc.report(diag.SemaInvalidUnaryOperand, span, "operator %s cannot be applied to %s", tc.unaryOpLabel(data.Op), tc.typeLabel(operandType))
		return types.NoTypeID
	}
	switch spec.Result {
	case types.UnaryResultNumeric, types.UnaryResultSame:
		return operandType
	case types.UnaryResultBool:
		return tc.types.Builtins().Bool
	case types.UnaryResultReference:
		mutable := data.Op == ast.ExprUnaryRefMut
		tc.handleBorrow(exprID, span, data.Op, data.Operand)
		if operandType == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(operandType, mutable))
	case types.UnaryResultDeref:
		elem, ok := tc.elementType(operandType)
		if !ok {
			tc.report(diag.SemaInvalidUnaryOperand, span, "cannot dereference %s", tc.typeLabel(operandType))
			return types.NoTypeID
		}
		return elem
	case types.UnaryResultAwait:
		return operandType
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeBinary(exprID ast.ExprID, span source.Span, data *ast.ExprBinaryData) types.TypeID {
	leftType := tc.typeExpr(data.Left)
	rightType := tc.typeExpr(data.Right)
	specs := types.BinarySpecs(data.Op)
	if len(specs) == 0 {
		return types.NoTypeID
	}
	leftFamily := tc.familyOf(leftType)
	rightFamily := tc.familyOf(rightType)
	for _, spec := range specs {
		assign := spec.Flags&types.BinaryFlagAssignment != 0
		if !assign {
			if leftType == types.NoTypeID || rightType == types.NoTypeID {
				continue
			}
			if !tc.familyMatches(leftFamily, spec.Left) || !tc.familyMatches(rightFamily, spec.Right) {
				continue
			}
			if spec.Flags&types.BinaryFlagSameFamily != 0 && leftFamily != rightFamily {
				continue
			}
		}
		switch spec.Result {
		case types.BinaryResultLeft:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return leftType
		case types.BinaryResultRight:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return rightType
		case types.BinaryResultBool:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return tc.types.Builtins().Bool
		case types.BinaryResultNumeric:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return tc.pickNumericResult(leftType, rightType)
		case types.BinaryResultRange:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return types.NoTypeID
		default:
			return types.NoTypeID
		}
	}
	if magic := tc.magicResultForBinary(leftType, rightType, data.Op); magic != types.NoTypeID {
		return magic
	}
	tc.report(diag.SemaInvalidBinaryOperands, span, "operator %s is not defined for %s and %s", tc.binaryOpLabel(data.Op), tc.typeLabel(leftType), tc.typeLabel(rightType))
	return types.NoTypeID
}

func (tc *typeChecker) pickNumericResult(left, right types.TypeID) types.TypeID {
	if left != types.NoTypeID {
		return left
	}
	if right != types.NoTypeID {
		return right
	}
	return tc.types.Builtins().Int
}

func (tc *typeChecker) elementType(id types.TypeID) (types.TypeID, bool) {
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return types.NoTypeID, false
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		return tt.Elem, true
	default:
		return types.NoTypeID, false
	}
}

func (tc *typeChecker) familyOf(id types.TypeID) types.FamilyMask {
	if id == types.NoTypeID || tc.types == nil {
		return types.FamilyNone
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return types.FamilyNone
	}
	switch tt.Kind {
	case types.KindBool:
		return types.FamilyBool
	case types.KindInt:
		return types.FamilySignedInt
	case types.KindUint:
		return types.FamilyUnsignedInt
	case types.KindFloat:
		return types.FamilyFloat
	case types.KindString:
		return types.FamilyString
	case types.KindArray:
		return types.FamilyArray
	case types.KindPointer:
		return types.FamilyPointer
	case types.KindReference:
		return types.FamilyReference
	default:
		return types.FamilyAny
	}
}

func (tc *typeChecker) familyMatches(actual, expected types.FamilyMask) bool {
	if expected == types.FamilyAny {
		return actual != types.FamilyNone
	}
	return actual&expected != 0
}

func (tc *typeChecker) typeLabel(id types.TypeID) string {
	if id == types.NoTypeID || tc.types == nil {
		return "unknown"
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return "unknown"
	}
	switch tt.Kind {
	case types.KindBool:
		return "bool"
	case types.KindInt:
		return "int"
	case types.KindUint:
		return "uint"
	case types.KindFloat:
		return "float"
	case types.KindString:
		return "string"
	case types.KindNothing:
		return "nothing"
	case types.KindUnit:
		return "unit"
	case types.KindReference:
		prefix := "&"
		if tt.Mutable {
			prefix = "&mut "
		}
		return prefix + tc.typeLabel(tt.Elem)
	case types.KindPointer:
		return fmt.Sprintf("*%s", tc.typeLabel(tt.Elem))
	case types.KindArray:
		return fmt.Sprintf("[%s]", tc.typeLabel(tt.Elem))
	case types.KindOwn:
		return fmt.Sprintf("own %s", tc.typeLabel(tt.Elem))
	default:
		return tt.Kind.String()
	}
}

func (tc *typeChecker) report(code diag.Code, span source.Span, format string, args ...interface{}) {
	if tc.reporter == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if b := diag.ReportError(tc.reporter, code, span, msg); b != nil {
		b.Emit()
	}
}

func (tc *typeChecker) binaryOpLabel(op ast.ExprBinaryOp) string {
	switch op {
	case ast.ExprBinaryAdd:
		return "+"
	case ast.ExprBinarySub:
		return "-"
	case ast.ExprBinaryMul:
		return "*"
	case ast.ExprBinaryDiv:
		return "/"
	case ast.ExprBinaryMod:
		return "%"
	case ast.ExprBinaryLogicalAnd:
		return "&&"
	case ast.ExprBinaryLogicalOr:
		return "||"
	case ast.ExprBinaryBitAnd:
		return "&"
	case ast.ExprBinaryBitOr:
		return "|"
	case ast.ExprBinaryBitXor:
		return "^"
	case ast.ExprBinaryShiftLeft:
		return "<<"
	case ast.ExprBinaryShiftRight:
		return ">>"
	default:
		return fmt.Sprintf("op#%d", op)
	}
}

func (tc *typeChecker) unaryOpLabel(op ast.ExprUnaryOp) string {
	switch op {
	case ast.ExprUnaryPlus:
		return "+"
	case ast.ExprUnaryMinus:
		return "-"
	case ast.ExprUnaryNot:
		return "!"
	case ast.ExprUnaryDeref:
		return "*"
	case ast.ExprUnaryRef:
		return "&"
	case ast.ExprUnaryRefMut:
		return "&mut"
	default:
		return fmt.Sprintf("op#%d", op)
	}
}

func (tc *typeChecker) symbolName(id source.StringID) string {
	return tc.lookupName(id)
}

func (tc *typeChecker) typeKeyForType(id types.TypeID) symbols.TypeKey {
	if id == types.NoTypeID || tc.types == nil {
		return ""
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindBool:
		return symbols.TypeKey("bool")
	case types.KindInt:
		return symbols.TypeKey("int")
	case types.KindUint:
		return symbols.TypeKey("uint")
	case types.KindFloat:
		return symbols.TypeKey("float")
	case types.KindString:
		return symbols.TypeKey("string")
	case types.KindNothing:
		return symbols.TypeKey("nothing")
	case types.KindUnit:
		return symbols.TypeKey("unit")
	default:
		return ""
	}
}

func (tc *typeChecker) typeFromKey(key symbols.TypeKey) types.TypeID {
	if key == "" {
		return types.NoTypeID
	}
	switch string(key) {
	case "bool":
		return tc.types.Builtins().Bool
	case "int":
		return tc.types.Builtins().Int
	case "uint":
		return tc.types.Builtins().Uint
	case "float":
		return tc.types.Builtins().Float
	case "string":
		return tc.types.Builtins().String
	case "nothing":
		return tc.types.Builtins().Nothing
	case "unit":
		return tc.types.Builtins().Unit
	default:
		return types.NoTypeID
	}
}
