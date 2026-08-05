package buildpipeline

import (
	"context"
	"testing"

	"surge/internal/types"
)

func TestCompileConsumesPostSemaInstantiationClosure(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	path := writeAnalysisSource(t, `
fn g<T>(value: T) -> T { return value; }
fn f<T>(value: T) -> T { return g(value); }
fn use_it() -> int { return f(7); }
`)
	result, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 64,
		Analysis:       true,
	})
	if err != nil {
		t.Fatalf("production compile: %v", err)
	}
	if result.MIR == nil || result.Diagnose == nil || result.Diagnose.Sema == nil || result.Diagnose.Sema.InstantiationIdentity == nil || result.Diagnose.Sema.InstantiationClosure == nil {
		t.Fatalf("production pipeline did not retain/consume finalized closure: %+v", result)
	}
	found := map[string]bool{}
	for _, instance := range result.Diagnose.Sema.InstantiationClosure.Instances {
		sym := result.Diagnose.Symbols.Table.Symbols.Get(instance.Template)
		if sym == nil {
			continue
		}
		name, _ := result.Diagnose.Symbols.Table.Strings.Lookup(sym.Name)
		if name != "f" && name != "g" {
			continue
		}
		if len(instance.TemplateArgs) != 1 || types.ContainsGenericParam(result.Diagnose.Sema.TypeInterner, instance.TemplateArgs[0]) {
			t.Fatalf("%s reached product lowering with unresolved args: %v", name, instance.TemplateArgs)
		}
		found[name] = true
	}
	if !found["f"] || !found["g"] {
		t.Fatalf("production closure missing f<int>/g<int>: %v", found)
	}
}
