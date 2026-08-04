package layout

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"surge/internal/types"
)

// Registry is a frozen, per-module set of owned physical layouts. It retains
// neither the mutable builder nor the type interner and is safe for concurrent
// reads after construction.
type Registry struct {
	target  Target
	entries map[types.TypeID]PhysicalLayout
	aliases map[types.TypeID]types.TypeID
	// aliasOrder preserves every exact root in deterministic first-seen order.
	// It is part of the observable frozen state because exact-ID Lookup uses it.
	aliasOrder []types.TypeID
	order      []types.TypeID
}

// FinalizeRegistry computes roots in their deterministic first-seen order and
// freezes owned copies. Final MIR may contain no Deferred or Error entry.
func FinalizeRegistry(builder *LayoutEngine, roots []types.TypeID) (*Registry, error) {
	if builder == nil {
		return nil, &LayoutError{Kind: ErrInvalidTarget, Operation: "nil layout builder"}
	}
	if err := validateTarget(builder.Target, types.NoTypeID); err != nil {
		return nil, err
	}
	registry := &Registry{
		target:     builder.Target,
		entries:    make(map[types.TypeID]PhysicalLayout, len(roots)),
		aliases:    make(map[types.TypeID]types.TypeID, len(roots)),
		aliasOrder: make([]types.TypeID, 0, len(roots)),
		order:      make([]types.TypeID, 0, len(roots)),
	}
	for _, root := range roots {
		if root == types.NoTypeID {
			return nil, &LayoutError{Kind: ErrUnknownType, Type: root, Operation: "registry root"}
		}
		canonical, err := builder.CanonicalType(root)
		if err != nil {
			return nil, err
		}
		if _, exists := registry.aliases[root]; !exists {
			registry.aliasOrder = append(registry.aliasOrder, root)
		}
		registry.aliases[root] = canonical
		if _, exists := registry.entries[canonical]; exists {
			continue
		}
		physical, layoutErr := builder.LayoutOf(canonical)
		if layoutErr != nil {
			return nil, layoutErr
		}
		if physical.State() == StateDeferred {
			return nil, &LayoutError{
				Kind:      ErrDeferred,
				Type:      canonical,
				Operation: fmt.Sprintf("final MIR registry (%s)", types.Label(builder.Types, canonical)),
			}
		}
		if _, ok := physical.Physical(); !ok {
			return nil, &LayoutError{Kind: ErrInvalidQuery, Type: canonical, Operation: "non-physical registry entry"}
		}
		registry.entries[canonical] = physical.clone()
		registry.order = append(registry.order, canonical)
	}
	return registry, nil
}

// Target returns the frozen target value.
func (r *Registry) Target() (Target, bool) {
	if r == nil {
		return Target{}, false
	}
	return r.target, true
}

// Lookup returns an owned copy of a frozen entry.
func (r *Registry) Lookup(id types.TypeID) (PhysicalLayout, bool) {
	if r == nil || id == types.NoTypeID {
		return PhysicalLayout{}, false
	}
	if canonical, ok := r.aliases[id]; ok {
		id = canonical
	}
	physical, ok := r.entries[id]
	if !ok {
		return PhysicalLayout{}, false
	}
	return physical.clone(), true
}

// Require returns physical-only facts or a fail-closed missing-entry error.
func (r *Registry) Require(id types.TypeID) (PhysicalFacts, error) {
	physical, ok := r.Lookup(id)
	if !ok {
		return PhysicalFacts{}, &LayoutError{Kind: ErrInvalidQuery, Type: id, Operation: "missing finalized registry entry"}
	}
	facts, ok := physical.Physical()
	if !ok {
		return PhysicalFacts{}, &LayoutError{Kind: ErrInvalidQuery, Type: id, Operation: "non-physical finalized registry entry"}
	}
	return facts, nil
}

// SizeOf returns the frozen physical size for an exact registry type.
func (r *Registry) SizeOf(id types.TypeID) (uint64, error) {
	l, err := r.Require(id)
	if err != nil {
		return 0, err
	}
	return l.Size, nil
}

// AlignOf returns the frozen physical alignment for an exact registry type.
func (r *Registry) AlignOf(id types.TypeID) (uint64, error) {
	l, err := r.Require(id)
	if err != nil {
		return 0, err
	}
	return l.Align, nil
}

// FieldOffset returns one frozen aggregate field offset.
func (r *Registry) FieldOffset(id types.TypeID, index int) (uint64, error) {
	l, err := r.Require(id)
	if err != nil {
		return 0, err
	}
	offset, ok := l.FieldOffset(index)
	if !ok {
		return 0, &LayoutError{Kind: ErrInvalidQuery, Type: id, Operation: "field offset"}
	}
	return offset, nil
}

// TypeIDs returns the canonical entries in deterministic first-seen order.
func (r *Registry) TypeIDs() []types.TypeID {
	if r == nil {
		return nil
	}
	return append([]types.TypeID(nil), r.order...)
}

// Len returns the number of canonical physical layouts in the registry.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.order)
}

// Dump writes a stable representation suitable for determinism tests.
func (r *Registry) Dump(w io.Writer) error {
	if r == nil {
		return fmt.Errorf("missing finalized layout registry")
	}
	if w == nil {
		return fmt.Errorf("missing layout registry writer")
	}
	if _, err := fmt.Fprintf(w, "target %s addr=%d ptr=%d/%d max_align=%d\n", r.target.Triple, r.target.AddressBits, r.target.PointerSize, r.target.PointerAlign, r.target.MaxABIAlign); err != nil {
		return err
	}
	for _, exact := range r.aliasOrder {
		if _, err := fmt.Fprintf(w, "alias type#%d -> type#%d\n", exact, r.aliases[exact]); err != nil {
			return err
		}
	}
	for _, id := range r.order {
		l := r.entries[id]
		facts := l.facts
		if _, err := fmt.Fprintf(w, "type#%d %s size=%d align=%d stride=%d fields=%v", id, l.state, facts.Size, facts.Align, facts.Stride, facts.fieldOffsets); err != nil {
			return err
		}
		for i := range facts.unionCases {
			unionCase := facts.unionCases[i]
			if _, err := fmt.Fprintf(w, " case%d=%d/%d/%d:%v", i, unionCase.PayloadOffset, unionCase.PayloadSize, unionCase.PayloadAlign, unionCase.fieldOffsets); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, " tag=%d/%d\n", facts.TagSize, facts.TagAlign); err != nil {
			return err
		}
	}
	return nil
}

// Hash returns the SHA-256 hash of Dump.
func (r *Registry) Hash() (string, error) {
	var buf bytes.Buffer
	if err := r.Dump(&buf); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}
