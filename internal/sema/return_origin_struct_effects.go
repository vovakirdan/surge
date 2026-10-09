package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (a *returnOriginAnalyzer) preservesBorrowedStructSlot(fn *returnOriginFunction, slot int) bool {
	return a.preserveStructSlot(fn, slot, make(map[string]bool), make(map[string]bool))
}

func (a *returnOriginAnalyzer) preserveStructSlot(fn *returnOriginFunction, slot int, visiting, proven map[string]bool) bool {
	if fn == nil || fn.info == nil || fn.candidate == nil || !fn.item.Body.IsValid() || len(fn.candidate.TemplateParams) != 0 ||
		slot < 0 || slot >= len(fn.info.Params) || slot >= len(fn.params) {
		return false
	}
	key := fmt.Sprintf("%s/%d", fn.key, slot)
	if proven[key] || visiting[key] {
		return true
	}
	in := fn.unit.Sema.TypeInterner
	outer, ok := in.Lookup(returnOriginResolveAlias(in, fn.info.Params[slot]))
	if !ok || outer.Kind != types.KindReference || !outer.Mutable {
		return false
	}
	info, ok := in.StructInfo(returnOriginResolveAlias(in, outer.Elem))
	if !ok || info == nil || !returnOriginPlainStruct(fn, info) {
		return false
	}
	body := &returnOriginBody{analyzer: a, function: fn}
	protected := make(map[source.StringID]bool)
	fieldTypes := make(map[source.StringID]types.TypeID)
	for _, field := range info.Fields {
		fieldTypes[field.Name] = field.Type
		if returnOriginTypeShape(in, field.Type, nil) != returnOriginRefFree || body.holdsLoan(field.Type) {
			protected[field.Name] = true
		}
	}
	if len(protected) == 0 {
		return false
	}
	visiting[key] = true
	defer delete(visiting, key)
	param := fn.params[slot]
	for id := range fn.unit.Sema.ExprTypes {
		node := fn.unit.Builder.Exprs.Get(id)
		if node == nil || node.Span.File != fn.item.Span.File || node.Span.Start < fn.item.Span.Start || node.Span.End > fn.item.Span.End {
			continue
		}
		if data, binary := fn.unit.Builder.Exprs.Binary(id); binary && data != nil && data.Op == ast.ExprBinaryAssign {
			root, field := returnOriginStructPlace(fn.unit, data.Left)
			if root == param && (field == source.NoStringID || protected[field]) {
				return false
			}
		}
		call, called := fn.unit.Builder.Exprs.Call(id)
		if !called || call == nil || call.HasNamedArgs() {
			continue
		}
		selected := fn.unit.Symbols.ExprSymbols[id]
		callee, reason := a.selectedCallableFunction(fn.unit, selected)
		if reason != "" || callee == nil || len(call.Args) != len(callee.info.Params) {
			if returnOriginCallTouchesStructParam(fn.unit, call, param) {
				return false
			}
			continue
		}
		for i, arg := range call.Args {
			root, field := returnOriginStructPlace(fn.unit, arg.Value)
			if root != param {
				continue
			}
			formal, ok := in.Lookup(returnOriginResolveAlias(in, callee.info.Params[i]))
			if field != source.NoStringID {
				if protected[field] && (!ok || formal.Kind != types.KindReference || formal.Mutable ||
					returnOriginResolveAlias(in, formal.Elem) != returnOriginResolveAlias(in, fieldTypes[field])) {
					return false
				}
				continue
			}
			if !ok || formal.Kind != types.KindReference || formal.Mutable && !a.preserveStructSlot(callee, i, visiting, proven) {
				return false
			}
		}
	}
	proven[key] = true
	return true
}

func returnOriginCallTouchesStructParam(u *returnOriginUnitIndex, call *ast.ExprCallData, param symbols.SymbolID) bool {
	for _, arg := range call.Args {
		if root, _ := returnOriginStructPlace(u, arg.Value); root == param {
			return true
		}
	}
	return false
}

func returnOriginStructPlace(u *returnOriginUnitIndex, id ast.ExprID) (symbols.SymbolID, source.StringID) {
	field := source.NoStringID
	for id.IsValid() {
		node := u.Builder.Exprs.Get(id)
		if node == nil {
			break
		}
		switch node.Kind {
		case ast.ExprIdent:
			return u.Symbols.ExprSymbols[id], field
		case ast.ExprMember:
			data, _ := u.Builder.Exprs.Member(id)
			field, id = data.Field, data.Target
		case ast.ExprGroup:
			data, _ := u.Builder.Exprs.Group(id)
			id = data.Inner
		case ast.ExprUnary:
			data, _ := u.Builder.Exprs.Unary(id)
			id = data.Operand
		case ast.ExprIndex:
			data, _ := u.Builder.Exprs.Index(id)
			id = data.Target
		default:
			return symbols.NoSymbolID, source.NoStringID
		}
	}
	return symbols.NoSymbolID, source.NoStringID
}
