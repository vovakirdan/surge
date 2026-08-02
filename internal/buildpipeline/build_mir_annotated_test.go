package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const buildMIRAnnotationSource = `
@entrypoint
fn main() -> int {
    let message: string = "hello";
    return len(&message) to int;
}
`

func TestBuildMIRAnnotationModes(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	sourcePath := filepath.Join(t.TempDir(), "mir_annotation.sg")
	if err := os.WriteFile(sourcePath, []byte(buildMIRAnnotationSource), 0o600); err != nil {
		t.Fatalf("write MIR annotation source: %v", err)
	}

	buildAndReadMIR := func(t *testing.T, emitMIR, annotated bool) string {
		t.Helper()
		result, err := Build(context.Background(), &BuildRequest{
			CompileRequest: CompileRequest{
				TargetPath:     sourcePath,
				BaseDir:        testRepoRoot(t),
				MaxDiagnostics: 20,
			},
			OutputName:       "mir-annotation",
			OutputRoot:       t.TempDir(),
			Backend:          BackendVM,
			EmitMIR:          emitMIR,
			EmitMIRAnnotated: annotated,
		})
		if err != nil {
			t.Fatalf("build MIR annotation mode emit=%v annotated=%v: %v", emitMIR, annotated, err)
		}
		data, err := os.ReadFile(filepath.Join(result.TmpDir, "out.mir"))
		if err != nil {
			t.Fatalf("read kept MIR dump emit=%v annotated=%v: %v", emitMIR, annotated, err)
		}
		return string(data)
	}

	annotated := buildAndReadMIR(t, false, true)
	if !strings.Contains(annotated, "owes_release") || !strings.Contains(annotated, "[effect=mint]") {
		t.Fatalf("annotated-only build did not propagate ownership annotations:\n%s", annotated)
	}

	plain := buildAndReadMIR(t, true, false)
	if strings.Contains(plain, "owes_release") || strings.Contains(plain, "[effect=") {
		t.Fatalf("plain MIR emission unexpectedly contains ownership annotations:\n%s", plain)
	}
}
