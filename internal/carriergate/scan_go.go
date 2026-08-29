package carriergate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

var compositeKinds = map[string]struct{}{
	"KindStruct": {}, "KindTuple": {}, "KindUnion": {}, "KindEnum": {}, "KindArray": {},
}

func scanGoFile(filePath string, data []byte) ([]rawFinding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, data, 0)
	if err != nil {
		return nil, err
	}
	findings := make([]rawFinding, 0, 16)
	addAt := func(category, value string, pos token.Pos) {
		findings = append(findings, makeRawFinding(category, filePath, value, data, fset.Position(pos).Offset))
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.Ident:
			if category := goIdentifierCategory(filePath, value.Name); category != "" {
				addAt(category, value.Name, value.Pos())
			}
		case *ast.BasicLit:
			if strings.HasPrefix(filePath, "internal/backend/llvm/") && value.Kind == token.STRING {
				scanLLVMString(value, addAt)
			}
		case *ast.Field:
			if tokenName, ok := vmOwnerField(filePath, value); ok {
				addAt(categoryVMUniversalOwner, tokenName, value.Pos())
			}
		case *ast.CallExpr:
			if vmCallValueBuffer(filePath, value) {
				addAt(categoryVMUniversalOwner, "make([]Value)", value.Pos())
			}
		}
		return true
	})
	if filePath == "internal/backend/llvm/types.go" {
		findings = append(findings, scanCompositePtrReturns(fset, filePath, data, file)...)
	}
	return findings, nil
}

func goIdentifierCategory(filePath, name string) string {
	switch {
	case strings.HasPrefix(filePath, "internal/asyncrt/") && name == "any":
		return categoryAsyncAny
	case strings.HasPrefix(filePath, "internal/backend/llvm/"):
		switch name {
		case "emitValueToI64", "emitI64ToValue":
			return categoryLLVMWordBridge
		case "isBoxedComposite", "emitCloneGlueBody", "emitDropGlueBody":
			return categoryCompositeBox
		case "crossingDropStates", "crossingDropResults", "registerCrossingDropResult",
			"channelPayloadDropID", "abandonedStateDropID":
			return categoryNumericDrop
		// The emitter reserving, sizing and releasing a frame the runtime
		// holds past the suspension. `abandonedStateDropID` is deliberately
		// NOT here: it names the same frame, but what it carries is a numeric
		// drop id, and that is the category already counting it.
		//
		// Every one of these names is EMPTY in this tree: the frame is
		// reserved and released through its own descriptor now
		// (runtime/native/rt_frame.h) and the two `AsyncStateFree` spellings
		// went with the builtin. The list stays because the frozen census is a
		// census of a COMMIT that had all five, so deleting a name re-derives
		// that commit as having fewer -- the manifest disagreeing with the
		// thing it is a census of -- and leaves the category unable to see the
		// shape come back.
		case "emitRuntimeOwnedStorage", "requireSuspensionFrameRelease",
			"emitSuspensionFrameReleaseBody", "emitAsyncStateFreeIntrinsic",
			"AsyncStateFreeBuiltin":
			return categoryFrameOwner
		}
	case strings.HasPrefix(filePath, "internal/vm/"):
		switch name {
		case "VKHandleStruct", "VKHandleTag", "OKStruct", "OKTag":
			return categoryVMBoxKind
		case "AllocStruct", "AllocTag", "cloneValueComposite":
			return categoryCompositeBox
		}
	}
	return ""
}

func scanLLVMString(literal *ast.BasicLit, add func(string, string, token.Pos)) {
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return
	}
	for _, marker := range []string{"ptrtoint", "inttoptr"} {
		for start := 0; start < len(value); {
			index := strings.Index(value[start:], marker)
			if index < 0 {
				break
			}
			index += start
			if identifierBoundary(value, index, len(marker)) {
				add(categoryLLVMPointerWord, marker, literal.Pos()+token.Pos(index))
			}
			start = index + len(marker)
		}
	}
}

func identifierBoundary(value string, start, length int) bool {
	isIdent := func(char byte) bool {
		return char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
	}
	return (start == 0 || !isIdent(value[start-1])) && (start+length == len(value) || !isIdent(value[start+length]))
}

func vmOwnerField(filePath string, field *ast.Field) (string, bool) {
	if !strings.HasPrefix(filePath, "internal/vm/") || len(field.Names) == 0 {
		return "", false
	}
	name := field.Names[0].Name
	typeName := goTypeName(field.Type)
	switch path.Base(filePath) {
	case "frame.go":
		return name + " " + typeName, name == "V" && typeName == "Value"
	case "object.go":
		ok := (name == "Fields" || name == "Arr") && typeName == "[]Value"
		return name + " " + typeName, ok
	case "map_object.go":
		ok := (name == "Key" || name == "Value") && typeName == "Value"
		return name + " " + typeName, ok
	case "async_runtime.go":
		ok := (name == "state" || name == "value") && typeName == "Value" || name == "fields" && typeName == "[]Value"
		return name + " " + typeName, ok
	case "vm_dispatch_async_types.go":
		return name + " " + typeName, name == "storeVal" && typeName == "Value"
	}
	return "", false
}

func goTypeName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.ArrayType:
		if value.Len == nil {
			return "[]" + goTypeName(value.Elt)
		}
	}
	return ""
}

func vmCallValueBuffer(filePath string, call *ast.CallExpr) bool {
	base := path.Base(filePath)
	if base != "vm_dispatch_call.go" && base != "tag.go" {
		return false
	}
	makeIdent, ok := call.Fun.(*ast.Ident)
	if !ok || makeIdent.Name != "make" || len(call.Args) == 0 {
		return false
	}
	arrayType, ok := call.Args[0].(*ast.ArrayType)
	return ok && arrayType.Len == nil && goTypeName(arrayType.Elt) == "Value"
}

func scanCompositePtrReturns(fset *token.FileSet, filePath string, data []byte, file *ast.File) []rawFinding {
	findings := make([]rawFinding, 0, 5)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "llvmType" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			kinds := compositeCaseNames(clause)
			if len(kinds) == 0 {
				return false
			}
			ast.Inspect(&ast.BlockStmt{List: clause.Body}, func(child ast.Node) bool {
				ret, ok := child.(*ast.ReturnStmt)
				if !ok || !returnsPtrLiteral(ret) {
					return true
				}
				tokenName := strings.Join(kinds, "|") + "->ptr"
				findings = append(findings, makeRawFinding(categoryLLVMCompositePtr, filePath, tokenName, data, fset.Position(ret.Pos()).Offset))
				return false
			})
			return false
		})
	}
	return findings
}

func compositeCaseNames(clause *ast.CaseClause) []string {
	set := make(map[string]struct{})
	for _, expr := range clause.List {
		ast.Inspect(expr, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok {
				if _, composite := compositeKinds[selector.Sel.Name]; composite {
					set[selector.Sel.Name] = struct{}{}
				}
			}
			return true
		})
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	slicesSort(names)
	return names
}

func returnsPtrLiteral(ret *ast.ReturnStmt) bool {
	if len(ret.Results) == 0 {
		return false
	}
	literal, ok := ret.Results[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(literal.Value)
	return err == nil && value == "ptr"
}

func slicesSort(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
