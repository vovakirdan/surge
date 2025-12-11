package directive

import (
	"testing"

	"surge/internal/source"
)

func TestRegistry_Add(t *testing.T) {
	r := NewRegistry()

	r.Add(&Scenario{
		Namespace:  "test",
		Index:      0,
		ModulePath: "example",
		SourceFile: "test.sg",
		Span:       source.Span{Start: 0, End: 10},
	})

	if r.Len() != 1 {
		t.Errorf("expected 1 scenario, got %d", r.Len())
	}

	scenarios := r.All()
	if len(scenarios) != 1 {
		t.Errorf("expected 1 scenario in All(), got %d", len(scenarios))
	}
	if scenarios[0].Namespace != "test" {
		t.Errorf("expected namespace 'test', got %q", scenarios[0].Namespace)
	}
	if scenarios[0].FunctionName != "__directive_test_0__" {
		t.Errorf("expected function name '__directive_test_0__', got %q", scenarios[0].FunctionName)
	}
}

func TestRegistry_FilterByNamespace(t *testing.T) {
	r := NewRegistry()

	// Add scenarios of different namespaces
	r.Add(&Scenario{Namespace: "test", Index: 0, SourceFile: "a.sg"})
	r.Add(&Scenario{Namespace: "test", Index: 1, SourceFile: "a.sg"})
	r.Add(&Scenario{Namespace: "bench", Index: 0, SourceFile: "a.sg"})
	r.Add(&Scenario{Namespace: "example", Index: 0, SourceFile: "b.sg"})

	// Filter by test namespace
	testScenarios := r.FilterByNamespace([]string{"test"})
	if len(testScenarios) != 2 {
		t.Errorf("expected 2 test scenarios, got %d", len(testScenarios))
	}

	// Filter by bench namespace
	benchScenarios := r.FilterByNamespace([]string{"bench"})
	if len(benchScenarios) != 1 {
		t.Errorf("expected 1 bench scenario, got %d", len(benchScenarios))
	}

	// Filter by multiple namespaces
	multiFilter := r.FilterByNamespace([]string{"test", "bench"})
	if len(multiFilter) != 3 {
		t.Errorf("expected 3 scenarios for test+bench filter, got %d", len(multiFilter))
	}

	// Empty filter returns all
	allScenarios := r.FilterByNamespace(nil)
	if len(allScenarios) != 4 {
		t.Errorf("expected 4 scenarios with empty filter, got %d", len(allScenarios))
	}

	// Filter with non-existent namespace
	none := r.FilterByNamespace([]string{"nonexistent"})
	if len(none) != 0 {
		t.Errorf("expected 0 scenarios for nonexistent namespace, got %d", len(none))
	}
}

func TestRegistry_Len(t *testing.T) {
	r := NewRegistry()

	if r.Len() != 0 {
		t.Errorf("expected 0 for empty registry, got %d", r.Len())
	}

	r.Add(&Scenario{Namespace: "test", Index: 0})
	r.Add(&Scenario{Namespace: "test", Index: 1})

	if r.Len() != 2 {
		t.Errorf("expected 2 scenarios, got %d", r.Len())
	}
}

func TestScenario_GenerateFunctionName(t *testing.T) {
	tests := []struct {
		namespace string
		index     int
		expected  string
	}{
		{"test", 0, "__directive_test_0__"},
		{"test", 5, "__directive_test_5__"},
		{"bench", 0, "__directive_bench_0__"},
		{"example", 123, "__directive_example_123__"},
	}

	for _, tc := range tests {
		s := Scenario{Namespace: tc.namespace, Index: tc.index}
		result := s.GenerateFunctionName()
		if result != tc.expected {
			t.Errorf("GenerateFunctionName() for (%s, %d) = %q, want %q",
				tc.namespace, tc.index, result, tc.expected)
		}
	}
}
