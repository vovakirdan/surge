package panicgate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// emitterDir is the only package that writes panic-raising code into a native
// module. TestEmitterRaisesOnlyFromTheKnownPackage keeps that true.
const emitterDir = "internal/backend/llvm"

// ScanEmitter enumerates the panic raises the native emitter writes into a
// module.
//
// A raise reaches the emitted module in two shapes. Most go through a helper -
// emitPanicNumeric, emitPanicCoded, emitPanicBounds - which the same fixed
// point used for the C runtime follows outward to the callers that name the
// condition. A handful write the call themselves and take the text from an
// interned string constant; those are read from the constant.
func ScanEmitter(root string) ([]Site, error) {
	dir := filepath.Join(root, emitterDir)
	fset := token.NewFileSet()
	pkgFiles, consts, err := parseEmitterPackage(fset, dir)
	if err != nil {
		return nil, err
	}

	raisers := emitterPrimitiveRaisers()
	var sites []Site
	for range 8 {
		sites = nil
		var forwards []forward
		for _, pf := range pkgFiles {
			fileSites, fileForwards := scanGoFile(fset, pf, consts, raisers)
			sites = append(sites, fileSites...)
			forwards = append(forwards, fileForwards...)
		}
		if !addForwards(raisers, forwards) {
			break
		}
	}
	sortSites(sites)
	return sites, nil
}

type emitterFile struct {
	rel  string
	file *ast.File
}

func parseEmitterPackage(fset *token.FileSet, dir string) ([]emitterFile, map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var files []emitterFile
	consts := map[string]string{}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		files = append(files, emitterFile{rel: filepath.ToSlash(filepath.Join(emitterDir, name)), file: parsed})
		collectStringConsts(parsed, consts)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, consts, nil
}

// collectStringConsts records the package's string constants so a raise that
// names its message through one, rather than inline, still resolves.
func collectStringConsts(file *ast.File, out map[string]string) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := stringLit(vs.Values[i]); ok {
					out[name.Name] = lit
				}
			}
		}
	}
}

func scanGoFile(fset *token.FileSet, pf emitterFile, consts map[string]string, raisers map[string][]raiser) ([]Site, []forward) {
	var sites []Site
	var forwards []forward
	for _, decl := range pf.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		fnSites, fnForwards := scanGoFunc(fset, pf.rel, fn, consts, raisers)
		sites = append(sites, fnSites...)
		forwards = append(forwards, fnForwards...)
	}
	return sites, forwards
}

func scanGoFunc(
	fset *token.FileSet,
	rel string,
	fn *ast.FuncDecl,
	consts map[string]string,
	raisers map[string][]raiser,
) ([]Site, []forward) {
	params := goParamNames(fn)
	isPrimitive := emitterPrimitiveRaisers()[fn.Name.Name] != nil
	interned := internedKeys(fn, consts)

	var sites []Site
	var forwards []forward
	ordinal := 0
	add := func(pos token.Pos, raiserName, message string) {
		ordinal++
		sites = append(sites, Site{
			File:     rel,
			Function: fn.Name.Name,
			Ordinal:  ordinal,
			Raiser:   raiserName,
			Message:  message,
			Line:     fset.Position(pos).Line,
		})
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if verb, ok := directRaiseVerb(call); ok {
			// A helper that writes the runtime call itself is the mechanism,
			// not a site; its callers are the sites. Everything else takes its
			// message from the nearest interned constant above it, and a raise
			// with no constant to read is one whose text the program supplies.
			if !isPrimitive {
				add(call.Pos(), verb, nearestInterned(interned, call.Pos()))
			}
			return true
		}
		name := calleeName(call)
		for _, r := range raisers[name] {
			if r.Arg >= len(call.Args) {
				continue
			}
			arg := call.Args[r.Arg]
			if ident, ok := arg.(*ast.Ident); ok {
				if idx := indexOf(params, ident.Name); idx >= 0 {
					forwards = append(forwards, forward{Function: fn.Name.Name, Arg: idx, Resolve: r.Resolve})
					continue
				}
			}
			message := Computed
			if lit, ok := goLiteral(arg, consts); ok {
				if msg, ok := r.Resolve(lit); ok {
					message = msg
				}
			}
			add(call.Pos(), name, message)
		}
		return true
	})
	return sites, forwards
}

// directRaiseVerb reports whether a call writes one of the runtime's panic
// reporters straight into the module's text, and which one.
func directRaiseVerb(call *ast.CallExpr) (string, bool) {
	if calleeName(call) != "Fprintf" || len(call.Args) < 2 {
		return "", false
	}
	format, ok := stringLit(call.Args[1])
	if !ok {
		return "", false
	}
	for _, verb := range []string{
		"@rt_fatal_static(", "@rt_panic_bounds(", "@rt_panic_numeric(", "@rt_panic_code(", "@rt_panic(",
	} {
		if strings.Contains(format, verb) {
			return strings.TrimSuffix(strings.TrimPrefix(verb, "@"), "("), true
		}
	}
	return "", false
}

type internedKey struct {
	pos token.Pos
	key string
}

// internedKeys lists, in source order, every `stringConsts[...]` lookup in a
// function whose key resolves to text.
func internedKeys(fn *ast.FuncDecl, consts map[string]string) []internedKey {
	var out []internedKey
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		sel, ok := idx.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "stringConsts" {
			return true
		}
		if lit, ok := goLiteral(idx.Index, consts); ok {
			out = append(out, internedKey{pos: idx.Pos(), key: lit})
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].pos < out[j].pos })
	return out
}

func nearestInterned(keys []internedKey, pos token.Pos) string {
	best := Computed
	for _, k := range keys {
		if k.pos < pos {
			best = k.key
		}
	}
	return best
}

func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	default:
		return ""
	}
}

func goParamNames(fn *ast.FuncDecl) []string {
	var out []string
	if fn.Type.Params == nil {
		return out
	}
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			out = append(out, "")
			continue
		}
		for _, name := range field.Names {
			out = append(out, name.Name)
		}
	}
	return out
}

// goLiteral reads a string constant, a named string constant, or an integer
// from an expression.
func goLiteral(expr ast.Expr, consts map[string]string) (string, bool) {
	if lit, ok := stringLit(expr); ok {
		return lit, true
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if v, ok := consts[e.Name]; ok {
			return v, true
		}
	case *ast.BasicLit:
		if e.Kind == token.INT {
			return e.Value, true
		}
	}
	return "", false
}

func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}
