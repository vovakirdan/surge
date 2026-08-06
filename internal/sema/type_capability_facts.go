package sema

import (
	"fmt"
	"slices"
	"strings"

	"surge/internal/types"
)

// TypeAttrFacts is the detached form of the four type attributes that carry
// capability meaning: whether a value may move between shards, whether it is
// pinned to one, and whether it may be sent at all.
//
// The checker's own attribute store holds live AST nodes and is thrown away
// with the file it was built for, so a whole-program consumer needs a copy that
// owns nothing. These are named booleans rather than a set of attribute names
// on purpose: a set would let a newly catalogued attribute acquire capability
// meaning by accident, and would leave the merge sensitive to which record was
// folded in first.
//
// Copy is deliberately absent. It already reaches every consumer through
// Result.CopyTypes and the shared interner, and a fact with two authorities is
// a fact that can disagree with itself.
type TypeAttrFacts struct {
	ShardMovable bool
	ShardPinned  bool
	NoSend       bool
	Send         bool
}

// or folds two views of one type together.
func (f TypeAttrFacts) or(other TypeAttrFacts) TypeAttrFacts {
	return TypeAttrFacts{
		ShardMovable: f.ShardMovable || other.ShardMovable,
		ShardPinned:  f.ShardPinned || other.ShardPinned,
		NoSend:       f.NoSend || other.NoSend,
		Send:         f.Send || other.Send,
	}
}

// withoutContradictions drops both halves of a pair one declaration set against
// itself, leaving the type with no opinion on that axis.
//
// The merge refusal exists to catch modules that disagree with each other, and
// a single declaration holding both halves is not that. A struct declaring both
// has already been told so by its own diagnostic, and that diagnostic is the
// answer its author gets. Not every declaration form reaches that check — a
// type alias carrying both shard attributes is accepted in silence today — so
// carrying the pair forward would make the whole-program merge the reporter for
// a file-local mistake, at the wrong site and in the wrong voice, and would
// fail a build that currently succeeds.
func (f TypeAttrFacts) withoutContradictions() TypeAttrFacts {
	if f.ShardMovable && f.ShardPinned {
		f.ShardMovable = false
		f.ShardPinned = false
	}
	if f.Send && f.NoSend {
		f.Send = false
		f.NoSend = false
	}
	return f
}

// typeAttrFactsFromInfos reads exactly the four capability attributes out of a
// type's recorded attributes. Attributes arriving from an import are already
// name-only records built by recordImportedTypeAttrNames, so they answer here
// the same way a local declaration does.
func typeAttrFactsFromInfos(infos []AttrInfo) TypeAttrFacts {
	var facts TypeAttrFacts
	if _, ok := hasAttr(infos, "shard_movable"); ok {
		facts.ShardMovable = true
	}
	if _, ok := hasAttr(infos, "shard_pinned"); ok {
		facts.ShardPinned = true
	}
	if _, ok := hasAttr(infos, "nosend"); ok {
		facts.NoSend = true
	}
	if _, ok := hasAttr(infos, "send"); ok {
		facts.Send = true
	}
	return facts
}

// flushTypeAttrFacts copies the capability attributes into the result, beside
// the Copy flush and under the same rule: nothing that points at the AST.
func (tc *typeChecker) flushTypeAttrFacts() {
	if tc == nil || tc.result == nil || len(tc.typeAttrs) == 0 {
		return
	}
	out := make(map[types.TypeID]TypeAttrFacts, len(tc.typeAttrs))
	for typeID, infos := range tc.typeAttrs {
		if typeID == types.NoTypeID {
			continue
		}
		facts := typeAttrFactsFromInfos(infos).withoutContradictions()
		if facts == (TypeAttrFacts{}) {
			continue
		}
		out[typeID] = facts
	}
	if len(out) == 0 {
		return
	}
	tc.result.TypeAttrFacts = out
}

// MergeTypeAttrFacts folds src's attribute facts into dst.
//
// The fold is a per-fact OR and nothing else, which makes it commutative: the
// merged table is the same whichever order the records arrive in, so two
// compiles of one program agree. A pair the OR exposes as contradictory is not
// resolved here — TypeAttrFactMerge.Validate reports it once, after every
// record has been folded in.
func MergeTypeAttrFacts(dst, src *Result) {
	if dst == nil || src == nil || len(src.TypeAttrFacts) == 0 {
		return
	}
	if dst.TypeAttrFacts == nil {
		dst.TypeAttrFacts = make(map[types.TypeID]TypeAttrFacts, len(src.TypeAttrFacts))
	}
	for id, facts := range src.TypeAttrFacts {
		dst.TypeAttrFacts[id] = dst.TypeAttrFacts[id].or(facts)
	}
}

// TypeAttrFactMerge folds many records' attribute facts into one result and
// remembers which module contributed each fact.
//
// The origins are kept for one reason: a contradiction no single file can see —
// one module calling a type movable while another pins it — only appears after
// the fold, and by then the facts are anonymous booleans. Whoever has to fix
// such a program needs to be told which modules to look at.
type TypeAttrFactMerge struct {
	contributors map[types.TypeID]map[string]TypeAttrFacts
}

// NewTypeAttrFactMerge starts an empty fold.
func NewTypeAttrFactMerge() *TypeAttrFactMerge {
	return &TypeAttrFactMerge{contributors: make(map[types.TypeID]map[string]TypeAttrFacts)}
}

// Fold merges one record's facts into dst and attributes them to modulePath.
func (m *TypeAttrFactMerge) Fold(dst, src *Result, modulePath string) {
	if m == nil || dst == nil || src == nil || len(src.TypeAttrFacts) == 0 {
		return
	}
	for id, facts := range src.TypeAttrFacts {
		byModule := m.contributors[id]
		if byModule == nil {
			byModule = make(map[string]TypeAttrFacts, 1)
			m.contributors[id] = byModule
		}
		byModule[modulePath] = byModule[modulePath].or(facts)
	}
	MergeTypeAttrFacts(dst, src)
}

// Validate reports every mutually exclusive pair the merged table holds, once
// each, naming every module that contributed to it.
//
// Refusing here is what merging facts rather than trusting one file buys:
// @shard_movable together with @shard_pinned, or @send together with @nosend,
// describes a type with no single capability answer, and folding either half
// away silently would settle a placement question on map order.
func (m *TypeAttrFactMerge) Validate(merged *Result) error {
	if m == nil || merged == nil || len(merged.TypeAttrFacts) == 0 {
		return nil
	}
	ids := make([]types.TypeID, 0, len(merged.TypeAttrFacts))
	for id := range merged.TypeAttrFacts {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var reports []string
	for _, id := range ids {
		facts := merged.TypeAttrFacts[id]
		if facts.ShardMovable && facts.ShardPinned {
			reports = append(reports, m.describeConflict(id,
				"shard_movable", func(f TypeAttrFacts) bool { return f.ShardMovable },
				"shard_pinned", func(f TypeAttrFacts) bool { return f.ShardPinned }))
		}
		if facts.Send && facts.NoSend {
			reports = append(reports, m.describeConflict(id,
				"send", func(f TypeAttrFacts) bool { return f.Send },
				"nosend", func(f TypeAttrFacts) bool { return f.NoSend }))
		}
	}
	if len(reports) == 0 {
		return nil
	}
	return fmt.Errorf("merged type attribute facts contradict: %s", strings.Join(reports, "; "))
}

func (m *TypeAttrFactMerge) describeConflict(
	id types.TypeID,
	left string, hasLeft func(TypeAttrFacts) bool,
	right string, hasRight func(TypeAttrFacts) bool,
) string {
	return fmt.Sprintf("type %d is @%s in %s and @%s in %s",
		uint32(id),
		left, formatModuleList(m.modulesWith(id, hasLeft)),
		right, formatModuleList(m.modulesWith(id, hasRight)))
}

// modulesWith names every module that contributed the given fact, sorted, so
// the refusal reads the same on every run.
func (m *TypeAttrFactMerge) modulesWith(id types.TypeID, has func(TypeAttrFacts) bool) []string {
	paths := make([]string, 0, len(m.contributors[id]))
	for path, facts := range m.contributors[id] {
		if has(facts) {
			paths = append(paths, moduleLabel(path))
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// unnamedModuleLabel stands in for a record that carries no module path, so a
// refusal never reads as though it named nothing.
const unnamedModuleLabel = "<unnamed module>"

func moduleLabel(path string) string {
	if path == "" {
		return unnamedModuleLabel
	}
	return path
}

func formatModuleList(paths []string) string {
	if len(paths) == 0 {
		return unnamedModuleLabel
	}
	return strings.Join(paths, ", ")
}
