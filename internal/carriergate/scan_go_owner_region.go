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
	return graph.structHasLayout(structType) && graph.structHasBacking(structType) &&
		graph.structHasLifecycleSlots(structType)
}

func (graph *goOwnerGraph) structHasLayout(structType *ast.StructType) bool {
	for _, field := range structType.Fields.List {
		if graph.layoutDescriptor(field.Type, make(map[string]bool)) {
			return true
		}
	}
	return false
}

func (graph *goOwnerGraph) layoutDescriptor(expr ast.Expr, visiting map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		if visiting[value.Name] {
			return false
		}
		visiting[value.Name] = true
		defer delete(visiting, value.Name)
		for _, decl := range graph.types[value.Name] {
			if structType, ok := decl.spec.Type.(*ast.StructType); ok && layoutFields(structType) {
				return true
			}
			if graph.layoutDescriptor(decl.spec.Type, visiting) {
				return true
			}
		}
	case *ast.StarExpr:
		return graph.layoutDescriptor(value.X, visiting)
	case *ast.ParenExpr:
		return graph.layoutDescriptor(value.X, visiting)
	case *ast.IndexExpr:
		return graph.layoutDescriptor(value.X, visiting)
	case *ast.IndexListExpr:
		return graph.layoutDescriptor(value.X, visiting)
	case *ast.StructType:
		if layoutFields(value) {
			return true
		}
		for _, field := range value.Fields.List {
			if graph.layoutDescriptor(field.Type, visiting) {
				return true
			}
		}
	}
	return false
}

func layoutFields(structType *ast.StructType) bool {
	hasSize, hasAlign := false, false
	for _, field := range structType.Fields.List {
		for _, name := range ownerFieldNames(field) {
			switch strings.ToLower(name) {
			case "size":
				hasSize = true
			case "align":
				hasAlign = true
			}
		}
	}
	return hasSize && hasAlign
}

func (graph *goOwnerGraph) structHasBacking(structType *ast.StructType) bool {
	for _, field := range structType.Fields.List {
		if graph.storageBacking(field.Type, make(map[string]bool)) {
			return true
		}
	}
	return false
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

func (graph *goOwnerGraph) structHasLifecycleSlots(structType *ast.StructType) bool {
	for _, field := range structType.Fields.List {
		if graph.lifecycleSlots(field.Type, false, make(map[string]bool)) {
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
