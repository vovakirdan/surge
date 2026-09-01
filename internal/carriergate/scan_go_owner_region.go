package carriergate

import (
	"go/ast"
	"path"
	"strconv"
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

// isTypedOwnerRegion accepts the one VM owner whose exact internal nominal
// contract is part of D8. Unknown regions fail closed. Callback count, selector
// spelling, and layout-looking field names prove nothing about ownership.
func (graph *goOwnerGraph) isTypedOwnerRegion(decl *goOwnerType, _ *goTypeEnv) bool {
	if graph.root != "internal/vm" || decl.file.pkg != graph.root || decl.name != "asyncOwnerRegion" ||
		decl.spec.Assign.IsValid() || decl.spec.TypeParams != nil {
		return false
	}
	ownerFields := map[string]string{
		"id":         graph.vmType("asyncOwnerID"),
		"generation": "uint32",
		"typeID":     "surge/internal/types.TypeID",
		"cell":       graph.vmType("storageMember"),
		"stride":     "uint64",
		"arena":      graph.vmType("Arena"),
		"slots":      "[]" + graph.vmType("asyncPayloadSlot"),
		"retiring":   "bool",
		"destroying": "bool",
		"retired":    "bool",
	}
	return graph.declHasExactFields(decl, ownerFields) &&
		graph.namedStructHasExactFields("asyncOwnerID", map[string]string{
			"kind": graph.vmType("asyncOwnerKind"), "id": "uint64", "arm": "uint32",
		}) &&
		graph.namedStructHasExactFields("storageMember", map[string]string{
			"Offset": "uint64", "Size": "uint64", "Align": "uint64",
			"TypeID": "surge/internal/types.TypeID", "Kind": graph.vmType("cellKind"),
		}) &&
		graph.namedStructHasExactFields("Arena", map[string]string{
			"bytes": "[]byte", "refs": "[]" + graph.vmType("Location"),
			"refIndex": "map[" + graph.vmType("Location") + "]uint64",
			"gen":      "uint32", "pins": "uint32",
		}) &&
		graph.namedStructHasExactFields("asyncPayloadSlot", map[string]string{
			"state": graph.vmType("asyncPayloadState"), "generation": "uint32",
			"role": graph.vmType("asyncSlotRole"), "parkSeq": "uint64",
		}) &&
		graph.namedUint8("cellKind") &&
		graph.namedUint8("asyncOwnerKind") &&
		graph.namedUint8("asyncPayloadState") &&
		graph.namedUint8("asyncSlotRole")
}

func (graph *goOwnerGraph) vmType(name string) string {
	return "surge/internal/vm." + name
}

func (graph *goOwnerGraph) namedStructHasExactFields(name string, expected map[string]string) bool {
	for _, decl := range graph.types[graph.root][name] {
		if decl.name == name && !decl.spec.Assign.IsValid() && decl.spec.TypeParams == nil &&
			graph.declHasExactFields(decl, expected) {
			return true
		}
	}
	return false
}

func (graph *goOwnerGraph) declHasExactFields(decl *goOwnerType, expected map[string]string) bool {
	structType, ok := decl.spec.Type.(*ast.StructType)
	if !ok {
		return false
	}
	actual := make(map[string]string, len(expected))
	for _, field := range structType.Fields.List {
		fieldType, ok := graph.semanticType(field.Type, decl.file, make(map[*ast.TypeSpec]bool))
		if !ok {
			return false
		}
		for _, name := range ownerFieldNames(field) {
			if _, duplicate := actual[name]; duplicate {
				return false
			}
			actual[name] = fieldType
		}
	}
	if len(actual) != len(expected) {
		return false
	}
	for name, want := range expected {
		if actual[name] != want {
			return false
		}
	}
	return true
}

func (graph *goOwnerGraph) namedUint8(name string) bool {
	for _, decl := range graph.types[graph.root][name] {
		if decl.name != name || decl.spec.Assign.IsValid() || decl.spec.TypeParams != nil {
			continue
		}
		actual, ok := graph.semanticType(decl.spec.Type, decl.file, make(map[*ast.TypeSpec]bool))
		if ok && actual == "uint8" {
			return true
		}
	}
	return false
}

func (graph *goOwnerGraph) semanticType(
	expr ast.Expr,
	file *goOwnerFile,
	aliases map[*ast.TypeSpec]bool,
) (string, bool) {
	switch value := expr.(type) {
	case *ast.Ident:
		declarations := graph.types[graph.fileForExpr(value).pkg][value.Name]
		if builtinOwnerType(value.Name) && len(declarations) == 0 {
			return value.Name, true
		}
		for _, decl := range declarations {
			if !decl.spec.Assign.IsValid() || aliases[decl.spec] {
				continue
			}
			aliases[decl.spec] = true
			resolved, ok := graph.semanticType(decl.spec.Type, decl.file, aliases)
			delete(aliases, decl.spec)
			return resolved, ok
		}
		return "surge/" + graph.root + "." + value.Name, true
	case *ast.SelectorExpr:
		qualifier, ok := value.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		importPath, ok := importedPackage(file, qualifier.Name)
		if !ok {
			return "", false
		}
		return importPath + "." + value.Sel.Name, true
	case *ast.StarExpr:
		inner, ok := graph.semanticType(value.X, file, aliases)
		return "*" + inner, ok
	case *ast.ArrayType:
		if value.Len != nil {
			return "", false
		}
		inner, ok := graph.semanticType(value.Elt, file, aliases)
		return "[]" + inner, ok
	case *ast.MapType:
		key, keyOK := graph.semanticType(value.Key, file, aliases)
		item, itemOK := graph.semanticType(value.Value, file, aliases)
		return "map[" + key + "]" + item, keyOK && itemOK
	case *ast.ParenExpr:
		return graph.semanticType(value.X, file, aliases)
	}
	return "", false
}

func importedPackage(file *goOwnerFile, qualifier string) (string, bool) {
	if file == nil || file.file == nil {
		return "", false
	}
	for _, spec := range file.file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == qualifier {
			return importPath, true
		}
	}
	return "", false
}

func builtinOwnerType(name string) bool {
	switch name {
	case "bool", "byte", "string", "uint8", "uint32", "uint64", "uintptr":
		return true
	default:
		return false
	}
}
