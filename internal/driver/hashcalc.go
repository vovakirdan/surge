package driver

import (
	"crypto/sha256"
	"sync"

	"surge/internal/project"
	"surge/internal/project/dag"
)

// combineDigest: H(content || dep1 || dep2 ...). deps уже в детерминированном порядке.
func combineDigest(content project.Digest, deps ...project.Digest) project.Digest {
	h := sha256.New()
	_, _ = h.Write(content[:])
	for _, d := range deps {
		_, _ = h.Write(d[:])
	}
	var out project.Digest
	copy(out[:], h.Sum(nil))
	return out
}

// ComputeModuleHashes вычисляет ModuleHash используя batch parallelism.
// Если Batches доступны, обрабатывает батчи в обратном порядке (зависимости сначала),
// и модули внутри каждого батча вычисляются параллельно.
// Fallback: если Batches пусты, использует sequential Order (для совместимости с тестами).
// Для циклического графа намеренно ничего не делает (оставляет нули).
func ComputeModuleHashes(_ dag.ModuleIndex, g dag.Graph, slots []dag.ModuleSlot, topo *dag.Topo) {
	if topo == nil || topo.Cyclic {
		return
	}

	// Use batch parallelism if Batches are available
	if len(topo.Batches) > 0 {
		// Process batches in reverse order (dependencies first)
		for i := len(topo.Batches) - 1; i >= 0; i-- {
			batch := topo.Batches[i]

			// Process modules in this batch in parallel
			var wg sync.WaitGroup
			for _, id := range batch {
				wg.Add(1)
				go func(id dag.ModuleID) {
					defer wg.Done()

					slot := &slots[int(id)]
					if !slot.Present {
						return
					}

					if slot.Meta == nil {
						return
					}

					// Collect dependency hashes
					deps := make([]project.Digest, 0, len(g.Edges[int(id)]))
					for _, to := range g.Edges[int(id)] {
						if !g.Present[int(to)] {
							continue
						}
						if slots[int(to)].Meta == nil {
							continue
						}
						deps = append(deps, slots[int(to)].Meta.ModuleHash)
					}

					// Compute and store hash
					slot.Meta.ModuleHash = combineDigest(slot.Meta.ContentHash, deps...)
				}(id)
			}

			// Wait for all modules in this batch to complete
			wg.Wait()
		}
	} else {
		// Fallback to sequential processing using Order (for tests/compatibility)
		for i := len(topo.Order) - 1; i >= 0; i-- {
			id := topo.Order[i]
			slot := &slots[int(id)]
			if !slot.Present {
				continue
			}
			if slot.Meta == nil {
				continue
			}

			deps := make([]project.Digest, 0, len(g.Edges[int(id)]))
			for _, to := range g.Edges[int(id)] {
				if !g.Present[int(to)] {
					continue
				}
				if slots[int(to)].Meta == nil {
					continue
				}
				deps = append(deps, slots[int(to)].Meta.ModuleHash)
			}

			slot.Meta.ModuleHash = combineDigest(slot.Meta.ContentHash, deps...)
		}
	}
}
