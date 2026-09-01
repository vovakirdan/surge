package carriergate

import (
	"go/ast"
	"strings"
)

func (graph *goOwnerGraph) payloadConstraint(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		if value.Name == "Payload" || value.Name == "any" {
			return value.Name
		}
		for _, decl := range graph.types[value.Name] {
			if graph.emptyInterface(decl.spec.Type, make(map[string]bool)) {
				return value.Name
			}
		}
	case *ast.InterfaceType:
		if value.Methods == nil || len(value.Methods.List) == 0 {
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

func (graph *goOwnerGraph) ownerPointer(field *ast.Field, pointee ast.Expr) bool {
	for _, name := range ownerFieldNames(field) {
		if ownerRoleName(name) {
			return true
		}
	}
	switch value := pointee.(type) {
	case *ast.Ident:
		return graph.directCarrier(value.Name) || ownerRoleName(value.Name)
	case *ast.IndexExpr:
		if ident, ok := value.X.(*ast.Ident); ok {
			return ownerRoleName(ident.Name)
		}
	case *ast.IndexListExpr:
		if ident, ok := value.X.(*ast.Ident); ok {
			return ownerRoleName(ident.Name)
		}
	case *ast.InterfaceType:
		return value.Methods == nil || len(value.Methods.List) == 0
	}
	return false
}

func directPointer(expr ast.Expr) (ast.Expr, bool) {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	pointer, ok := expr.(*ast.StarExpr)
	if !ok {
		return nil, false
	}
	return pointer.X, true
}

func ownerRoleName(name string) bool {
	name = strings.ToLower(name)
	for _, role := range []string{
		"capture", "cell", "envelope", "mailbox", "owner", "payload",
		"result", "resume", "slot", "state", "storage", "store", "value",
	} {
		if strings.Contains(name, role) {
			return true
		}
	}
	return false
}

func (graph *goOwnerGraph) emptyInterface(expr ast.Expr, visiting map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.InterfaceType:
		return value.Methods == nil || len(value.Methods.List) == 0
	case *ast.Ident:
		if value.Name == "any" {
			return true
		}
		if visiting[value.Name] {
			return false
		}
		visiting[value.Name] = true
		defer delete(visiting, value.Name)
		for _, decl := range graph.types[value.Name] {
			if graph.emptyInterface(decl.spec.Type, visiting) {
				return true
			}
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

func cloneOwnerBindings(source map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(source))
	for name, path := range source {
		cloned[name] = append([]string(nil), path...)
	}
	return cloned
}

func (graph *goOwnerGraph) isRootType(name string) bool {
	return graph.root == "internal/vm" && name == "VM" ||
		graph.root == "internal/asyncrt" && name == "Executor"
}

// generalSlot follows wrapper records until it crosses a collection.  The
// first record after that boundary is the owner.  A lifecycle slot there is a
// root pool; a task/channel/select wrapper there is an owner-specific region
// and deliberately stops the walk.
func (graph *goOwnerGraph) generalSlot(expr ast.Expr, crossedCollection bool, visiting map[string]bool) string {
	if visiting == nil {
		visiting = make(map[string]bool)
	}
	switch value := expr.(type) {
	case *ast.StarExpr:
		return graph.generalSlot(value.X, crossedCollection, visiting)
	case *ast.ArrayType:
		return graph.generalSlot(value.Elt, true, visiting)
	case *ast.MapType:
		return graph.generalSlot(value.Value, true, visiting)
	case *ast.ChanType:
		return graph.generalSlot(value.Value, true, visiting)
	case *ast.ParenExpr:
		return graph.generalSlot(value.X, crossedCollection, visiting)
	case *ast.Ident:
		if visiting[value.Name] {
			return ""
		}
		visiting[value.Name] = true
		defer delete(visiting, value.Name)
		for _, decl := range graph.types[value.Name] {
			if crossedCollection {
				if graph.isLifecycleSlot(decl) {
					return decl.name
				}
				return ""
			}
			if found := graph.generalSlot(decl.spec.Type, false, visiting); found != "" {
				return found
			}
		}
	case *ast.IndexExpr:
		return graph.generalSlot(value.X, crossedCollection, visiting)
	case *ast.IndexListExpr:
		return graph.generalSlot(value.X, crossedCollection, visiting)
	case *ast.StructType:
		if crossedCollection {
			return ""
		}
		for _, field := range value.Fields.List {
			if found := graph.generalSlot(field.Type, false, visiting); found != "" {
				return found
			}
		}
	}
	return ""
}

func (graph *goOwnerGraph) isLifecycleSlot(decl *goOwnerType) bool {
	structType, ok := decl.spec.Type.(*ast.StructType)
	if !ok {
		return false
	}
	hasState, hasGeneration, hasStorage := false, false, false
	for _, field := range structType.Fields.List {
		for _, name := range ownerFieldNames(field) {
			switch strings.ToLower(name) {
			case "state":
				hasState = true
			case "gen", "generation":
				hasGeneration = true
			}
		}
		if graph.storageBacking(field.Type, make(map[string]bool)) {
			hasStorage = true
		}
	}
	return hasState && hasGeneration && hasStorage
}

func (graph *goOwnerGraph) storageBacking(expr ast.Expr, visiting map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		if value.Name == "Arena" || value.Name == "StorageRef" || value.Name == "scratch" {
			return true
		}
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
		ident, ok := value.Elt.(*ast.Ident)
		return value.Len == nil && ok && ident.Name == "byte"
	case *ast.ParenExpr:
		return graph.storageBacking(value.X, visiting)
	}
	return false
}
