package sema

import (
	"fmt"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// selectPayloadLedger is what one `select` (or `race`) knows about the
// payloads its SEND arms give away, kept beside the moved-set because the
// moved-set is rolled back per arm and the awaits are not branches.
//
// Every arm's await is evaluated before the select runs, so a payload is
// staged before any winner exists. Two arms naming one binding would stage it
// twice — the loser's cell is destroyed and the winner's committed, one block
// freed twice — and an await that CONSUMES a binding another arm has staged
// frees it before the select ever runs. Both are refused from here.
type selectPayloadLedger struct {
	// keyword names the construct in diagnostics: `select` or `race`.
	keyword string
	// taken maps each binding a SEND arm gave away to the span of that arm's
	// payload.
	taken map[symbols.SymbolID]source.Span
	// arm is the binding the arm currently being typed gave away, or invalid
	// while the arm has given nothing yet. typeSelectExpr resets it before
	// each await and reads it after, to tell the arm's own conditional move
	// from everything else the await moved.
	arm symbols.SymbolID
}

// recordSelectSendPayload ledgers one SEND arm's payload. It reports false,
// with the refusal emitted, when another arm of the same select already gave
// the binding away.
func (tc *typeChecker) recordSelectSendPayload(binding symbols.SymbolID, span source.Span) bool {
	ledger := tc.selectSendPayloads
	if ledger == nil {
		return true
	}
	if prevSpan, taken := ledger.taken[binding]; taken {
		name := tc.bindingName(binding)
		if b := diag.ReportError(tc.reporter, diag.SemaSelectSendPayloadGivenTwice, span,
			fmt.Sprintf("'%s' is given away by two arms of this `%s`: every arm stages its payload "+
				"before the %s runs, so one value would sit in two arms' cells and be freed twice",
				name, ledger.keyword, ledger.keyword)); b != nil {
			b.WithNote(prevSpan, fmt.Sprintf("'%s' is already given away by this arm", name))
			if advice := tc.cloneAdviceFor(adviceSelectSendSecondArm, tc.bindingType(binding), name); advice.Help != "" {
				b.WithHelp(span, advice.Help)
			}
			b.Emit()
		}
		return false
	}
	ledger.taken[binding] = span
	ledger.arm = binding
	return true
}

// unconditionalAwaitMoves is what an arm's await moved for good: its moved-set
// without the arm's own SEND payload, which the runtime hands back should the
// arm lose. Everything else an await moves — a by-value argument of a call
// inside the await expression, say — is consumed before the select runs and
// stays consumed whichever arm wins.
func unconditionalAwaitMoves(moved map[Place]source.Span, payload symbols.SymbolID) map[Place]source.Span {
	if !payload.IsValid() {
		return moved
	}
	out := make(map[Place]source.Span, len(moved))
	for place, span := range moved {
		if place.Base == payload && len(place.Path) == 0 {
			continue
		}
		out[place] = span
	}
	return out
}

// refuseStagedPayloadsConsumedByAnAwait refuses a SEND payload that another
// arm's await consumed unconditionally: the select would stage a binding an
// await had already freed. The other order — the consuming await before the
// giving arm — is a plain use-after-move at the payload's read, because the
// giving arm is typed from awaitBase and sees the consumption.
func (tc *typeChecker) refuseStagedPayloadsConsumedByAnAwait(awaitBase map[Place]source.Span) {
	ledger := tc.selectSendPayloads
	if ledger == nil || len(ledger.taken) == 0 || len(awaitBase) == 0 {
		return
	}
	for binding, payloadSpan := range ledger.taken {
		_, moveSpan, consumed := movedPlaceCoveringIn(awaitBase, wholePlace(binding))
		if !consumed {
			continue
		}
		name := tc.bindingName(binding)
		if b := diag.ReportError(tc.reporter, diag.SemaSelectSendPayloadGivenTwice, payloadSpan,
			fmt.Sprintf("'%s' is given away by this arm, but another arm's await consumes it before the `%s` "+
				"runs: every await is evaluated first, whichever arm wins, so the value would be staged "+
				"after it was freed", name, ledger.keyword)); b != nil {
			if moveSpan != (source.Span{}) {
				b.WithNote(moveSpan, fmt.Sprintf("'%s' is consumed here, before the %s runs", name, ledger.keyword))
			}
			if advice := tc.cloneAdviceFor(adviceSelectSendSecondArm, tc.bindingType(binding), name); advice.Help != "" {
				b.WithHelp(payloadSpan, advice.Help)
			}
			b.Emit()
		}
	}
}
