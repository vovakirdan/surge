package carriergate

import "go/ast"

func (graph *goOwnerGraph) scanRootTypes() []rawFinding {
	rootName := graph.rootTypeName()
	if rootName == "" {
		return nil
	}
	findings := make([]rawFinding, 0)
	for _, rootDecl := range graph.types[rootName] {
		file, structType := graph.rootStruct(rootDecl.spec.Type, rootDecl.file, make(map[*ast.TypeSpec]bool))
		if structType == nil {
			continue
		}
		for _, field := range structType.Fields.List {
			for _, fieldName := range ownerFieldNames(field) {
				slotName := graph.generalSlot(field.Type, false, nil)
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
	expr ast.Expr,
	file *goOwnerFile,
	visiting map[*ast.TypeSpec]bool,
) (*goOwnerFile, *ast.StructType) {
	switch value := expr.(type) {
	case *ast.StructType:
		return file, value
	case *ast.Ident:
		for _, decl := range graph.types[value.Name] {
			if visiting[decl.spec] {
				continue
			}
			visiting[decl.spec] = true
			resolvedFile, structType := graph.rootStruct(decl.spec.Type, decl.file, visiting)
			delete(visiting, decl.spec)
			if structType != nil {
				return resolvedFile, structType
			}
		}
	case *ast.StarExpr:
		return graph.rootStruct(value.X, file, visiting)
	case *ast.ParenExpr:
		return graph.rootStruct(value.X, file, visiting)
	case *ast.IndexExpr:
		return graph.rootStruct(value.X, file, visiting)
	case *ast.IndexListExpr:
		return graph.rootStruct(value.X, file, visiting)
	}
	return nil, nil
}
