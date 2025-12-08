package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// LockIdentity uniquely identifies a lock for global ordering analysis.
// This is used to track lock acquisition order across different functions.
type LockIdentity struct {
	TypeName  string // Owner type (e.g., "Resource")
	FieldName string // Lock field (e.g., "lock_a")
}

// LockEdge represents an edge in the lock order graph.
// An edge from A to B means "lock A was held when lock B was acquired".
type LockEdge struct {
	Span source.Span // Location where the ordering was established
}

// LockOrderGraph tracks lock acquisition ordering across all functions.
// Used for deadlock detection via cycle finding.
type LockOrderGraph struct {
	// edges[A][B] = span means "acquired B while holding A" at span
	edges map[LockIdentity]map[LockIdentity]LockEdge
	// All known locks
	locks map[LockIdentity]struct{}
}

// LockCycle represents a detected deadlock cycle.
type LockCycle struct {
	Path  []LockIdentity // Locks in the cycle (A -> B -> C -> A)
	Spans []source.Span  // Where each edge was established
}

// NewLockOrderGraph creates a new empty lock order graph.
func NewLockOrderGraph() *LockOrderGraph {
	return &LockOrderGraph{
		edges: make(map[LockIdentity]map[LockIdentity]LockEdge),
		locks: make(map[LockIdentity]struct{}),
	}
}

// AddLock registers a lock identity in the graph.
func (g *LockOrderGraph) AddLock(lock LockIdentity) {
	g.locks[lock] = struct{}{}
}

// AddEdge records that lockTo was acquired while lockFrom was held.
// Returns true if this is a new edge, false if it already existed.
func (g *LockOrderGraph) AddEdge(lockFrom, lockTo LockIdentity, span source.Span) bool {
	g.AddLock(lockFrom)
	g.AddLock(lockTo)

	if g.edges[lockFrom] == nil {
		g.edges[lockFrom] = make(map[LockIdentity]LockEdge)
	}

	if _, exists := g.edges[lockFrom][lockTo]; exists {
		return false // Edge already exists
	}

	g.edges[lockFrom][lockTo] = LockEdge{Span: span}
	return true
}

// DetectCycles finds all cycles in the lock order graph using DFS.
// Each cycle represents a potential deadlock.
func (g *LockOrderGraph) DetectCycles() []LockCycle {
	// State for DFS: 0=unvisited, 1=in-path, 2=done
	state := make(map[LockIdentity]int)
	parent := make(map[LockIdentity]LockIdentity)
	var cycles []LockCycle

	var dfs func(node LockIdentity)
	dfs = func(node LockIdentity) {
		state[node] = 1 // Mark as in-path

		for neighbor := range g.edges[node] {
			if state[neighbor] == 1 {
				// Found a back edge -> cycle detected
				cycle := g.reconstructCycle(node, neighbor, parent)
				cycles = append(cycles, cycle)
			} else if state[neighbor] == 0 {
				parent[neighbor] = node
				dfs(neighbor)
			}
		}

		state[node] = 2 // Mark as done
	}

	// Run DFS from each unvisited node
	for lock := range g.locks {
		if state[lock] == 0 {
			dfs(lock)
		}
	}

	return cycles
}

// reconstructCycle builds a cycle from the DFS state.
// from -> ... -> to -> from (to is already in the path when we find the back edge to->from... actually wait,
// we found edge node->neighbor where neighbor is in-path, meaning we go from neighbor back to node)
func (g *LockOrderGraph) reconstructCycle(node, backTo LockIdentity, parent map[LockIdentity]LockIdentity) LockCycle {
	var path []LockIdentity
	var spans []source.Span

	// Build path from backTo to node (following parent pointers)
	current := node
	for current != backTo {
		path = append([]LockIdentity{current}, path...)
		next, ok := parent[current]
		if !ok {
			break
		}
		// Get span for edge next -> current
		if edge, exists := g.edges[next][current]; exists {
			spans = append([]source.Span{edge.Span}, spans...)
		}
		current = next
	}

	// Add backTo at the beginning
	path = append([]LockIdentity{backTo}, path...)

	// Add the back edge span (node -> backTo)
	if edge, exists := g.edges[node][backTo]; exists {
		spans = append(spans, edge.Span)
	}

	return LockCycle{Path: path, Spans: spans}
}

// checkForDeadlocks analyzes the lock order graph and reports any cycles.
func (tc *typeChecker) checkForDeadlocks() {
	if tc.lockOrderGraph == nil {
		return
	}

	cycles := tc.lockOrderGraph.DetectCycles()
	for _, cycle := range cycles {
		if len(cycle.Path) < 2 {
			continue
		}

		// Report at the location of the last edge (completing the cycle)
		var reportSpan source.Span
		if len(cycle.Spans) > 0 {
			reportSpan = cycle.Spans[len(cycle.Spans)-1]
		}

		// Build cycle description: A -> B -> C -> A
		desc := ""
		for i, lock := range cycle.Path {
			if i > 0 {
				desc += " -> "
			}
			if lock.TypeName != "" {
				desc += lock.TypeName + "."
			}
			desc += lock.FieldName
		}
		// Close the cycle
		if len(cycle.Path) > 0 {
			first := cycle.Path[0]
			desc += " -> "
			if first.TypeName != "" {
				desc += first.TypeName + "."
			}
			desc += first.FieldName
		}

		tc.report(diag.SemaLockPotentialDeadlock, reportSpan,
			"potential deadlock: lock acquisition order cycle: %s", desc)
	}
}

// recordLockOrderEdge records a lock ordering edge when acquiring a new lock.
// Called during lock analysis when a lock is acquired while other locks are held.
func (tc *typeChecker) recordLockOrderEdge(la *lockAnalyzer, newLock LockIdentity, span source.Span) {
	if tc.lockOrderGraph == nil || la == nil {
		return
	}

	// Record edges from all currently held locks to the new lock
	for _, held := range la.state.HeldLocks() {
		heldLock := LockIdentity{
			TypeName:  tc.getLockTypeName(la, held.Key),
			FieldName: tc.lookupName(held.Key.FieldName),
		}

		// Skip self-edges and edges with empty field names
		if heldLock == newLock || heldLock.FieldName == "" || newLock.FieldName == "" {
			continue
		}

		tc.lockOrderGraph.AddEdge(heldLock, newLock, span)
	}
}

// getLockTypeName gets the type name for a lock key.
// Uses the receiver type name stored in the lock analyzer.
func (tc *typeChecker) getLockTypeName(la *lockAnalyzer, key LockKey) string {
	// For field locks on self, use the receiver type
	if la != nil && la.selfSym.IsValid() && key.Base == la.selfSym {
		return la.receiverTypeName
	}
	// For local variable locks, return empty (they're unique per function)
	return ""
}

// getSymbolTypeName gets the base type name for a symbol.
// Used to get the receiver type name for lock ordering.
func (tc *typeChecker) getSymbolTypeName(symID symbols.SymbolID) string {
	if !symID.IsValid() || tc.symbols == nil || tc.symbols.Table == nil {
		return ""
	}

	// Look up the symbol's type from bindingTypes (set during parameter analysis)
	typeID, ok := tc.bindingTypes[symID]
	if !ok || typeID == 0 {
		return ""
	}

	return tc.baseTypeName(typeID)
}
