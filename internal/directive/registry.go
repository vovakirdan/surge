package directive

import (
	"sync"

	"surge/internal/ast"
)

// Registry collects all directive scenarios found during compilation.
// It is populated after semantic analysis and used by the runner.
type Registry struct {
	mu          sync.Mutex
	scenarios   []Scenario
	byNamespace map[string][]int // namespace -> indices into scenarios slice
}

// NewRegistry creates an empty directive registry.
func NewRegistry() *Registry {
	return &Registry{
		scenarios:   make([]Scenario, 0),
		byNamespace: make(map[string][]int),
	}
}

// Add registers a new directive scenario.
func (r *Registry) Add(scenario *Scenario) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := len(r.scenarios)
	scenario.FunctionName = scenario.GenerateFunctionName()
	r.scenarios = append(r.scenarios, *scenario)
	r.byNamespace[scenario.Namespace] = append(r.byNamespace[scenario.Namespace], idx)
}

// All returns all registered scenarios.
func (r *Registry) All() []Scenario {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Scenario(nil), r.scenarios...)
}

// FilterByNamespace returns scenarios matching any of the given namespaces.
// If namespaces is empty, returns all scenarios.
func (r *Registry) FilterByNamespace(namespaces []string) []Scenario {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(namespaces) == 0 {
		return append([]Scenario(nil), r.scenarios...)
	}

	// Build set of allowed namespaces
	allowed := make(map[string]bool)
	for _, ns := range namespaces {
		allowed[ns] = true
	}

	var result []Scenario
	for _, s := range r.scenarios {
		if allowed[s.Namespace] {
			result = append(result, s)
		}
	}
	return result
}

// Len returns the total number of scenarios.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.scenarios)
}

// CollectFromFile extracts directive scenarios from a parsed AST file.
func (r *Registry) CollectFromFile(builder *ast.Builder, fileID ast.FileID, modulePath, sourceFile string) {
	file := builder.Files.Get(fileID)
	if file == nil || len(file.Directives) == 0 {
		return
	}

	// Track index per namespace within this file
	namespaceIndex := make(map[string]int)

	for _, block := range file.Directives {
		namespace := builder.StringsInterner.MustLookup(block.Namespace)

		idx := namespaceIndex[namespace]
		namespaceIndex[namespace]++

		r.Add(&Scenario{
			Namespace:  namespace,
			Index:      idx,
			ModulePath: modulePath,
			SourceFile: sourceFile,
			Span:       block.Span,
		})
	}
}
