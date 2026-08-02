package mir

import (
	"strings"

	"surge/internal/sema"
	"surge/internal/types"
)

type ownershipDumpAnnotations struct {
	verifier     ownershipFuncVerifier
	releaseRoots map[LocalID]struct{}
}

func newOwnershipDumpAnnotations(
	m *Module,
	f *Func,
	typesIn *types.Interner,
	semaRes *sema.Result,
) *ownershipDumpAnnotations {
	a := &ownershipDumpAnnotations{
		verifier: ownershipFuncVerifier{
			f:       f,
			typesIn: typesIn,
			semaRes: semaRes,
		},
		releaseRoots: make(map[LocalID]struct{}),
	}
	if m != nil {
		a.verifier.globals = m.Globals
	}
	if f == nil {
		return a
	}

	for bi := range f.Blocks {
		for ii := range f.Blocks[bi].Instrs {
			ins := &f.Blocks[bi].Instrs[ii]
			switch ins.Kind {
			case InstrDrop:
				a.noteReleaseRoot(ins.Drop.Place)
			case InstrEnvelopeRelease:
				a.noteReleaseRoot(ins.EnvelopeRelease.Place)
			}
		}
	}
	return a
}

func (a *ownershipDumpAnnotations) noteReleaseRoot(place Place) {
	if a == nil || place.Kind != PlaceLocal || place.Local == NoLocalID {
		return
	}
	idx := int(place.Local)
	if idx < 0 || a.verifier.f == nil || idx >= len(a.verifier.f.Locals) {
		return
	}
	a.releaseRoots[place.Local] = struct{}{}
}

func (a *ownershipDumpAnnotations) formatLocalFlags(local LocalID, base string) string {
	if a == nil {
		return base
	}
	if _, ok := a.releaseRoots[local]; !ok {
		return base
	}
	if base == "" {
		return "[owes_release]"
	}
	return strings.TrimSuffix(base, "]") + ", owes_release]"
}

func (a *ownershipDumpAnnotations) formatInstr(ins *Instr, base string) string {
	effect := a.assignmentEffect(ins)
	if effect == "" {
		return base
	}
	return base + " [effect=" + effect + "]"
}

func (a *ownershipDumpAnnotations) assignmentEffect(ins *Instr) string {
	if a == nil || ins == nil || ins.Kind != InstrAssign {
		return ""
	}
	effective := a.verifier.effectiveRValue(&ins.Assign.Src)
	resultTy, ok := placeTypeWithMapElems(
		a.verifier.typesIn,
		a.verifier.f,
		a.verifier.globals,
		ins.Assign.Dst,
	)
	if !ok {
		return ""
	}
	switch classifyRValue(&effective, resultTy, a.verifier.typesIn, a.verifier.semaRes) {
	case ownershipMints:
		return "mint"
	case ownershipAliases:
		return "alias"
	case ownershipTransfers:
		return "transfer"
	default:
		return ""
	}
}
