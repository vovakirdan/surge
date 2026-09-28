package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// returnOriginMapLiteralTyping is the refusal of a map literal whose own type is
// not the canonical core Map of exactly its entries' key and value types.
const returnOriginMapLiteralTyping = "map literal disagrees with its typed key and value"

// returnOriginMapLiteralUnvisited is the refusal of a map literal whose entries
// hold a form that needs scopes or bindings the symbol resolver records: the
// resolver does not walk map literal entries (symbols/resolve_walk.go), so no
// such fact exists for them.
const returnOriginMapLiteralUnvisited = "map literal entry needs scopes the resolver did not record"

// returnOriginMapLiteralTask is the refusal of a map literal whose key or value
// type may hold a task handle: a task stored in a map is never drained by the
// checker, so its lifetime is the task check's question, not this transfer's.
const returnOriginMapLiteralTask = "map literal whose entries may hold a task needs the task check"

// mapLiteral answers `{ k => v, ... }` as the MIR lowers it (lower_expr_literals.go):
// a fresh rt_map_new, then each key and then its value evaluated in source order
// and handed to rt_map_insert. It is therefore the Map rules of an insert into a
// fresh owning map (return_origin_map.go): the literal holds the joined contents
// of its values, a payload-free value keeps no loan (G6), and keys are never
// tracked, so a key type that can carry a loan is refused and every other key
// type records NoBorrowedState(K). The container and its key and value types
// are read from the literal's own typed result, never from a name.
func (b *returnOriginBody) mapLiteral(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	in := u.Sema.TypeInterner
	node := u.Builder.Exprs.Get(id)
	data, ok := u.Builder.Exprs.Map(id)
	c, isMap := returnOriginMapContainer(in, u.Sema.ExprTypes[id])
	key := returnOriginMapKey(in, c)
	typed := ok && data != nil && isMap && !c.reference && key != types.NoTypeID && len(data.Entries) != 0
	if typed {
		for _, entry := range data.Entries {
			typed = typed && u.Sema.ExprTypes[entry.Key] == key && u.Sema.ExprTypes[entry.Value] == c.element
		}
	}
	valuesFree := typed && b.elementsFree(c)
	out := originExprValue(env, returnOriginValueOf())
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	if ok && data != nil && !returnOriginMapEntriesVisitable(u.Builder.Exprs, b.untypedTagCall, data.Entries) {
		b.pending(node.Span, returnOriginMapLiteralUnvisited)
		out.value = unknown
		return out, nil
	}
	contents := returnOriginValueOf()
	var entries []ast.ExprMapEntry
	if ok && data != nil {
		entries = data.Entries
	}
	for _, entry := range entries {
		for _, child := range [...]ast.ExprID{entry.Key, entry.Value} {
			next, err := b.expr(child, out.flow.normal, targets)
			if err != nil {
				return returnOriginExprResult{}, err
			}
			switch {
			case child == entry.Key && b.shape(child) == returnOriginRefFree:
				b.discardLoans(next.value, node.Span) // an untracked key keeps no loan
			case child == entry.Value && valuesFree:
				b.discardLoans(next.value, node.Span) // G6, as a store into a payload-free backing
			case child == entry.Value:
				contents = contents.join(next.value)
			}
			out.flow.normal = returnOriginEnv{}
			out.flow = out.flow.join(next.flow)
			if !out.flow.normal.reachable {
				out.value = returnOriginValue{}
				return out, nil
			}
		}
	}
	switch {
	case !typed:
		b.pending(node.Span, returnOriginMapLiteralTyping)
		out.value = unknown
	case b.analyzer.mapLiteralMayHoldTask(in, key) || b.analyzer.mapLiteralMayHoldTask(in, c.element):
		b.pending(node.Span, returnOriginMapLiteralTask)
		out.value = unknown
	case b.analyzer.loanCarrier(key), b.analyzer.loanCarrier(c.element):
		// As an insert (checkMapIntrinsicUse): a stored key or value that can
		// carry storage loans needs the backing loan transfer.
		b.pending(node.Span, returnOriginCursorLoanElement)
		out.value = unknown
	case valuesFree:
		out.value = b.requireOpaqueState(returnOriginView(b.function), key, node.Span)
	default:
		out.value = contents.join(b.requireOpaqueState(returnOriginView(b.function), key, node.Span))
	}
	return out, nil
}

// returnOriginMapEntriesVisitable admits entries built only from forms whose
// transfer reads no resolver scope: an identifier without a resolved symbol
// already keeps its own row. Every other form, a block, a compare, a task or
// crossing body among them, and a tag constructor call whose target the
// checker left untyped (the resolver never saw it), is refused before it is
// evaluated.
func returnOriginMapEntriesVisitable(exprs *ast.Exprs, untypedTag func(call, target ast.ExprID) bool, entries []ast.ExprMapEntry) bool {
	var visit func(id ast.ExprID) bool
	visit = func(id ast.ExprID) bool {
		node := exprs.Get(id)
		if node == nil {
			return false
		}
		var children []ast.ExprID
		switch node.Kind {
		case ast.ExprIdent, ast.ExprLit:
		case ast.ExprGroup:
			data, _ := exprs.Group(id)
			children = []ast.ExprID{data.Inner}
		case ast.ExprUnary:
			data, _ := exprs.Unary(id)
			children = []ast.ExprID{data.Operand}
		case ast.ExprBinary:
			data, _ := exprs.Binary(id)
			children = []ast.ExprID{data.Left, data.Right}
		case ast.ExprCast:
			data, _ := exprs.Cast(id)
			children = []ast.ExprID{data.Value}
		case ast.ExprCall:
			data, _ := exprs.Call(id)
			if untypedTag(id, data.Target) {
				return false
			}
			children = []ast.ExprID{data.Target}
			for _, arg := range data.Args {
				children = append(children, arg.Value)
			}
		case ast.ExprIndex:
			data, _ := exprs.Index(id)
			children = []ast.ExprID{data.Target, data.Index}
		case ast.ExprMember:
			data, _ := exprs.Member(id)
			children = []ast.ExprID{data.Target}
		case ast.ExprTupleIndex:
			data, _ := exprs.TupleIndex(id)
			children = []ast.ExprID{data.Target}
		case ast.ExprArray:
			data, _ := exprs.Array(id)
			children = data.Elements
		case ast.ExprTuple:
			data, _ := exprs.Tuple(id)
			children = data.Elements
		case ast.ExprStruct:
			data, _ := exprs.Struct(id)
			for _, field := range data.Fields {
				children = append(children, field.Value)
			}
		case ast.ExprMap:
			data, _ := exprs.Map(id)
			for _, entry := range data.Entries {
				children = append(children, entry.Key, entry.Value)
			}
		default:
			return false
		}
		for _, child := range children {
			if child.IsValid() && !visit(child) {
				return false
			}
		}
		return true
	}
	for _, entry := range entries {
		if !visit(entry.Key) || !visit(entry.Value) {
			return false
		}
	}
	return true
}

// mapLiteralMayHoldTask says whether a map literal's key or value type may hold
// a core task handle: it names the core Task anywhere in its structure, it still
// names a generic parameter, or the core Task family is not known at all.
func (a *returnOriginAnalyzer) mapLiteralMayHoldTask(in *types.Interner, id types.TypeID) bool {
	if a.taskType == types.NoTypeID || types.ContainsGenericParam(in, id) {
		return true
	}
	seen := make(map[types.TypeID]bool)
	var walk func(id types.TypeID) bool
	walk = func(id types.TypeID) bool {
		if id == types.NoTypeID || seen[id] {
			return false
		}
		seen[id] = true
		if a.isCoreTask(in, id) {
			return true
		}
		typ, ok := in.Lookup(id)
		if !ok {
			return true
		}
		switch typ.Kind {
		case types.KindAlias:
			target, found := in.AliasTarget(id)
			return !found || walk(target)
		case types.KindOwn, types.KindFar, types.KindArray, types.KindReference, types.KindPointer:
			return walk(typ.Elem)
		case types.KindStruct:
			info, found := in.StructInfo(id)
			if !found || info == nil {
				return true
			}
			for _, arg := range info.TypeArgs {
				if walk(arg) {
					return true
				}
			}
			for _, field := range info.Fields {
				if walk(field.Type) {
					return true
				}
			}
		case types.KindUnion:
			info, found := in.UnionInfo(id)
			if !found || info == nil {
				return true
			}
			for _, member := range info.Members {
				if walk(member.Type) {
					return true
				}
				for _, arg := range member.TagArgs {
					if walk(arg) {
						return true
					}
				}
			}
		case types.KindTuple:
			info, found := in.TupleInfo(id)
			if !found || info == nil {
				return true
			}
			for _, elem := range info.Elems {
				if walk(elem) {
					return true
				}
			}
		}
		return false
	}
	return walk(id)
}

// untypedTagCall is the tag constructor call the tag transfer cannot read: its
// target has no type (return_origin_tags.go stops the analysis on it).
func (b *returnOriginBody) untypedTagCall(call, target ast.ExprID) bool {
	u := b.function.unit
	sym := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[call])
	return sym != nil && sym.Kind == symbols.SymbolTag && u.Sema.ExprTypes[target] == types.NoTypeID
}
