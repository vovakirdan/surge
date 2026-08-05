package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

func TestCloneUsesPublicWinnerFromAnotherModule(t *testing.T) {
	result := diagnoseCloneProject(t, map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Model = { text: string }
extern<Model> {
    pub fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
`,
		"main.sg": `
pragma module::app;
import model::Model;
@entrypoint fn main() -> int {
    let value = Model { text = "seed" };
    let copy = clone(&value);
    return 0;
}
`,
	})
	requireNoCloneErrors(t, result)

	binding := requireCloneBinding(t, result, "main.sg", "Model")
	if !strings.Contains(binding.CalleeKey, "model/value.sg") {
		t.Fatalf("direct clone bound to %q, want the model module body", binding.CalleeKey)
	}
	requireClosureAndMonoNames(t, result, []string{"__clone"})
}

func TestCloneUsesModulePrivateWinnerInsideItsOwnModule(t *testing.T) {
	result := diagnoseCloneProject(t, map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Model = { text: string }
extern<Model> {
    fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
pub fn duplicate(value: &Model) -> Model { return clone(value); }
`,
		"main.sg": `
pragma module::app;
import model::{Model, duplicate};
@entrypoint fn main() -> int {
    let value = Model { text = "seed" };
    let copy = duplicate(&value);
    return 0;
}
`,
	})
	requireNoCloneErrors(t, result)

	binding := requireCloneBinding(t, result, "model/value.sg", "Model")
	if !strings.Contains(binding.CalleeKey, "model/value.sg") {
		t.Fatalf("module-private clone bound to %q", binding.CalleeKey)
	}
	requireClosureAndMonoNames(t, result, []string{"__clone", "duplicate"})
}

func TestCloneGenericInstantiationAgreesWithDirectUse(t *testing.T) {
	result := diagnoseCloneProject(t, map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Model = { text: string }
extern<Model> {
    pub fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
`,
		"main.sg": `
pragma module::app;
import model::Model;
fn dup<T>(value: &T) -> T { return clone(value); }
@entrypoint fn main() -> int {
    let value = Model { text = "seed" };
    let direct = clone(&value);
    let generic = dup(&value);
    return 0;
}
`,
	})
	requireNoCloneErrors(t, result)

	binding := requireCloneBinding(t, result, "main.sg", "Model")
	deferredKey := ""
	for _, call := range result.Sema.InstantiationClosure.ResolvedDeferredCalls {
		if call.Kind != sema.DeferredCloneCall || call.Outcome != sema.DeferredCallableResolved {
			continue
		}
		if !strings.Contains(call.CalleeKey, "|Model|__clone|") {
			continue
		}
		deferredKey = call.CalleeKey
	}
	if deferredKey == "" {
		t.Fatalf("generic clone never resolved: %+v", result.Sema.InstantiationClosure.ResolvedDeferredCalls)
	}
	if deferredKey != binding.CalleeKey {
		t.Fatalf("generic clone chose %q but the direct use chose %q", deferredKey, binding.CalleeKey)
	}
}

func TestCloneDedupesImportAliasesOfOneDeclaration(t *testing.T) {
	result := diagnoseCloneProject(t, map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Model = { text: string }
extern<Model> {
    pub fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
`,
		"helper.sg": `
pragma module::app;
import model::Model;
pub fn helper_copy(value: &Model) -> Model { return clone(value); }
`,
		"main.sg": `
pragma module::app;
import model::Model;
@entrypoint fn main() -> int {
    let value = Model { text = "seed" };
    let direct = clone(&value);
    let helped = helper_copy(&value);
    return 0;
}
`,
	})
	requireNoCloneErrors(t, result)

	keys := make(map[string]struct{})
	for _, binding := range result.Sema.DirectCloneBindings {
		if strings.Contains(binding.CalleeKey, "|Model|__clone|") {
			keys[binding.CalleeKey] = struct{}{}
		}
	}
	if len(keys) != 1 {
		t.Fatalf("aliases of one declaration produced %d canonical bodies: %v", len(keys), keys)
	}
}

func TestCloneRejectsTwoEquallySpecificImplementations(t *testing.T) {
	bag := diagnoseCloneProjectDiagnostics(t, cloneConflictProject())
	items := cloneDiagnosticsWithCode(bag, diag.SemaCloneHookConflict)
	if len(items) != 1 {
		t.Fatalf("conflict diagnostics = %d, want one: %s", len(items), formatCloneDiagnostics(bag))
	}
	conflict := items[0]
	if !strings.Contains(conflict.Message, "equally specific canonical __clone implementations") {
		t.Fatalf("conflict message = %q", conflict.Message)
	}
	declarations := make(map[string]struct{})
	for _, note := range conflict.Notes {
		declarations[note.Span.String()] = struct{}{}
	}
	if len(declarations) != 2 {
		t.Fatalf("conflict notes point at %d declarations, want one per implementation: %+v", len(declarations), conflict.Notes)
	}
}

func TestCloneReportsInvisibleWinnerWithoutFallingBack(t *testing.T) {
	bag := diagnoseCloneProjectDiagnostics(t, cloneInvisibleWinnerProject(`
    let value: Box = { value: 7 };
    let copy = clone(&value);
`))
	requireInvisibleCloneWinner(t, bag)
}

func TestCloneReportsInvisibleWinnerThroughGenericInstantiation(t *testing.T) {
	bag := diagnoseCloneProjectDiagnostics(t, cloneInvisibleWinnerProject(`
    let value: Box = { value: 7 };
    let copy = dup(&value);
`))
	requireInvisibleCloneWinner(t, bag)
}

func TestCloneBindingsArePublishedForSingleFilePrograms(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "main.sg")
	if err := os.WriteFile(entry, []byte(`
pragma module::app;
type Box = { text: string }
extern<Box> {
    fn __clone(self: &Box) -> Box {
        return Box { text = clone(&self.text) };
    }
}
@entrypoint fn main() -> int {
    let value = Box { text = "seed" };
    let copy = clone(&value);
    return 0;
}
`), 0o600); err != nil {
		t.Fatalf("write single file: %v", err)
	}
	_, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, KeepArtifacts: true,
	}, 1)
	if err != nil {
		t.Fatalf("diagnose single-file project: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("single-file project produced %d results", len(results))
	}
	single := &results[0]
	if single.Bag != nil && single.Bag.HasErrors() {
		t.Fatalf("single-file diagnostics:\n%s", formatCloneDiagnostics(single.Bag))
	}
	clones := 0
	for _, sym := range single.Sema.CloneSymbols {
		if sym.IsValid() {
			clones++
		}
	}
	if clones == 0 {
		t.Fatalf("single-file program published no clone decisions: %+v", single.Sema.DirectCloneBindings)
	}
}

func TestCloneConflictDiagnosticsAreByteStable(t *testing.T) {
	project := cloneConflictProject()
	first := formatCloneDiagnostics(diagnoseCloneProjectDiagnostics(t, project))
	second := formatCloneDiagnostics(diagnoseCloneProjectDiagnostics(t, project))
	if first != second {
		t.Fatalf("clone conflict diagnostics changed between compiles:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(first, "SEM3185") {
		t.Fatalf("stable output does not contain the conflict: %s", first)
	}
}

func cloneConflictProject() map[string]string {
	return map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Model = { text: string }
`,
		"left/hook.sg": `
pragma module::left;
import model::Model;
extern<Model> {
    pub fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
`,
		"right/hook.sg": `
pragma module::right;
import model::Model;
extern<Model> {
    pub fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
`,
		"main.sg": `
pragma module::app;
import model::Model;
import left;
import right;
@entrypoint fn main() -> int {
    let value = Model { text = "seed" };
    let copy = clone(&value);
    return 0;
}
`,
	}
}

// cloneInvisibleWinnerProject keeps Box's canonical implementation module
// private while another module publishes a perfectly usable public `__clone` of
// its own. The use site must be told it cannot reach Box's implementation
// instead of quietly cloning through whatever else is visible.
func cloneInvisibleWinnerProject(body string) map[string]string {
	return map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Box = { value: int }
extern<Box> {
    fn __clone(self: &Box) -> Box {
        return { value: self.value };
    }
}
`,
		"shared/generic.sg": `
pragma module::shared;
pub type Wrapper = { value: int }
extern<Wrapper> {
    pub fn __clone(self: &Wrapper) -> Wrapper {
        return { value: self.value };
    }
}
`,
		"main.sg": `
pragma module::app;
import model::Box;
import shared::Wrapper;
fn dup<T>(value: &T) -> T { return clone(value); }
@entrypoint fn main() -> int {
` + body + `    return 0;
}
`,
	}
}

func requireInvisibleCloneWinner(t *testing.T, bag *diag.Bag) {
	t.Helper()
	items := cloneDiagnosticsWithCode(bag, diag.SemaCloneHookNotVisible)
	if len(items) != 1 {
		t.Fatalf("visibility diagnostics = %d, want one: %s", len(items), formatCloneDiagnostics(bag))
	}
	if notClonable := cloneDiagnosticsWithCode(bag, diag.SemaTypeNotClonable); len(notClonable) != 0 {
		t.Fatalf("an invisible canonical implementation was reported as missing: %s", formatCloneDiagnostics(bag))
	}
	item := items[0]
	if !strings.Contains(item.Message, "is not visible from module") {
		t.Fatalf("visibility message = %q", item.Message)
	}
	for _, note := range item.Notes {
		if strings.Contains(note.Msg, "shared") || strings.Contains(note.Msg, "Wrapper") {
			t.Fatalf("visibility diagnostic offered a different implementation: %q", note.Msg)
		}
	}
}

// requireCloneBinding finds the decision for one use site. receiverKey selects
// among the several clones a file may contain, including the string clones the
// `__clone` bodies themselves perform.
func requireCloneBinding(t *testing.T, result *DiagnoseResult, sourceKey, receiverKey string) sema.DirectCloneBinding {
	t.Helper()
	for _, binding := range result.Sema.DirectCloneBindings {
		if binding.SourceKey == sourceKey && strings.Contains(binding.CalleeKey, "|"+receiverKey+"|__clone|") {
			return binding
		}
	}
	t.Fatalf("no %s clone binding for %s: %+v", receiverKey, sourceKey, result.Sema.DirectCloneBindings)
	return sema.DirectCloneBinding{}
}

func requireNoCloneErrors(t *testing.T, result *DiagnoseResult) {
	t.Helper()
	if result.Bag != nil && result.Bag.HasErrors() {
		t.Fatalf("clone project diagnostics:\n%s", formatCloneDiagnostics(result.Bag))
	}
	if result.Sema == nil || result.Sema.InstantiationClosure == nil {
		t.Fatalf("clone project produced no closure: %+v", result)
	}
}

func cloneDiagnosticsWithCode(bag *diag.Bag, code diag.Code) []*diag.Diagnostic {
	if bag == nil {
		return nil
	}
	out := make([]*diag.Diagnostic, 0, 1)
	for _, item := range bag.Items() {
		if item.Code == code {
			out = append(out, item)
		}
	}
	return out
}

func formatCloneDiagnostics(bag *diag.Bag) string {
	if bag == nil {
		return ""
	}
	var out strings.Builder
	for _, item := range bag.Items() {
		out.WriteString(item.Code.ID())
		out.WriteString(" ")
		out.WriteString(item.Primary.String())
		out.WriteString(" ")
		out.WriteString(item.Message)
		out.WriteString("\n")
		for _, note := range item.Notes {
			out.WriteString("  note ")
			out.WriteString(note.Span.String())
			out.WriteString(" ")
			out.WriteString(note.Msg)
			out.WriteString("\n")
		}
	}
	return out.String()
}

func writeCloneProject(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// diagnoseCloneProject compiles a multi-module project rooted at its main.sg.
func diagnoseCloneProject(t *testing.T, files map[string]string) *DiagnoseResult {
	t.Helper()
	root := writeCloneProject(t, files)
	result, err := DiagnoseWithOptions(context.Background(), filepath.Join(root, "main.sg"), &DiagnoseOptions{
		Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64,
		EmitHIR: true, EmitInstantiations: true,
	})
	if err != nil {
		t.Fatalf("diagnose clone project: %v", err)
	}
	if result == nil {
		t.Fatal("clone project produced no result")
	}
	return result
}
