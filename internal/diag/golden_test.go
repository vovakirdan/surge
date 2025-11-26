package diag

import (
	"testing"

	"surge/internal/source"
)

func TestFormatGoldenDiagnostics(t *testing.T) {
	fs := source.NewFileSet()
	fs.SetBaseDir("/workspace")

	userFile := fs.Add("/workspace/testdata/golden/sample.sg", []byte("a\nb\n"), 0)
	internalFile := fs.Add("/workspace/internal/helper.sg", []byte("x\n"), 0)

	diags := []*Diagnostic{
		{
			Severity: SevError,
			Code:     SynUnexpectedToken,
			Message:  "first line\nsecond",
			Primary:  source.Span{File: userFile, Start: 0, End: 1},
			Notes: []Note{
				{Span: source.Span{File: internalFile, Start: 0, End: 0}, Msg: "skip me"},
				{Span: source.Span{File: userFile, Start: 2, End: 3}, Msg: "note line"},
			},
		},
		{
			Severity: SevWarning,
			Code:     SemaError,
			Message:  "another",
			Primary:  source.Span{File: userFile, Start: 2, End: 3},
		},
	}

	expected := "error SYN2001 testdata/golden/sample.sg:1:1 first line second\n" +
		"note SYN2001 testdata/golden/sample.sg:2:1 note line\n" +
		"warning SEM3001 testdata/golden/sample.sg:2:1 another"

	if got := FormatGoldenDiagnostics(diags, fs, true); got != expected {
		t.Fatalf("unexpected golden diagnostics:\nwant:\n%s\n\ngot:\n%s", expected, got)
	}
}
