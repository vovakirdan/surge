package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These are transient alternatives of the already typed subject, not payload
// projections. Opaque alternatives retain both matching continuations.
type returnOriginCompareChoice struct {
	tag     source.StringID
	arity   int
	literal ast.ExprLitKind
	opaque  bool
}

type returnOriginComparePattern struct {
	bindings []symbols.SymbolID
	runtime  []ast.ExprID
	known    bool
	all      bool
	choice   returnOriginCompareChoice
}

func (b *returnOriginBody) compareChoices(id types.TypeID) []returnOriginCompareChoice {
	in := b.function.unit.Sema.TypeInterner
	for range 64 {
		typ, ok := in.Lookup(id)
		if !ok {
			break
		}
		switch typ.Kind {
		case types.KindAlias:
			id, _ = in.AliasTarget(id)
			continue
		case types.KindOwn, types.KindReference:
			id = typ.Elem
			continue
		case types.KindBool:
			return []returnOriginCompareChoice{{literal: ast.ExprLitTrue}, {literal: ast.ExprLitFalse}}
		case types.KindNothing:
			return []returnOriginCompareChoice{{literal: ast.ExprLitNothing}}
		case types.KindUnion:
			if info, found := in.UnionInfo(id); found && info != nil && len(info.Members) > 0 {
				out := make([]returnOriginCompareChoice, 0, len(info.Members))
				for _, member := range info.Members {
					choice := returnOriginCompareChoice{opaque: true}
					switch member.Kind {
					case types.UnionMemberTag:
						if member.TagName != source.NoStringID {
							choice = returnOriginCompareChoice{tag: member.TagName, arity: len(member.TagArgs)}
						}
					case types.UnionMemberNothing:
						choice = returnOriginCompareChoice{literal: ast.ExprLitNothing}
					}
					out = append(out, choice)
				}
				return out
			}
		}
		break
	}
	return []returnOriginCompareChoice{{opaque: true}}
}

func (p returnOriginComparePattern) split(choices []returnOriginCompareChoice) (matched, missed []returnOriginCompareChoice) {
	for _, choice := range choices {
		switch {
		case p.known && p.all:
			matched = append(matched, choice)
		case !p.known || choice.opaque:
			matched, missed = append(matched, choice), append(missed, choice)
		case p.choice == choice:
			matched = append(matched, choice)
		default:
			missed = append(missed, choice)
		}
	}
	return matched, missed
}

func (b *returnOriginBody) readComparePattern(id ast.ExprID, scope symbols.ScopeID) (returnOriginComparePattern, error) {
	u := b.function.unit
	node := u.Builder.Exprs.Get(id)
	if node == nil {
		return returnOriginComparePattern{}, fmt.Errorf("return origins: missing compare pattern %d", id)
	}
	out := returnOriginComparePattern{known: true, all: true}
	symID := u.Symbols.ExprSymbols[id]
	sym := u.Symbols.Table.Symbols.Get(symID)
	switch node.Kind {
	case ast.ExprGroup:
		group, _ := u.Builder.Exprs.Group(id)
		return b.readComparePattern(group.Inner, scope)
	case ast.ExprIdent:
		ident, _ := u.Builder.Exprs.Ident(id)
		if name, _ := u.Builder.StringsInterner.Lookup(ident.Name); name == "_" && !symID.IsValid() {
			return out, nil
		}
		if sym != nil && sym.Kind == symbols.SymbolLet {
			typ, present := u.Sema.BindingTypes[symID]
			if sym.Scope != scope || !present || typ == types.NoTypeID || sym.Type != typ {
				return out, fmt.Errorf("return origins: pattern binding %d has no exact original scope and type", symID)
			}
			out.bindings = append(out.bindings, symID)
			return out, nil
		}
	case ast.ExprLit:
		lit, _ := u.Builder.Exprs.Literal(id)
		if lit.Kind == ast.ExprLitNothing || lit.Kind == ast.ExprLitTrue || lit.Kind == ast.ExprLitFalse {
			out.all, out.choice.literal = false, lit.Kind
			return out, nil
		}
	case ast.ExprCall:
		call, _ := u.Builder.Exprs.Call(id)
		tag := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[call.Target])
		if tag != nil && tag.Kind == symbols.SymbolTag {
			out.all, out.choice = false, returnOriginCompareChoice{tag: tag.Name, arity: len(call.Args)}
			for _, arg := range call.Args {
				child, err := b.readComparePattern(arg.Value, scope)
				if err != nil {
					return out, err
				}
				out.bindings = append(out.bindings, child.bindings...)
				out.runtime = append(out.runtime, child.runtime...)
				out.known = out.known && child.known && child.all
			}
			return out, nil
		}
	case ast.ExprTuple:
		// Preserve original bindings/children; tuple matching is still pending.
		tuple, _ := u.Builder.Exprs.Tuple(id)
		out.known, out.all = false, false
		for _, elem := range tuple.Elements {
			child, err := b.readComparePattern(elem, scope)
			if err != nil {
				return out, err
			}
			out.bindings = append(out.bindings, child.bindings...)
			out.runtime = append(out.runtime, child.runtime...)
		}
		return out, nil
	}
	if sym != nil && sym.Kind == symbols.SymbolTag {
		out.all, out.choice = false, returnOriginCompareChoice{tag: sym.Name}
		return out, nil
	}
	// Unsupported runtime tests still use the ordinary typed-expression reader.
	out.known, out.all, out.runtime = false, false, []ast.ExprID{id}
	return out, nil
}

func (b *returnOriginBody) bindCompareOrigins(bindings []symbols.SymbolID, subject returnOriginValue, env returnOriginEnv) returnOriginEnv {
	u := b.function.unit
	for _, id := range bindings {
		sym := u.Symbols.Table.Symbols.Get(id)
		value := subject.clone()
		switch returnOriginView(b.function).shape(sym.Type) {
		case returnOriginRefFree:
			if !b.analyzer.loanCarrier(sym.Type) {
				value = returnOriginValueOf()
			}
		case returnOriginShapeUnknown:
			b.pending(sym.Span, "pattern binding needs concrete reference contents")
			value = value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
		}
		env = env.assign(id, sym.Scope, value)
	}
	return env
}
