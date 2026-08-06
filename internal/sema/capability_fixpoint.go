package sema

import (
	"fmt"
	"slices"

	"surge/internal/types"
)

// capabilityNode is one type's settled answer on the four structural axes.
type capabilityNode struct {
	droppable     bool
	traceable     bool
	shardMovable  bool
	crossClonable bool

	droppableReason  string
	traceableReason  string
	shardReason      string
	crossCloneReason string

	// shardRefused and crossRefused name the component that settled a refusal,
	// or NoTypeID when this type refused on its own account. Following them
	// rebuilds the path from the type asked about down to the one at fault.
	shardRefused types.TypeID
	crossRefused types.TypeID
}

func (n *capabilityNode) verdicts() [4]bool {
	return [4]bool{n.droppable, n.traceable, n.shardMovable, n.crossClonable}
}

func (n *capabilityNode) shardRefusal() types.TypeID { return n.shardRefused }
func (n *capabilityNode) crossRefusal() types.TypeID { return n.crossRefused }

// capabilityLookup answers with a component's current node — settled if an
// earlier group decided it, and the group's own working value while a group is
// still iterating.
type capabilityLookup func(types.TypeID) *capabilityNode

// capabilityDepthLimit bounds the component recursion. It is not a correctness
// device — cycles are handled by the strongly-connected-component pass below —
// but a nesting chain long enough to exhaust the stack is a refusal, not a
// crash.
const capabilityDepthLimit = 4096

// capabilityWalk is the state of one strongly-connected-component pass
// (Tarjan). Groups come out in reverse topological order, so by the time a
// group is settled every group it can reach already is.
type capabilityWalk struct {
	index   map[types.TypeID]int
	lowlink map[types.TypeID]int
	onStack map[types.TypeID]bool
	stack   []types.TypeID
	next    int
	depth   int
}

func (c *CapabilityClassifier) solve(root types.TypeID) error {
	if _, done := c.solved[root]; done {
		return nil
	}
	return c.visit(root, &capabilityWalk{
		index:   make(map[types.TypeID]int),
		lowlink: make(map[types.TypeID]int),
		onStack: make(map[types.TypeID]bool),
	})
}

func (c *CapabilityClassifier) visit(id types.TypeID, walk *capabilityWalk) error {
	walk.depth++
	defer func() { walk.depth-- }()
	if walk.depth > capabilityDepthLimit {
		return fmt.Errorf(
			"capability classification gave up after %d nested components, reaching type %d",
			capabilityDepthLimit, uint32(id),
		)
	}
	walk.index[id] = walk.next
	walk.lowlink[id] = walk.next
	walk.next++
	walk.stack = append(walk.stack, id)
	walk.onStack[id] = true

	for _, component := range c.components(id) {
		if _, settled := c.solved[component]; settled {
			continue
		}
		if _, seen := walk.index[component]; !seen {
			if err := c.visit(component, walk); err != nil {
				return err
			}
			walk.lowlink[id] = min(walk.lowlink[id], walk.lowlink[component])
			continue
		}
		if walk.onStack[component] {
			walk.lowlink[id] = min(walk.lowlink[id], walk.index[component])
		}
	}

	if walk.lowlink[id] != walk.index[id] {
		return nil
	}
	var group []types.TypeID
	for {
		member := walk.stack[len(walk.stack)-1]
		walk.stack = walk.stack[:len(walk.stack)-1]
		walk.onStack[member] = false
		group = append(group, member)
		if member == id {
			break
		}
	}
	return c.settle(group)
}

// settle runs the fixpoint over one strongly connected group.
//
// A group of more than one member is a cycle, and cycles here are ordinary
// rather than exotic: a runtime handle's payloads are components of the handle,
// so `type Tree = { kids: Array<Tree> }` reaches itself. Plain memoized
// recursion has no answer to give such a group, so each axis is seeded at its
// own extreme and iterated:
//
//   - ShardMovable seeds TRUE — the greatest fixpoint. This is parity with the
//     attribute validator's coinductive `if visiting[resolved] { return true }`:
//     a marked type must not become immovable merely for containing itself.
//   - CarrierDroppable, Traceable and CrossClonable seed FALSE — least
//     fixpoints. A cycle alone must not manufacture a drop obligation, a trace
//     root, or a crossing duplicate; each has to be earned by a real component.
func (c *CapabilityClassifier) settle(group []types.TypeID) error {
	slices.Sort(group)
	working := make(map[types.TypeID]*capabilityNode, len(group))
	for _, id := range group {
		working[id] = &capabilityNode{shardMovable: true}
	}
	at := func(id types.TypeID) *capabilityNode {
		if settled, ok := c.solved[id]; ok {
			return settled
		}
		return working[id]
	}
	// Each axis moves in one direction only over a finite set of booleans, so
	// one round per member is the worst case and the extra round is the one
	// that observes nothing changing.
	for round := 0; round <= len(group); round++ {
		changed := false
		for _, id := range group {
			next := c.evaluate(id, at)
			if next.verdicts() != working[id].verdicts() {
				changed = true
			}
			working[id] = next
		}
		if changed {
			continue
		}
		for _, id := range group {
			c.solved[id] = working[id]
		}
		return nil
	}
	return fmt.Errorf(
		"capability classification did not settle across %d mutually recursive types, starting at type %d",
		len(group), uint32(group[0]),
	)
}

func (c *CapabilityClassifier) evaluate(id types.TypeID, at capabilityLookup) *capabilityNode {
	var node capabilityNode
	node.droppable, node.droppableReason = c.evaluateDroppable(id, at)
	node.traceable, node.traceableReason = c.evaluateTraceable(id, at)
	node.shardMovable, node.shardReason, node.shardRefused = c.evaluateShardMovable(id, at)
	node.crossClonable, node.crossCloneReason, node.crossRefused = c.evaluateCrossClonable(id, at)
	return &node
}

func (n *capabilityNode) isDroppable() bool     { return n.droppable }
func (n *capabilityNode) isTraceable() bool     { return n.traceable }
func (n *capabilityNode) isShardMovable() bool  { return n.shardMovable }
func (n *capabilityNode) isCrossClonable() bool { return n.crossClonable }

// firstComponent names the first component answering true, in declaration
// order, so an "it holds one" reason always points at the same member.
func (c *CapabilityClassifier) firstComponent(
	id types.TypeID,
	at capabilityLookup,
	holds func(*capabilityNode) bool,
) (culprit types.TypeID, found bool) {
	for _, component := range c.components(id) {
		if holds(at(component)) {
			return component, true
		}
	}
	return types.NoTypeID, false
}

// allComponents requires every component to answer true and names the first
// one that does not.
func (c *CapabilityClassifier) allComponents(
	id types.TypeID,
	at capabilityLookup,
	holds func(*capabilityNode) bool,
	allReason, refusedReason string,
) (verdict bool, reason string, refused types.TypeID) {
	for _, component := range c.components(id) {
		if holds(at(component)) {
			continue
		}
		return false, fmt.Sprintf(refusedReason, c.label(component)), component
	}
	return true, allReason, types.NoTypeID
}

// The `own` marker says who holds a value, not what the value is, so every axis
// forwards through it unchanged.
func (c *CapabilityClassifier) forwardDroppable(elem types.TypeID, at capabilityLookup) (verdict bool, reason string) {
	node := at(c.resolve(elem))
	return node.droppable, node.droppableReason
}

func (c *CapabilityClassifier) forwardTraceable(elem types.TypeID, at capabilityLookup) (verdict bool, reason string) {
	node := at(c.resolve(elem))
	return node.traceable, node.traceableReason
}

func (c *CapabilityClassifier) forwardShardMovable(
	elem types.TypeID,
	at capabilityLookup,
) (verdict bool, reason string, refused types.TypeID) {
	resolved := c.resolve(elem)
	node := at(resolved)
	if node.shardMovable {
		return true, node.shardReason, types.NoTypeID
	}
	return false, node.shardReason, resolved
}

func (c *CapabilityClassifier) forwardCrossClonable(
	elem types.TypeID,
	at capabilityLookup,
) (verdict bool, reason string, refused types.TypeID) {
	resolved := c.resolve(elem)
	node := at(resolved)
	if node.crossClonable {
		return true, node.crossCloneReason, types.NoTypeID
	}
	return false, node.crossCloneReason, resolved
}
