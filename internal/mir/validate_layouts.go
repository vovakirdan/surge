package mir

import (
	"fmt"

	"surge/internal/layout"
	"surge/internal/types"
)

func validateFinalizedLayouts(m *Module, typesIn *types.Interner) error {
	if m == nil {
		return nil
	}
	if m.Meta == nil || m.Meta.Layouts == nil {
		return fmt.Errorf("mir: missing finalized layout registry")
	}
	if typesIn == nil {
		return fmt.Errorf("mir: finalized layout validation requires a type interner")
	}
	target, ok := m.Meta.Layouts.Target()
	if !ok {
		return fmt.Errorf("mir: missing finalized layout target")
	}
	census, err := collectOperationRoots(m, typesIn, layout.New(target, typesIn))
	if err != nil {
		return err
	}
	for _, id := range census.Values() {
		physical, ok := m.Meta.Layouts.Lookup(id)
		if !ok {
			return fmt.Errorf("mir: finalized layout registry missing type#%d", id)
		}
		if _, ok := physical.Physical(); !ok {
			return fmt.Errorf("mir: finalized layout registry has unusable %s entry for type#%d", physical.State(), id)
		}
	}
	return nil
}
