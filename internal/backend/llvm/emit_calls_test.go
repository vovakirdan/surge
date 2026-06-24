package llvm

import (
	"context"
	"os"
	"regexp"
	"strings"
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

func TestEmitFixedWidthLiteralCastAvoidsBigIntMaterialization(t *testing.T) {
	sourceCode := `fn loop_fixed() -> int64 {
    let mut i: int64 = 0:int64;
    let mut sum: int64 = 0:int64;
    while i < 10000:int64 {
        sum = sum + 1:int64;
        i = i + 1:int64;
    }
    return sum;
}

@entrypoint
fn main() -> int {
    let out: int64 = loop_fixed();
    if out == 10000:int64 {
        return 0;
    }
    return 1;
}
`

	ir := emitLLVMFromSource(t, sourceCode)
	body := findI64FunctionBodyContaining(t, ir, "store i64 10000")

	if strings.Contains(body, "rt_bigint_from_literal") || strings.Contains(body, "rt_bigint_to_i64") {
		t.Fatalf("fixed-width literal casts should not materialize BigInt in loop_fixed:\n%s", body)
	}
	for _, want := range []string{"store i64 10000", "store i64 1", "add i64", "icmp slt i64"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in loop_fixed IR:\n%s", want, body)
		}
	}
}

func TestEmitFixedWidthLiteralCastPreservesIntegerLiteralBases(t *testing.T) {
	sourceCode := `fn base_literals() -> uint64 {
    let hex: uint64 = 0xFF:uint64;
    let binary: uint64 = 0b1010:uint64;
    let octal: uint64 = 0o7:uint64;
    let grouped: uint64 = 1_000:uint64;
    let leading_zero: uint64 = 010:uint64;
    return hex + binary + octal + grouped + leading_zero;
}

@entrypoint
fn main() -> int {
    if base_literals() == 1282:uint64 {
        return 0;
    }
    return 1;
}
`

	ir := emitLLVMFromSource(t, sourceCode)
	body := findI64FunctionBodyContaining(t, ir, "store i64 255")

	for _, forbidden := range []string{"rt_bigint_from_literal", "rt_biguint_from_literal", "rt_bigint_to_u64", "rt_biguint_to_u64"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("fixed-width literal casts should preserve supported integer bases without %s:\n%s", forbidden, body)
		}
	}
	for _, want := range []string{"store i64 255", "store i64 10", "store i64 7", "store i64 1000"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in base_literals IR:\n%s", want, body)
		}
	}
}

func TestEmitFixedWidthUintFromStrUsesI64ParseValue(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let text: string = "42";
    let parsed = compare uint64.from_str(&text) {
        Success(v) => v;
        _ => {
            return 1;
        }
    };
    if parsed == 42:uint64 {
        return 0;
    }
    return 2;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !strings.Contains(ir, "call i1 @rt_parse_uint(") {
		t.Fatalf("expected uint parser call in IR:\n%s", ir)
	}
	bodyRe := regexp.MustCompile(`(?s)define (?:ptr|i64) @fn\.\d+\([^)]*\) \{.*?rt_parse_uint.*?\n\}`)
	body := bodyRe.FindString(ir)
	if body == "" {
		t.Fatalf("cannot find uint parser function body in IR:\n%s", ir)
	}
	if strings.Contains(body, "rt_biguint_to_u64") {
		t.Fatalf("fixed-width uint from_str must not treat raw i64 parse output as BigUint ptr:\n%s", ir)
	}
}

func findI64FunctionBodyContaining(t *testing.T, ir, needle string) string {
	t.Helper()

	re := regexp.MustCompile(`(?s)define i64 @fn\.\d+\(\) \{.*?\n\}`)
	for _, body := range re.FindAllString(ir, -1) {
		if strings.Contains(body, needle) && strings.Contains(body, "add i64") {
			return body
		}
	}
	t.Fatalf("no i64 function body containing %q found in IR:\n%s", needle, ir)
	return ""
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
