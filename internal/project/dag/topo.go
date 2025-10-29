package dag

import (
	"fmt"
	"slices"

	"fortio.org/safecast"
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
