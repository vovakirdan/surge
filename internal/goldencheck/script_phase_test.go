package goldencheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenScriptFailsClosedForRequiredPhases(t *testing.T) {
	tests := []struct {
		name       string
		phase      string
		source     string
		wantOutput string
	}{
		{name: "diagnostics valid", phase: "diagnostics", source: "valid.sg", wantOutput: "diagnostics failed for valid case"},
		{name: "tokenize", phase: "tokenize", source: "valid.sg", wantOutput: "tokenize failed with status 1"},
		{name: "parse", phase: "parse", source: "valid.sg", wantOutput: "parse failed with status 1"},
		{name: "formatter valid", phase: "fmt", source: "valid.sg", wantOutput: "unlisted formatter failure"},
		{name: "HIR", phase: "hir", source: "hir/case.sg", wantOutput: "unlisted HIR failure with status 1"},
		{name: "HIR borrow", phase: "hir-borrow", source: "hir_borrow/case.sg", wantOutput: "unlisted HIR borrow failure with status 1"},
		{name: "instantiations", phase: "instantiations", source: "instantiations/case.sg", wantOutput: "unlisted instantiations failure with status 1"},
		{name: "monomorphization", phase: "mono", source: "mono/case.sg", wantOutput: "unlisted monomorphization failure with status 1"},
		{name: "MIR", phase: "mir", source: "mir/case.sg", wantOutput: "unlisted MIR failure with status 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScriptFixture(t, test.source)
			before, scanErr := Scan(filepath.Join(fixture.root, "testdata", "golden"))
			if scanErr != nil {
				t.Fatal(scanErr)
			}
			output, err := fixture.run(t, test.phase, nil, nil)
			if err == nil {
				t.Fatalf("script accepted failed phase %q\n%s", test.phase, output)
			}
			if !strings.Contains(output, test.wantOutput) {
				t.Fatalf("output = %q, want %q", output, test.wantOutput)
			}
			after, scanErr := Scan(filepath.Join(fixture.root, "testdata", "golden"))
			if scanErr != nil {
				t.Fatal(scanErr)
			}
			if changes := Diff(before, after); len(changes) != 0 {
				t.Fatalf("failed phase changed live corpus: %#v", changes)
			}
			assertNoGoldenStaging(t, fixture.root)
		})
	}
}

func TestGoldenScriptAcceptsCleanRequiredPhases(t *testing.T) {
	fixture := newScriptFixture(t, "hir/case.sg")
	if output, err := fixture.run(t, "", nil, nil); err != nil {
		t.Fatalf("clean script failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "testdata", "golden", "hir", "case.diag")); err != nil {
		t.Fatalf("successful generation did not install reviewable output: %v", err)
	}
	assertNoGoldenStaging(t, fixture.root)
}

func assertNoGoldenStaging(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "testdata", ".golden-update.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("staging paths remain: %v", matches)
	}
}
