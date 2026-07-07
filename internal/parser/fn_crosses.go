package parser

import (
	"surge/internal/diag"
)

// crossesPlacementMessage is the shared message for a misplaced `crosses`
// effect keyword (SynCrossesPlacement / SYN2034).
const crossesPlacementMessage = "`crosses` must appear after the parameter list and before the return type"

// parseCrossesEffectAfterParams consumes a contextual `crosses` effect keyword
// in its only valid position: after the parameter list and before the return
// type (or body). It returns true when `crosses` was present. A repeated
// `crosses` (`fn f(...) crosses crosses`) is rejected as SynCrossesPlacement.
func (p *Parser) parseCrossesEffectAfterParams() bool {
	if !p.atContextualCrosses() {
		return false
	}
	p.advance() // consume the valid `crosses`
	for p.atContextualCrosses() {
		dupTok := p.advance()
		p.emitDiagnostic(
			diag.SynCrossesPlacement,
			diag.SevError,
			dupTok.Span,
			"duplicate `crosses` effect; it may appear at most once after the parameter list",
			nil,
		)
	}
	return true
}

// rejectMisplacedCrosses consumes and rejects one or more `crosses` tokens that
// appear in an invalid signature slot (before the parameter list, or after the
// return type), letting the parser recover onto the following construct.
func (p *Parser) rejectMisplacedCrosses() {
	for p.atContextualCrosses() {
		tok := p.advance()
		p.emitDiagnostic(
			diag.SynCrossesPlacement,
			diag.SevError,
			tok.Span,
			crossesPlacementMessage,
			nil,
		)
	}
}
