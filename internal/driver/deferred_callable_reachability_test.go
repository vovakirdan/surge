package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/mono"
)

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
