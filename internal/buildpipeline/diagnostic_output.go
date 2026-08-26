package buildpipeline

import (
	"fmt"
	"io"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/source"
)

// sourceSpan keeps the signature below readable without importing the span
// spelling into every line.
type sourceSpan = source.Span

// printBuildDiagnostics writes what `surge build` owes its user.
//
// It used to write `d.Message` and nothing else, which meant a build reported
// the sentence and dropped the code, the position, every note the checker had
// assembled, the actionable help, and the titles of edits the compiler was
// prepared to make. Diagnostics that exist only inside the compiler are not
// diagnostics; Global Rule 11 asks for them at the user boundary, and this is
// the boundary `surge build --ui=off` goes through.
//
// Fix TITLES are printed, never the edits: build does not modify sources. The
// title is what tells the author that `surge fix` has something to offer here.
func printBuildDiagnostics(w io.Writer, res *driver.DiagnoseResult) {
	if res == nil || res.Bag == nil {
		return
	}
	for _, d := range res.Bag.Items() {
		if d == nil || d.Severity != diag.SevError {
			continue
		}
		fmt.Fprintf(w, "error %s %s %s\n", d.Code.ID(), buildSpanLabel(res, d.Primary), d.Message) //nolint:errcheck
		for _, note := range d.Notes {
			fmt.Fprintf(w, "  note %s %s\n", buildSpanLabel(res, note.Span), note.Msg) //nolint:errcheck
		}
		for _, help := range d.Help {
			fmt.Fprintf(w, "  help %s %s\n", buildSpanLabel(res, help.Span), help.Msg) //nolint:errcheck
		}
		printBuildFixTitles(w, res, d)
	}
}

func printBuildFixTitles(w io.Writer, res *driver.DiagnoseResult, d *diag.Diagnostic) {
	if len(d.Fixes) == 0 {
		return
	}
	fixes, err := diag.MaterializeFixes(diag.FixBuildContext{FileSet: res.FileSet}, d.Fixes)
	if err != nil {
		// A fix that cannot be built is not the build's failure to report; the
		// diagnostic above it still stands.
		return
	}
	for _, fix := range fixes {
		if fix == nil || fix.Title == "" {
			continue
		}
		fmt.Fprintf(w, "  fix [%s] %s\n", fix.Applicability.String(), fix.Title) //nolint:errcheck
	}
}

// buildSpanLabel renders `path:line:col` the way `surge diag` does.
//
// It asks HasFile rather than testing the id against zero: file 0 is a real
// file, and treating it as absent would hide every position in a single-file
// build. A span the set cannot answer for prints as `-`, because a diagnostic
// about the project as a whole has no position and inventing one would read as
// a location.
func buildSpanLabel(res *driver.DiagnoseResult, span sourceSpan) string {
	if res.FileSet == nil || !res.FileSet.HasFile(span.File) {
		return "-"
	}
	file := res.FileSet.Get(span.File)
	if file == nil {
		return "-"
	}
	start, _ := res.FileSet.Resolve(span)
	return fmt.Sprintf("%s:%d:%d", file.FormatPath("auto", res.FileSet.BaseDir()), start.Line, start.Col)
}
