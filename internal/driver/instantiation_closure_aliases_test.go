package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestMonoCanonicalizesSiblingStdlibCallableAliases(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	root := t.TempDir()
	moduleDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir demo module: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "a.sg"), []byte(`
pragma module::demo;
pub fn length_a<T>(values: &T[]) -> uint {
    return values.__len();
}
`), 0o600); err != nil {
		t.Fatalf("write a.sg: %v", err)
	}
	entry := filepath.Join(moduleDir, "b.sg")
	if err := os.WriteFile(entry, []byte(`
pragma module::demo;
pub fn length_b<T>(values: &T[]) -> uint {
    return values.__len();
}
@entrypoint fn main() -> uint {
    let values: string[] = ["value"];
    return length_a::<string>(&values) + length_b::<string>(&values);
}
`), 0o600); err != nil {
		t.Fatalf("write b.sg: %v", err)
	}

	result, err := DiagnoseWithOptions(context.Background(), entry, &DiagnoseOptions{
		Stage:              DiagnoseStageAll,
		BaseDir:            root,
		MaxDiagnostics:     64,
		EmitHIR:            true,
		EmitInstantiations: true,
	})
	if err != nil {
		t.Fatalf("diagnose sibling aliases: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil ||
		result.Sema.InstantiationClosure == nil || result.Sema.InstantiationIdentity == nil {
		if result != nil && result.Bag != nil {
			t.Fatalf("sibling alias diagnostics:\n%s", diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
		}
		t.Fatalf("incomplete sibling alias result: %+v", result)
	}

	var wanted sema.InstanceKey
	matchingInstances := 0
	for i := range result.Sema.InstantiationClosure.Instances {
		instance := &result.Sema.InstantiationClosure.Instances[i]
		sym := result.Symbols.Table.Symbols.Get(instance.Template)
		if sym == nil {
			continue
		}
		name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
		if name == "__len" {
			wanted = instance.Key
			matchingInstances++
		}
	}
	if matchingInstances != 1 {
		t.Fatalf("closure instances for canonical __len = %d, want one", matchingInstances)
	}
	rawAliases := make(map[symbols.SymbolID][]types.TypeID)
	canonicalUses := make(map[sema.InstanceKey]struct{})
	for i := range result.Sema.InstantiationClosure.UseSites {
		use := &result.Sema.InstantiationClosure.UseSites[i]
		if use.Callee != wanted || !strings.HasPrefix(use.SourceKey, "demo/") {
			continue
		}
		rawAliases[use.CalleeTemplate] = append([]types.TypeID(nil), use.TemplateArgs...)
		canonicalUses[use.Callee] = struct{}{}
	}
	if got := len(rawAliases); got != 2 {
		t.Fatalf("raw __len aliases = %d, want two distinct per-file symbols: %v", got, rawAliases)
	}
	if got := len(canonicalUses); got != 1 {
		t.Fatalf("canonical __len keys = %d, want one: %v", got, canonicalUses)
	}

	combined, err := CombineHIRWithModules(context.Background(), result)
	if err != nil {
		t.Fatalf("combine sibling alias HIR: %v", err)
	}
	mm, err := mono.MonomorphizeModule(combined, result.Instantiations, result.Sema, mono.Options{EnableDCE: true})
	if err != nil {
		t.Fatalf("mono sibling aliases: %v", err)
	}
	sharedInstance := symbols.NoSymbolID
	for alias, args := range rawAliases {
		instance, found, lookupErr := mm.Callables.LookupChecked(alias, args)
		if lookupErr != nil {
			t.Fatalf("lookup raw alias %d: %v", alias, lookupErr)
		}
		if !found || !instance.IsValid() {
			t.Fatalf("raw alias %d has no retained mono instance", alias)
		}
		if !sharedInstance.IsValid() {
			sharedInstance = instance
		} else if instance != sharedInstance {
			t.Fatalf("raw aliases mapped to different instances: %d and %d", sharedInstance, instance)
		}
	}
	emitted := 0
	for _, fn := range mm.Funcs {
		if fn == nil || len(fn.TypeArgs) == 0 {
			continue
		}
		key, keyErr := sema.NewInstanceKey(*result.Sema.InstantiationIdentity, fn.OrigSym, fn.TypeArgs)
		if keyErr != nil {
			t.Fatalf("canonical mono key for %d: %v", fn.OrigSym, keyErr)
		}
		if key == wanted {
			emitted++
			if fn.InstanceSym != sharedInstance {
				t.Fatalf("canonical mono function has instance %d, callable map has %d", fn.InstanceSym, sharedInstance)
			}
		}
	}
	if emitted != 1 {
		t.Fatalf("emitted mono funcs for canonical __len = %d, want one", emitted)
	}
}

func TestModuleQualifiedGenericFunctionValueUsesCanonicalClosure(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "lib.sg"), []byte(`
pragma module::lib, no_std;
fn leaf<T>(value: T) -> T { return value; }
fn unused_leaf<T>(value: T) -> T { return value; }
pub fn id<T>(value: T) -> T { return leaf(value); }
pub fn unused<T>(value: T) -> T { return unused_leaf(value); }
`), 0o600); err != nil {
		t.Fatalf("write lib: %v", err)
	}
	mainPath := filepath.Join(root, "main.sg")
	if err := os.WriteFile(mainPath, []byte(`
pragma module::app, no_std;
import lib;
@entrypoint fn main() -> int {
    let selected: fn(int) -> int = lib.id;
    return selected(9);
}
`), 0o600); err != nil {
		t.Fatalf("write main: %v", err)
	}
	result, err := DiagnoseWithOptions(context.Background(), mainPath, &DiagnoseOptions{
		Stage:              DiagnoseStageAll,
		BaseDir:            root,
		MaxDiagnostics:     64,
		EmitHIR:            true,
		EmitInstantiations: true,
	})
	if err != nil {
		t.Fatalf("diagnose module-qualified function value: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationClosure == nil {
		t.Fatalf("module-qualified function value diagnostics/result: %+v", result)
	}

	closureNames := make(map[string]bool)
	for _, instance := range result.Sema.InstantiationClosure.Instances {
		sym := result.Symbols.Table.Symbols.Get(instance.Template)
		if sym == nil || sym.ModulePath != "lib" {
			continue
		}
		name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
		closureNames[name] = true
		if (name == "id" || name == "leaf") && (len(instance.TemplateArgs) != 1 || instance.TemplateArgs[0] != result.Sema.TypeInterner.Builtins().Int) {
			t.Fatalf("%s function-value instance args = %v, want int", name, instance.TemplateArgs)
		}
	}
	if !closureNames["id"] || !closureNames["leaf"] {
		t.Fatalf("selected function-value chain missing from closure: %v", closureNames)
	}
	if closureNames["unused"] || closureNames["unused_leaf"] {
		t.Fatalf("unselected function-value sibling leaked into closure: %v", closureNames)
	}

	combined, err := CombineHIRWithModules(context.Background(), result)
	if err != nil {
		t.Fatalf("combine module-qualified function value HIR: %v", err)
	}
	mm, err := mono.MonomorphizeModule(combined, result.Instantiations, result.Sema, mono.Options{EnableDCE: true})
	if err != nil {
		t.Fatalf("mono module-qualified function value: %v", err)
	}
	monoNames := make(map[string]bool)
	for _, fn := range mm.Funcs {
		if fn == nil {
			continue
		}
		if sym := result.Symbols.Table.Symbols.Get(fn.OrigSym); sym != nil && sym.ModulePath == "lib" {
			name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
			monoNames[name] = true
		}
	}
	if !monoNames["id"] || !monoNames["leaf"] {
		t.Fatalf("selected function-value chain missing after DCE: %v", monoNames)
	}
	if monoNames["unused"] || monoNames["unused_leaf"] {
		t.Fatalf("unselected function-value sibling survived DCE: %v", monoNames)
	}
}
