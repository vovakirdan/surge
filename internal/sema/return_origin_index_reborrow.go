package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// localFieldReborrowIndex certifies the narrow place made by
// `let cells = &mut self.cells; cells[i]`: the local holds a reborrow of one
// exact canonical container field, so its evaluated owner is the original
// referent rather than an arbitrary external reference cell. Parameters,
// calls, shared layers and selected/user index operations stay outside.
func (b *returnOriginBody) localFieldReborrowIndex(id ast.ExprID, owner returnOriginValue) (returnOriginIndexType, bool) {
	u := b.function.unit
	in := u.Sema.TypeInterner
	data, ok := u.Builder.Exprs.Index(id)
	if !ok || data == nil || u.Sema.ExprTypes[data.Index] != in.Builtins().Int || !returnOriginExactProjectionOwner(owner) {
		return returnOriginIndexType{}, false
	}
	for _, expr := range [...]ast.ExprID{id, data.Target, data.Index} {
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return returnOriginIndexType{}, false
		}
	}
	if _, selected := u.Sema.IndexSymbols[id]; selected {
		return returnOriginIndexType{}, false
	}
	if _, selected := u.Sema.IndexSetSymbols[id]; selected {
		return returnOriginIndexType{}, false
	}
	target := b.ungroup(data.Target)
	if node := u.Builder.Exprs.Get(target); node == nil || node.Kind != ast.ExprIdent {
		return returnOriginIndexType{}, false
	}
	sym := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[target])
	if sym == nil || sym.Kind != symbols.SymbolLet || !b.within(sym.Scope, b.function.scope) ||
		sym.Type != u.Sema.ExprTypes[data.Target] {
		return returnOriginIndexType{}, false
	}
	declNode := u.Builder.Stmts.Get(sym.Decl.Stmt)
	decl := u.Builder.Stmts.Let(sym.Decl.Stmt)
	if declNode == nil || declNode.Kind != ast.StmtLet || decl == nil || !decl.Value.IsValid() ||
		u.stmtSymbols[sym.Decl.Stmt] != u.Symbols.ExprSymbols[target] || u.Sema.ExprTypes[decl.Value] != sym.Type {
		return returnOriginIndexType{}, false
	}
	borrow, ok := u.Builder.Exprs.Unary(b.ungroup(decl.Value))
	if !ok || borrow == nil || borrow.Op != ast.ExprUnaryRefMut {
		return returnOriginIndexType{}, false
	}
	memberID := b.ungroup(borrow.Operand)
	member, ok := u.Builder.Exprs.Member(memberID)
	if !ok || member == nil || u.Sema.ExprTypes[memberID] == types.NoTypeID {
		return returnOriginIndexType{}, false
	}
	for _, expr := range [...]ast.ExprID{decl.Value, memberID, member.Target} {
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return returnOriginIndexType{}, false
		}
	}
	_, outer, ok := returnOriginReferenceLayer(in, sym.Type)
	innerID, inner, nested := returnOriginReferenceLayer(in, outer.Elem)
	if !ok || !outer.Mutable || !nested || !inner.Mutable || u.Sema.ExprTypes[memberID] != innerID {
		return returnOriginIndexType{}, false
	}
	container, canonical := returnOriginIndexContainer(in, inner.Elem)
	resultID, result, typed := returnOriginIndexResolve(in, u.Sema.ExprTypes[id])
	if !canonical || container.reference || !typed || resultID == types.NoTypeID || result.Kind != types.KindReference ||
		result.Mutable || result.Elem != container.element || !returnOriginMemberField(in, u.Sema.ExprTypes[member.Target], member.Field, inner.Elem) {
		return returnOriginIndexType{}, false
	}
	container.reference = true
	return container, true
}

func returnOriginReferenceLayer(in *types.Interner, id types.TypeID) (types.TypeID, types.Type, bool) {
	id, typ, ok := returnOriginIndexResolve(in, id)
	return id, typ, ok && typ.Kind == types.KindReference
}

func returnOriginMemberField(in *types.Interner, target types.TypeID, name source.StringID, fieldType types.TypeID) bool {
	_, targetType, ok := returnOriginIndexResolve(in, target)
	if !ok || targetType.Kind != types.KindReference {
		return false
	}
	info, found := in.StructInfo(targetType.Elem)
	if !found || info == nil {
		return false
	}
	matches := 0
	for _, field := range info.Fields {
		if field.Name == name && field.Type == fieldType {
			matches++
		}
	}
	return matches == 1
}
