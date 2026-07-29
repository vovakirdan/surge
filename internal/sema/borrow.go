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
	// BorrowShared represents a shared borrow.
	BorrowShared BorrowKind = iota
	// BorrowMut represents a mutable borrow.
	BorrowMut
)

// PlaceKind enumerates addressable locations.
type placeKey string

// PlaceSegmentKind identifies the kind of projection applied to a base binding.
type PlaceSegmentKind uint8

const (
	// PlaceSegmentField represents a field access segment.
	PlaceSegmentField PlaceSegmentKind = iota
	// PlaceSegmentIndex represents an index access segment.
	PlaceSegmentIndex
	// PlaceSegmentDeref represents a dereference segment.
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
	// Reserved marks a two-phase mutable ARGUMENT borrow: until Activate
	// upgrades it, the borrow lives in the place's shared list and conflicts
	// like a shared borrow (blocks other muts, writes, and moves; coexists
	// with shared reads), so a sibling argument may still read the place.
	Reserved bool
}

type borrowState struct {
	shared []BorrowID
	mut    BorrowID
}

// BorrowIssueKind enumerates reasons a borrow-related action fails.
type BorrowIssueKind uint8

const (
	// BorrowIssueNone indicates no borrow issue.
	BorrowIssueNone BorrowIssueKind = iota
	// BorrowIssueConflictShared indicates a conflict with a shared borrow.
	BorrowIssueConflictShared
	// BorrowIssueConflictMut indicates a conflict with a mutable borrow.
	BorrowIssueConflictMut
	// BorrowIssueFrozen indicates a frozen borrow.
	BorrowIssueFrozen
	// BorrowIssueTaken indicates a taken borrow.
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
	bt.ensurePath(key, append([]PlaceSegment(nil), segments...))
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

func (bt *BorrowTable) ensurePath(key placeKey, segments []PlaceSegment) {
	if _, exists := bt.paths[key]; exists {
		return
	}
	if len(segments) == 0 {
		bt.paths[key] = nil
		return
	}
	bt.paths[key] = append([]PlaceSegment(nil), segments...)
}

// BeginBorrow registers a borrow originating from expr within scope. parent allows reborrows to bypass conflicts with the originating borrow.
func (bt *BorrowTable) BeginBorrow(expr ast.ExprID, span source.Span, kind BorrowKind, place Place, scope symbols.ScopeID, parent BorrowID) (BorrowID, BorrowIssue) {
	if bt == nil || !place.IsValid() || !scope.IsValid() || !expr.IsValid() {
		return NoBorrowID, BorrowIssue{}
	}
	combined := bt.combinedState(place)
	switch kind {
	case BorrowShared:
		if combined.mut != NoBorrowID && combined.mut != parent {
			return NoBorrowID, BorrowIssue{Kind: BorrowIssueConflictMut, Borrow: combined.mut}
		}
	case BorrowMut:
		if combined.mut != NoBorrowID && combined.mut != parent {
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

// BeginBorrowReserved registers a two-phase mutable argument borrow. Until
// Activate runs, the borrow is held in the shared list (so writes, moves,
// and other mutable borrows of the place are blocked, while shared reads
// stay legal). Admission still rejects an active mutable borrow and any
// other reservation of an overlapping place — two `&mut` arguments to one
// call alias the moment the callee runs.
func (bt *BorrowTable) BeginBorrowReserved(expr ast.ExprID, span source.Span, place Place, scope symbols.ScopeID, parent BorrowID) (BorrowID, BorrowIssue) {
	if bt == nil || !place.IsValid() || !scope.IsValid() || !expr.IsValid() {
		return NoBorrowID, BorrowIssue{}
	}
	combined := bt.combinedState(place)
	if combined.mut != NoBorrowID && combined.mut != parent {
		return NoBorrowID, BorrowIssue{Kind: BorrowIssueConflictMut, Borrow: combined.mut}
	}
	for _, sid := range combined.shared {
		if info := bt.Info(sid); info != nil && info.Reserved {
			return NoBorrowID, BorrowIssue{Kind: BorrowIssueConflictMut, Borrow: sid}
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
		Kind:  BorrowMut,
		Place: place,
		Span:  span,
		Life: Interval{
			FromExpr: expr,
			ToScope:  scope,
		},
		Reserved: true,
	}
	bt.infos = append(bt.infos, info)
	state.shared = append(state.shared, id)
	bt.placeState[place] = state
	bt.exprBorrow[expr] = id
	bt.scopeBorrows[scope] = append(bt.scopeBorrows[scope], id)
	return id, BorrowIssue{}
}

// Activate upgrades a reserved two-phase borrow to a live exclusive borrow.
// It runs when every argument of the reserving call has been evaluated:
// sibling temporaries have released their loans by then, so any borrow the
// check still sees is genuinely alive at the call and must reject the
// exclusive hand-off.
func (bt *BorrowTable) Activate(id BorrowID) BorrowIssue {
	if bt == nil || id == NoBorrowID || int(id) >= len(bt.infos) {
		return BorrowIssue{}
	}
	info := &bt.infos[id]
	if !info.Reserved || !info.Place.IsValid() {
		return BorrowIssue{}
	}
	state := bt.placeState[info.Place]
	state.shared = dropBorrowID(state.shared, id)
	bt.placeState[info.Place] = state
	combined := bt.combinedState(info.Place)
	if combined.mut != NoBorrowID {
		// Leave the reservation dissolved; the conflict is reported and the
		// exclusive slot stays with its current holder.
		bt.detachBorrow(id)
		return BorrowIssue{Kind: BorrowIssueConflictMut, Borrow: combined.mut}
	}
	if len(combined.shared) > 0 {
		bt.detachBorrow(id)
		return BorrowIssue{Kind: BorrowIssueConflictShared, Borrow: combined.shared[0]}
	}
	info.Reserved = false
	state = bt.placeState[info.Place]
	state.mut = id
	bt.placeState[info.Place] = state
	return BorrowIssue{}
}

// detachBorrow removes a dissolved reservation's bookkeeping (expr and scope
// registrations) after Activate failed, so scope teardown does not double-
// remove it.
func (bt *BorrowTable) detachBorrow(id BorrowID) {
	info := bt.Info(id)
	if info == nil {
		return
	}
	delete(bt.exprBorrow, info.Life.FromExpr)
	if scopeList := bt.scopeBorrows[info.Life.ToScope]; len(scopeList) > 0 {
		bt.scopeBorrows[info.Life.ToScope] = dropBorrowID(scopeList, id)
	}
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
		switch {
		case info.Reserved:
			// A never-activated reservation still lives in the shared list.
			state.shared = dropBorrowID(state.shared, id)
		case info.Kind == BorrowShared:
			state.shared = dropBorrowID(state.shared, id)
		case info.Kind == BorrowMut:
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

// ScopeBorrows returns borrows whose lexical lifetime ends at scope.
// The returned slice is a copy and is safe to retain.
func (bt *BorrowTable) ScopeBorrows(scope symbols.ScopeID) []BorrowID {
	if bt == nil || !scope.IsValid() {
		return nil
	}
	ids := bt.scopeBorrows[scope]
	if len(ids) == 0 {
		return nil
	}
	return append([]BorrowID(nil), ids...)
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
	segs := bt.paths[place.Path]
	return append([]PlaceSegment(nil), segs...)
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
	// The nil guard is kept even though the answer no longer needs the table:
	// this method used to return false for a nil receiver, and preserving that
	// is the point of this step.
	if bt == nil {
		return false
	}
	return placesOverlap(a, b)
}

// placesOverlap reports whether two places name storage that can be the same:
// one path is a prefix of the other, so `o` overlaps `o.inner` while `o.left`
// and `o.right` do not.
//
// Deliberately a free function that reads only the interned key. The table-bound
// version decoded each key back into segments through `BorrowTable.paths`, which
// made the answer depend on WHICH table was asked — a place interned by one
// table and queried through another lost its path and read as a whole-binding
// place. The moved-set is keyed by Place and outlives any single query, so that
// dependency had to go.
//
// The prefix test is exact rather than approximate because `internPath`
// terminates every segment with `;`: `f:1;` is not a prefix of `f:12;`, so no
// two distinct fields can be confused for a prefix pair.
// placeCovers reports whether a NAMES b or contains it: `o` covers `o.inner`,
// and every place covers itself. Overlap is this relation in either direction;
// covering is the directed one, and the moved-set needs the direction to know
// which of two entries makes the other redundant.
func placeCovers(a, b Place) bool {
	if !a.IsValid() || !b.IsValid() || a.Base != b.Base {
		return false
	}
	return strings.HasPrefix(string(b.Path), string(a.Path))
}

func placesOverlap(a, b Place) bool {
	if !a.IsValid() || !b.IsValid() || a.Base != b.Base {
		return false
	}
	return strings.HasPrefix(string(a.Path), string(b.Path)) ||
		strings.HasPrefix(string(b.Path), string(a.Path))
}

func (bt *BorrowTable) formatPlaceLabel(place Place, base string, interner *source.Interner) string {
	if base == "" {
		base = "value"
	}
	return formatPlaceSegments(base, bt.placeSegments(place), interner)
}

// formatPlaceSegments renders a base name plus a projection path. Split out of
// formatPlaceLabel so a caller holding raw segments — one that has not interned
// a path, and does not want to — can render the same text.
func formatPlaceSegments(base string, segs []PlaceSegment, interner *source.Interner) string {
	if base == "" {
		base = "value"
	}
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

// DropBorrow removes a borrow from the table and expires its lifetime.
func (bt *BorrowTable) DropBorrow(id BorrowID) {
	if bt == nil || id == NoBorrowID {
		return
	}
	info := bt.Info(id)
	if info == nil || !info.Place.IsValid() {
		return
	}
	state := bt.placeState[info.Place]
	switch {
	case info.Reserved:
		// A never-activated reservation still lives in the shared list.
		state.shared = dropBorrowID(state.shared, id)
	case info.Kind == BorrowShared:
		state.shared = dropBorrowID(state.shared, id)
	case info.Kind == BorrowMut:
		if state.mut == id {
			state.mut = NoBorrowID
		}
	}
	if len(state.shared) == 0 && state.mut == NoBorrowID {
		delete(bt.placeState, info.Place)
	} else {
		bt.placeState[info.Place] = state
	}
	delete(bt.exprBorrow, info.Life.FromExpr)
	if scopeList := bt.scopeBorrows[info.Life.ToScope]; len(scopeList) > 0 {
		bt.scopeBorrows[info.Life.ToScope] = dropBorrowID(scopeList, id)
	}
}
