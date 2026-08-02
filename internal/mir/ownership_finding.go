package mir

import (
	"fmt"

	"surge/internal/source"
)

// OwnershipFinding reports one consuming sink whose value this pass could not
// establish as owned. It is DATA: the pass returns these and changes nothing.
type OwnershipFinding struct {
	Function string
	FuncID   FuncID
	// Local is the local whose value the sink consumes, or NoLocalID where the
	// sink's operand names no place at all.
	Local LocalID
	// LocalName is the local's MIR name, for a message that reads without a
	// dump next to it.
	LocalName string
	// DefSite names the definition that failed to resolve — "parameter L0" for
	// an entry root, "bb2#1" for an instruction — or "use" when the operand
	// occupying the sink was itself aliasing and no definition was ever
	// queried.
	DefSite string
	// ConsumingSite is where the release or consumption happens.
	ConsumingSite string
	// ConsumingPosition identifies the operand or place inside that instruction.
	// It keeps two failing arguments, fields, or arms at one MIR point distinct.
	ConsumingPosition string
	ConsumingKind     OwnershipSinkKind
	Span              source.Span
}

func (f OwnershipFinding) String() string {
	return fmt.Sprintf("%s: %s of %s (def %s) at %s",
		f.Function, f.ConsumingKind, f.localLabel(), f.DefSite, f.ConsumingSite)
}

func (f *OwnershipFinding) localLabel() string {
	if f.Local == NoLocalID {
		return "<no place>"
	}
	if f.LocalName == "" {
		return fmt.Sprintf("L%d", f.Local)
	}
	return fmt.Sprintf("L%d(%s)", f.Local, f.LocalName)
}

func (v *ownershipFuncVerifier) findings(
	local LocalID,
	st *ownershipResolveState,
	at ownershipPoint,
	kind OwnershipSinkKind,
	position string,
) []OwnershipFinding {
	defs := []string{"use"}
	if blames := st.sortedBlames(); len(blames) != 0 {
		defs = make([]string, 0, len(blames))
		for _, blame := range blames {
			defs = append(defs, describeDefSite(blame))
		}
	}
	if kind == OwnershipSinkUnresolvedContract {
		defs = []string{"unresolved contract"}
	}
	out := make([]OwnershipFinding, 0, len(defs))
	for _, def := range defs {
		out = append(out, v.finding(local, at, kind, position, def))
	}
	return out
}

func (v *ownershipFuncVerifier) finding(
	local LocalID,
	at ownershipPoint,
	kind OwnershipSinkKind,
	position string,
	def string,
) OwnershipFinding {
	name := ""
	span := v.f.Span
	if local >= 0 && int(local) < len(v.f.Locals) {
		loc := &v.f.Locals[local]
		name = loc.Name
		// Async/crossing lowering creates bookkeeping locals without a source
		// span. Keep the owning function's real span for those locals instead
		// of interpreting Span{} as file zero, byte zero in the caller's FileSet.
		if loc.Span != (source.Span{}) {
			span = loc.Span
		}
	}
	return OwnershipFinding{
		Function:          v.f.Name,
		FuncID:            v.f.ID,
		Local:             local,
		LocalName:         name,
		DefSite:           def,
		ConsumingSite:     describePoint(v.f, at),
		ConsumingPosition: position,
		ConsumingKind:     kind,
		Span:              span,
	}
}

func describeDefSite(d ownershipDefSite) string {
	if d.IsParamRoot() {
		return fmt.Sprintf("parameter L%d", d.Local)
	}
	return fmt.Sprintf("bb%d#%d", d.Block, d.Instr)
}

func indexedOwnershipPosition(label string, index int) string {
	return fmt.Sprintf("%s[%d]", label, index)
}

func prefixedOwnershipPosition(prefix, position string) string {
	if prefix == "" {
		return position
	}
	return prefix + "." + position
}

func ownershipFieldPosition(prefix string, index int, name string) string {
	position := prefixedOwnershipPosition(prefix, indexedOwnershipPosition("field", index))
	if name == "" {
		return position
	}
	return position + ":" + name
}

func describePoint(f *Func, at ownershipPoint) string {
	if f != nil && int(at.Block) < len(f.Blocks) && at.Instr >= len(f.Blocks[at.Block].Instrs) {
		return fmt.Sprintf("bb%d#term", at.Block)
	}
	return fmt.Sprintf("bb%d#%d", at.Block, at.Instr)
}
