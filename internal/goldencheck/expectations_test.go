package goldencheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpectationsRoundTripAndVerifyCorpus(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "case.sg"), "case", 0o644)
	snapshot, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	expectations := Expectations{
		Version:          expectationVersion,
		GoldenRoot:       "testdata/golden",
		EntryCount:       len(snapshot.Entries),
		CorpusSHA256:     snapshot.Digest(),
		DiagnosticZero:   []string{"z/invalid/case.sg", "a/invalid/case.sg"},
		FormatterFailure: []string{"invalid/fmt.sg"},
	}
	filename := filepath.Join(t.TempDir(), "expectations.json")
	if writeErr := WriteExpectations(filename, &expectations); writeErr != nil {
		t.Fatal(writeErr)
	}
	loaded, err := LoadExpectations(filename)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DiagnosticZero[0] != "a/invalid/case.sg" {
		t.Fatalf("paths were not sorted: %v", loaded.DiagnosticZero)
	}
	if err := loaded.VerifyCorpus(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestLoadExpectationsRejectsUnsafeAndAmbiguousPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "NUL", path: "invalid/a\x00b.sg"},
		{name: "parent", path: "../invalid/a.sg"},
		{name: "backslash", path: `invalid\a.sg`},
		{name: "not source", path: "invalid/a.fmt"},
		{name: "valid fixture", path: "valid/a.sg"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "expectations.json")
			body := `{"version":1,"golden_root":"testdata/golden","entry_count":0,"corpus_sha256":"","diagnostic_zero":[]}`
			body = strings.TrimSuffix(body, "}") + `,"formatter_failure":[` + quoteJSON(test.path) + `]}`
			if err := os.WriteFile(filename, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadExpectations(filename); err == nil {
				t.Fatalf("LoadExpectations accepted %q", test.path)
			}
		})
	}
}

func TestLoadExpectationsRejectsAlternateGoldenRoot(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "expectations.json")
	body := `{"version":1,"golden_root":"elsewhere","entry_count":0,"corpus_sha256":"","diagnostic_zero":[],"formatter_failure":[],"emit_failure":[]}`
	if err := os.WriteFile(filename, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExpectations(filename); err == nil || !strings.Contains(err.Error(), "testdata/golden") {
		t.Fatalf("alternate golden_root error = %v", err)
	}
}

func quoteJSON(value string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, char := range value {
		switch char {
		case 0:
			quoted.WriteString(`\u0000`)
		case '\\':
			quoted.WriteString(`\\`)
		case '"':
			quoted.WriteString(`\"`)
		default:
			quoted.WriteRune(char)
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}
