package driver

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"surge/internal/diag"
)

type layoutDiagnosticSnapshot struct {
	Code    diag.Code
	Message string
	Start   uint32
}

func TestDiagnoseStagesShareDeterministicLayoutDiagnostics(t *testing.T) {
	sourceCode := `@align(8589934592)
type TooAligned = { value: bool };

type Node = { next: Node };
`
	path := filepath.Join(t.TempDir(), "layout.sg")
	if err := os.WriteFile(path, []byte(sourceCode), 0o600); err != nil {
		t.Fatal(err)
	}

	var want []layoutDiagnosticSnapshot
	for _, stage := range []DiagnoseStage{DiagnoseStageSema, DiagnoseStageAll} {
		result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{
			Stage:          stage,
			MaxDiagnostics: 16,
		})
		if err != nil {
			t.Fatalf("DiagnoseWithOptions(%v): %v", stage, err)
		}
		var got []layoutDiagnosticSnapshot
		for _, item := range result.Bag.Items() {
			switch item.Code {
			case diag.SemaRecursiveUnsized, diag.SemaLayoutOverflow,
				diag.SemaLayoutUnsupportedAlignment, diag.SemaLayoutDeferred:
				got = append(got, layoutDiagnosticSnapshot{
					Code: item.Code, Message: item.Message, Start: item.Primary.Start,
				})
			}
		}
		if len(got) != 2 {
			t.Fatalf("stage %v layout diagnostics = %+v", stage, got)
		}
		if want == nil {
			want = got
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("stage %v diagnostics = %+v, want %+v", stage, got, want)
		}
	}
}
