package diagnose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/driver"
)

func TestDiagnoseWorkspaceOverlayPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")

	diskContent := "@entrypoint\nfn main() {\n    print(\"hi\")\n}\n"
	if err := os.WriteFile(path, []byte(diskContent), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	opts := DiagnoseOptions{
		ProjectRoot:    path,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: 8,
	}

	diags, err := DiagnoseWorkspace(context.Background(), &opts, FileOverlay{})
	if err != nil {
		t.Fatalf("diagnose without overlay: %v", err)
	}
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics for disk content")
	}

	overlayContent := "@entrypoint\nfn main() {\n    print(\"hi\");\n}\n"
	diags, err = DiagnoseWorkspace(context.Background(), &opts, FileOverlay{
		Files: map[string]string{
			path: overlayContent,
		},
	})
	if err != nil {
		t.Fatalf("diagnose with overlay: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics with overlay, got %d", len(diags))
	}
}
