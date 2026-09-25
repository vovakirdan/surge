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
//
// Coverage is decided here, alternative by alternative, and only a pattern that
// matches EVERY value of an alternative removes it: a pattern that is `all`
// matches every alternative, a pattern that selects one alternative removes it
// only when it is not `partial`, and a partial pattern (a constant compared at
// run time, a tag whose payload patterns can miss, a tuple with a refutable
// element) keeps its alternative on both continuations. The checker's own
// consumption (type_expr_compare.go, matchedUnionMembers) is not used: it lets a
// tag pattern consume its member whatever its payload patterns are, and ignores
// guards, so `Success(nothing)` alone would read as covering `Success`.
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
	// partial: the pattern is known and selects choice, but may miss some values
	// of it, so the alternative stays on the unmatched continuation too.
	partial bool
	choice  returnOriginCompareChoice
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
		case !p.known || choice.opaque || p.choice.opaque:
			matched, missed = append(matched, choice), append(missed, choice)
		case p.choice == choice && p.partial:
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
		return b.runtimeTestPattern(id), nil
	case ast.ExprUnary:
		// A negated numeric literal (`-1`) is a constant too.
		if unary, ok := u.Builder.Exprs.Unary(id); ok && unary != nil && unary.Op == ast.ExprUnaryMinus {
			if inner := u.Builder.Exprs.Get(unary.Operand); inner != nil && inner.Kind == ast.ExprLit {
				return b.runtimeTestPattern(id), nil
			}
		}
	case ast.ExprMember:
		// An enum variant (`Color::Red`) is a constant the checker resolved.
		if _, variant := u.Sema.EnumVariantUses[id]; variant {
			return b.runtimeTestPattern(id), nil
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
				out.known = out.known && child.known
				// A payload pattern that can miss leaves the tag's alternative partly unmatched.
				out.partial = out.partial || !child.all
			}
			return out, nil
		}
	case ast.ExprTuple:
		// A tuple subject is one alternative; the pattern covers it only when every
		// element pattern does, and is partial otherwise.
		tuple, _ := u.Builder.Exprs.Tuple(id)
		out.choice = returnOriginCompareChoice{opaque: true}
		for _, elem := range tuple.Elements {
			child, err := b.readComparePattern(elem, scope)
			if err != nil {
				return out, err
			}
			out.bindings = append(out.bindings, child.bindings...)
			out.runtime = append(out.runtime, child.runtime...)
			out.known = out.known && child.known
			out.all = out.all && child.all
		}
		out.partial = !out.all
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

// runtimeTestPattern is a constant pattern: its operand is evaluated like any
// typed expression, it binds nothing, and it may match or miss any value of any
// alternative, so both continuations stay.
// A constant the checker left untyped (a string literal pattern today) keeps the
// compare-pattern row instead of being evaluated.
func (b *returnOriginBody) runtimeTestPattern(id ast.ExprID) returnOriginComparePattern {
	if b.function.unit.Sema.ExprTypes[id] == types.NoTypeID {
		return returnOriginComparePattern{choice: returnOriginCompareChoice{opaque: true}}
	}
	return returnOriginComparePattern{known: true, partial: true, choice: returnOriginCompareChoice{opaque: true}, runtime: []ast.ExprID{id}}
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
