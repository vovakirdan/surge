package directive

import (
	"fmt"

	"surge/internal/source"
)

// Scenario represents a single directive block that can be executed.
// Each directive block in source code (e.g., /// test: ... ) becomes one scenario.
type Scenario struct {
	// Namespace is the directive module name (e.g., "test", "benchmark")
	Namespace string

	// Index is the sequential number of this scenario within its source file.
	// Used to generate unique function names: __directive_test_0__, __directive_test_1__, etc.
	Index int

	// ModulePath is the normalized path of the module containing this directive.
	ModulePath string

	// SourceFile is the path to the source file containing this directive.
	SourceFile string

	// Span is the source location of the directive block.
	Span source.Span

	// FunctionName is the generated function name for execution.
	// Format: __directive_<namespace>_<index>__
	FunctionName string
}

// GenerateFunctionName creates the canonical function name for this scenario.
func (s *Scenario) GenerateFunctionName() string {
	return fmt.Sprintf("__directive_%s_%d__", s.Namespace, s.Index)
}
