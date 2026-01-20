package driver

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

func TestMergeModuleDiagnostics(t *testing.T) {
	rootBag := diag.NewBag(4)
	depBag := diag.NewBag(4)
	depBag.Add(&diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.UnknownCode,
		Message:  "dep error",
		Primary:  source.Span{File: 1, Start: 0, End: 1},
	})

	res := &DiagnoseResult{
		Bag: rootBag,
		moduleRecords: map[string]*moduleRecord{
			"root": {Bag: rootBag},
			"dep":  {Bag: depBag},
		},
	}

	if rootBag.HasErrors() {
		t.Fatalf("expected empty root bag before merge")
	}

	res.MergeModuleDiagnostics()

	if !rootBag.HasErrors() {
		t.Fatalf("expected merged diagnostics to include errors")
	}

	found := false
	for _, d := range rootBag.Items() {
		if d != nil && d.Message == "dep error" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dependency diagnostic to be merged into root bag")
	}
}
