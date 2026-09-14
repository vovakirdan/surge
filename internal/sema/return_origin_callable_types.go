package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// This immutable tree keeps the declared relation at each function-type node.
// It is not a body summary or evidence about a callable's captured contents.
type returnOriginCallableType struct {
	slots   []uint32
	promise source.Span
	params  []*returnOriginCallableType
	result  *returnOriginCallableType
}

func (b *returnOriginBody) readCallableType(u *returnOriginUnitIndex, typ types.TypeID, expr ast.TypeID, span source.Span, active map[types.TypeID]bool, views ...returnOriginTypeView) *returnOriginCallableType {
	in := u.Sema.TypeInterner
	view := returnOriginView(b.function)
	if len(views) != 0 {
		view = views[0]
	}
	if active[typ] || !view.validType(typ) {
		b.pending(span, "callable type needs a finite concrete original declaration")
		return nil
	}
	active[typ] = true
	defer delete(active, typ)
	if alias, ok := in.AliasInfo(typ); ok {
		owner, target := b.callableAliasOwner(typ, alias, span)
		if owner == nil {
			return nil
		}
		return b.readCallableType(owner, alias.Target, target, span, active, view)
	}
	info, ok := in.FnInfo(typ)
	node := u.Builder.Types.Get(expr)
	if !ok || node == nil || node.Kind != ast.TypeExprFn || node.Span.File != u.Builder.Files.Get(u.FileID).Span.File {
		b.pending(span, "callable promise needs its original owning function-type syntax")
		return nil
	}
	syntax := symbols.FunctionTypeReturnSourceSyntax(u.Builder, expr)
	return b.buildCallableType(u, symbols.NoSymbolID, expr, syntax, info, span, active, view)
}

// Nominal identity supplies the source file and full declaration span. Resolve
// that declaration in its retained unit, including units with no function body.
// An imported symbol's empty Decl or a same-name alias is never an owner proof.
func (b *returnOriginBody) callableAliasOwner(typ types.TypeID, alias *types.AliasInfo, span source.Span) (*returnOriginUnitIndex, ast.TypeID) {
	var owner *returnOriginUnitIndex
	var target ast.TypeID
	if len(alias.TypeArgs) != 0 {
		b.pending(span, "generic callable alias needs its original instantiation obligation")
		return nil, ast.NoTypeID
	}
	for _, unit := range b.analyzer.units {
		file := unit.Builder.Files.Get(unit.FileID)
		if file.Span.File != alias.Decl.File {
			continue
		}
		for _, id := range file.Items {
			item, ok := unit.Builder.Items.Type(id)
			if !ok || item.Span != alias.Decl {
				continue
			}
			decl := unit.Builder.Items.TypeAlias(item)
			ids := unit.Symbols.ItemSymbols[id]
			if owner != nil || decl == nil || len(ids) != 1 || len(item.Generics) != 0 || item.TypeParamsCount != 0 {
				b.pending(span, "callable alias has no unique nongeneric original declaration")
				return nil, ast.NoTypeID
			}
			sym := unit.Symbols.Table.Symbols.Get(ids[0])
			if sym == nil || sym.Kind != symbols.SymbolType || sym.Type != typ ||
				sym.Decl.ASTFile != unit.FileID || sym.Decl.Item != id || sym.Decl.SourceFile != alias.Decl.File {
				b.pending(span, "callable alias disagrees with its original typed declaration")
				return nil, ast.NoTypeID
			}
			owner, target = unit, decl.Target
		}
	}
	if owner == nil || !target.IsValid() || alias.Target == types.NoTypeID {
		b.pending(span, "callable alias lacks its retained original source unit")
		return nil, ast.NoTypeID
	}
	return owner, target
}

func (b *returnOriginBody) functionCallableType(fn *returnOriginFunction, span source.Span) *returnOriginCallableType {
	sym := fn.unit.Symbols.Table.Symbols.Get(fn.symbol)
	if sym == nil || b.callableType(sym.Type, span, returnOriginView(fn)) == nil {
		return nil
	}
	return b.buildCallableType(fn.unit, fn.symbol, ast.NoTypeID,
		symbols.FunctionReturnSourceSyntax(fn.unit.Builder, fn.item), fn.info, span, make(map[types.TypeID]bool), returnOriginView(fn))
}

func (b *returnOriginBody) buildCallableType(u *returnOriginUnitIndex, owner symbols.SymbolID, expr ast.TypeID, syntax symbols.ReturnSourceSyntax, info *types.FnInfo, span source.Span, active map[types.TypeID]bool, views ...returnOriginTypeView) *returnOriginCallableType {
	view := returnOriginView(b.function)
	if len(views) != 0 {
		view = views[0]
	}
	params := syntax.Params()
	if len(params) != len(info.Params) || !syntax.Sources().Equal(info.ReturnSources()) {
		b.pending(span, "callable syntax does not match its original typed signature")
		return nil
	}
	// The immutable original request remains authoritative under a read view;
	// a substituted descriptor never impersonates its source declaration.
	if !syntax.Sources().IsAllInputs() {
		for _, request := range u.Sema.ReturnSourceDeclarations {
			if request.Owner == owner && request.TypeExpr == expr && request.Syntax.Span() == syntax.Span() &&
				(!slices.Equal(request.Params(), info.Params) || request.Result() != info.Result) {
				b.pending(span, "callable promise needs its original concrete typed request")
				return nil
			}
		}
	}
	slots, valid := b.declaredSources(u, owner, expr, syntax, info, span, &returnOriginSignature{params: info.Params, result: info.Result, binding: &view})
	if !valid {
		return nil
	}
	out := &returnOriginCallableType{slots: slices.Clone(slots), promise: syntax.Span(),
		params: make([]*returnOriginCallableType, len(params))}
	for i, typ := range info.Params {
		if returnOriginFnInfo(u.Sema.TypeInterner, typ) == nil {
			continue
		}
		out.params[i] = b.readCallableType(u, typ, params[i], span, active, view)
		if out.params[i] == nil {
			return nil
		}
	}
	if returnOriginFnInfo(u.Sema.TypeInterner, info.Result) != nil {
		out.result = b.readCallableType(u, info.Result, syntax.Result(), span, active, view)
		if out.result == nil {
			return nil
		}
	}
	return out
}
