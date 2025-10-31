package dag

import (
	"fmt"
	"slices"

	"fortio.org/safecast"
	"surge/internal/project"
)

type Topo struct {
	Order   []ModuleID   // линейный порядок (только реальные модули)
	Batches [][]ModuleID // волны независимых модулей
	Cyclic  bool
	Cycles  []ModuleID // узлы, оставшиеся в цикле
}

func ToposortKahn(g Graph) *Topo {
	nodeCount := len(g.Edges)
	indeg := make([]int, len(g.Indeg))
	copy(indeg, g.Indeg)

	topo := &Topo{
		Order:   make([]ModuleID, 0, nodeCount),
		Batches: make([][]ModuleID, 0),
	}

	active := 0
	for i := range nodeCount {
		if g.Present[i] {
			active++
		}
	}

	current := make([]ModuleID, 0, nodeCount)
	for i := range nodeCount {
		if !g.Present[i] {
			continue
		}
		if indeg[i] == 0 {
			mID, err := safecast.Conv[ModuleID](i)
			if err != nil {
				panic(fmt.Errorf("module id overflow: %w", err))
			}
			current = append(current, mID)
		}
	}
	slices.Sort(current)

	visited := 0
	for len(current) > 0 {
		batch := make([]ModuleID, len(current))
		copy(batch, current)
		topo.Batches = append(topo.Batches, batch)

		next := make([]ModuleID, 0)
		for _, id := range batch {
			topo.Order = append(topo.Order, id)
			visited++
			for _, to := range g.Edges[int(id)] {
				if !g.Present[int(to)] {
					continue
				}
				indeg[int(to)]--
				if indeg[int(to)] == 0 {
					next = append(next, to)
				}
			}
		}
		slices.Sort(next)
		current = next
	}

	if visited != active {
		topo.Cyclic = true
		for i := range nodeCount {
			if !g.Present[i] {
				continue
			}
			if indeg[i] > 0 {
				mID, err := safecast.Conv[ModuleID](i)
				if err != nil {
					panic(fmt.Errorf("module id overflow: %w", err))
				}
				topo.Cycles = append(topo.Cycles, mID)
			}
		}
		slices.Sort(topo.Cycles)
	}

	return topo
}

// ComputeModuleHashes вычисляет ModuleHash для каждого присутствующего узла:
// H( content || dep1 || dep2 ... ), где dep* — уже посчитанные хеши зависимостей.
// Использует topo.Order, поэтому корректен только при ацикличном графе.
// При цикле — хеши для узлов из цикла остаются нулями.
func ComputeModuleHashes(idx ModuleIndex, g Graph, slots []ModuleSlot, topo *Topo) {
    if topo == nil || topo.Cyclic {
        return // для циклических — намеренно пропускаем (можно позже добавить стабилизацию)
    }
    // Идём по порядку Kahn: родители всегда раньше детей НЕ гарантируется,
    // поэтому считаем "снизу-вверх": от узлов с нулевой out-degree вверх?
    // Проще: у нас Edges[from] = deps (to). Тогда корректно идти В ОБРАТНОМ порядке topo.Order.
    // На момент узла все его deps уже обработаны.
    for i := len(topo.Order) - 1; i >= 0; i-- {
        id := topo.Order[i]
        slot := &slots[int(id)]
        if !slot.Present {
            continue
        }
        // Собираем хеши зависимостей в детерминированном порядке (Edges уже отсортированы).
        deps := make([]project.Digest, 0, len(g.Edges[int(id)]))
        for _, to := range g.Edges[int(id)] {
            if !g.Present[int(to)] {
                continue
            }
            deps = append(deps, slots[int(to)].Meta.ModuleHash)
        }
        slot.Meta.ModuleHash = project.Combine(slot.Meta.ContentHash, deps...)
    }
}
