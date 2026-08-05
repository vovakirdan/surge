package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"surge/internal/hir"
	"surge/internal/sema"
)

func TestEntrypointPublicationStableAcrossSerialFileOrder(t *testing.T) {
	leftRoot, _, leftB := writeEntrypointPublicationProject(t)
	rightRoot, rightA, _ := writeEntrypointPublicationProject(t)
	left := diagnoseSerialEntrypointPublication(t, leftRoot, leftB)
	right := diagnoseSerialEntrypointPublication(t, rightRoot, rightA)
	if left != right {
		t.Fatalf("serial entrypoint publication changed with root file order:\nleft: %s\nright: %s", left, right)
	}
}

func TestEntrypointPublicationSurvivesParallelAuthorityCopy(t *testing.T) {
	leftRoot, _, _ := writeEntrypointPublicationProject(t)
	rightRoot, _, _ := writeEntrypointPublicationProject(t)
	left := diagnoseParallelEntrypointPublication(t, leftRoot, 1)
	right := diagnoseParallelEntrypointPublication(t, rightRoot, 4)
	if left != right {
		t.Fatalf("parallel entrypoint publication changed with worker order:\nleft:\n%s\nright:\n%s", left, right)
	}
}

func writeEntrypointPublicationProject(t *testing.T) (root, aPath, bPath string) {
	t.Helper()
	root = t.TempDir()
	moduleDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module: %v", err)
	}
	aPath = filepath.Join(moduleDir, "a.sg")
	bPath = filepath.Join(moduleDir, "b.sg")
	files := map[string]string{
		aPath: `
pragma module::demo;
type Payload = { value: int };
extern<Payload> {
    pub fn from_stdin(_text: string) -> Erring<Payload, Error> {
        return Success(Payload { value = 29 });
    }
}
`,
		bPath: `
pragma module::demo;
@entrypoint("stdin")
fn main(payload: Payload) -> int { return payload.value; }
`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root, aPath, bPath
}

func diagnoseSerialEntrypointPublication(t *testing.T, root, entry string) string {
	t.Helper()
	result, err := DiagnoseWithOptions(context.Background(), entry, &DiagnoseOptions{
		Stage: DiagnoseStageSema, BaseDir: root, MaxDiagnostics: 64,
	})
	if err != nil {
		t.Fatalf("serial diagnose: %v", err)
	}
	if result == nil || result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil {
		t.Fatalf("serial diagnose result = %+v", result)
	}
	if _, err := CombineHIRWithModules(context.Background(), result); err != nil {
		t.Fatalf("serial HIR combination: %v", err)
	}
	if result.rootRecord == nil {
		t.Fatal("serial diagnosis did not retain the module record")
	}
	found := ""
	for _, fileID := range result.rootRecord.FileIDs {
		bindings := result.rootRecord.Sema[fileID].EntrypointCallableBindings
		if len(bindings) == 0 {
			continue
		}
		if found != "" {
			t.Fatalf("multiple files received entrypoint bindings in serial publication")
		}
		found = entrypointPublicationSnapshot(t, bindings)
	}
	if found == "" {
		t.Fatal("no file received the serial entrypoint binding")
	}
	return found
}

func diagnoseParallelEntrypointPublication(t *testing.T, root string, workers int) string {
	t.Helper()
	_, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, FullModuleGraph: true, KeepArtifacts: true,
	}, workers)
	if err != nil {
		t.Fatalf("parallel diagnose: %v", err)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	var snapshot strings.Builder
	for i := range results {
		result := &results[i]
		if result.Bag == nil || result.Bag.HasErrors() || result.Sema == nil || result.Symbols == nil {
			var diagnostics strings.Builder
			if result.Bag != nil {
				for _, item := range result.Bag.Items() {
					fmt.Fprintf(&diagnostics, "%s: %s; ", item.Code.ID(), item.Message)
				}
			}
			t.Fatalf("parallel result %s = %+v; diagnostics=%s", result.Path, result, diagnostics.String())
		}
		if _, err := hir.Lower(context.Background(), result.Builder, result.ASTFile, result.Sema, result.Symbols); err != nil {
			t.Fatalf("lower retained result %s: %v", result.Path, err)
		}
		bindings := result.Sema.EntrypointCallableBindings
		if filepath.Base(result.Path) == "b.sg" {
			snapshot.WriteString(entrypointPublicationSnapshot(t, bindings))
			for _, binding := range bindings {
				if result.Symbols.Table.Symbols.Get(binding.Entrypoint) == nil || result.Symbols.Table.Symbols.Get(binding.Callee) == nil {
					t.Fatalf("%s binding escaped local symbol vocabulary: %+v", result.Path, binding)
				}
			}
		} else if len(bindings) != 0 {
			t.Fatalf("non-owning file %s received entrypoint bindings: %+v", result.Path, bindings)
		}
	}
	return snapshot.String()
}

func entrypointPublicationSnapshot(t *testing.T, bindings []sema.EntrypointCallableBinding) string {
	t.Helper()
	if len(bindings) != 1 {
		t.Fatalf("entrypoint bindings = %+v", bindings)
	}
	binding := bindings[0]
	return fmt.Sprintf("%s|%s|%d", binding.SourceKey, binding.CalleeKey, binding.Outcome)
}
