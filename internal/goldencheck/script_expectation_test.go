package goldencheck

import (
	"os"
	"strings"
	"testing"
)

func TestGoldenScriptDiagnosticExpectationContract(t *testing.T) {
	tests := []struct {
		name        string
		failPhases  string
		expectZero  bool
		wantFailure string
	}{
		{name: "default invalid diagnostic failure", failPhases: "diagnostics"},
		{name: "listed zero status", expectZero: true},
		{name: "unlisted zero status", wantFailure: "invalid diagnostics status 0, want 1"},
		{name: "listed zero becomes failure", failPhases: "diagnostics", expectZero: true, wantFailure: "expected zero-status diagnostics became failing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScriptFixture(t, "invalid/case.sg")
			var expected []string
			if test.expectZero {
				expected = []string{fixture.sourceRel}
			}
			output, err := fixture.run(t, test.failPhases, expected, nil)
			if test.wantFailure == "" {
				if err != nil {
					t.Fatalf("expected outcome failed: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, test.wantFailure) {
				t.Fatalf("error = %v, output = %q, want %q", err, output, test.wantFailure)
			}
		})
	}
}

func TestGoldenScriptFormatterExpectationContract(t *testing.T) {
	tests := []struct {
		name        string
		failPhases  string
		expectFail  bool
		wantFailure string
	}{
		{name: "listed failure", failPhases: "diagnostics,fmt", expectFail: true},
		{name: "unlisted failure", failPhases: "diagnostics,fmt", wantFailure: "unlisted formatter failure"},
		{name: "listed failure becomes success", failPhases: "diagnostics", expectFail: true, wantFailure: "expected formatter failure became successful"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScriptFixture(t, "invalid/case.sg")
			var expected []string
			if test.expectFail {
				expected = []string{fixture.sourceRel}
			}
			output, err := fixture.run(t, test.failPhases, nil, expected)
			if test.wantFailure == "" {
				if err != nil {
					t.Fatalf("expected formatter failure rejected: %v\n%s", err, output)
				}
				data, readErr := os.ReadFile(strings.TrimSuffix(fixture.sourcePath, ".sg") + ".fmt")
				if readErr != nil || string(data) != "fn main() {}\n" {
					t.Fatalf("formatter fallback = %q, %v", data, readErr)
				}
				return
			}
			if err == nil || !strings.Contains(output, test.wantFailure) {
				t.Fatalf("error = %v, output = %q, want %q", err, output, test.wantFailure)
			}
		})
	}
}

func TestGoldenScriptRejectsUnseenExpectations(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	emit := []PhaseExpectation{{Phase: "hir_borrow", Path: "invalid/missing.sg"}}
	output, err := fixture.runWithEmit(t, "", []string{"invalid/missing.sg"}, []string{"invalid/missing.sg"}, emit)
	if err == nil || !strings.Contains(output, "stale zero-status diagnostic expectation") || !strings.Contains(output, "stale formatter-failure expectation") || !strings.Contains(output, "stale emit-failure expectation") {
		t.Fatalf("error = %v, output = %q", err, output)
	}
}

func TestGoldenScriptEmitExpectationContract(t *testing.T) {
	tests := []struct {
		name        string
		failPhases  string
		expectFail  bool
		wantFailure string
	}{
		{name: "listed failure", failPhases: "diagnostics,hir-borrow", expectFail: true},
		{name: "unlisted failure", failPhases: "diagnostics,hir-borrow", wantFailure: "unlisted HIR borrow failure"},
		{name: "listed failure becomes success", failPhases: "diagnostics", expectFail: true, wantFailure: "expected HIR borrow failure became successful"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScriptFixture(t, "hir_borrow/invalid/case.sg")
			var expected []PhaseExpectation
			if test.expectFail {
				expected = []PhaseExpectation{{Phase: "hir_borrow", Path: fixture.sourceRel}}
			}
			output, err := fixture.runWithEmit(t, test.failPhases, nil, nil, expected)
			if test.wantFailure == "" {
				if err != nil {
					t.Fatalf("expected emit failure rejected: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, test.wantFailure) {
				t.Fatalf("error = %v, output = %q, want %q", err, output, test.wantFailure)
			}
		})
	}
}
