package carriergate

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
)

func (graph *goOwnerGraph) typeParamBindings(
	spec *ast.TypeSpec,
	args []ast.Expr,
	outer map[string]goCarrierBinding,
) map[string]goCarrierBinding {
	bindings := cloneOwnerBindings(outer)
	if spec.TypeParams == nil {
		return bindings
	}
	argIndex := 0
	for _, field := range spec.TypeParams.List {
		for _, name := range field.Names {
			delete(bindings, name.Name)
			if argIndex < len(args) {
				bindings[name.Name] = goCarrierBinding{
					path:      graph.carrierPath(args[argIndex], outer, make(map[goOwnerVisitKey]bool)),
					canonical: graph.canonicalCarrierType(args[argIndex], outer),
				}
			} else {
				binding := goCarrierBinding{canonical: graph.canonicalCarrierType(field.Type, outer)}
				if label := graph.payloadConstraint(field.Type); label != "" {
					binding.path = []string{"universal"}
				}
				bindings[name.Name] = binding
			}
			argIndex++
		}
	}
	return bindings
}

func (graph *goOwnerGraph) carrierVisitKey(
	spec *ast.TypeSpec,
	bindings map[string]goCarrierBinding,
) goOwnerVisitKey {
	parts := make([]string, 0)
	if spec.TypeParams != nil {
		for _, field := range spec.TypeParams.List {
			for _, name := range field.Names {
				parts = append(parts, name.Name+"="+bindings[name.Name].canonical)
			}
		}
	}
	return goOwnerVisitKey{spec: spec, bindings: strings.Join(parts, ";")}
}

func (graph *goOwnerGraph) slotVisitKey(
	spec *ast.TypeSpec,
	bindings map[string]ast.Expr,
) goOwnerVisitKey {
	parts := make([]string, 0)
	if spec.TypeParams != nil {
		for _, field := range spec.TypeParams.List {
			for _, name := range field.Names {
				actual, ok := bindings[name.Name]
				if !ok {
					parts = append(parts, name.Name+"=<unbound>")
					continue
				}
				parts = append(parts, name.Name+"="+graph.canonicalSlotType(actual, bindings))
			}
		}
	}
	return goOwnerVisitKey{spec: spec, bindings: strings.Join(parts, ";")}
}

func (graph *goOwnerGraph) canonicalCarrierType(
	expr ast.Expr,
	bindings map[string]goCarrierBinding,
) string {
	resolve := func(name string) (string, bool) {
		binding, ok := bindings[name]
		return binding.canonical, ok
	}
	return graph.canonicalType(expr, resolve, make(map[*ast.TypeSpec]bool))
}

func (graph *goOwnerGraph) canonicalSlotType(
	expr ast.Expr,
	bindings map[string]ast.Expr,
) string {
	resolving := make(map[string]bool)
	var resolve func(string) (string, bool)
	resolve = func(name string) (string, bool) {
		actual, ok := bindings[name]
		if !ok {
			return "", false
		}
		if resolving[name] {
			return "$cycle(" + name + ")", true
		}
		resolving[name] = true
		canonical := graph.canonicalType(actual, resolve, make(map[*ast.TypeSpec]bool))
		delete(resolving, name)
		return canonical, true
	}
	return graph.canonicalType(expr, resolve, make(map[*ast.TypeSpec]bool))
}

func (graph *goOwnerGraph) canonicalType(
	expr ast.Expr,
	resolve func(string) (string, bool),
	aliases map[*ast.TypeSpec]bool,
) string {
	switch value := expr.(type) {
	case *ast.Ident:
		if resolved, ok := resolve(value.Name); ok {
			return resolved
		}
		for _, decl := range graph.types[value.Name] {
			if !decl.spec.Assign.IsValid() || aliases[decl.spec] {
				continue
			}
			aliases[decl.spec] = true
			canonical := graph.canonicalType(decl.spec.Type, resolve, aliases)
			delete(aliases, decl.spec)
			return canonical
		}
		return value.Name
	case *ast.StarExpr:
		return "*" + graph.canonicalType(value.X, resolve, aliases)
	case *ast.ArrayType:
		length := ""
		if value.Len != nil {
			length = graph.renderType(value.Len)
		}
		return "[" + length + "]" + graph.canonicalType(value.Elt, resolve, aliases)
	case *ast.MapType:
		return "map[" + graph.canonicalType(value.Key, resolve, aliases) + "]" +
			graph.canonicalType(value.Value, resolve, aliases)
	case *ast.ChanType:
		var prefix string
		switch value.Dir {
		case ast.SEND:
			prefix = "chan<- "
		case ast.RECV:
			prefix = "<-chan "
		default:
			prefix = "chan "
		}
		return prefix + graph.canonicalType(value.Value, resolve, aliases)
	case *ast.Ellipsis:
		return "..." + graph.canonicalType(value.Elt, resolve, aliases)
	case *ast.ParenExpr:
		return graph.canonicalType(value.X, resolve, aliases)
	case *ast.IndexExpr:
		return graph.canonicalType(value.X, resolve, aliases) + "[" +
			graph.canonicalType(value.Index, resolve, aliases) + "]"
	case *ast.IndexListExpr:
		indices := make([]string, len(value.Indices))
		for i := range value.Indices {
			indices[i] = graph.canonicalType(value.Indices[i], resolve, aliases)
		}
		return graph.canonicalType(value.X, resolve, aliases) + "[" + strings.Join(indices, ",") + "]"
	case *ast.SelectorExpr:
		return graph.canonicalType(value.X, resolve, aliases) + "." + value.Sel.Name
	default:
		return graph.renderType(expr)
	}
}

func (graph *goOwnerGraph) renderType(expr ast.Expr) string {
	var rendered bytes.Buffer
	if err := format.Node(&rendered, token.NewFileSet(), expr); err != nil {
		return "<invalid>"
	}
	return rendered.String()
}
