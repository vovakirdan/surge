package mir_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
)

func TestLowerAsyncStateMachineAllowsAwaitInLoop(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let res = (async {
        let i = 0;
        while (i < 2) {
            checkpoint().await();
            i = i + 1;
        }
        return i;
    }).await();
    return compare res {
        Success(v) => v;
        Cancelled() => 0;
    };
}
`
	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		EmitHIR:            true,
		EmitInstantiations: true,
	}
	tmpFile, err := os.CreateTemp(t.TempDir(), "async_single_*.sg")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, writeErr := tmpFile.WriteString(sourceCode); writeErr != nil {
		tmpFile.Close()
		t.Fatalf("failed to write source code: %v", writeErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		t.Fatalf("failed to close temp file: %v", closeErr)
	}

	result, err := driver.DiagnoseWithOptions(context.Background(), tmpFile.Name(), opts)
	if err == nil && result != nil && result.Bag.HasErrors() {
		var sb strings.Builder
		for _, d := range result.Bag.Items() {
			sb.WriteString(d.Message)
			sb.WriteString("\n")
		}
		t.Fatalf("compilation errors:\n%s", sb.String())
	}
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}
	if result.HIR == nil || result.Instantiations == nil || result.Sema == nil {
		t.Fatalf("missing compilation outputs")
	}
	hirModule, err := driver.CombineHIRWithModules(context.Background(), result)
	if err != nil {
		t.Fatalf("HIR merge failed: %v", err)
	}
	if hirModule == nil {
		hirModule = result.HIR
	}
	mm, err := mono.MonomorphizeModule(hirModule, result.Instantiations, result.Sema, mono.Options{
		MaxDepth: 64,
	})
	if err != nil {
		t.Fatalf("monomorphization failed: %v", err)
	}
	mirMod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		t.Fatalf("MIR lowering failed: %v", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}
	if err := mir.LowerAsyncStateMachine(mirMod, result.Sema, result.Symbols.Table); err != nil {
		t.Fatalf("async lowering failed: %v", err)
	}
}
