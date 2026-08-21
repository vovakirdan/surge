package valueops

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"surge/internal/symbols"
	"surge/internal/types"
)

// Role records why a type is an operation root. Roles are additive bits, not an
// enumeration: every root is a value root, and a root that was also reached as a
// map key additionally carries RoleKey.
type Role uint8

const (
	// RoleValue marks a type that needs a value operation plan. Every root has
	// it.
	RoleValue Role = 1 << iota
	// RoleKey marks a type that is also used as a map key and therefore needs
	// the key callbacks on top of its value plan.
	RoleKey
)

// Root is one census root with its roles, in the caller's deterministic order.
type Root struct {
	Type types.TypeID
	Role Role
}

// KeyOps are the two always-present rt_key_ops callbacks for one key type.
type KeyOps struct {
	Hash  symbols.SymbolID
	Equal symbols.SymbolID
}

// Input is the complete plain-data description of one module's operation plans.
//
// Everything here is a value, a slice of values, or a map of values. No compiler
// object crosses this boundary: no interner, no analysis result, no symbol
// table, no AST. That is what lets a finalized registry outlive the compiler
// state it was derived from without ever being able to observe a change in it.
type Input struct {
	// Roots is the ordered census. Its order is the registry's order; nothing in
	// this package depends on map iteration order.
	Roots []Root
	// Values holds one entry per exact root type.
	Values map[types.TypeID]Entry
	// Keys holds the key callbacks for every root carrying RoleKey. An entry is
	// required for each such root even while the callbacks are staged, so that
	// an absent map entry is always an error rather than an implied zero.
	Keys map[types.TypeID]KeyOps
}

// Registry is a frozen, per-module set of owned operation plans. It retains no
// builder and no compiler state and is safe for concurrent reads after
// construction.
//
// Keys are EXACT semantic type ids throughout. Two exact ids may share one
// stored entry, but only when sharesWith proves the two plans indistinguishable.
type Registry struct {
	entries  map[types.TypeID]Entry
	aliases  map[types.TypeID]types.TypeID
	order    []types.TypeID
	repOrder []types.TypeID
	keys     map[types.TypeID]KeyOps
	keyOrder []types.TypeID
}

// shareKey buckets candidate entries by cheap fields so the exhaustive
// sharesWith comparison runs only against plausible matches. It is a search
// optimization and never a sharing decision.
type shareKey struct {
	flags  Flags
	clone  symbols.SymbolID
	recipe int
	size   uint64
	align  uint64
}

func bucketOf(e *Entry) shareKey {
	return shareKey{
		flags:  e.Flags,
		clone:  e.CloneInit,
		recipe: e.RecipeIndex,
		size:   e.Facts.Size,
		align:  e.Facts.Align,
	}
}

// Finalize validates the whole input and freezes owned copies. It fails closed:
// a root without an entry, an entry that misstates its own type, a negative
// recipe index, a key root without key operations, or any entry that violates
// the slot invariant refuses the whole registry rather than producing a partial
// one.
func Finalize(in Input) (*Registry, error) {
	registry := &Registry{
		entries: make(map[types.TypeID]Entry, len(in.Roots)),
		aliases: make(map[types.TypeID]types.TypeID, len(in.Roots)),
		keys:    make(map[types.TypeID]KeyOps),
	}
	buckets := make(map[shareKey][]types.TypeID, len(in.Roots))
	for index, root := range in.Roots {
		entry, err := validatedEntry(in, index, root)
		if err != nil {
			return nil, err
		}
		registry.admit(&entry, buckets)
		if root.Role&RoleKey != 0 {
			if err := registry.admitKey(in, root); err != nil {
				return nil, err
			}
		}
	}
	return registry, nil
}

// validatedEntry checks one root against the input and returns its owned entry.
func validatedEntry(in Input, index int, root Root) (Entry, error) {
	if root.Type == types.NoTypeID {
		return Entry{}, fmt.Errorf("valueops: operation root %d has no type", index)
	}
	if unknown := root.Role &^ (RoleValue | RoleKey); unknown != 0 {
		return Entry{}, fmt.Errorf(
			"valueops: operation root %d for type#%d has unknown role bits %s",
			index, root.Type, formatHex(uint64(unknown)),
		)
	}
	if root.Role&RoleValue == 0 {
		return Entry{}, fmt.Errorf(
			"valueops: operation root %d for type#%d is not a value root; every root is a value root",
			index, root.Type,
		)
	}
	entry, ok := in.Values[root.Type]
	if !ok {
		return Entry{}, fmt.Errorf("valueops: operation root type#%d has no value entry", root.Type)
	}
	if entry.Type != root.Type {
		return Entry{}, fmt.Errorf(
			"valueops: value entry for operation root type#%d states type#%d",
			root.Type, entry.Type,
		)
	}
	if entry.RecipeIndex < 0 {
		return Entry{}, fmt.Errorf(
			"valueops: type#%d has negative descriptor recipe index %d",
			entry.Type, entry.RecipeIndex,
		)
	}
	if err := entry.checkSlots(); err != nil {
		return Entry{}, err
	}
	return entry.clone(), nil
}

// admit records one validated entry, sharing storage with an existing entry only
// when the two are provably indistinguishable.
//
// A repeated root is recorded once and then skipped. It cannot contradict what
// was already stored: Input.Values is keyed by exact type, so one type has
// exactly one plan by construction rather than by assertion.
func (r *Registry) admit(entry *Entry, buckets map[shareKey][]types.TypeID) {
	if _, seen := r.aliases[entry.Type]; seen {
		return
	}
	r.order = append(r.order, entry.Type)
	bucket := bucketOf(entry)
	for _, candidate := range buckets[bucket] {
		stored := r.entries[candidate]
		if stored.sharesWith(entry) {
			r.aliases[entry.Type] = candidate
			return
		}
	}
	r.entries[entry.Type] = *entry
	r.aliases[entry.Type] = entry.Type
	r.repOrder = append(r.repOrder, entry.Type)
	buckets[bucket] = append(buckets[bucket], entry.Type)
}

// admitKey records the key callbacks for one root carrying RoleKey. As with
// admit, a repeated key root cannot disagree with itself because Input.Keys is
// keyed by exact type.
func (r *Registry) admitKey(in Input, root Root) error {
	ops, ok := in.Keys[root.Type]
	if !ok {
		return fmt.Errorf("valueops: key root type#%d has no key operations", root.Type)
	}
	if _, seen := r.keys[root.Type]; seen {
		return nil
	}
	r.keys[root.Type] = ops
	r.keyOrder = append(r.keyOrder, root.Type)
	return nil
}

// Value returns an owned copy of the operation plan for one exact type, or a
// fail-closed error. The returned entry always reports the requested exact type,
// so entry sharing is invisible to callers.
func (r *Registry) Value(id types.TypeID) (Entry, error) {
	if r == nil {
		return Entry{}, errors.New("valueops: missing finalized operation registry")
	}
	representative, ok := r.aliases[id]
	if !ok {
		return Entry{}, fmt.Errorf("valueops: no operation entry for type#%d", id)
	}
	stored := r.entries[representative]
	entry := stored.clone()
	entry.Type = id
	return entry, nil
}

// Key returns an owned copy of the key operation plan for one exact map-key
// type, or a fail-closed error for a type that is not a key.
func (r *Registry) Key(id types.TypeID) (KeyEntry, error) {
	if r == nil {
		return KeyEntry{}, errors.New("valueops: missing finalized operation registry")
	}
	ops, ok := r.keys[id]
	if !ok {
		return KeyEntry{}, fmt.Errorf("valueops: type#%d has no key operation entry", id)
	}
	value, err := r.Value(id)
	if err != nil {
		return KeyEntry{}, err
	}
	return KeyEntry{Value: value, Hash: ops.Hash, Equal: ops.Equal}, nil
}

// TypeIDs returns every exact value root in deterministic first-seen order.
func (r *Registry) TypeIDs() []types.TypeID {
	if r == nil {
		return nil
	}
	return append([]types.TypeID(nil), r.order...)
}

// KeyTypeIDs returns every exact key root in deterministic first-seen order.
func (r *Registry) KeyTypeIDs() []types.TypeID {
	if r == nil {
		return nil
	}
	return append([]types.TypeID(nil), r.keyOrder...)
}

// Len returns the number of exact value roots.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.order)
}

// KeyLen returns the number of exact key roots.
func (r *Registry) KeyLen() int {
	if r == nil {
		return 0
	}
	return len(r.keyOrder)
}

// Dump writes a stable representation suitable for determinism gates. Every
// field that participates in an entry's identity appears here, because Hash is
// the hash of this text.
func (r *Registry) Dump(w io.Writer) error {
	if r == nil {
		return errors.New("valueops: missing finalized operation registry")
	}
	if w == nil {
		return errors.New("valueops: missing operation registry writer")
	}
	if _, err := fmt.Fprintf(w, "valueops values=%d keys=%d entries=%d\n",
		len(r.order), len(r.keyOrder), len(r.repOrder)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "staging %s\n", StagingNotice); err != nil {
		return err
	}
	for _, exact := range r.order {
		if _, err := fmt.Fprintf(w, "alias type#%d -> type#%d\n", exact, r.aliases[exact]); err != nil {
			return err
		}
	}
	for _, id := range r.repOrder {
		entry := r.entries[id]
		if err := dumpEntry(w, &entry); err != nil {
			return err
		}
	}
	for _, id := range r.keyOrder {
		ops := r.keys[id]
		if _, err := fmt.Fprintf(w, "key type#%d hash=%s equal=%s\n",
			id, formatSymbol(ops.Hash), formatSymbol(ops.Equal)); err != nil {
			return err
		}
	}
	return nil
}

func dumpEntry(w io.Writer, e *Entry) error {
	if _, err := fmt.Fprintf(w,
		"entry type#%d size=%d align=%d stride=%d tag=%d/%d fields=%v aligns=%v",
		e.Type, e.Facts.Size, e.Facts.Align, e.Facts.Stride,
		e.Facts.TagSize, e.Facts.TagAlign,
		e.Facts.FieldOffsets(), e.Facts.FieldAligns()); err != nil {
		return err
	}
	for i, unionCase := range e.Facts.UnionCases() {
		if _, err := fmt.Fprintf(w, " case%d=%d/%d/%d:%v", i,
			unionCase.PayloadOffset, unionCase.PayloadSize, unionCase.PayloadAlign,
			unionCase.FieldOffsets()); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, " flags=%s clone_init=%s recipe=%d caps=%s\n",
		e.Flags, formatSymbol(e.CloneInit), e.RecipeIndex,
		formatCapabilities(e.Capabilities)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w,
		"  evidence clone_method=%q clone=%q droppable=%q traceable=%q shard=%q cross_clone=%q shard_path=%v cross_clone_path=%v\n",
		e.Evidence.CloneMethodKey, e.Evidence.CloneReason,
		e.Evidence.DroppableReason, e.Evidence.TraceableReason,
		e.Evidence.ShardReason, e.Evidence.CrossCloneReason,
		e.Evidence.ShardPath, e.Evidence.CrossClonePath)
	return err
}

// formatCapabilities renders the staged verdicts in a fixed order so the dump
// text, and therefore the hash, is stable.
func formatCapabilities(c Capabilities) string {
	var out []byte
	appendVerdict := func(name string, set bool) {
		if len(out) > 0 {
			out = append(out, ',')
		}
		if !set {
			out = append(out, '!')
		}
		out = append(out, name...)
	}
	appendVerdict("traceable", c.Traceable)
	appendVerdict("shard_movable", c.ShardMovable)
	appendVerdict("cross_clonable", c.CrossClonable)
	return string(out)
}

func formatSymbol(id symbols.SymbolID) string {
	if id == symbols.NoSymbolID {
		return "none"
	}
	return fmt.Sprintf("sym#%d", id)
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
