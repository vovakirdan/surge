package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestCanonicalInstantiationSourceRejectsAmbiguousExternalFiles(t *testing.T) {
	base := t.TempDir()
	externalA, externalB := filepath.Join(t.TempDir(), "shared.sg"), filepath.Join(t.TempDir(), "shared.sg")
	content := []byte("fn local<T>(x: T) -> T { return x; }\n")
	files := source.NewFileSetWithBase(base)
	left := files.Add(externalA, content, source.FileVirtual)
	right := files.Add(externalB, content, source.FileVirtual)
	resolve := canonicalInstantiationSourceResolver(&DiagnoseResult{FileSet: files})
	expectedKey := fmt.Sprintf("external/shared.sg@%x", files.Get(left).Hash)
	for _, id := range []source.FileID{left, right} {
		_, err := resolve(id)
		if err == nil || !strings.Contains(err.Error(), "canonical source identity") || !strings.Contains(err.Error(), expectedKey) || !strings.Contains(err.Error(), "stable module mapping") {
			t.Fatalf("ambiguous external source error = %v", err)
		}
		if strings.Contains(err.Error(), externalA) || strings.Contains(err.Error(), externalB) {
			t.Fatalf("ambiguous source diagnostic leaked physical path: %v", err)
		}
	}
}

func TestCanonicalInstantiationSourceRejectsConflictingNamespacesForOneFile(t *testing.T) {
	index := canonicalSourceIndex{
		keys:    make(map[source.FileID]string),
		errors:  make(map[source.FileID]error),
		reverse: make(map[string][]canonicalSourceOrigin),
	}
	index.set(0, "module_a/main.sg", "/checkout/main.sg")
	index.set(0, "module_b/main.sg", "/checkout/main.sg")
	if err := index.errors[0]; err == nil || !strings.Contains(err.Error(), "conflicting canonical identities") || !strings.Contains(err.Error(), "exactly one logical module namespace") {
		t.Fatalf("conflicting namespace error = %v", err)
	}
}

func TestCanonicalInstantiationSourceTreatsRelativeAndAbsolutePathsAsOneFile(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "main.sg")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	index := canonicalSourceIndex{
		keys:    make(map[source.FileID]string),
		errors:  make(map[source.FileID]error),
		reverse: make(map[string][]canonicalSourceOrigin),
	}
	index.set(1, "app/main.sg", rel)
	index.set(2, "app/main.sg", abs)
	if index.errors[1] != nil || index.errors[2] != nil {
		t.Fatalf("one physical file was treated as two origins: %v %v", index.errors[1], index.errors[2])
	}
}

func TestDiagnoseRejectsGenericModuleExecutionRoots(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.sg")
	if err := os.WriteFile(path, []byte(`
pragma module::global_roots, no_std;
fn id<T>(value: T) -> T { return value; }
let runtime_global = id(1);
const runtime_const = id::<int>(1);
@entrypoint fn main() -> int { return 0; }
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		BaseDir:        root,
		MaxDiagnostics: 32,
	})
	if err != nil {
		t.Fatalf("diagnose invalid execution roots: %v", err)
	}
	if result == nil || result.Bag == nil {
		t.Fatalf("missing diagnostics result")
	}
	want := map[diag.Code]bool{
		diag.SemaModuleLevelLet:   false,
		diag.SemaConstNotConstant: false,
	}
	moduleLetHasGuidance := false
	for _, item := range result.Bag.Items() {
		if _, tracked := want[item.Code]; tracked {
			want[item.Code] = true
		}
		if item.Code == diag.SemaModuleLevelLet && len(item.Notes) > 0 {
			moduleLetHasGuidance = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("missing %s in diagnostics: %+v", code.ID(), result.Bag.Items())
		}
	}
	if !moduleLetHasGuidance {
		t.Fatalf("module-level let diagnostic lost its migration guidance")
	}
}

func TestDiagnoseRetainsOnlyReachedNonGenericTagConstructors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.sg")
	if err := os.WriteFile(path, []byte(`
pragma module::app, no_std;
tag Used(int);
tag Unused(int);
type Choice = Used(int) | Unused(int);
@entrypoint fn main() -> int {
	let selected: Choice = Used(1);
	let _ = selected;
    return 0;
}
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		BaseDir:        root,
		MaxDiagnostics: 32,
	})
	if err != nil {
		t.Fatalf("diagnose tag reachability: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationClosure == nil {
		if result != nil && result.Bag != nil {
			var details []string
			for _, item := range result.Bag.Items() {
				details = append(details, fmt.Sprintf("%s: %s", item.Code.ID(), item.Message))
			}
			t.Fatalf("tag reachability diagnostics: %v", details)
		}
		t.Fatalf("tag reachability result: %+v", result)
	}
	liveTags := make(map[string]bool)
	for _, id := range result.Sema.InstantiationClosure.LiveCallables {
		sym := result.Symbols.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolTag {
			continue
		}
		name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
		liveTags[name] = true
	}
	if !liveTags["Used"] || liveTags["Unused"] {
		t.Fatalf("live tags = %v, want only Used", liveTags)
	}
}

func TestDiagnoseRetainsSyntheticRangeConstructors(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	root := t.TempDir()
	path := filepath.Join(root, "main.sg")
	if err := os.WriteFile(path, []byte(`
@entrypoint fn main() -> int {
    let _ = [1..3];
    let _ = [1..];
    let _ = [..3];
    let _ = [..];
    return 0;
}
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		BaseDir:        root,
		MaxDiagnostics: 32,
	})
	if err != nil {
		t.Fatalf("diagnose range reachability: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationClosure == nil {
		if result != nil && result.Bag != nil {
			t.Fatalf("range reachability diagnostics:\n%s", diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
		}
		t.Fatalf("range reachability result: %+v", result)
	}
	want := map[string]bool{
		"rt_range_int_new":        false,
		"rt_range_int_from_start": false,
		"rt_range_int_to_end":     false,
		"rt_range_int_full":       false,
	}
	for _, id := range result.Sema.InstantiationClosure.LiveCallables {
		sym := result.Symbols.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, retained := range want {
		if !retained {
			t.Fatalf("synthetic range constructor %s is absent from authoritative closure", name)
		}
	}
}

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

func TestDiagnoseFinalizesCanonicalInstantiationClosureAcrossRootsAndFileOrders(t *testing.T) {
	buildProject := func(root string) (aPath, bPath string) {
		t.Helper()
		pkg := filepath.Join(root, "pkg")
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatalf("mkdir project: %v", err)
		}
		aPath = filepath.Join(pkg, "a.sg")
		bPath = filepath.Join(pkg, "b.sg")
		if err := os.WriteFile(aPath, []byte(`
pragma module::demo, no_std;
fn h<T>(x: T) -> T { return x; }
fn g<T>(x: T) -> T { return h(x); }
`), 0o600); err != nil {
			t.Fatalf("write a.sg: %v", err)
		}
		if err := os.WriteFile(bPath, []byte(`
pragma module::demo, no_std;
fn f<T>(x: T) -> T { return g(x); }
fn main() { let _ = f(1); }
`), 0o600); err != nil {
			t.Fatalf("write b.sg: %v", err)
		}
		return aPath, bPath
	}

	rootA, rootB := t.TempDir(), t.TempDir()
	_, entryA := buildProject(rootA)
	entryB, _ := buildProject(rootB)
	left := diagnoseInstantiationClosure(t, rootA, entryA)
	right := diagnoseInstantiationClosure(t, rootB, entryB)
	if left.snapshot != right.snapshot {
		t.Fatalf("canonical closure changed across checkout roots/file registration order:\nleft:\n%s\nright:\n%s", left.snapshot, right.snapshot)
	}
	if left.rootFileID == right.rootFileID {
		t.Fatalf("test did not perturb source registration: both root witness FileID=%d", left.rootFileID)
	}
	if !strings.Contains(left.snapshot, "pkg/b.sg") || strings.Contains(left.snapshot, rootA) || strings.Contains(right.snapshot, rootB) {
		t.Fatalf("witness source is not checkout-relative:\n%s", left.snapshot)
	}
}

func TestDiagnoseClosureRemapsImportedGenericEdgesAndParamOwners(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "lib.sg"), []byte(`
pragma module::lib;
pub fn h<T>(value: T) -> T { return value; }
pub fn f<T>(value: T) -> T { return h(value); }
`), 0o600); err != nil {
		t.Fatalf("write lib: %v", err)
	}
	mainPath := filepath.Join(root, "main.sg")
	if err := os.WriteFile(mainPath, []byte(`
import lib::{f};
@entrypoint fn main() -> int { return f(7); }
`), 0o600); err != nil {
		t.Fatalf("write main: %v", err)
	}
	result, err := DiagnoseWithOptions(context.Background(), mainPath, &DiagnoseOptions{
		Stage:          DiagnoseStageSema,
		BaseDir:        root,
		MaxDiagnostics: 64,
	})
	if err != nil {
		t.Fatalf("diagnose imported closure: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationClosure == nil {
		t.Fatalf("imported closure diagnostics/result: %+v", result)
	}
	found := make(map[string]bool)
	for _, instance := range result.Sema.InstantiationClosure.Instances {
		sym := result.Symbols.Table.Symbols.Get(instance.Template)
		if sym == nil || sym.ModulePath != "lib" {
			continue
		}
		name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
		if name != "f" && name != "h" {
			continue
		}
		if len(instance.TemplateArgs) != 1 || instance.TemplateArgs[0] != result.Sema.TypeInterner.Builtins().Int || types.ContainsGenericParam(result.Sema.TypeInterner, instance.TemplateArgs[0]) {
			t.Fatalf("%s imported args were not remapped/substituted to int: %v", name, instance.TemplateArgs)
		}
		found[name] = true
		compat := result.Sema.FunctionInstantiations[instance.Template]
		if len(compat) != 1 || len(compat[0]) != 1 || compat[0][0] != instance.TemplateArgs[0] {
			t.Fatalf("%s compatibility view is not finalized concrete closure: %v", name, compat)
		}
		if name == "h" {
			if len(instance.Witness.Steps) != 1 || instance.Witness.Steps[0].SourceKey != "lib/lib.sg" {
				t.Fatalf("transitive imported witness was not remapped: %+v", instance.Witness)
			}
		}
	}
	if !found["f"] || !found["h"] {
		t.Fatalf("missing imported f<int>/h<int> closure instances: %v", found)
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

func TestDependencyDeferredBoundMethodActivatesOnlyReachableGenericLeaf(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "lib.sg"), []byte(`
pragma module::lib, no_std;

pub type Foo = { value: int }

fn leaf<U>(value: U) -> U { return value; }
fn leaf2<U>(value: U) -> U { return value; }

extern<Foo> {
  fn Bar(self: Foo) -> int { return leaf(self.value); }
  fn Bar2(self: Foo) -> int { return leaf2(self.value); }
}

pub contract FooLike<T> {
  fn Bar(self: T) -> int;
}

pub fn invoke<T: FooLike<T>>(value: T) -> int { return value.Bar(); }
`), 0o600); err != nil {
		t.Fatalf("write lib: %v", err)
	}
	mainPath := filepath.Join(root, "main.sg")
	if err := os.WriteFile(mainPath, []byte(`
pragma module::app, no_std;
import lib::{Foo, invoke};
@entrypoint fn main() -> int { return invoke(Foo { value = 7 }); }
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
		t.Fatalf("diagnose deferred dependency dispatch: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationClosure == nil {
		t.Fatalf("deferred dependency diagnostics/result: %+v", result)
	}
	combined, err := CombineHIRWithModules(context.Background(), result)
	if err != nil {
		t.Fatalf("combine deferred dependency HIR: %v", err)
	}
	mm, err := mono.MonomorphizeModule(combined, result.Instantiations, result.Sema, mono.Options{EnableDCE: true})
	if err != nil {
		t.Fatalf("mono deferred dependency dispatch: %v", err)
	}
	resolvedNames := make(map[string]bool)
	for _, resolved := range result.Sema.InstantiationClosure.ResolvedDeferredCalls {
		if sym := result.Symbols.Table.Symbols.Get(resolved.Callee); sym != nil {
			name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
			resolvedNames[name] = true
		}
	}

	liveNames := make(map[string]bool)
	for _, id := range result.Sema.InstantiationClosure.LiveCallables {
		if sym := result.Symbols.Table.Symbols.Get(id); sym != nil {
			name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
			liveNames[name] = true
		}
	}
	instanceNames := make(map[string]bool)
	for _, instance := range result.Sema.InstantiationClosure.Instances {
		if sym := result.Symbols.Table.Symbols.Get(instance.Template); sym != nil {
			name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
			instanceNames[name] = true
		}
	}
	monoNames := make(map[string]bool)
	for _, fn := range mm.Funcs {
		if fn == nil {
			continue
		}
		if sym := result.Symbols.Table.Symbols.Get(fn.OrigSym); sym != nil {
			name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
			monoNames[name] = true
		}
	}
	if !liveNames["Bar"] || !instanceNames["leaf"] || !monoNames["leaf"] {
		t.Fatalf("reachable deferred chain missing: live=%v instances=%v mono=%v", liveNames, instanceNames, monoNames)
	}
	if !resolvedNames["Bar"] || resolvedNames["Bar2"] {
		t.Fatalf("deferred resolution did not select exact Bar callable: %v", resolvedNames)
	}
	if liveNames["Bar2"] || instanceNames["leaf2"] || monoNames["Bar2"] || monoNames["leaf2"] {
		t.Fatalf("unused deferred sibling leaked into authority: live=%v instances=%v mono=%v", liveNames, instanceNames, monoNames)
	}
}

func TestDiagnoseDirFullModuleGraphSharesFinalizedClosure(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module: %v", err)
	}
	files := map[string]string{
		"a.sg": `
pragma module::demo, no_std;
fn h<T>(value: T) -> T { return value; }
fn g<T>(value: T) -> T { return h(value); }
`,
		"b.sg": `
pragma module::demo, no_std;
fn f<T>(value: T) -> T { return g(value); }
@entrypoint fn main() -> int { return f(1); }
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(moduleDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	_, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage:           DiagnoseStageAll,
		MaxDiagnostics:  64,
		FullModuleGraph: true,
		KeepArtifacts:   true,
	}, 2)
	if err != nil {
		t.Fatalf("full-module diagnose: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("directory results = %d, want 2", len(results))
	}
	sharedSnapshot := ""
	for _, result := range results {
		if result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationIdentity == nil || result.Sema.InstantiationClosure == nil {
			t.Fatalf("%s did not retain finalized authority: %+v", result.Path, result)
		}
		var snapshot strings.Builder
		localCount := 0
		for _, instance := range result.Sema.InstantiationClosure.Instances {
			fmt.Fprintf(&snapshot, "%s|%s\n", instance.Key.TemplateKey, instance.Key.ArgsKey)
			if strings.HasPrefix(instance.Witness.SourceKey, "demo/") {
				localCount++
			}
		}
		if localCount != 3 {
			t.Fatalf("%s local closure count = %d, want f/g/h", result.Path, localCount)
		}
		if sharedSnapshot == "" {
			sharedSnapshot = snapshot.String()
		} else if snapshot.String() != sharedSnapshot {
			t.Fatalf("per-file full-module authorities diverged:\nfirst:\n%s\n%s:\n%s", sharedSnapshot, result.Path, snapshot.String())
		}
	}
}

func TestDiagnoseInstantiationExpansionErrorStableAcrossCheckoutRoots(t *testing.T) {
	build := func(root string) string {
		t.Helper()
		path := filepath.Join(root, "main.sg")
		if err := os.WriteFile(path, []byte(`
pragma module::grow, no_std;
fn grow<T>() -> nothing {
    grow::<T[]>();
    return nothing;
}
fn main() { grow::<int>(); }
`), 0o600); err != nil {
			t.Fatalf("write expansion source: %v", err)
		}
		return path
	}
	rootA, rootB := t.TempDir(), t.TempDir()
	leftPath, rightPath := build(rootA), build(rootB)
	_, leftErr := DiagnoseWithOptions(context.Background(), leftPath, &DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: rootA, MaxDiagnostics: 64})
	_, rightErr := DiagnoseWithOptions(context.Background(), rightPath, &DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: rootB, MaxDiagnostics: 64})
	if leftErr == nil || rightErr == nil {
		t.Fatalf("expanding recursion must fail: left=%v right=%v", leftErr, rightErr)
	}
	left, right := leftErr.Error(), rightErr.Error()
	if left != right {
		t.Fatalf("expansion error changed across checkout roots:\nleft:  %s\nright: %s", left, right)
	}
	if strings.Contains(left, rootA) || strings.Contains(right, rootB) || !strings.Contains(left, "main.sg") {
		t.Fatalf("expansion error leaked checkout path or lost source witness: %s", left)
	}
}

type diagnosedClosure struct {
	snapshot   string
	rootFileID uint32
}

func diagnoseInstantiationClosure(t *testing.T, root, entry string) diagnosedClosure {
	t.Helper()
	result, err := DiagnoseWithOptions(context.Background(), entry, &DiagnoseOptions{
		Stage:          DiagnoseStageSema,
		BaseDir:        root,
		MaxDiagnostics: 64,
	})
	if err != nil {
		t.Fatalf("diagnose %s: %v", entry, err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationClosure == nil || result.Sema.InstantiationIdentity == nil {
		t.Fatalf("diagnostic path did not finalize closure: result=%+v", result)
	}
	closure := result.Sema.InstantiationClosure
	var out strings.Builder
	localInstances := 0
	for _, instance := range closure.Instances {
		if strings.HasPrefix(instance.Witness.SourceKey, "pkg/") {
			localInstances++
		}
		fmt.Fprintf(&out, "%s|%s|%s:%d-%d", instance.Key.TemplateKey, instance.Key.ArgsKey, instance.Witness.SourceKey, instance.Witness.Site.Start, instance.Witness.Site.End)
		for _, step := range instance.Witness.Steps {
			fmt.Fprintf(&out, " -> %s|%s|%s:%d-%d", step.Callee.TemplateKey, step.Callee.ArgsKey, step.SourceKey, step.Site.Start, step.Site.End)
		}
		out.WriteByte('\n')
	}
	roots := result.Sema.InstantiationGraph.Roots()
	if len(roots) == 0 {
		t.Fatalf("diagnostic path graph has no roots")
	}
	if localInstances != 3 {
		t.Fatalf("local closure instances = %d, want f/g/h; total=%d", localInstances, len(closure.Instances))
	}
	return diagnosedClosure{snapshot: out.String(), rootFileID: uint32(roots[0].Witness.Site.File)}
}
