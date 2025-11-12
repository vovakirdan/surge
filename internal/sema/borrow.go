package sema

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

// BorrowID identifies an active borrow entry.
type BorrowID uint32

// NoBorrowID marks the absence of a borrow.
const NoBorrowID BorrowID = 0

// BorrowKind differentiates shared vs mutable borrows.
type BorrowKind uint8

const (
	BorrowShared BorrowKind = iota
	BorrowMut
)

// PlaceKind enumerates addressable locations.
type placeKey string

// PlaceSegmentKind identifies the kind of projection applied to a base binding.
type PlaceSegmentKind uint8

const (
	PlaceSegmentField PlaceSegmentKind = iota
	PlaceSegmentIndex
	PlaceSegmentDeref
)

// PlaceSegment stores one projection step (field/index/deref).
type PlaceSegment struct {
	Kind PlaceSegmentKind
	Name source.StringID // only for fields
}

// Place describes an addressable location participating in borrows.
type Place struct {
	Base symbols.SymbolID
	Path placeKey
}

// IsValid reports whether the place references a known binding.
func (p Place) IsValid() bool {
	return p.Base.IsValid()
}

// Interval captures lexical lifetime of a borrow.
type Interval struct {
	FromExpr ast.ExprID
	ToScope  symbols.ScopeID
}

// BorrowInfo stores metadata about each borrow.
type BorrowInfo struct {
	ID    BorrowID
	Kind  BorrowKind
	Place Place
	Span  source.Span
	Life  Interval
}

type borrowState struct {
	shared []BorrowID
	mut    BorrowID
}

// BorrowIssueKind enumerates reasons a borrow-related action fails.
type BorrowIssueKind uint8

const (
	BorrowIssueNone BorrowIssueKind = iota
	BorrowIssueConflictShared
	BorrowIssueConflictMut
	BorrowIssueFrozen
	BorrowIssueTaken
)

// BorrowIssue carries information about conflicts.
type BorrowIssue struct {
	Kind   BorrowIssueKind
	Borrow BorrowID
}

// BorrowTable tracks active borrows and per-place state.
type BorrowTable struct {
	infos        []BorrowInfo
	placeState   map[Place]borrowState
	exprBorrow   map[ast.ExprID]BorrowID
	scopeBorrows map[symbols.ScopeID][]BorrowID
	paths        map[placeKey][]PlaceSegment
}

// NewBorrowTable builds an empty borrow table ready for tracking.
func NewBorrowTable() *BorrowTable {
	return &BorrowTable{
		infos:        []BorrowInfo{{}},
		placeState:   make(map[Place]borrowState),
		exprBorrow:   make(map[ast.ExprID]BorrowID),
		scopeBorrows: make(map[symbols.ScopeID][]BorrowID),
		paths:        make(map[placeKey][]PlaceSegment),
	}
}

// CanonicalPlace interns the provided projection path and returns a comparable place key.
func (bt *BorrowTable) CanonicalPlace(base symbols.SymbolID, segments []PlaceSegment) Place {
	if bt == nil || !base.IsValid() {
		return Place{}
	}
	key := bt.internPath(segments)
	if _, exists := bt.paths[key]; !exists {
		if len(segments) > 0 {
			bt.paths[key] = append([]PlaceSegment(nil), segments...)
		} else {
			bt.paths[key] = nil
		}
	}
	return Place{
		Base: base,
		Path: key,
	}
}

func (bt *BorrowTable) internPath(segments []PlaceSegment) placeKey {
	if len(segments) == 0 {
		return placeKey("")
	}
	var b strings.Builder
	for _, seg := range segments {
		switch seg.Kind {
		case PlaceSegmentField:
			fmt.Fprintf(&b, "f:%d;", seg.Name)
		case PlaceSegmentIndex:
			b.WriteString("i:;")
		case PlaceSegmentDeref:
			b.WriteString("d:;")
		default:
			b.WriteString("?:;")
		}
	}
	return placeKey(b.String())
}

// BeginBorrow registers a borrow originating from expr within scope.
func (bt *BorrowTable) BeginBorrow(expr ast.ExprID, span source.Span, kind BorrowKind, place Place, scope symbols.ScopeID) (BorrowID, BorrowIssue) {
	if bt == nil || !place.IsValid() || !scope.IsValid() || !expr.IsValid() {
		return NoBorrowID, BorrowIssue{}
	}
	combined := bt.combinedState(place)
	switch kind {
	case BorrowShared:
		if combined.mut != NoBorrowID {
			return NoBorrowID, BorrowIssue{Kind: BorrowIssueConflictMut, Borrow: combined.mut}
		}
	case BorrowMut:
		if combined.mut != NoBorrowID {
			return NoBorrowID, BorrowIssue{Kind: BorrowIssueConflictMut, Borrow: combined.mut}
		}
		if len(combined.shared) > 0 {
			return NoBorrowID, BorrowIssue{Kind: BorrowIssueConflictShared, Borrow: combined.shared[0]}
		}
	}
	state := bt.placeState[place]
	value, err := safecast.Conv[uint32](len(bt.infos))
	if err != nil {
		panic(fmt.Errorf("borrow table overflow: %w", err))
	}
	id := BorrowID(value)
	info := BorrowInfo{
		ID:    id,
		Kind:  kind,
		Place: place,
		Span:  span,
		Life: Interval{
			FromExpr: expr,
			ToScope:  scope,
		},
	}
	bt.infos = append(bt.infos, info)
	switch kind {
	case BorrowShared:
		state.shared = append(state.shared, id)
	case BorrowMut:
		state.mut = id
	}
	bt.placeState[place] = state
	bt.exprBorrow[expr] = id
	bt.scopeBorrows[scope] = append(bt.scopeBorrows[scope], id)
	return id, BorrowIssue{}
}

// MutationAllowed verifies whether the place can be mutated.
func (bt *BorrowTable) MutationAllowed(place Place) BorrowIssue {
	if bt == nil || !place.IsValid() {
		return BorrowIssue{}
	}
	state := bt.combinedState(place)
	if len(state.shared) == 0 && state.mut == NoBorrowID {
		return BorrowIssue{}
	}
	if len(state.shared) > 0 {
		return BorrowIssue{Kind: BorrowIssueFrozen, Borrow: state.shared[0]}
	}
	if state.mut != NoBorrowID {
		return BorrowIssue{Kind: BorrowIssueTaken, Borrow: state.mut}
	}
	return BorrowIssue{}
}

// MoveAllowed verifies whether the place can be moved from.
func (bt *BorrowTable) MoveAllowed(place Place) BorrowIssue {
	if bt == nil || !place.IsValid() {
		return BorrowIssue{}
	}
	state := bt.combinedState(place)
	if len(state.shared) == 0 && state.mut == NoBorrowID {
		return BorrowIssue{}
	}
	if len(state.shared) > 0 {
		return BorrowIssue{Kind: BorrowIssueFrozen, Borrow: state.shared[0]}
	}
	if state.mut != NoBorrowID {
		return BorrowIssue{Kind: BorrowIssueTaken, Borrow: state.mut}
	}
	return BorrowIssue{}
}

// EndScope expires all borrows whose lexical lifetime ends at scope.
func (bt *BorrowTable) EndScope(scope symbols.ScopeID) {
	if bt == nil || !scope.IsValid() {
		return
	}
	ids := bt.scopeBorrows[scope]
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		info := bt.Info(id)
		if info == nil {
			continue
		}
		state := bt.placeState[info.Place]
		switch info.Kind {
		case BorrowShared:
			state.shared = dropBorrowID(state.shared, id)
		case BorrowMut:
			if state.mut == id {
				state.mut = NoBorrowID
			}
		}
		if len(state.shared) == 0 && state.mut == NoBorrowID {
			delete(bt.placeState, info.Place)
		} else {
			bt.placeState[info.Place] = state
		}
	}
	delete(bt.scopeBorrows, scope)
}

// Info returns metadata for the borrow.
func (bt *BorrowTable) Info(id BorrowID) *BorrowInfo {
	if bt == nil || id == NoBorrowID || int(id) >= len(bt.infos) {
		return nil
	}
	return &bt.infos[id]
}

// ExprBorrow returns borrow id associated with an expression if any.
func (bt *BorrowTable) ExprBorrow(id ast.ExprID) BorrowID {
	if bt == nil {
		return NoBorrowID
	}
	return bt.exprBorrow[id]
}

// Infos returns a shallow copy of stored borrow infos (excluding sentinel).
func (bt *BorrowTable) Infos() []BorrowInfo {
	if bt == nil || len(bt.infos) <= 1 {
		return nil
	}
	out := make([]BorrowInfo, len(bt.infos)-1)
	copy(out, bt.infos[1:])
	return out
}

// ExprBorrowSnapshot returns a copy of Expr->Borrow map.
func (bt *BorrowTable) ExprBorrowSnapshot() map[ast.ExprID]BorrowID {
	if bt == nil || len(bt.exprBorrow) == 0 {
		return nil
	}
	out := make(map[ast.ExprID]BorrowID, len(bt.exprBorrow))
	for k, v := range bt.exprBorrow {
		out[k] = v
	}
	return out
}

func (bt *BorrowTable) placeSegments(place Place) []PlaceSegment {
	if bt == nil {
		return nil
	}
	return bt.paths[place.Path]
}

func (bt *BorrowTable) combinedState(place Place) borrowState {
	var combined borrowState
	for p, state := range bt.placeState {
		if !bt.placesOverlap(place, p) {
			continue
		}
		if len(state.shared) > 0 {
			combined.shared = append(combined.shared, state.shared...)
		}
		if state.mut != NoBorrowID && combined.mut == NoBorrowID {
			combined.mut = state.mut
		}
	}
	return combined
}

func (bt *BorrowTable) placesOverlap(a, b Place) bool {
	if bt == nil || !a.IsValid() || !b.IsValid() || a.Base != b.Base {
		return false
	}
	aSegs := bt.placeSegments(a)
	bSegs := bt.placeSegments(b)
	limit := aSegs
	if len(bSegs) < len(limit) {
		limit = bSegs
	}
	for i := range limit {
		aSeg := aSegs[i]
		bSeg := bSegs[i]
		if aSeg.Kind != bSeg.Kind || aSeg.Name != bSeg.Name {
			return false
		}
	}
	return true
}

func (bt *BorrowTable) formatPlaceLabel(place Place, base string, interner *source.Interner) string {
	if base == "" {
		base = "value"
	}
	segs := bt.placeSegments(place)
	if len(segs) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	for _, seg := range segs {
		switch seg.Kind {
		case PlaceSegmentField:
			b.WriteByte('.')
			if interner != nil && seg.Name != source.NoStringID {
				b.WriteString(interner.MustLookup(seg.Name))
			} else {
				fmt.Fprintf(&b, "#%d", seg.Name)
			}
		case PlaceSegmentIndex:
			b.WriteString("[?]")
		case PlaceSegmentDeref:
			b.WriteString(".*")
		default:
			b.WriteString(".?")
		}
	}
	return b.String()
}

func dropBorrowID(ids []BorrowID, target BorrowID) []BorrowID {
	if len(ids) == 0 {
		return ids
	}
	for i, id := range ids {
		if id == target {
			ids[i] = ids[len(ids)-1]
			return ids[:len(ids)-1]
		}
	}
	return ids
}
