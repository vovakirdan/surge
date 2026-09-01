package carriergate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
)

type goOwnerSource struct {
	path string
	data []byte
}

type goOwnerFile struct {
	path string
	pkg  string
	data []byte
	file *ast.File
}

type goOwnerType struct {
	name string
	spec *ast.TypeSpec
	file *goOwnerFile
}

type goOwnerVisitKey struct {
	spec     *ast.TypeSpec
	env      *goTypeEnv
	bindings string
}

type goEffectiveType struct {
	expr ast.Expr
	env  *goTypeEnv
}

type goTypeEnv struct {
	values map[string]goEffectiveType
}

type goTypeInstance struct {
	decl *goOwnerType
	env  *goTypeEnv
}

type goOwnerGraph struct {
	root     string
	category string
	fset     *token.FileSet
	files    map[string]*goOwnerFile
	packages map[string]map[string]bool
	types    map[string]map[string][]*goOwnerType
}

// scanGoOwners augments the frozen lexical census with package-wide owner
// paths.  The lexical findings stay intact: these findings close the different
// hole where a basename change, alias, pointer or container hides the same
// storage shape.
func scanGoOwners(root string, sources []goOwnerSource) ([]rawFinding, error) {
	category := goOwnerCategory(root)
	if category == "" {
		return nil, nil
	}
	graph := newGoOwnerGraph(root, category)
	sort.Slice(sources, func(i, j int) bool { return sources[i].path < sources[j].path })
	files := make([]*goOwnerFile, 0, len(sources))
	for i := range sources {
		source := &sources[i]
		parsed, err := parser.ParseFile(graph.fset, source.path, source.data, 0)
		if err != nil {
			return nil, err
		}
		ownerFile := &goOwnerFile{
			path: source.path, pkg: path.Dir(source.path), data: source.data, file: parsed,
		}
		files = append(files, ownerFile)
		graph.files[source.path] = ownerFile
		if graph.packages[ownerFile.pkg] == nil {
			graph.packages[ownerFile.pkg] = make(map[string]bool)
		}
		graph.packages[ownerFile.pkg][parsed.Name.Name] = true
		graph.collectTypes(ownerFile)
	}
	return graph.scanFiles(files), nil
}

func goOwnerCategory(root string) string {
	switch root {
	case "internal/vm":
		return categoryVMUniversalOwner
	case "internal/asyncrt":
		return categoryAsyncAny
	default:
		return ""
	}
}

func newGoOwnerGraph(root, category string) *goOwnerGraph {
	return &goOwnerGraph{
		root: root, category: category, fset: token.NewFileSet(),
		files:    make(map[string]*goOwnerFile),
		packages: make(map[string]map[string]bool),
		types:    make(map[string]map[string][]*goOwnerType),
	}
}

func (graph *goOwnerGraph) scanFiles(files []*goOwnerFile) []rawFinding {
	findings := make([]rawFinding, 0, 32)
	for _, file := range files {
		for _, decl := range file.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, rawSpec := range gen.Specs {
				spec, ok := rawSpec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := spec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				findings = append(findings, graph.scanStruct(file, spec, structType)...)
			}
		}
	}
	findings = append(findings, graph.scanRootTypes()...)
	return findings
}

func (graph *goOwnerGraph) scanStruct(file *goOwnerFile, spec *ast.TypeSpec, structType *ast.StructType) []rawFinding {
	findings := make([]rawFinding, 0, len(structType.Fields.List))
	env := graph.instantiateTypeParams(spec, nil, nil)
	visitKey := graph.typeVisitKey(spec, env)
	for _, field := range structType.Fields.List {
		for _, fieldName := range ownerFieldNames(field) {
			visiting := map[goOwnerVisitKey]bool{visitKey: true}
			if carrierPath := graph.carrierFieldPath(field, env, visiting); len(carrierPath) != 0 {
				tokenName := spec.Name.Name + "." + fieldName + "->" + strings.Join(carrierPath, "->")
				findings = append(findings, graph.finding(file, field, tokenName))
			}
		}
	}
	return findings
}

func (graph *goOwnerGraph) collectTypes(file *goOwnerFile) {
	if graph.types[file.pkg] == nil {
		graph.types[file.pkg] = make(map[string][]*goOwnerType)
	}
	for _, decl := range file.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, rawSpec := range gen.Specs {
			spec, ok := rawSpec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			name := spec.Name.Name
			graph.types[file.pkg][name] = append(
				graph.types[file.pkg][name],
				&goOwnerType{name: name, spec: spec, file: file},
			)
		}
	}
}

func (graph *goOwnerGraph) finding(file *goOwnerFile, field *ast.Field, tokenName string) rawFinding {
	offset := graph.fset.Position(field.Pos()).Offset
	line, _ := sourceEvidence(file.data, offset)
	ownerField := strings.SplitN(tokenName, "->", 2)[0]
	return rawFinding{
		Finding: Finding{
			Category: graph.category,
			Path:     file.path,
			Token:    tokenName,
			Evidence: "structural owner field " + ownerField,
			Line:     line,
		},
		offset: offset,
	}
}

func (graph *goOwnerGraph) carrierFieldPath(
	field *ast.Field,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) []string {
	return graph.carrierPath(field.Type, env, visiting)
}

func (graph *goOwnerGraph) carrierPath(
	expr ast.Expr,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) []string {
	switch value := expr.(type) {
	case *ast.Ident:
		if bound, owner, ok := env.lookup(value.Name); ok {
			visitKey := graph.bindingVisitKey(owner, value.Name, bound)
			if visiting[visitKey] {
				return nil
			}
			visiting[visitKey] = true
			defer delete(visiting, visitKey)
			return graph.carrierPath(bound.expr, bound.env, visiting)
		}
		if graph.directCarrier(value) {
			return []string{graph.carrierTerminal(value.Name)}
		}
		for _, decl := range graph.typesForExpr(value, value.Name) {
			if found := graph.carrierFromType(decl, nil, env, visiting); len(found) != 0 {
				return found
			}
		}
	case *ast.SelectorExpr:
		if graph.directSelectorCarrier(value) {
			return []string{graph.carrierTerminal(value.Sel.Name)}
		}
		for _, decl := range graph.typesForBase(value) {
			if found := graph.carrierFromType(decl, nil, env, visiting); len(found) != 0 {
				return found
			}
		}
	case *ast.StarExpr:
		return graph.carrierPath(value.X, env, visiting)
	case *ast.ArrayType:
		return graph.carrierPath(value.Elt, env, visiting)
	case *ast.MapType:
		if found := graph.carrierPath(value.Key, env, visiting); len(found) != 0 {
			return found
		}
		return graph.carrierPath(value.Value, env, visiting)
	case *ast.ChanType:
		return graph.carrierPath(value.Value, env, visiting)
	case *ast.Ellipsis:
		return graph.carrierPath(value.Elt, env, visiting)
	case *ast.ParenExpr:
		return graph.carrierPath(value.X, env, visiting)
	case *ast.IndexExpr:
		return graph.carrierFromInstance(value.X, []ast.Expr{value.Index}, env, visiting)
	case *ast.IndexListExpr:
		return graph.carrierFromInstance(value.X, value.Indices, env, visiting)
	case *ast.InterfaceType:
		if graph.emptyInterface(value, env, make(map[goOwnerVisitKey]bool)) {
			return []string{"universal"}
		}
	case *ast.StructType:
		return graph.carrierFromStruct(value, env, visiting)
	}
	return nil
}

func (graph *goOwnerGraph) carrierFromInstance(
	base ast.Expr,
	args []ast.Expr,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) []string {
	if instances, local := graph.localInstances(base, args, env); local {
		for _, instance := range instances {
			if found := graph.carrierFromTypeEnv(instance.decl, instance.env, visiting); len(found) != 0 {
				return found
			}
		}
		// A local generic declaration is not opaque. If its own graph has no
		// carrier, its arguments are phantom for this path; inspecting them as
		// storage would both defeat recursion guards and invent ownership.
		return nil
	}
	// A qualified generic belongs to another scanned package.  Its arguments
	// are still visible here, so Value cannot hide merely by crossing that
	// package boundary.
	for _, arg := range args {
		if found := graph.carrierPath(arg, env, visiting); len(found) != 0 {
			return found
		}
	}
	return nil
}

func (graph *goOwnerGraph) carrierFromType(
	decl *goOwnerType,
	args []ast.Expr,
	outer *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) []string {
	env := graph.instantiateTypeParams(decl.spec, args, outer)
	return graph.carrierFromTypeEnv(decl, env, visiting)
}

func (graph *goOwnerGraph) carrierFromTypeEnv(
	decl *goOwnerType,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) []string {
	visitKey := graph.typeVisitKey(decl.spec, env)
	if visiting[visitKey] {
		return nil
	}
	visiting[visitKey] = true
	defer delete(visiting, visitKey)
	if structType, ok := decl.spec.Type.(*ast.StructType); ok {
		if found := graph.carrierFromStruct(structType, env, visiting); len(found) != 0 {
			return append([]string{decl.name + "." + found[0]}, found[1:]...)
		}
		return nil
	}
	if found := graph.carrierPath(decl.spec.Type, env, visiting); len(found) != 0 {
		return found
	}
	return nil
}

func (graph *goOwnerGraph) carrierFromStruct(
	structType *ast.StructType,
	env *goTypeEnv,
	visiting map[goOwnerVisitKey]bool,
) []string {
	for _, field := range structType.Fields.List {
		if found := graph.carrierFieldPath(field, env, visiting); len(found) != 0 {
			return append([]string{ownerFieldNames(field)[0]}, found...)
		}
	}
	return nil
}
