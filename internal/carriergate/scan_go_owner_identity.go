package carriergate

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strconv"
	"strings"
)

type goCanonicalState struct {
	aliases  map[*ast.TypeSpec]bool
	bindings map[goOwnerVisitKey]bool
	resolved map[goOwnerVisitKey]string
}

func (env *goTypeEnv) lookup(name string) (goEffectiveType, *goTypeEnv, bool) {
	if env == nil {
		return goEffectiveType{}, nil, false
	}
	value, ok := env.values[name]
	return value, env, ok
}

func (graph *goOwnerGraph) localInstances(
	base ast.Expr,
	args []ast.Expr,
	outer *goTypeEnv,
) ([]goTypeInstance, bool) {
	declarations := graph.typesForBase(base)
	instances := make([]goTypeInstance, 0, len(declarations))
	for _, decl := range declarations {
		instances = append(instances, goTypeInstance{
			decl: decl,
			env:  graph.instantiateTypeParams(decl.spec, args, outer),
		})
	}
	return instances, len(declarations) != 0
}

func (graph *goOwnerGraph) typesForExpr(expr ast.Expr, name string) []*goOwnerType {
	file := graph.fileForExpr(expr)
	if file == nil {
		return nil
	}
	if declarations := graph.types[file.pkg][name]; len(declarations) != 0 {
		return declarations
	}
	declarations := make([]*goOwnerType, 0)
	for _, pkg := range graph.dotImportedPackages(file) {
		declarations = append(declarations, graph.types[pkg][name]...)
	}
	return declarations
}

func (graph *goOwnerGraph) typesForBase(expr ast.Expr) []*goOwnerType {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	switch value := expr.(type) {
	case *ast.Ident:
		return graph.typesForExpr(value, value.Name)
	case *ast.SelectorExpr:
		pkg, ok := graph.selectorPackage(value)
		if !ok {
			return nil
		}
		return graph.types[pkg][value.Sel.Name]
	}
	return nil
}

func (graph *goOwnerGraph) fileForExpr(expr ast.Expr) *goOwnerFile {
	if expr == nil {
		return nil
	}
	return graph.files[graph.fset.Position(expr.Pos()).Filename]
}

func (graph *goOwnerGraph) selectorPackage(selector *ast.SelectorExpr) (string, bool) {
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	file := graph.fileForExpr(selector)
	if file == nil {
		return "", false
	}
	for _, spec := range file.file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		pkg := strings.TrimPrefix(importPath, "surge/")
		declaredNames, scoped := graph.packages[pkg]
		if !scoped {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == qualifier.Name {
				return pkg, true
			}
			continue
		}
		if declaredNames[qualifier.Name] {
			return pkg, true
		}
	}
	return "", false
}

func (graph *goOwnerGraph) dotImportedPackages(file *goOwnerFile) []string {
	if file == nil || file.file == nil {
		return nil
	}
	packages := make([]string, 0)
	for _, spec := range file.file.Imports {
		if spec.Name == nil || spec.Name.Name != "." {
			continue
		}
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		pkg := strings.TrimPrefix(importPath, "surge/")
		if _, scoped := graph.packages[pkg]; scoped {
			packages = append(packages, pkg)
		}
	}
	return packages
}

func (graph *goOwnerGraph) directSelectorCarrier(selector *ast.SelectorExpr) bool {
	pkg, ok := graph.selectorPackage(selector)
	return ok && pkg == graph.root && graph.isRootCarrierName(selector.Sel.Name)
}

func (graph *goOwnerGraph) directDotCarrier(ident *ast.Ident) bool {
	file := graph.fileForExpr(ident)
	for _, pkg := range graph.dotImportedPackages(file) {
		if pkg == graph.root && graph.isRootCarrierName(ident.Name) {
			return true
		}
	}
	return false
}

func (graph *goOwnerGraph) isRootCarrierName(name string) bool {
	return graph.root == "internal/vm" && name == "Value" ||
		graph.root == "internal/asyncrt" && name == "Payload"
}

// instantiateTypeParams captures every actual against the immutable outer
// environment. Inner parameters may shadow the same spelling without changing
// what a composite actual such as *P meant at the call site.
func (graph *goOwnerGraph) instantiateTypeParams(
	spec *ast.TypeSpec,
	args []ast.Expr,
	outer *goTypeEnv,
) *goTypeEnv {
	if spec.TypeParams == nil {
		return nil
	}
	local := &goTypeEnv{values: make(map[string]goEffectiveType)}
	argIndex := 0
	for _, field := range spec.TypeParams.List {
		for _, name := range field.Names {
			if argIndex < len(args) {
				local.values[name.Name] = goEffectiveType{expr: args[argIndex], env: outer}
			} else {
				local.values[name.Name] = goEffectiveType{expr: field.Type, env: local}
			}
			argIndex++
		}
	}
	return local
}

func (graph *goOwnerGraph) typeVisitKey(spec *ast.TypeSpec, env *goTypeEnv) goOwnerVisitKey {
	parts := make([]string, 0)
	if spec.TypeParams != nil {
		for _, field := range spec.TypeParams.List {
			for _, name := range field.Names {
				actual, _, ok := env.lookup(name.Name)
				if !ok {
					parts = append(parts, name.Name+"=<unbound>")
					continue
				}
				parts = append(parts, name.Name+"="+graph.canonicalEffective(actual))
			}
		}
	}
	return goOwnerVisitKey{spec: spec, bindings: strings.Join(parts, ";")}
}

func (graph *goOwnerGraph) bindingVisitKey(
	owner *goTypeEnv,
	name string,
	actual goEffectiveType,
) goOwnerVisitKey {
	return goOwnerVisitKey{env: owner, bindings: name + "=" + graph.canonicalEffective(actual)}
}

func (graph *goOwnerGraph) canonicalEffective(actual goEffectiveType) string {
	state := &goCanonicalState{
		aliases:  make(map[*ast.TypeSpec]bool),
		bindings: make(map[goOwnerVisitKey]bool),
		resolved: make(map[goOwnerVisitKey]string),
	}
	return graph.canonicalType(actual.expr, actual.env, state)
}

func (graph *goOwnerGraph) canonicalType(
	expr ast.Expr,
	env *goTypeEnv,
	state *goCanonicalState,
) string {
	switch value := expr.(type) {
	case *ast.Ident:
		if actual, owner, ok := env.lookup(value.Name); ok {
			key := canonicalBindingKey(owner, value.Name)
			if resolved, cached := state.resolved[key]; cached {
				return resolved
			}
			if state.bindings[key] {
				return "$cycle(" + value.Name + ")"
			}
			state.bindings[key] = true
			canonical := graph.canonicalType(actual.expr, actual.env, state)
			delete(state.bindings, key)
			return canonical
		}
		for _, decl := range graph.typesForExpr(value, value.Name) {
			if !decl.spec.Assign.IsValid() {
				continue
			}
			instanceEnv := graph.instantiateTypeParams(decl.spec, nil, env)
			graph.resolveCanonicalBindings(decl.spec, instanceEnv, state)
			if state.aliases[decl.spec] {
				return "$alias-cycle(" + decl.name + ")"
			}
			state.aliases[decl.spec] = true
			canonical := graph.canonicalType(decl.spec.Type, instanceEnv, state)
			delete(state.aliases, decl.spec)
			return canonical
		}
		return value.Name
	case *ast.SelectorExpr:
		for _, decl := range graph.typesForBase(value) {
			if !decl.spec.Assign.IsValid() {
				continue
			}
			instanceEnv := graph.instantiateTypeParams(decl.spec, nil, env)
			graph.resolveCanonicalBindings(decl.spec, instanceEnv, state)
			if state.aliases[decl.spec] {
				return "$alias-cycle(" + decl.name + ")"
			}
			state.aliases[decl.spec] = true
			canonical := graph.canonicalType(decl.spec.Type, instanceEnv, state)
			delete(state.aliases, decl.spec)
			return canonical
		}
		if pkg, ok := graph.selectorPackage(value); ok {
			return pkg + "." + value.Sel.Name
		}
		return graph.canonicalType(value.X, env, state) + "." + value.Sel.Name
	case *ast.StarExpr:
		return "*" + graph.canonicalType(value.X, env, state)
	case *ast.ArrayType:
		length := ""
		if value.Len != nil {
			length = graph.renderType(value.Len)
		}
		return "[" + length + "]" + graph.canonicalType(value.Elt, env, state)
	case *ast.MapType:
		return "map[" + graph.canonicalType(value.Key, env, state) + "]" +
			graph.canonicalType(value.Value, env, state)
	case *ast.ChanType:
		prefix := "chan "
		switch value.Dir {
		case ast.SEND:
			prefix = "chan<- "
		case ast.RECV:
			prefix = "<-chan "
		}
		return prefix + graph.canonicalType(value.Value, env, state)
	case *ast.Ellipsis:
		return "..." + graph.canonicalType(value.Elt, env, state)
	case *ast.ParenExpr:
		return graph.canonicalType(value.X, env, state)
	case *ast.IndexExpr:
		return graph.canonicalInstance(value.X, []ast.Expr{value.Index}, env, state)
	case *ast.IndexListExpr:
		return graph.canonicalInstance(value.X, value.Indices, env, state)
	case *ast.StructType:
		return "struct{" + graph.canonicalFields(value.Fields, env, state) + "}"
	case *ast.InterfaceType:
		return "interface{" + graph.canonicalFields(value.Methods, env, state) + "}"
	case *ast.FuncType:
		return "func[" + graph.canonicalFields(value.TypeParams, env, state) + "](" +
			graph.canonicalFields(value.Params, env, state) + ")(" +
			graph.canonicalFields(value.Results, env, state) + ")"
	case *ast.UnaryExpr:
		return value.Op.String() + graph.canonicalType(value.X, env, state)
	case *ast.BinaryExpr:
		return graph.canonicalType(value.X, env, state) + value.Op.String() +
			graph.canonicalType(value.Y, env, state)
	default:
		return graph.renderType(expr)
	}
}

func (graph *goOwnerGraph) canonicalInstance(
	base ast.Expr,
	args []ast.Expr,
	env *goTypeEnv,
	state *goCanonicalState,
) string {
	for _, decl := range graph.typesForBase(base) {
		if !decl.spec.Assign.IsValid() {
			continue
		}
		instanceEnv := graph.instantiateTypeParams(decl.spec, args, env)
		graph.resolveCanonicalBindings(decl.spec, instanceEnv, state)
		if state.aliases[decl.spec] {
			return "$alias-cycle(" + decl.name + ")"
		}
		state.aliases[decl.spec] = true
		canonical := graph.canonicalType(decl.spec.Type, instanceEnv, state)
		delete(state.aliases, decl.spec)
		return canonical
	}
	indices := make([]string, len(args))
	for i := range args {
		indices[i] = graph.canonicalType(args[i], env, state)
	}
	return graph.canonicalType(base, env, state) + "[" + strings.Join(indices, ",") + "]"
}

func (graph *goOwnerGraph) canonicalFields(
	fields *ast.FieldList,
	env *goTypeEnv,
	state *goCanonicalState,
) string {
	if fields == nil {
		return ""
	}
	parts := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		names := make([]string, len(field.Names))
		for index, name := range field.Names {
			names[index] = name.Name
		}
		part := strings.Join(names, ",") + ":" +
			graph.canonicalType(field.Type, env, state)
		if field.Tag != nil {
			part += ":" + field.Tag.Value
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ";")
}

func (graph *goOwnerGraph) resolveCanonicalBindings(
	spec *ast.TypeSpec,
	env *goTypeEnv,
	state *goCanonicalState,
) {
	if spec.TypeParams == nil {
		return
	}
	for _, field := range spec.TypeParams.List {
		for _, name := range field.Names {
			actual, owner, ok := env.lookup(name.Name)
			if !ok {
				continue
			}
			key := canonicalBindingKey(owner, name.Name)
			if _, cached := state.resolved[key]; cached {
				continue
			}
			if state.bindings[key] {
				state.resolved[key] = "$cycle(" + name.Name + ")"
				continue
			}
			state.bindings[key] = true
			state.resolved[key] = graph.canonicalType(actual.expr, actual.env, state)
			delete(state.bindings, key)
		}
	}
}

func canonicalBindingKey(owner *goTypeEnv, name string) goOwnerVisitKey {
	return goOwnerVisitKey{env: owner, bindings: name}
}

func ownerBaseIdent(expr ast.Expr) (*ast.Ident, bool) {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	ident, ok := expr.(*ast.Ident)
	return ident, ok
}

func (graph *goOwnerGraph) renderType(expr ast.Expr) string {
	var rendered bytes.Buffer
	if err := format.Node(&rendered, token.NewFileSet(), expr); err != nil {
		return "<invalid>"
	}
	return rendered.String()
}
