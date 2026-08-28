// Package stdlibgate holds the gate that keeps the shipped standard library
// compiling.
//
// Nothing else in `make check` compiles a stdlib module. Every other suite
// reads the library only through fixtures that happen to import it, so a
// language change can refuse a shape the library still uses and every gate
// stays green over a library that no longer builds. That is not hypothetical:
// the refusal of a range returned over a frame-local array landed on trunk and
// `stdlib/fs` used exactly that shape in `walkdir`, which left the module
// uncompilable for two weeks with a green tree.
//
// The gate compiles each module the way a program consumes it - through an
// importer with an entrypoint, all the way to MIR - because that is the only
// reading that answers "can anyone still use this?". Compiling a library file
// as its own root answers a different and weaker question: it has no
// entrypoint to seed generic instantiation from, so it reports failures that
// no consumer of the library would ever see.
package stdlibgate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
)

// gateImportAlias is bound in the generated importer. It must not collide with
// a built-in name: binding a module to `bytes` collides with the built-in of
// that name and reports a duplicate declaration that belongs to the generated
// program rather than to the module under test.
const gateImportAlias = "module_under_gate"

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("stdlib gate: get working directory: %v", err)
	}
	for {
		goMod, readErr := os.ReadFile(filepath.Join(dir, "go.mod"))
		makeInfo, statErr := os.Stat(filepath.Join(dir, "Makefile"))
		if readErr == nil && statErr == nil && !makeInfo.IsDir() && declaresModule(string(goMod), "surge") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("stdlib gate: no repository root with module surge and Makefile above working directory")
		}
		dir = parent
	}
}

func declaresModule(goMod, module string) bool {
	for _, line := range strings.Split(goMod, "\n") {
		if strings.TrimSpace(line) == "module "+module {
			return true
		}
	}
	return false
}

// discoverStdlibModules returns every importable module path under stdlib/,
// derived from the tree rather than from a list, so a module added tomorrow is
// covered without anyone remembering to enrol it. A directory holding .sg
// files is one module; a .sg file directly under stdlib/ is a module of its
// own.
func discoverStdlibModules(t *testing.T, root string) []string {
	t.Helper()
	stdlibDir := filepath.Join(root, "stdlib")
	modules := map[string]struct{}{}
	err := filepath.WalkDir(stdlibDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".sg" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if dir := filepath.ToSlash(filepath.Dir(rel)); dir != "stdlib" {
			modules[dir] = struct{}{}
			return nil
		}
		modules[strings.TrimSuffix(rel, ".sg")] = struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("stdlib gate: walk %s: %v", stdlibDir, err)
	}
	out := make([]string, 0, len(modules))
	for module := range modules {
		out = append(out, module)
	}
	sort.Strings(out)
	return out
}

// TestStdlibModulesCompile is the gate. Every module under stdlib/ must reach
// MIR through an importing program.
func TestStdlibModulesCompile(t *testing.T) {
	root := repoRoot(t)
	// The tree under test is the one that ships, never an installed copy that
	// may be older than this commit.
	t.Setenv("SURGE_STDLIB", root)

	modules := discoverStdlibModules(t, root)
	// A gate that discovers nothing passes without checking anything, which is
	// the failure this package exists to end.
	if len(modules) == 0 {
		t.Fatal("stdlib gate: discovered no modules under stdlib/; the gate would pass without compiling anything")
	}

	for _, module := range modules {
		t.Run(module, func(t *testing.T) {
			program := writeImporter(t, module)
			result, compileErr := buildpipeline.Compile(context.Background(), &buildpipeline.CompileRequest{
				TargetPath:     program,
				BaseDir:        root,
				RootKind:       project.ModuleKindUnknown,
				MaxDiagnostics: 500,
				Backend:        buildpipeline.BackendLLVM,
			})
			if compileErr == nil && result.MIR != nil {
				return
			}
			detail := strings.Join(fatalDiagnostics(root, result), "\n  ")
			if detail == "" {
				detail = "(no source diagnostic)"
			}
			t.Errorf("stdlib module %s does not compile: %v\n  %s", module, compileErr, detail)
		})
	}
}

// writeImporter generates the consuming program for one module outside the
// repository, so a failed run cannot leave a stray .sg file behind that the
// corpus gates would then discover as a fixture.
func writeImporter(t *testing.T, module string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gate_importer.sg")
	program := fmt.Sprintf("import %s as %s;\n\n@entrypoint\nfn main() -> int {\n    return 0;\n}\n",
		module, gateImportAlias)
	if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
		t.Fatalf("stdlib gate: write importer for %s: %v", module, err)
	}
	return path
}

func fatalDiagnostics(root string, result buildpipeline.CompileResult) []string {
	if result.Diagnose == nil || result.Diagnose.Bag == nil {
		return nil
	}
	var entries []string
	for _, diagnostic := range result.Diagnose.Bag.Items() {
		if diagnostic == nil || diagnostic.Severity < diag.SevError {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s %s %s",
			diagnostic.Code.ID(),
			diagnosticLocation(root, result.Diagnose.FileSet, diagnostic.Primary),
			diagnostic.Message))
	}
	sort.Strings(entries)
	return entries
}

func diagnosticLocation(root string, files *source.FileSet, span source.Span) string {
	if files == nil || !files.HasFile(span.File) {
		return fmt.Sprintf("<unknown-file-%d>", span.File)
	}
	file := files.Get(span.File)
	if file == nil || file.Path == "" {
		return fmt.Sprintf("<unnamed-file-%d>", span.File)
	}
	path := file.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(files.BaseDir(), path)
	}
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		path = filepath.ToSlash(rel)
	}
	start, _ := files.Resolve(span)
	return fmt.Sprintf("%s:%d:%d", path, start.Line, start.Col)
}
