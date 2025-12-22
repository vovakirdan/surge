package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
)

func (fr *fileResolver) externReceiverTypeParams(target ast.TypeID) []source.StringID {
	if fr == nil || fr.builder == nil || !target.IsValid() {
		return nil
	}
	seen := make(map[source.StringID]struct{})
	params := make([]source.StringID, 0, 2)
	var visit func(ast.TypeID)
	visit = func(id ast.TypeID) {
		if !id.IsValid() {
			return
		}
		expr := fr.builder.Types.Get(id)
		if expr == nil {
			return
		}
		switch expr.Kind {
		case ast.TypeExprPath:
			path, _ := fr.builder.Types.Path(id)
			if path == nil {
				return
			}
			for _, seg := range path.Segments {
				for _, gen := range seg.Generics {
					if p, ok := fr.builder.Types.Path(gen); ok && p != nil && len(p.Segments) == 1 && len(p.Segments[0].Generics) == 0 {
						name := p.Segments[0].Name
						if name == source.NoStringID || fr.isKnownTypeName(name) {
							continue
						}
						if _, exists := seen[name]; !exists {
							seen[name] = struct{}{}
							params = append(params, name)
						}
						continue
					}
					visit(gen)
				}
			}
		case ast.TypeExprUnary:
			if unary, ok := fr.builder.Types.UnaryType(id); ok && unary != nil {
				visit(unary.Inner)
			}
		case ast.TypeExprArray:
			if arr, ok := fr.builder.Types.Array(id); ok && arr != nil {
				visit(arr.Elem)
			}
		case ast.TypeExprOptional:
			if opt, ok := fr.builder.Types.Optional(id); ok && opt != nil {
				visit(opt.Inner)
			}
		case ast.TypeExprErrorable:
			if errable, ok := fr.builder.Types.Errorable(id); ok && errable != nil {
				visit(errable.Inner)
				if errable.Error.IsValid() {
					visit(errable.Error)
				}
			}
		case ast.TypeExprTuple:
			if tup, ok := fr.builder.Types.Tuple(id); ok && tup != nil {
				for _, elem := range tup.Elems {
					visit(elem)
				}
			}
		case ast.TypeExprFn:
			if fn, ok := fr.builder.Types.Fn(id); ok && fn != nil {
				for _, param := range fn.Params {
					visit(param.Type)
				}
				visit(fn.Return)
			}
		}
	}
	visit(target)
	return params
}

func (fr *fileResolver) isKnownTypeName(id source.StringID) bool {
	name := fr.lookupString(id)
	if name == "" {
		return false
	}
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float", "float16", "float32", "float64",
		"bool", "string", "nothing", "unit":
		return true
	}
	if fr.resolver != nil && fr.result != nil && fr.result.Table != nil {
		if symID, ok := fr.resolver.Lookup(id); ok {
			if sym := fr.result.Table.Symbols.Get(symID); sym != nil && sym.Kind == SymbolType {
				return true
			}
		}
	}
	return false
}
