package carriergate

import (
	"go/ast"
	"strings"
)

func (graph *goOwnerGraph) isLifecycleSlot(decl *goOwnerType) bool {
	structType, ok := decl.spec.Type.(*ast.StructType)
	return ok && lifecycleHeader(structType)
}

func lifecycleHeader(structType *ast.StructType) bool {
	hasState, hasGeneration := false, false
	for _, field := range structType.Fields.List {
		for _, name := range ownerFieldNames(field) {
			switch strings.ToLower(name) {
			case "state":
				hasState = true
			case "gen", "generation":
				hasGeneration = true
			}
		}
	}
	return hasState && hasGeneration
}

func (graph *goOwnerGraph) isTypedOwnerRegion(decl *goOwnerType) bool {
	structType, ok := decl.spec.Type.(*ast.StructType)
	if !ok {
		return false
	}
	hasOps, hasBacking, hasSlots := false, false, false
	for _, field := range structType.Fields.List {
		if graph.valueOpsDescriptor(field.Type, make(map[*ast.TypeSpec]bool)) {
			hasOps = true
		}
		if graph.storageBacking(field.Type, make(map[string]bool)) {
			hasBacking = true
		}
		if graph.lifecycleSlots(field.Type, false, make(map[string]bool)) {
			hasSlots = true
		}
	}
	return hasOps && hasBacking && hasSlots
}

func (graph *goOwnerGraph) valueOpsDescriptor(expr ast.Expr, visiting map[*ast.TypeSpec]bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		for _, decl := range graph.types[value.Name] {
			if visiting[decl.spec] {
				continue
			}
			visiting[decl.spec] = true
			found := graph.valueOpsDescriptor(decl.spec.Type, visiting)
			delete(visiting, decl.spec)
			if found {
				return true
			}
		}
	case *ast.StarExpr:
		return graph.valueOpsDescriptor(value.X, visiting)
	case *ast.ParenExpr:
		return graph.valueOpsDescriptor(value.X, visiting)
	case *ast.IndexExpr:
		return graph.valueOpsDescriptor(value.X, visiting)
	case *ast.IndexListExpr:
		return graph.valueOpsDescriptor(value.X, visiting)
	case *ast.StructType:
		return graph.valueOpsStruct(value)
	}
	return false
}

func (graph *goOwnerGraph) valueOpsStruct(structType *ast.StructType) bool {
	hasLayout, hasMove, hasPlan := false, false, false
	for _, field := range structType.Fields.List {
		if graph.fullLayout(field.Type, make(map[*ast.TypeSpec]bool)) {
			hasLayout = true
		}
		params, results, callable := graph.callableArity(field.Type, make(map[*ast.TypeSpec]bool))
		if !callable || params < 2 {
			continue
		}
		if results == 0 {
			hasMove = true
		} else {
			hasPlan = true
		}
	}
	return hasLayout && hasMove && hasPlan
}

func (graph *goOwnerGraph) fullLayout(expr ast.Expr, visiting map[*ast.TypeSpec]bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		for _, decl := range graph.types[value.Name] {
			if visiting[decl.spec] {
				continue
			}
			visiting[decl.spec] = true
			found := graph.fullLayout(decl.spec.Type, visiting)
			delete(visiting, decl.spec)
			if found {
				return true
			}
		}
	case *ast.StarExpr:
		return graph.fullLayout(value.X, visiting)
	case *ast.ParenExpr:
		return graph.fullLayout(value.X, visiting)
	case *ast.StructType:
		return fullLayoutFields(value)
	}
	return false
}

func fullLayoutFields(structType *ast.StructType) bool {
	found := make(map[string]bool, 4)
	for _, field := range structType.Fields.List {
		for _, name := range ownerFieldNames(field) {
			switch strings.ToLower(name) {
			case "size", "align", "stride", "flags":
				found[strings.ToLower(name)] = true
			}
		}
	}
	return found["size"] && found["align"] && found["stride"] && found["flags"]
}

func (graph *goOwnerGraph) callableArity(
	expr ast.Expr,
	visiting map[*ast.TypeSpec]bool,
) (params, results int, found bool) {
	switch value := expr.(type) {
	case *ast.Ident:
		for _, decl := range graph.types[value.Name] {
			if visiting[decl.spec] {
				continue
			}
			visiting[decl.spec] = true
			params, results, found := graph.callableArity(decl.spec.Type, visiting)
			delete(visiting, decl.spec)
			if found {
				return params, results, true
			}
		}
	case *ast.ParenExpr:
		return graph.callableArity(value.X, visiting)
	case *ast.FuncType:
		return fieldCount(value.Params), fieldCount(value.Results), true
	}
	return 0, 0, false
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
		} else {
			count += len(field.Names)
		}
	}
	return count
}

func (graph *goOwnerGraph) storageBacking(expr ast.Expr, visiting map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		if visiting[value.Name] {
			return false
		}
		visiting[value.Name] = true
		defer delete(visiting, value.Name)
		for _, decl := range graph.types[value.Name] {
			if graph.storageBacking(decl.spec.Type, visiting) {
				return true
			}
		}
	case *ast.StarExpr:
		return graph.storageBacking(value.X, visiting)
	case *ast.ArrayType:
		if graph.byteType(value.Elt, make(map[string]bool)) {
			return true
		}
		return graph.storageBacking(value.Elt, visiting)
	case *ast.MapType:
		return graph.storageBacking(value.Value, visiting)
	case *ast.ChanType:
		return graph.storageBacking(value.Value, visiting)
	case *ast.ParenExpr:
		return graph.storageBacking(value.X, visiting)
	case *ast.IndexExpr:
		return graph.storageBacking(value.X, visiting)
	case *ast.IndexListExpr:
		return graph.storageBacking(value.X, visiting)
	case *ast.StructType:
		for _, field := range value.Fields.List {
			if graph.storageBacking(field.Type, visiting) {
				return true
			}
		}
	}
	return false
}

func (graph *goOwnerGraph) byteType(expr ast.Expr, visiting map[string]bool) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	if ident.Name == "byte" {
		return true
	}
	if visiting[ident.Name] {
		return false
	}
	visiting[ident.Name] = true
	defer delete(visiting, ident.Name)
	for _, decl := range graph.types[ident.Name] {
		if graph.byteType(decl.spec.Type, visiting) {
			return true
		}
	}
	return false
}

func (graph *goOwnerGraph) lifecycleSlots(expr ast.Expr, crossedCollection bool, visiting map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		if visiting[value.Name] {
			return false
		}
		visiting[value.Name] = true
		defer delete(visiting, value.Name)
		for _, decl := range graph.types[value.Name] {
			if structType, ok := decl.spec.Type.(*ast.StructType); ok && crossedCollection && lifecycleHeader(structType) {
				return true
			}
			if graph.lifecycleSlots(decl.spec.Type, crossedCollection, visiting) {
				return true
			}
		}
	case *ast.StarExpr:
		return graph.lifecycleSlots(value.X, crossedCollection, visiting)
	case *ast.ArrayType:
		return graph.lifecycleSlots(value.Elt, true, visiting)
	case *ast.MapType:
		return graph.lifecycleSlots(value.Value, true, visiting)
	case *ast.ChanType:
		return graph.lifecycleSlots(value.Value, true, visiting)
	case *ast.ParenExpr:
		return graph.lifecycleSlots(value.X, crossedCollection, visiting)
	case *ast.IndexExpr:
		return graph.lifecycleSlots(value.X, crossedCollection, visiting)
	case *ast.IndexListExpr:
		return graph.lifecycleSlots(value.X, crossedCollection, visiting)
	case *ast.StructType:
		if crossedCollection && lifecycleHeader(value) {
			return true
		}
		for _, field := range value.Fields.List {
			if graph.lifecycleSlots(field.Type, crossedCollection, visiting) {
				return true
			}
		}
	}
	return false
}
