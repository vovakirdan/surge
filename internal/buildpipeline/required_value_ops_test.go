package buildpipeline

import (
	"context"
	"slices"
	"testing"

	"surge/internal/hir"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/sema"
)

// clonableWithoutACloneCall declares a valid `__clone` that no expression ever
// calls. The implementation still has to be emitted: a carrier duplicating a
// Wrapper reaches it through the value's operation table, not through source.
const clonableWithoutACloneCall = `
type Wrapper = { count: int };

extern<Wrapper> {
    pub fn __clone(self: &Wrapper) -> Wrapper {
        return Wrapper { count = self.count };
    }
}

fn observe(w: &Wrapper) -> int { return w.count; }

fn build() -> int {
    let w = Wrapper { count = 7 };
    return observe(&w);
}
`

func TestCompileEmitsACloneNoSourceExpressionAsksFor(t *testing.T) {
	emitted := monoFunctionNames(compileForRequiredOps(t, clonableWithoutACloneCall))
	if !slices.Contains(emitted, "__clone") {
		t.Fatalf("clonable Wrapper reached MIR with no emitted clone implementation: %v", emitted)
	}
}

// TestCompileEmitsACloneDeclaredInADependencyModule pins that a required
// operation reaches an implementation the seed policy cannot. Error and its
// `__clone` are declared in core, which this program only imports, and the
// policy that seeds reachability deliberately admits root-module bodies only.
//
// The control compile is what makes this an argument rather than an
// observation: the only difference between the two programs is whether a value
// of a clonable dependency type exists, so the clone body appearing in one and
// not the other can only be that value's doing.
func TestCompileEmitsACloneDeclaredInADependencyModule(t *testing.T) {
	usesADependencyType := monoFunctionNames(compileForRequiredOps(t, `
fn describe() -> uint {
    let e = Error { message = "bad", code = 2 };
    return e.code;
}
`))
	if !slices.Contains(usesADependencyType, "__clone") {
		t.Fatalf("dependency-module clone implementation was never emitted: %v", usesADependencyType)
	}

	namesNoClonableType := monoFunctionNames(compileForRequiredOps(t, `
fn describe() -> uint { return 2; }
`))
	if slices.Contains(namesNoClonableType, "__clone") {
		t.Fatalf("a program naming no clonable type still emitted a clone implementation: %v", namesNoClonableType)
	}
}

func TestCompileEntersMonomorphizationExactlyOnce(t *testing.T) {
	original := monomorphizeModule
	entries := 0
	monomorphizeModule = func(
		module *hir.Module,
		instantiations *mono.InstantiationMap,
		semaResult *sema.Result,
		opts mono.Options,
	) (*mono.MonoModule, error) {
		entries++
		return original(module, instantiations, semaResult, opts)
	}
	t.Cleanup(func() { monomorphizeModule = original })

	compileForRequiredOps(t, clonableWithoutACloneCall)

	if entries != 1 {
		t.Fatalf("compile entered monomorphization %d times, want exactly one; a required operation "+
			"discovered after the single pass has nothing left to emit it", entries)
	}
}

// TestCompileEmitsTheSameFunctionsTwice is the determinism gate: two compiles of
// one program must agree on what the required operations pulled in.
func TestCompileEmitsTheSameFunctionsTwice(t *testing.T) {
	first := monoFunctionNames(compileForRequiredOps(t, clonableWithoutACloneCall))
	second := monoFunctionNames(compileForRequiredOps(t, clonableWithoutACloneCall))
	if !slices.Equal(first, second) {
		t.Fatalf("two compiles disagreed on emitted functions:\nfirst  %v\nsecond %v", first, second)
	}
}

func compileForRequiredOps(t *testing.T, source string) *mir.Module {
	t.Helper()
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	result, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     writeAnalysisSource(t, source),
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	})
	if err != nil {
		if result.Diagnose != nil && result.Diagnose.Bag != nil {
			for _, item := range result.Diagnose.Bag.Items() {
				t.Logf("diagnostic %s: %s", item.Code, item.Message)
			}
		}
		t.Fatalf("Compile: %v", err)
	}
	if result.MIR == nil {
		t.Fatal("compile produced no MIR")
	}
	return result.MIR
}

func monoFunctionNames(module *mir.Module) []string {
	names := make([]string, 0, len(module.Funcs))
	for _, id := range module.SortedFuncIDs() {
		if fn := module.Funcs[id]; fn != nil {
			names = append(names, fn.Name)
		}
	}
	return names
}
