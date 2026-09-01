package vm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Rule 13 negative control: async delivery may not hand a composite Value
// alias to code that can outlive or reuse the concrete owner slot.
func TestAsyncPayloadAPIRequiresCallerOwnedDestination(t *testing.T) {
	files, err := parseProductionVMFiles()
	if err != nil {
		t.Fatal(err)
	}
	foundMove := false
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			switch fn.Name.Name {
			case "takeAsyncPayload":
				if resultMentionsValue(fn.Type.Results) {
					t.Fatal("takeAsyncPayload returns an escaped Value alias")
				}
			case "moveAsyncPayloadIntoStorage":
				foundMove = destinationMoveSignature(fn.Type)
			}
		}
	}
	if !foundMove {
		t.Fatal("missing moveAsyncPayloadIntoStorage(claim, callerOwnedDestination) error-only API")
	}
}

func TestAsyncPayloadCapabilityCarriesOnlyOwnerCoordinates(t *testing.T) {
	files, err := parseProductionVMFiles()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"index", "ownerGeneration", "ownerID", "ownerKind", "parkSeq", "region", "slotGeneration",
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != "asyncPayload" {
					continue
				}
				shape, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					t.Fatal("asyncPayload is not a metadata-only struct")
				}
				got := make([]string, 0, len(shape.Fields.List))
				for _, field := range shape.Fields.List {
					for _, name := range field.Names {
						got = append(got, name.Name)
					}
				}
				sort.Strings(got)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("asyncPayload fields = %v, want only %v", got, want)
				}
				return
			}
		}
	}
	t.Fatal("asyncPayload declaration not found")
}

func parseProductionVMFiles() ([]*ast.File, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			return nil, parseErr
		}
		files = append(files, file)
	}
	return files, nil
}

func resultMentionsValue(results *ast.FieldList) bool {
	if results == nil {
		return false
	}
	for _, result := range results.List {
		if ident, ok := result.Type.(*ast.Ident); ok && ident.Name == "Value" {
			return true
		}
	}
	return false
}

func destinationMoveSignature(fn *ast.FuncType) bool {
	if fn.Params == nil || len(fn.Params.List) != 2 || fn.Results == nil || len(fn.Results.List) != 1 {
		return false
	}
	claim, claimOK := fn.Params.List[0].Type.(*ast.Ident)
	destination, destinationOK := fn.Params.List[1].Type.(*ast.Ident)
	result, resultOK := fn.Results.List[0].Type.(*ast.StarExpr)
	resultName, nameOK := result.X.(*ast.Ident)
	return claimOK && claim.Name == "asyncPayload" &&
		destinationOK && destination.Name == "StorageRef" &&
		resultOK && nameOK && resultName.Name == "VMError"
}
