package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

func TestEntrypointParserHelpRequiresLocalExtensibilityProof(t *testing.T) {
	t.Run("local nominal", func(t *testing.T) {
		result, _ := diagnoseEntrypointContract(t, `
type Payload = { value: int };
@entrypoint("stdin") fn main(payload: Payload) -> int { return payload.value; }
`)
		diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromStdin)
		requireEntrypointNote(t, diagnostic, "help: add this exact public static method to Payload")
	})

	for _, tc := range []struct {
		name   string
		source string
	}{
		{
			name: "alias",
			source: `
type Payload = { value: int };
type Alias = Payload;
@entrypoint("stdin") fn main(payload: Alias) -> int { return payload.value; }
`,
		},
		{
			name: "builtin",
			source: `
@entrypoint("stdin") fn main(value: unit) -> int { return 0; }
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := diagnoseEntrypointContract(t, tc.source)
			diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromStdin)
			requireNeutralEntrypointParserNote(t, diagnostic)
		})
	}

	t.Run("imported", func(t *testing.T) {
		root := t.TempDir()
		libDir := filepath.Join(root, "lib")
		if err := os.MkdirAll(libDir, 0o755); err != nil {
			t.Fatalf("mkdir lib: %v", err)
		}
		if err := os.WriteFile(filepath.Join(libDir, "lib.sg"), []byte(`
pragma module::lib;
pub type Payload = { value: int };
`), 0o600); err != nil {
			t.Fatalf("write lib: %v", err)
		}
		mainPath := filepath.Join(root, "main.sg")
		if err := os.WriteFile(mainPath, []byte(`
pragma module::app;
import lib::{Payload};
@entrypoint("stdin") fn main(payload: Payload) -> int { return payload.value; }
`), 0o600); err != nil {
			t.Fatalf("write main: %v", err)
		}
		result, err := DiagnoseWithOptions(context.Background(), mainPath, &DiagnoseOptions{
			Stage: DiagnoseStageSema, BaseDir: root, MaxDiagnostics: 64,
		})
		if err != nil {
			t.Fatalf("diagnose imported type: %v", err)
		}
		diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromStdin)
		requireNeutralEntrypointParserNote(t, diagnostic)
	})
}

func requireNeutralEntrypointParserNote(t *testing.T, diagnostic *diag.Diagnostic) {
	t.Helper()
	foundNeutral := false
	for _, note := range diagnostic.Notes {
		if strings.Contains(note.Msg, "help: add this exact public static method") {
			t.Fatalf("unproven implementation help = %+v", diagnostic.Notes)
		}
		if strings.Contains(note.Msg, "cannot prove that this parameter type is locally extensible") {
			foundNeutral = true
		}
	}
	if !foundNeutral {
		t.Fatalf("neutral extensibility note missing: %+v", diagnostic.Notes)
	}
}
