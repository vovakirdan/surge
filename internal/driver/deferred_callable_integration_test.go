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
)

func TestDeferredMethodResolvesPerConcreteGenericCallerInstance(t *testing.T) {
	result := diagnoseDeferredProgram(t, `
pragma module::app, no_std;

type Alpha = { value: int }
type Beta = { value: int }

fn alpha_leaf<U>(value: U) -> U { return value; }
fn beta_leaf<U>(value: U) -> U { return value; }

extern<Alpha> { fn Pick(self: Alpha) -> int { return alpha_leaf(self.value); } }
extern<Beta> { fn Pick(self: Beta) -> int { return beta_leaf(self.value); } }

contract Pickable<T> { fn Pick(self: T) -> int; }
fn invoke<T: Pickable<T>>(value: T) -> int { return value.Pick(); }

@entrypoint fn main() -> int {
    return invoke(Alpha { value = 2 }) + invoke(Beta { value = 3 });
}
`)

	calls := result.Sema.InstantiationClosure.ResolvedDeferredCalls
	if len(calls) != 2 {
		t.Fatalf("resolved deferred calls = %d, want one per invoke<T> instance: %+v", len(calls), calls)
	}
	seenCallees := make(map[string]bool)
	seenCallers := make(map[string]bool)
	for _, call := range calls {
		if call.Kind != sema.DeferredMethodCall || call.Outcome != sema.DeferredCallableResolved || !call.Callee.IsValid() {
			t.Fatalf("unexpected concrete deferred method: %+v", call)
		}
		seenCallees[call.CalleeKey] = true
		seenCallers[call.Caller.ArgsKey] = true
	}
	if len(seenCallees) != 2 || len(seenCallers) != 2 {
		t.Fatalf("concrete caller instances collapsed: callees=%v callers=%v", seenCallees, seenCallers)
	}
	requireClosureAndMonoNames(t, result, []string{"alpha_leaf", "beta_leaf"})
}

func TestDeferredBoolUsesExactResolvedCallable(t *testing.T) {
	result := diagnoseDeferredProgram(t, `
pragma module::app, no_std;

type Flag = { value: bool }
extern<Flag> { fn __bool(self: Flag) -> bool { return self.value; } }
contract Truthy<T> { fn __bool(self: T) -> bool; }

fn classify<T: Truthy<T>>(value: T) -> int {
    if value { return 1; }
    return 0;
}

@entrypoint fn main() -> int { return classify(Flag { value = true }); }
`)

	calls := result.Sema.InstantiationClosure.ResolvedDeferredCalls
	if len(calls) != 1 || calls[0].Kind != sema.DeferredBoolCall || calls[0].Outcome != sema.DeferredCallableResolved {
		t.Fatalf("resolved bool authority = %+v", calls)
	}
	sym := result.Symbols.Table.Symbols.Get(calls[0].Callee)
	name := ""
	if sym != nil {
		name, _ = result.Symbols.Table.Strings.Lookup(sym.Name)
	}
	if name != "__bool" {
		t.Fatalf("resolved bool callee = %q, want __bool", name)
	}
	requireClosureAndMonoNames(t, result, []string{"classify", "__bool"})
}

func TestDeferredCloneDistinguishesBuiltinCopyAndUserMethod(t *testing.T) {
	result := diagnoseDeferredProgram(t, `
pragma module::app;

type Box = { value: string }
extern<Box> {
    fn __clone(self: &Box) -> Box {
        return Box { value = clone(&self.value) };
    }
}

fn duplicate<T>(value: &T) -> T { return clone(value); }

@entrypoint fn main() -> int {
    let number = 7;
    let _ = duplicate(&number);
    let box = Box { value = "value" };
    let _ = duplicate(&box);
    return 0;
}
`)

	calls := result.Sema.InstantiationClosure.ResolvedDeferredCalls
	if len(calls) != 2 {
		t.Fatalf("resolved clone calls = %d, want Copy and user method: %+v", len(calls), calls)
	}
	seenCopy, seenMethod := false, false
	for _, call := range calls {
		if call.Kind != sema.DeferredCloneCall {
			t.Fatalf("unexpected deferred clone kind: %+v", call)
		}
		switch call.Outcome {
		case sema.DeferredCallableBuiltinCopy:
			seenCopy = true
			if call.Callee.IsValid() {
				t.Fatalf("builtin Copy clone retained a callable: %+v", call)
			}
		case sema.DeferredCallableResolved:
			seenMethod = true
			sym := result.Symbols.Table.Symbols.Get(call.Callee)
			name := ""
			if sym != nil {
				name, _ = result.Symbols.Table.Strings.Lookup(sym.Name)
			}
			if name != "__clone" {
				t.Fatalf("resolved clone callee = %q, want __clone", name)
			}
		default:
			t.Fatalf("unexpected clone outcome: %+v", call)
		}
	}
	if !seenCopy || !seenMethod {
		t.Fatalf("clone outcomes: copy=%t method=%t", seenCopy, seenMethod)
	}
	// The `__clone` body itself clones a string directly; that concrete use goes
	// through the same authority as the generic ones.
	direct := 0
	for _, binding := range result.Sema.DirectCloneBindings {
		if binding.SourceKey == "main.sg" && strings.Contains(binding.CalleeKey, "|string|__clone|") {
			direct++
		}
	}
	if direct != 1 {
		t.Fatalf("direct string clone bindings = %d, want one: %+v", direct, result.Sema.DirectCloneBindings)
	}
	requireClosureAndMonoNames(t, result, []string{"duplicate", "__clone"})
}

func TestDeferredStaticMethodDoesNotInjectSelf(t *testing.T) {
	result := diagnoseDeferredProgram(t, `
pragma module::app, no_std;

type Token = { value: int }
fn static_leaf<T>(value: T) -> T { return value; }

extern<Token> {
    fn Build() -> Token { return Token { value = static_leaf(7) }; }
}

contract Factory<T> { fn Build() -> T; }
fn invoke<T: Factory<T>>() -> T { return T.Build(); }

@entrypoint fn main() -> int {
    let value = invoke::<Token>();
    return value.value;
}
`)

	calls := result.Sema.InstantiationClosure.ResolvedDeferredCalls
	if len(calls) != 1 || calls[0].Kind != sema.DeferredMethodCall || !calls[0].StaticReceiver {
		t.Fatalf("resolved static authority = %+v", calls)
	}
	requireClosureAndMonoNames(t, result, []string{"invoke", "Build", "static_leaf"})
}

func diagnoseDeferredProgram(t *testing.T, sourceText string) *DiagnoseResult {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "main.sg")
	if err := os.WriteFile(path, []byte(sourceText), 0o600); err != nil {
		t.Fatalf("write deferred source: %v", err)
	}
	result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{
		Stage:              DiagnoseStageAll,
		BaseDir:            root,
		MaxDiagnostics:     64,
		EmitHIR:            true,
		EmitInstantiations: true,
	})
	if err != nil {
		t.Fatalf("diagnose deferred source: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Sema.InstantiationClosure == nil {
		if result != nil && result.Bag != nil {
			t.Fatalf("deferred diagnostics:\n%s", diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
		}
		t.Fatalf("deferred result: %+v", result)
	}
	return result
}

func requireClosureAndMonoNames(t *testing.T, result *DiagnoseResult, expected []string) {
	t.Helper()
	closureNames := make(map[string]bool)
	for _, callable := range result.Sema.InstantiationClosure.LiveCallables {
		if sym := result.Symbols.Table.Symbols.Get(callable); sym != nil {
			name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
			closureNames[name] = true
		}
	}
	for _, instance := range result.Sema.InstantiationClosure.Instances {
		if sym := result.Symbols.Table.Symbols.Get(instance.Template); sym != nil {
			name, _ := result.Symbols.Table.Strings.Lookup(sym.Name)
			closureNames[name] = true
		}
	}
	combined, err := CombineHIRWithModules(context.Background(), result)
	if err != nil {
		t.Fatalf("combine deferred HIR: %v", err)
	}
	mm, err := mono.MonomorphizeModule(combined, result.Instantiations, result.Sema, mono.Options{EnableDCE: true})
	if err != nil {
		t.Fatalf("mono deferred program: %v", err)
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
	for _, name := range expected {
		if !closureNames[name] && name != "__bool" && name != "__clone" {
			t.Fatalf("%s missing from closure instances: %v", name, closureNames)
		}
		if !monoNames[name] {
			t.Fatalf("%s missing after mono/DCE: %v", name, monoNames)
		}
	}
}
