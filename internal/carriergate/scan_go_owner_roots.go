package carriergate

import "go/ast"

func (graph *goOwnerGraph) scanRootTypes() []rawFinding {
	rootName := graph.rootTypeName()
	if rootName == "" {
		return nil
	}
	findings := make([]rawFinding, 0)
	for _, rootDecl := range graph.types[graph.root][rootName] {
		rootEnv := graph.instantiateTypeParams(rootDecl.spec, nil, nil)
		file, structType, env := graph.rootStruct(
			goEffectiveType{expr: rootDecl.spec.Type, env: rootEnv},
			rootDecl.file,
			make(map[goOwnerVisitKey]bool),
		)
		if structType == nil {
			continue
		}
		for _, field := range structType.Fields.List {
			for _, fieldName := range ownerFieldNames(field) {
				slotName := graph.generalSlot(field.Type, false, nil, env)
				if slotName == "" {
					continue
				}
				tokenName := rootName + "." + fieldName + "->general-slot(" + slotName + ")"
				findings = append(findings, graph.finding(file, field, tokenName))
			}
		}
	}
	return findings
}

func (graph *goOwnerGraph) rootTypeName() string {
	switch graph.root {
	case "internal/vm":
		return "VM"
	case "internal/asyncrt":
		return "Executor"
	default:
		return ""
	}
}

func (graph *goOwnerGraph) rootStruct(
	actual goEffectiveType,
	file *goOwnerFile,
	visiting map[goOwnerVisitKey]bool,
) (*goOwnerFile, *ast.StructType, *goTypeEnv) {
	switch value := actual.expr.(type) {
	case *ast.StructType:
		return file, value, actual.env
	case *ast.Ident:
		if bound, _, ok := actual.env.lookup(value.Name); ok {
			return graph.rootStruct(bound, file, visiting)
		}
		return graph.rootStructInstances(value, nil, actual.env, visiting)
	case *ast.StarExpr:
		return graph.rootStruct(goEffectiveType{expr: value.X, env: actual.env}, file, visiting)
	case *ast.ParenExpr:
		return graph.rootStruct(goEffectiveType{expr: value.X, env: actual.env}, file, visiting)
	case *ast.IndexExpr:
		return graph.rootStructInstances(value.X, []ast.Expr{value.Index}, actual.env, visiting)
	case *ast.IndexListExpr:
		return graph.rootStructInstances(value.X, value.Indices, actual.env, visiting)
	case *ast.SelectorExpr:
		return graph.rootStructInstances(value, nil, actual.env, visiting)
	}
	return nil, nil, nil
}

func (graph *goOwnerGraph) rootStructInstances(
	base ast.Expr,
	args []ast.Expr,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) (*goOwnerFile, *ast.StructType, *goTypeEnv) {
	instances, local := graph.localInstances(base, args, env)
	if !local {
		return nil, nil, nil
	}
	for _, instance := range instances {
		key := graph.typeVisitKey(instance.decl.spec, instance.env)
		if visiting[key] {
			continue
		}
		visiting[key] = true
		file, structType, resolvedEnv := graph.rootStruct(
			goEffectiveType{expr: instance.decl.spec.Type, env: instance.env},
			instance.decl.file,
			visiting,
		)
		delete(visiting, key)
		if structType != nil {
			return file, structType, resolvedEnv
		}
	}
	return nil, nil, nil
}
