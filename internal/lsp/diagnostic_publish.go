package lsp

import (
	"fmt"
	"sort"

	"surge/internal/driver/diagnose"
)

// buildPublishedDiagnostics turns one analysis into the per-URI sets the server
// publishes.
//
// The compiler assembles notes, help and fixes for every diagnostic, and this
// used to keep the message and the range and drop the rest. What a client shows
// is not the compiler's decision, but what it is GIVEN is, and giving it the
// message alone made the language server the least informative of the three
// user-facing paths.
func buildPublishedDiagnostics(plan analysisPlan, diags []diagnose.Diagnostic) map[string][]lspDiagnostic {
	grouped := make(map[string][]lspDiagnostic, len(diags))
	for i := range diags {
		d := &diags[i]
		uri := pathToURI(d.FilePath)
		if uri == "" {
			continue
		}
		entry := lspDiagnostic{
			Range:    diagnosticRange(d.StartLine, d.StartCol, d.EndLine, d.EndCol),
			Severity: d.Severity,
			Code:     d.Code,
			Source:   "surge",
			Message:  d.Message,
		}
		entry.RelatedInformation = relatedInformationFor(d)
		entry.Data = diagnosticDataFor(plan, uri, d, i)
		grouped[uri] = append(grouped[uri], entry)
	}
	return grouped
}

// relatedInformationFor maps both auxiliary channels onto the one field LSP
// gives us.
//
// Notes come before help, which is the order they are meant to be read in: the
// explanation, then the way out. LSP has no way to label the two apart, so the
// order is the only distinction that survives the protocol and it is kept
// deliberate rather than incidental.
func relatedInformationFor(d *diagnose.Diagnostic) []diagnosticRelated {
	total := len(d.Notes) + len(d.Help)
	if total == 0 {
		return nil
	}
	out := make([]diagnosticRelated, 0, total)
	for _, channel := range [][]diagnose.RelatedLocation{d.Notes, d.Help} {
		for _, entry := range channel {
			uri := pathToURI(entry.FilePath)
			if uri == "" {
				uri = pathToURI(d.FilePath)
			}
			out = append(out, diagnosticRelated{
				Location: lspLocation{
					URI:   uri,
					Range: diagnosticRange(entry.StartLine, entry.StartCol, entry.EndLine, entry.EndCol),
				},
				Message: entry.Message,
			})
		}
	}
	return out
}

// diagnosticDataFor stamps the identity a later Code Action request is checked
// against.
//
// Nothing here is an edit, and nothing here is trusted on the way back: it says
// which analysis, which document, which version and which fixes existed. The
// server holds the snapshot and the guards, so a replayed or forged value can
// only fail to match.
func diagnosticDataFor(plan analysisPlan, uri string, d *diagnose.Diagnostic, ordinal int) *diagnosticData {
	state, known := plan.docs[uri]
	if !known {
		// A diagnostic in a document this analysis did not track has no version
		// to bind an action to, so it is published without one.
		return nil
	}
	return &diagnosticData{
		AnalysisID:   plan.seq,
		SnapshotID:   uint64(state.snapshotID), //nolint:gosec // an id, not an arithmetic value
		URI:          uri,
		Version:      state.version,
		DiagnosticID: diagnosticIdentity(d, ordinal),
		FixIDs:       fixIDsOf(d.Fixes, false),
		SafeFixIDs:   fixIDsOf(d.Fixes, true),
	}
}

// diagnosticIdentity names one diagnostic within one analysis.
//
// It is built from what the diagnostic IS — its code and where it points —
// plus its ordinal, so that two identical diagnostics at one position stay
// distinguishable and the same program produces the same ids twice running.
func diagnosticIdentity(d *diagnose.Diagnostic, ordinal int) string {
	return fmt.Sprintf("%s:%d:%d:%d", d.Code, d.StartLine, d.StartCol, ordinal)
}

// fixIDsOf names the attached edits, optionally only the always-safe ones.
// Only ids cross the wire; the edits stay in the server's registry.
func fixIDsOf(offers []diagnose.FixOffer, safeOnly bool) []string {
	out := make([]string, 0, len(offers))
	for i := range offers {
		if safeOnly && !offers[i].AlwaysSafe {
			continue
		}
		out = append(out, offers[i].ID)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func diagnosticRange(startLine, startCol, endLine, endCol int) lspRange {
	start := position{Line: maxZero(startLine - 1), Character: maxZero(startCol - 1)}
	end := position{Line: maxZero(endLine - 1), Character: maxZero(endCol - 1)}
	if endLine == 0 && endCol == 0 {
		end = start
	}
	return lspRange{Start: start, End: end}
}

// publishTargets lists the documents this analysis is responsible for, in a
// stable order so two runs publish in the same sequence.
func publishTargets(plan analysisPlan) []string {
	targets := make([]string, 0, len(plan.docs))
	for uri := range plan.docs {
		targets = append(targets, uri)
	}
	sort.Strings(targets)
	return targets
}
