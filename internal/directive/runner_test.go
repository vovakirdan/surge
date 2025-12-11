package directive

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunner_Run_Empty(t *testing.T) {
	r := NewRegistry()
	var buf bytes.Buffer

	runner := NewRunner(r, RunnerConfig{
		Filter: []string{"test"},
		Output: &buf,
	})

	result := runner.Run()

	if result.Total != 0 {
		t.Errorf("expected 0 total, got %d", result.Total)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}
	if result.Passed != 0 {
		t.Errorf("expected 0 passed, got %d", result.Passed)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}

	// Should still print summary
	if !strings.Contains(buf.String(), "Directive execution summary") {
		t.Errorf("expected summary in output, got: %s", buf.String())
	}
}

func TestRunner_Run_WithScenarios(t *testing.T) {
	r := NewRegistry()
	r.Add(&Scenario{Namespace: "test", Index: 0, SourceFile: "example.sg"})
	r.Add(&Scenario{Namespace: "test", Index: 1, SourceFile: "example.sg"})

	var buf bytes.Buffer
	runner := NewRunner(r, RunnerConfig{
		Filter: []string{"test"},
		Output: &buf,
	})

	result := runner.Run()

	if result.Total != 2 {
		t.Errorf("expected 2 total, got %d", result.Total)
	}
	if result.Skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", result.Skipped)
	}

	output := buf.String()
	if !strings.Contains(output, "example.sg#0") {
		t.Errorf("expected example.sg#0 in output, got: %s", output)
	}
	if !strings.Contains(output, "example.sg#1") {
		t.Errorf("expected example.sg#1 in output, got: %s", output)
	}
	if !strings.Contains(output, "SKIPPED") {
		t.Errorf("expected SKIPPED in output, got: %s", output)
	}
	if !strings.Contains(output, "2 total, 2 skipped, 0 passed, 0 failed") {
		t.Errorf("expected correct summary, got: %s", output)
	}
}

func TestRunner_Run_FilteredNamespace(t *testing.T) {
	r := NewRegistry()
	r.Add(&Scenario{Namespace: "test", Index: 0, SourceFile: "a.sg"})
	r.Add(&Scenario{Namespace: "bench", Index: 0, SourceFile: "b.sg"})
	r.Add(&Scenario{Namespace: "test", Index: 1, SourceFile: "a.sg"})

	var buf bytes.Buffer
	runner := NewRunner(r, RunnerConfig{
		Filter: []string{"test"}, // Only run test namespace
		Output: &buf,
	})

	result := runner.Run()

	if result.Total != 2 {
		t.Errorf("expected 2 total (filtered), got %d", result.Total)
	}

	output := buf.String()
	if strings.Contains(output, "bench") {
		t.Errorf("bench scenarios should not appear in output: %s", output)
	}
}

func TestRunner_Run_NoFilter(t *testing.T) {
	r := NewRegistry()
	r.Add(&Scenario{Namespace: "test", Index: 0, SourceFile: "a.sg"})
	r.Add(&Scenario{Namespace: "bench", Index: 0, SourceFile: "b.sg"})

	var buf bytes.Buffer
	runner := NewRunner(r, RunnerConfig{
		Filter: nil, // No filter - run all
		Output: &buf,
	})

	result := runner.Run()

	if result.Total != 2 {
		t.Errorf("expected 2 total (all), got %d", result.Total)
	}

	output := buf.String()
	if !strings.Contains(output, "test") {
		t.Errorf("expected test in output: %s", output)
	}
	if !strings.Contains(output, "bench") {
		t.Errorf("expected bench in output: %s", output)
	}
}
