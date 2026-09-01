package carriergate

import "go/ast"

func (graph *goOwnerGraph) directCarrier(ident *ast.Ident) bool {
	if ident.Name == "any" {
		return true
	}
	file := graph.fileForExpr(ident)
	if file != nil && file.pkg == graph.root {
		return graph.isRootCarrierName(ident.Name)
	}
	return graph.directDotCarrier(ident)
}

func (graph *goOwnerGraph) carrierTerminal(name string) string {
	if graph.root == "internal/vm" && name == "Value" {
		return "Value"
	}
	return "universal"
}

func (graph *goOwnerGraph) emptyInterface(
	expr ast.Expr,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) bool {
	switch value := expr.(type) {
	case *ast.InterfaceType:
		if value.Methods == nil || len(value.Methods.List) == 0 {
			return true
		}
		for _, field := range value.Methods.List {
			if len(field.Names) != 0 || !graph.emptyInterface(field.Type, env, visiting) {
				return false
			}
		}
		return true
	case *ast.Ident:
		if value.Name == "any" {
			return true
		}
		if actual, owner, ok := env.lookup(value.Name); ok {
			key := graph.bindingVisitKey(owner, value.Name, actual)
			if visiting[key] {
				return false
			}
			visiting[key] = true
			found := graph.emptyInterface(actual.expr, actual.env, visiting)
			delete(visiting, key)
			return found
		}
		instances, _ := graph.localInstances(value, nil, env)
		for _, instance := range instances {
			key := graph.typeVisitKey(instance.decl.spec, instance.env)
			if visiting[key] {
				continue
			}
			visiting[key] = true
			if graph.emptyInterface(instance.decl.spec.Type, instance.env, visiting) {
				delete(visiting, key)
				return true
			}
			delete(visiting, key)
		}
	case *ast.ParenExpr:
		return graph.emptyInterface(value.X, env, visiting)
	case *ast.IndexExpr:
		return graph.emptyInterfaceInstance(value.X, []ast.Expr{value.Index}, env, visiting)
	case *ast.IndexListExpr:
		return graph.emptyInterfaceInstance(value.X, value.Indices, env, visiting)
	case *ast.SelectorExpr:
		return graph.emptyInterfaceInstance(value, nil, env, visiting)
	}
	return false
}

func (graph *goOwnerGraph) emptyInterfaceInstance(
	base ast.Expr,
	args []ast.Expr,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) bool {
	instances, local := graph.localInstances(base, args, env)
	if !local {
		return false
	}
	for _, instance := range instances {
		key := graph.typeVisitKey(instance.decl.spec, instance.env)
		if visiting[key] {
			continue
		}
		visiting[key] = true
		found := graph.emptyInterface(instance.decl.spec.Type, instance.env, visiting)
		delete(visiting, key)
		if found {
			return true
		}
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

// generalSlot follows every neutral wrapper after it crosses a collection. It
// exempts only the closed nominal D8 typed-owner contract. Other type or field
// names are not owner evidence: a reachable lifecycle slot belongs to a root
// general pool and is a finding.
func (graph *goOwnerGraph) generalSlot(
	expr ast.Expr,
	crossedCollection bool,
	visiting map[goOwnerVisitKey]bool,
	env *goTypeEnv,
) string {
	if visiting == nil {
		visiting = make(map[goOwnerVisitKey]bool)
	}
	return graph.generalSlotBound(expr, crossedCollection, visiting, env)
}

func (graph *goOwnerGraph) generalSlotBound(
	expr ast.Expr,
	crossedCollection bool,
	visiting map[goOwnerVisitKey]bool,
	env *goTypeEnv,
) string {
	switch value := expr.(type) {
	case *ast.StarExpr:
		return graph.generalSlotBound(value.X, crossedCollection, visiting, env)
	case *ast.ArrayType:
		return graph.generalSlotBound(value.Elt, true, visiting, env)
	case *ast.MapType:
		if found := graph.generalSlotBound(value.Key, true, visiting, env); found != "" {
			return found
		}
		return graph.generalSlotBound(value.Value, true, visiting, env)
	case *ast.ChanType:
		return graph.generalSlotBound(value.Value, true, visiting, env)
	case *ast.ParenExpr:
		return graph.generalSlotBound(value.X, crossedCollection, visiting, env)
	case *ast.Ident:
		if actual, owner, ok := env.lookup(value.Name); ok {
			bindingKey := graph.bindingVisitKey(owner, value.Name, actual)
			if visiting[bindingKey] {
				return ""
			}
			visiting[bindingKey] = true
			defer delete(visiting, bindingKey)
			return graph.generalSlotBound(actual.expr, crossedCollection, visiting, actual.env)
		}
		return graph.generalSlotInstance(value, nil, crossedCollection, visiting, env)
	case *ast.IndexExpr:
		return graph.generalSlotInstance(value.X, []ast.Expr{value.Index}, crossedCollection, visiting, env)
	case *ast.IndexListExpr:
		return graph.generalSlotInstance(value.X, value.Indices, crossedCollection, visiting, env)
	case *ast.SelectorExpr:
		return graph.generalSlotInstance(value, nil, crossedCollection, visiting, env)
	case *ast.StructType:
		if crossedCollection && lifecycleHeader(value) {
			return "anonymous"
		}
		for _, field := range value.Fields.List {
			if found := graph.generalSlotBound(field.Type, crossedCollection, visiting, env); found != "" {
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
	env *goTypeEnv,
) string {
	if ident, ok := ownerBaseIdent(base); ok && len(args) == 0 {
		if actual, owner, bound := env.lookup(ident.Name); bound {
			bindingKey := graph.bindingVisitKey(owner, ident.Name, actual)
			if visiting[bindingKey] {
				return ""
			}
			visiting[bindingKey] = true
			defer delete(visiting, bindingKey)
			return graph.generalSlotBound(actual.expr, crossedCollection, visiting, actual.env)
		}
	}
	if instances, local := graph.localInstances(base, args, env); local {
		for _, instance := range instances {
			visitKey := graph.typeVisitKey(instance.decl.spec, instance.env)
			if visiting[visitKey] {
				continue
			}
			visiting[visitKey] = true
			if crossedCollection && graph.isTypedOwnerRegion(instance.decl, instance.env) {
				delete(visiting, visitKey)
				continue
			}
			if crossedCollection && graph.isLifecycleSlot(instance.decl) {
				delete(visiting, visitKey)
				return instance.decl.name
			}
			if found := graph.generalSlotBound(
				instance.decl.spec.Type,
				crossedCollection,
				visiting,
				instance.env,
			); found != "" {
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
		if found := graph.generalSlotBound(arg, crossedCollection, visiting, env); found != "" {
			return found
		}
	}
	return ""
}
