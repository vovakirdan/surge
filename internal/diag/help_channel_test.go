package diag

import (
	"strings"
	"testing"

	"surge/internal/source"
)

func helpTestSpan() source.Span {
	return source.Span{File: 0, Start: 0, End: 4}
}

// TestHelpSurvivesTheReporterChain is the plumbing proof.
//
// Help was added to Diagnostic after Reporter's parameter list was fixed, so
// every wrapper on the way to the bag had a chance to drop it silently — the
// diagnostic would still arrive, still carry its message, and simply have lost
// the one line telling the author what to do.
func TestHelpSurvivesTheReporterChain(t *testing.T) {
	bag := NewBag(8)
	chain := NewDedupReporter(BagReporter{Bag: bag})
	b := ReportError(chain, SemaTypeNotClonable, helpTestSpan(), "headline")
	b.WithNote(helpTestSpan(), "this is the rule")
	b.WithHelp(helpTestSpan(), "this is the way out")
	b.Emit()

	items := bag.Items()
	if len(items) != 1 {
		t.Fatalf("bag holds %d diagnostics, want one", len(items))
	}
	if len(items[0].Notes) != 1 || items[0].Notes[0].Msg != "this is the rule" {
		t.Fatalf("notes = %+v", items[0].Notes)
	}
	if len(items[0].Help) != 1 || items[0].Help[0].Msg != "this is the way out" {
		t.Fatalf("help = %+v", items[0].Help)
	}
}

// TestDedupIgnoresHelpWhenDeciding keeps the new channel out of identity.
//
// Two reports that agree on code, severity, span and message are one
// diagnostic however differently they were advised. Letting Help into the key
// would turn a single mistake into two entries the moment one path had more to
// say about it.
func TestDedupIgnoresHelpWhenDeciding(t *testing.T) {
	bag := NewBag(8)
	chain := NewDedupReporter(BagReporter{Bag: bag})
	for _, help := range []string{"first advice", "second advice"} {
		b := ReportError(chain, SemaTypeNotClonable, helpTestSpan(), "headline")
		b.WithHelp(helpTestSpan(), help)
		b.Emit()
	}
	if items := bag.Items(); len(items) != 1 {
		t.Fatalf("differently advised duplicates became %d diagnostics", len(items))
	}
}

// TestGoldenFormatHidesHelpWithoutNotes is the Global Rule 12 guard.
//
// `.diag` goldens are generated without notes, so the corpus pins headlines.
// Help must ride exactly that switch: if it leaked into the default rendering,
// every future friendly-diagnostic improvement would rewrite fixtures it has
// nothing to do with, and the corpus would stop being a regression detector.
func TestGoldenFormatHidesHelpWithoutNotes(t *testing.T) {
	fs := source.NewFileSet()
	fs.AddVirtual("probe.sg", []byte("line one\n"))
	d := NewError(SemaTypeNotClonable, helpTestSpan(), "headline")
	d.WithNote(helpTestSpan(), "explanatory note")
	d.WithHelp(helpTestSpan(), "actionable help")

	withoutNotes := FormatGoldenDiagnostics([]*Diagnostic{d}, fs, false)
	if strings.Contains(withoutNotes, "actionable help") || strings.Contains(withoutNotes, "explanatory note") {
		t.Fatalf("the default golden rendering carries auxiliary channels:\n%s", withoutNotes)
	}
	if !strings.Contains(withoutNotes, "headline") {
		t.Fatalf("the default golden rendering lost the headline:\n%s", withoutNotes)
	}

	withNotes := FormatGoldenDiagnostics([]*Diagnostic{d}, fs, true)
	if !strings.Contains(withNotes, "actionable help") {
		t.Fatalf("asking for notes did not surface help:\n%s", withNotes)
	}
	if !strings.Contains(withNotes, "help ") {
		t.Fatalf("help is not labelled as help:\n%s", withNotes)
	}
}
