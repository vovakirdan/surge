package sema

import (
	"testing"

	"surge/internal/source"
)

func TestLockOrderGraph_NoCycle(t *testing.T) {
	graph := NewLockOrderGraph()

	// A -> B -> C (no cycle)
	lockA := LockIdentity{TypeName: "Resource", FieldName: "lock_a"}
	lockB := LockIdentity{TypeName: "Resource", FieldName: "lock_b"}
	lockC := LockIdentity{TypeName: "Resource", FieldName: "lock_c"}

	span := source.Span{}
	graph.AddEdge(lockA, lockB, span)
	graph.AddEdge(lockB, lockC, span)

	cycles := graph.DetectCycles()
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %d", len(cycles))
	}
}

func TestLockOrderGraph_SimpleCycle(t *testing.T) {
	graph := NewLockOrderGraph()

	// A -> B -> A (cycle)
	lockA := LockIdentity{TypeName: "Resource", FieldName: "lock_a"}
	lockB := LockIdentity{TypeName: "Resource", FieldName: "lock_b"}

	span := source.Span{}
	graph.AddEdge(lockA, lockB, span)
	graph.AddEdge(lockB, lockA, span)

	cycles := graph.DetectCycles()
	if len(cycles) == 0 {
		t.Errorf("expected at least one cycle, got none")
	}
}

func TestLockOrderGraph_ThreeCycle(t *testing.T) {
	graph := NewLockOrderGraph()

	// A -> B -> C -> A (3-way cycle)
	lockA := LockIdentity{TypeName: "Resource", FieldName: "lock_a"}
	lockB := LockIdentity{TypeName: "Resource", FieldName: "lock_b"}
	lockC := LockIdentity{TypeName: "Resource", FieldName: "lock_c"}

	span := source.Span{}
	graph.AddEdge(lockA, lockB, span)
	graph.AddEdge(lockB, lockC, span)
	graph.AddEdge(lockC, lockA, span)

	cycles := graph.DetectCycles()
	if len(cycles) == 0 {
		t.Errorf("expected at least one cycle, got none")
	}
}

func TestLockOrderGraph_DisconnectedGraphs(t *testing.T) {
	graph := NewLockOrderGraph()

	// Two separate chains: A -> B and C -> D (no cycle)
	lockA := LockIdentity{TypeName: "Resource1", FieldName: "lock_a"}
	lockB := LockIdentity{TypeName: "Resource1", FieldName: "lock_b"}
	lockC := LockIdentity{TypeName: "Resource2", FieldName: "lock_c"}
	lockD := LockIdentity{TypeName: "Resource2", FieldName: "lock_d"}

	span := source.Span{}
	graph.AddEdge(lockA, lockB, span)
	graph.AddEdge(lockC, lockD, span)

	cycles := graph.DetectCycles()
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %d", len(cycles))
	}
}

func TestLockOrderGraph_SelfLoop(t *testing.T) {
	graph := NewLockOrderGraph()

	// A -> A (self loop - cycle)
	lockA := LockIdentity{TypeName: "Resource", FieldName: "lock_a"}

	span := source.Span{}
	graph.AddEdge(lockA, lockA, span)

	cycles := graph.DetectCycles()
	if len(cycles) == 0 {
		t.Errorf("expected at least one cycle for self loop, got none")
	}
}

func TestLockOrderGraph_DuplicateEdge(t *testing.T) {
	graph := NewLockOrderGraph()

	lockA := LockIdentity{TypeName: "Resource", FieldName: "lock_a"}
	lockB := LockIdentity{TypeName: "Resource", FieldName: "lock_b"}

	span := source.Span{}

	// First edge should be new
	isNew := graph.AddEdge(lockA, lockB, span)
	if !isNew {
		t.Errorf("expected first edge to be new")
	}

	// Same edge should not be new
	isNew = graph.AddEdge(lockA, lockB, span)
	if isNew {
		t.Errorf("expected duplicate edge to not be new")
	}
}
