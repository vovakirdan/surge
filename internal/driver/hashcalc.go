package driver

import (
	"crypto/sha256"

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

// ComputeModuleHashes вычисляет ModuleHash по обратному порядку топосортировки.
// Для циклического графа намеренно ничего не делает (оставляет нули).
func ComputeModuleHashes(idx dag.ModuleIndex, g dag.Graph, slots []dag.ModuleSlot, topo *dag.Topo) {
	if topo == nil || topo.Cyclic {
		return
	}
	for i := len(topo.Order) - 1; i >= 0; i-- {
		id := topo.Order[i]
		slot := &slots[int(id)]
		if !slot.Present {
			continue
		}
		deps := make([]project.Digest, 0, len(g.Edges[int(id)]))
		for _, to := range g.Edges[int(id)] {
			if !g.Present[int(to)] {
				continue
			}
			deps = append(deps, slots[int(to)].Meta.ModuleHash)
		}
		slot.Meta.ModuleHash = combineDigest(slot.Meta.ContentHash, deps...)
	}
}
