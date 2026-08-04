package buildpipeline

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/mir"
)

func TestCompilePublishesAndFullyValidatesLayoutRegistry(t *testing.T) {
	path := writeAnalysisSource(t, `
type Pair = { left: bool, right: uint64 };
fn helper(value: Pair) -> Pair { return value; }
`)
	result, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if result.MIR == nil || result.MIR.Meta == nil || result.MIR.Meta.Layouts == nil {
		t.Fatal("production compile returned MIR without a finalized layout registry")
	}
	if err := mir.Validate(result.MIR, result.Diagnose.Sema.TypeInterner); err != nil {
		t.Fatalf("full production validation: %v", err)
	}
}

func TestCompileReportsLayoutDiagnosticBeforeMIRBackstop(t *testing.T) {
	path := writeAnalysisSource(t, `
@align(8589934592)
type TooAligned = { value: bool };
`)
	result, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	})
	if err == nil {
		t.Fatal("Compile accepted an unsupported physical alignment")
	}
	if result.MIR != nil {
		t.Fatal("compile reached MIR after a post-sema layout error")
	}
	if strings.Contains(err.Error(), "type#") {
		t.Fatalf("compile leaked an internal TypeID: %v", err)
	}
	found := false
	for _, item := range result.Diagnose.Bag.Items() {
		if item.Code == diag.SemaLayoutUnsupportedAlignment {
			found = true
			if strings.Contains(item.Message, "type#") {
				t.Fatalf("diagnostic leaked an internal TypeID: %s", item.Message)
			}
		}
	}
	if !found {
		t.Fatalf("missing layout diagnostic: %+v", result.Diagnose.Bag.Items())
	}
}
