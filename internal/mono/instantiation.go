package mono

import (
	"slices"
	"strconv"
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// InstantiationKind identifies the kind of entity being instantiated.
type InstantiationKind uint8

const (
	// InstFn represents a function instantiation.
	InstFn InstantiationKind = iota
	// InstType represents a type instantiation.
	InstType
	// InstTag represents a tag instantiation.
	InstTag
)

// InstantiationKey is a comparable key for instantiations.
//
// Note: Go maps cannot use slices as keys, so we store a stable ArgsKey string.
// The corresponding normalized TypeArgs are stored in InstEntry.
type InstantiationKey struct {
	Sym     symbols.SymbolID
	ArgsKey string
}

// UseSite records a location where an instantiation occurs.
type UseSite struct {
	Span   source.Span
	Caller symbols.SymbolID
	Note   string
}

// BoundInfo is reserved for future "bounds snapshot" debugging.
// v1: left empty on purpose.
type BoundInfo struct{}

// InstEntry captures all instantiations of a particular generic symbol.
type InstEntry struct {
	Kind InstantiationKind
	Key  InstantiationKey

	// Normalized type arguments.
	TypeArgs []types.TypeID

	UseSites []UseSite

	// Optional debugging/meta.
	BoundsSnapshot []BoundInfo
}

// InstantiationMap tracks all generic instantiations across a module.
type InstantiationMap struct {
	Entries map[InstantiationKey]*InstEntry
}

// NewInstantiationMap creates a new empty InstantiationMap.
func NewInstantiationMap() *InstantiationMap {
	return &InstantiationMap{Entries: make(map[InstantiationKey]*InstEntry)}
}

// NormalizeTypeArgs produces a deterministic slice used for instantiation keys.
//
// v1: this is intentionally conservative and does not erase nominal identity
// (e.g., `type UserId = uint64` stays distinct from `uint64` in type args).
func NormalizeTypeArgs(_ *types.Interner, args []types.TypeID) []types.TypeID {
	if len(args) == 0 {
		return nil
	}
	return slices.Clone(args)
}

// Record registers a generic instantiation at a specific site.
func (m *InstantiationMap) Record(kind InstantiationKind, sym symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string) {
	if m == nil || !sym.IsValid() || len(typeArgs) == 0 {
		return
	}
	if m.Entries == nil {
		m.Entries = make(map[InstantiationKey]*InstEntry)
	}

	normalized := NormalizeTypeArgs(nil, typeArgs)
	key := InstantiationKey{Sym: sym, ArgsKey: typeArgsKey(normalized)}
	entry := m.Entries[key]
	if entry == nil {
		entry = &InstEntry{
			Kind:     kind,
			Key:      key,
			TypeArgs: normalized,
		}
		m.Entries[key] = entry
	}

	if site != (source.Span{}) {
		us := UseSite{Span: site, Caller: caller, Note: note}
		for _, existing := range entry.UseSites {
			if existing == us {
				return
			}
		}
		entry.UseSites = append(entry.UseSites, us)
	}
}

func typeArgsKey(args []types.TypeID) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	for i, arg := range args {
		if i > 0 {
			b.WriteByte('#')
		}
		b.WriteString(strconv.FormatUint(uint64(arg), 10))
	}
	return b.String()
}
