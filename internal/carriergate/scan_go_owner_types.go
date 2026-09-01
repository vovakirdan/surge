package carriergate

import (
	"go/ast"
)

func (graph *goOwnerGraph) payloadConstraint(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		if value.Name == "Payload" || value.Name == "any" {
			return value.Name
		}
		for _, decl := range graph.types[value.Name] {
			if graph.emptyInterface(decl.spec.Type, make(map[*ast.TypeSpec]bool)) {
				return value.Name
			}
		}
	case *ast.InterfaceType:
		if graph.emptyInterface(value, make(map[*ast.TypeSpec]bool)) {
			return "interface{}"
		}
	}
	return ""
}

func (graph *goOwnerGraph) directCarrier(name string) bool {
	return name == "any" || graph.root == "internal/vm" && name == "Value" ||
		graph.root == "internal/asyncrt" && name == "Payload"
}

func (graph *goOwnerGraph) carrierTerminal(name string) string {
	if graph.root == "internal/vm" && name == "Value" {
		return "Value"
	}
	return "universal"
}

func (graph *goOwnerGraph) emptyInterface(expr ast.Expr, visiting map[*ast.TypeSpec]bool) bool {
	switch value := expr.(type) {
	case *ast.InterfaceType:
		if value.Methods == nil || len(value.Methods.List) == 0 {
			return true
		}
		for _, field := range value.Methods.List {
			if len(field.Names) != 0 || !graph.emptyInterface(field.Type, visiting) {
				return false
			}
		}
		return true
	case *ast.Ident:
		if value.Name == "any" {
			return true
		}
		for _, decl := range graph.types[value.Name] {
			if visiting[decl.spec] {
				continue
			}
			visiting[decl.spec] = true
			if graph.emptyInterface(decl.spec.Type, visiting) {
				delete(visiting, decl.spec)
				return true
			}
			delete(visiting, decl.spec)
		}
	case *ast.ParenExpr:
		return graph.emptyInterface(value.X, visiting)
	}
	return false
}

func ownerFieldNames(field *ast.Field) []string {
	if len(field.Names) != 0 {
		names := make([]string, len(field.Names))
		for i := range field.Names {
			names[i] = field.Names[i].Name
		}
		return names
	}
	switch value := field.Type.(type) {
	case *ast.Ident:
		return []string{value.Name}
	case *ast.StarExpr:
		if ident, ok := value.X.(*ast.Ident); ok {
			return []string{ident.Name}
		}
	}
	return []string{"embedded"}
}

func cloneOwnerBindings(source map[string]goCarrierBinding) map[string]goCarrierBinding {
	cloned := make(map[string]goCarrierBinding, len(source))
	for name, binding := range source {
		binding.path = append([]string(nil), binding.path...)
		cloned[name] = binding
	}
	return cloned
}

// generalSlot follows every neutral wrapper after it crosses a collection. It
// stops only at a region whose type proves all three owner obligations: exact
// layout metadata, backing storage, and lifecycle slots.  Names are not owner
// evidence; without the complete shape, a reachable lifecycle slot belongs to
// a root general pool and is a finding.
func (graph *goOwnerGraph) generalSlot(
	expr ast.Expr,
	crossedCollection bool,
	visiting map[goOwnerVisitKey]bool,
) string {
	if visiting == nil {
		visiting = make(map[goOwnerVisitKey]bool)
	}
	return graph.generalSlotBound(expr, crossedCollection, visiting, nil)
}

func (graph *goOwnerGraph) generalSlotBound(
	expr ast.Expr,
	crossedCollection bool,
	visiting map[goOwnerVisitKey]bool,
	bindings map[string]ast.Expr,
) string {
	switch value := expr.(type) {
	case *ast.StarExpr:
		return graph.generalSlotBound(value.X, crossedCollection, visiting, bindings)
	case *ast.ArrayType:
		return graph.generalSlotBound(value.Elt, true, visiting, bindings)
	case *ast.MapType:
		if found := graph.generalSlotBound(value.Key, true, visiting, bindings); found != "" {
			return found
		}
		return graph.generalSlotBound(value.Value, true, visiting, bindings)
	case *ast.ChanType:
		return graph.generalSlotBound(value.Value, true, visiting, bindings)
	case *ast.ParenExpr:
		return graph.generalSlotBound(value.X, crossedCollection, visiting, bindings)
	case *ast.Ident:
		if actual, ok := bindings[value.Name]; ok {
			bindingKey := goOwnerVisitKey{bindings: "$" + value.Name + "=" + graph.canonicalSlotType(actual, bindings)}
			if visiting[bindingKey] {
				return ""
			}
			visiting[bindingKey] = true
			defer delete(visiting, bindingKey)
			return graph.generalSlotBound(actual, crossedCollection, visiting, bindings)
		}
		return graph.generalSlotInstance(value, nil, crossedCollection, visiting, bindings)
	case *ast.IndexExpr:
		return graph.generalSlotInstance(value.X, []ast.Expr{value.Index}, crossedCollection, visiting, bindings)
	case *ast.IndexListExpr:
		return graph.generalSlotInstance(value.X, value.Indices, crossedCollection, visiting, bindings)
	case *ast.StructType:
		if crossedCollection && lifecycleHeader(value) {
			return "anonymous"
		}
		for _, field := range value.Fields.List {
			if found := graph.generalSlotBound(field.Type, crossedCollection, visiting, bindings); found != "" {
				return found
			}
		}
	}
	return ""
}

func (graph *goOwnerGraph) generalSlotInstance(
	base ast.Expr,
	args []ast.Expr,
	crossedCollection bool,
	visiting map[goOwnerVisitKey]bool,
	bindings map[string]ast.Expr,
) string {
	ident, local := base.(*ast.Ident)
	if local {
		if actual, ok := bindings[ident.Name]; ok && len(args) == 0 {
			bindingKey := goOwnerVisitKey{bindings: "$" + ident.Name + "=" + graph.canonicalSlotType(actual, bindings)}
			if visiting[bindingKey] {
				return ""
			}
			visiting[bindingKey] = true
			defer delete(visiting, bindingKey)
			return graph.generalSlotBound(actual, crossedCollection, visiting, bindings)
		}
		for _, decl := range graph.types[ident.Name] {
			instanceBindings := slotTypeBindings(decl.spec, args, bindings)
			visitKey := graph.slotVisitKey(decl.spec, instanceBindings)
			if visiting[visitKey] {
				continue
			}
			visiting[visitKey] = true
			if crossedCollection && graph.isTypedOwnerRegion(decl) {
				delete(visiting, visitKey)
				continue
			}
			if crossedCollection && graph.isLifecycleSlot(decl) {
				delete(visiting, visitKey)
				return decl.name
			}
			if found := graph.generalSlotBound(decl.spec.Type, crossedCollection, visiting, instanceBindings); found != "" {
				delete(visiting, visitKey)
				return found
			}
			delete(visiting, visitKey)
		}
		return ""
	}
	// A qualified generic is opaque to this package. Its arguments remain
	// visible, so a lifecycle slot cannot hide behind the package boundary.
	for _, arg := range args {
		if found := graph.generalSlotBound(arg, crossedCollection, visiting, bindings); found != "" {
			return found
		}
	}
	return ""
}

func slotTypeBindings(spec *ast.TypeSpec, args []ast.Expr, outer map[string]ast.Expr) map[string]ast.Expr {
	bindings := make(map[string]ast.Expr, len(outer))
	for name, actual := range outer {
		bindings[name] = actual
	}
	if spec.TypeParams == nil {
		return bindings
	}
	argIndex := 0
	for _, field := range spec.TypeParams.List {
		for _, name := range field.Names {
			delete(bindings, name.Name)
			if argIndex < len(args) {
				actual := args[argIndex]
				if ident, ok := actual.(*ast.Ident); ok {
					if resolved, exists := outer[ident.Name]; exists {
						actual = resolved
					}
				}
				bindings[name.Name] = actual
			}
			argIndex++
		}
	}
	return bindings
}
