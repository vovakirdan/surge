package llvm

import (
	"context"
	"os"
	"regexp"
	"testing"

	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
)

func TestEmitCallTypesNothingToCalleeParam(t *testing.T) {
	sourceCode := `type Holder = {}

extern<Holder> {
    fn pass(self: &Holder, value: Option<string>) -> Option<string> {
        return value;
    }
}

@entrypoint
fn main() -> int {
    let holder: Holder = {};
    let value = holder.pass(nothing);
    compare value {
        Some(text) => print(text);
        nothing => print("none");
    };
    return 0;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if regexp.MustCompile(`call ptr @fn\.\d+\(i8 0\)`).MatchString(ir) {
		t.Fatalf("untyped nothing leaked into call ABI:\n%s", ir)
	}
	if !regexp.MustCompile(`call ptr @fn\.\d+\(ptr `).MatchString(ir) {
		t.Fatalf("expected typed ptr call for Option<string> argument:\n%s", ir)
	}
}

func TestEmitErringOptionNestedTagPipeline(t *testing.T) {
	sourceCode := `fn demo(flag: bool) -> Erring<Option<string>, Error> {
    if flag {
        return Success(Some("x"));
    }
    return Success(nothing);
}

@entrypoint
fn main() -> int {
    let v = demo(true);
    compare v {
        Success(Some(s)) => {
            print(s);
            return 0;
        }
        Success(nothing) => {
            print("none");
            return 0;
        }
        err => {
            let _ = err;
            return 1;
        }
    };
    return 1;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !regexp.MustCompile(`call ptr @rt_alloc`).MatchString(ir) {
		t.Fatalf("expected nested union construction to emit runtime allocation:\n%s", ir)
	}
}

func emitLLVMFromSource(t *testing.T, sourceCode string) string {
	t.Helper()

	mirMod, result := lowerMIRFromSource(t, sourceCode)

	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	return ir
}

func lowerMIRFromSource(t *testing.T, sourceCode string) (*mir.Module, *driver.DiagnoseResult) {
	t.Helper()

	tmpFile, err := os.CreateTemp(t.TempDir(), "emit-call-*.sg")
	if err != nil {
		t.Fatalf("create temp source: %v", err)
	}
	defer func() {
		if removeErr := os.Remove(tmpFile.Name()); removeErr != nil {
			t.Fatalf("remove temp source: %v", removeErr)
		}
	}()

	_, err = tmpFile.WriteString(sourceCode)
	if err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			t.Fatalf("close temp source after write failure: %v", closeErr)
		}
		t.Fatalf("write temp source: %v", err)
	}
	err = tmpFile.Close()
	if err != nil {
		t.Fatalf("close temp source: %v", err)
	}

	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		EmitHIR:            true,
		EmitInstantiations: true,
	}
	result, err := driver.DiagnoseWithOptions(context.Background(), tmpFile.Name(), &opts)
	if err != nil {
		t.Fatalf("diagnose source: %v", err)
	}
	if result.Bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", result.Bag.Items())
	}
	if result.HIR == nil || result.Instantiations == nil || result.Sema == nil || result.Symbols == nil {
		t.Fatalf("missing compilation artifacts for LLVM emission test")
	}

	hirModule, err := driver.CombineHIRWithModules(context.Background(), result)
	if err != nil {
		t.Fatalf("combine HIR modules: %v", err)
	}
	if hirModule == nil {
		hirModule = result.HIR
	}

	mm, err := mono.MonomorphizeModule(hirModule, result.Instantiations, result.Sema, mono.Options{MaxDepth: 64})
	if err != nil {
		t.Fatalf("monomorphize: %v", err)
	}
	mirMod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		t.Fatalf("lower to MIR: %v", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}
	err = mir.LowerAsyncStateMachine(mirMod, result.Sema, result.Symbols.Table)
	if err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
	}
	err = mir.Validate(mirMod, result.Sema.TypeInterner)
	if err != nil {
		t.Fatalf("validate MIR: %v", err)
	}

	return mirMod, result
}
