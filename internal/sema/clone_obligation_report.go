package sema

import (
	"fmt"
	"strings"

	"surge/internal/diag"
	"surge/internal/source"
)

// reportCloneObligation builds the diagnostic for one unmet obligation.
//
// One builder serves the concrete use and the instantiated generic one, which
// is what makes "the two paths report the same diagnostic" a property of the
// code rather than a claim two call sites happen to agree on today. The generic
// path adds its two witness notes and changes nothing else.
//
// No fix is attached, ever. The compiler cannot invent a clone body, and adding
// `@copy` would change what every other use of the type means; an edit that
// large is the author's to make. Ambiguous advice stays a note.
func reportCloneObligation(
	c *CapabilityClassifier,
	obligation *CloneObligation,
	evidence *CloneEvidence,
) error {
	spec, known := obligation.Op.spec()
	if !known {
		return fmt.Errorf("clone obligation %d has no diagnostic spec", uint8(obligation.Op))
	}
	container := obligation.ContainerLabel
	if container == "" {
		container = "this value"
	} else {
		container = "`" + container + "`"
	}
	diagnostic := &diag.Diagnostic{
		Severity: diag.SevError,
		Code:     spec.code,
		Primary:  obligation.Site,
		Message:  fmt.Sprintf(spec.headline, container, obligation.SubjectLabel),
	}
	appendCloneObligationNotes(c, diagnostic, obligation, evidence, spec)
	return &CloneObligationError{diagnostic: diagnostic}
}

func appendCloneObligationNotes(
	c *CapabilityClassifier,
	diagnostic *diag.Diagnostic,
	obligation *CloneObligation,
	evidence *CloneEvidence,
	spec cloneObligationSpec,
) {
	site := obligation.Site
	if obligation.InstantiationSite != (source.Span{}) {
		// The template is legal on its own and other instantiations of it still
		// compile. Saying so, and pointing at the substitution that broke this
		// one, is the difference between a diagnostic about this call and one
		// that reads as if the generic itself were wrong.
		diagnostic.Notes = append(diagnostic.Notes,
			diag.Note{
				Span: site,
				Msg: fmt.Sprintf("the generic %s is legal on its own; instantiations with a clonable type still compile",
					obligation.Op),
			},
			diag.Note{
				Span: obligation.InstantiationSite,
				Msg:  fmt.Sprintf("`%s` arrives from this instantiation", obligation.SubjectLabel),
			},
		)
	}
	diagnostic.Notes = append(diagnostic.Notes, diag.Note{Span: site, Msg: evidence.Reason})
	if path := cloneRefusalPathLabels(c, evidence); path != "" {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: site,
			Msg:  fmt.Sprintf("the first component that cannot be cloned is reached through %s", path),
		})
	}
	if evidence.Decl != (source.Span{}) {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: evidence.Decl,
			Msg:  fmt.Sprintf("a `__clone` claiming `%s` is declared here", obligation.SubjectLabel),
		})
	}
	diagnostic.Help = append(diagnostic.Help, diag.Note{Span: site, Msg: spec.consumeHelp})
	if evidence.CanDefineHere {
		// The sentence comes from the shared renderer, so the signature it
		// teaches is the same one every other emitter teaches.
		diagnostic.Help = append(diagnostic.Help, diag.Note{
			Span: site,
			Msg:  cloneDefinitionHelp(obligation.SubjectLabel),
		})
	}
}

// cloneRefusalPathLabels renders the root-to-culprit chain, and nothing at all
// when the refusal is the subject's own.
//
// A one-element path would render as the type naming itself, which the headline
// has already said. Repeating it would spend a note to say nothing.
func cloneRefusalPathLabels(c *CapabilityClassifier, evidence *CloneEvidence) string {
	if c == nil || len(evidence.Path) < 2 {
		return ""
	}
	return strings.Join(c.labels(evidence.Path), " -> ")
}
