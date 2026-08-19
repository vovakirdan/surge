package vm

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/types"
)

// withElementLayouts gives a VM the frozen layout registry an array now needs.
//
// A dynamic array's elements are exact-layout storage, so the stride, the
// alignment and the cell kind all come from the registry. Under the universal
// element list a test could allocate an array with no layout information at
// all, because a Go slice of Values needs none; a run cannot, and refusing
// rather than guessing is the point. Every test that builds an array by hand
// therefore says which element types it will use.
func withElementLayouts(t *testing.T, interner *types.Interner, roots ...types.TypeID) *mir.Module {
	t.Helper()
	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, roots)
	if err != nil {
		t.Fatalf("freezing the element layouts must succeed: %v", err)
	}
	return &mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}}
}
