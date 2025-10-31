package driver_test

import (
	"bytes"
	"testing"

	"surge/internal/driver"
	"surge/internal/project"
	"surge/internal/project/dag"
)

func digest(b byte) project.Digest {
	var d project.Digest
	for i := range d { d[i] = b }
	return d
}

func TestComputeModuleHashes_DeterministicAndTransitive(t *testing.T) {
	// Граф: A -> B, B -> C
	// ID: 0:A, 1:B, 2:C  (как будто BuildIndex уже сделал)
	g := dag.Graph{
		Edges: [][]dag.ModuleID{
			/*A*/ {1},
			/*B*/ {2},
			/*C*/ {},
		},
		Indeg:   []int{0,1,1},
		Present: []bool{true,true,true},
	}
	// Топосорт без циклов
	topo := &dag.Topo{
		Order:   []dag.ModuleID{0,1,2}, // порядок не так важен, считаем в обратном
		Batches: nil,
		Cyclic:  false,
	}

	// Слоты с content-хешами
	slots := []dag.ModuleSlot{
		{Meta: project.ModuleMeta{Path:"A", ContentHash: digest('A')}, Present:true},
		{Meta: project.ModuleMeta{Path:"B", ContentHash: digest('B')}, Present:true},
		{Meta: project.ModuleMeta{Path:"C", ContentHash: digest('C')}, Present:true},
	}

	driver.ComputeModuleHashes(dag.ModuleIndex{}, g, slots, topo)

	// Хеш C = H(C)
	if bytes.Equal(slots[2].Meta.ModuleHash[:], make([]byte,32)) {
		t.Fatal("C.ModuleHash should be non-zero")
	}
	// Хеш B = H(B || C)
	if bytes.Equal(slots[1].Meta.ModuleHash[:], slots[2].Meta.ModuleHash[:]) {
		t.Fatal("B.ModuleHash must differ from C")
	}
	// Хеш A = H(A || B)
	if bytes.Equal(slots[0].Meta.ModuleHash[:], slots[1].Meta.ModuleHash[:]) {
		t.Fatal("A.ModuleHash must differ from B")
	}

	prevA := slots[0].Meta.ModuleHash

	// Меняем «внука»: C
	slots[2].Meta.ContentHash = digest('X')
	driver.ComputeModuleHashes(dag.ModuleIndex{}, g, slots, topo)
	if bytes.Equal(slots[0].Meta.ModuleHash[:], prevA[:]) {
		t.Fatal("A.ModuleHash must change when transitive dep (C) changes")
	}
}

func TestComputeModuleHashes_CycleDoesNothing(t *testing.T) {
	g := dag.Graph{
		Edges:   [][]dag.ModuleID{{1},{0}},
		Indeg:   []int{1,1},
		Present: []bool{true,true},
	}
	topo := &dag.Topo{Cyclic:true}
	slots := []dag.ModuleSlot{
		{Meta: project.ModuleMeta{Path:"A", ContentHash: digest('A')}, Present:true},
		{Meta: project.ModuleMeta{Path:"B", ContentHash: digest('B')}, Present:true},
	}
	driver.ComputeModuleHashes(dag.ModuleIndex{}, g, slots, topo)
	if slots[0].Meta.ModuleHash != ([32]byte{}) || slots[1].Meta.ModuleHash != ([32]byte{}) {
		t.Fatal("hashes must stay zero on cycles")
	}
}
