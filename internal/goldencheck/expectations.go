package goldencheck

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	expectationVersion = 1
	goldenRootPath     = "testdata/golden"
)

// Expectations freezes the corpus digest and every tolerated tool outcome.
type Expectations struct {
	Version          int                `json:"version"`
	GoldenRoot       string             `json:"golden_root"`
	EntryCount       int                `json:"entry_count"`
	CorpusSHA256     string             `json:"corpus_sha256"`
	DiagnosticZero   []string           `json:"diagnostic_zero"`
	FormatterFailure []string           `json:"formatter_failure"`
	EmitFailure      []PhaseExpectation `json:"emit_failure"`
}

// PhaseExpectation is one intentionally nonzero emit command.
type PhaseExpectation struct {
	Phase string `json:"phase"`
	Path  string `json:"path"`
}

// LoadExpectations reads and validates the frozen expectation contract.
func LoadExpectations(filename string) (Expectations, error) {
	// #nosec G304 -- the caller supplies the repository-owned manifest path.
	data, err := os.ReadFile(filename)
	if err != nil {
		return Expectations{}, fmt.Errorf("read expectations: %w", err)
	}
	var expectations Expectations
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&expectations); err != nil {
		return Expectations{}, fmt.Errorf("decode expectations: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Expectations{}, fmt.Errorf("decode expectations: trailing JSON content")
	}
	if expectations.Version != expectationVersion {
		return Expectations{}, fmt.Errorf("expectations version is %d, want %d", expectations.Version, expectationVersion)
	}
	if expectations.GoldenRoot != goldenRootPath {
		return Expectations{}, fmt.Errorf("golden_root must be %q", goldenRootPath)
	}
	if expectations.CorpusSHA256 != "" {
		digest, err := hex.DecodeString(expectations.CorpusSHA256)
		if err != nil || len(digest) != 32 || expectations.CorpusSHA256 != strings.ToLower(expectations.CorpusSHA256) {
			return Expectations{}, fmt.Errorf("corpus_sha256 must be 64 lowercase hexadecimal characters")
		}
	}
	for name, paths := range map[string][]string{
		"diagnostic_zero":   expectations.DiagnosticZero,
		"formatter_failure": expectations.FormatterFailure,
	} {
		if err := validatePathList(paths); err != nil {
			return Expectations{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := validatePhaseExpectations(expectations.EmitFailure); err != nil {
		return Expectations{}, fmt.Errorf("emit_failure: %w", err)
	}
	return expectations, nil
}

func validatePhaseExpectations(expectations []PhaseExpectation) error {
	allowed := map[string]bool{"hir": true, "hir_borrow": true, "instantiations": true, "mono": true, "mir": true}
	for i, expectation := range expectations {
		if !allowed[expectation.Phase] {
			return fmt.Errorf("entry %d has unknown phase %q", i, expectation.Phase)
		}
		if err := validateRelativePath(expectation.Path); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if path.Ext(expectation.Path) != ".sg" {
			return fmt.Errorf("entry %q is not a .sg source", expectation.Path)
		}
		if !isInvalidFixture(expectation.Path) {
			return fmt.Errorf("entry %q is not under an invalid fixture directory", expectation.Path)
		}
		if i != 0 && !phaseExpectationLess(expectations[i-1], expectation) {
			return fmt.Errorf("entries must be unique and sorted")
		}
	}
	return nil
}

func phaseExpectationLess(left, right PhaseExpectation) bool {
	return left.Phase < right.Phase || left.Phase == right.Phase && left.Path < right.Path
}

func validatePathList(paths []string) error {
	for i, filename := range paths {
		if err := validateRelativePath(filename); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if path.Ext(filename) != ".sg" {
			return fmt.Errorf("entry %q is not a .sg source", filename)
		}
		if !isInvalidFixture(filename) {
			return fmt.Errorf("entry %q is not under an invalid fixture directory", filename)
		}
		if i != 0 && paths[i-1] >= filename {
			return fmt.Errorf("entries must be unique and sorted: %q then %q", paths[i-1], filename)
		}
	}
	return nil
}

func isInvalidFixture(filename string) bool {
	return strings.HasPrefix(filename, "invalid/") || strings.Contains(filename, "/invalid/")
}

func validateRelativePath(filename string) error {
	if filename == "" || strings.ContainsRune(filename, 0) {
		return fmt.Errorf("path is empty or contains NUL")
	}
	if path.IsAbs(filename) || path.Clean(filename) != filename || filename == "." || strings.HasPrefix(filename, "../") {
		return fmt.Errorf("path %q is not a canonical relative slash path", filename)
	}
	if strings.Contains(filename, `\`) {
		return fmt.Errorf("path %q contains a non-portable backslash", filename)
	}
	return nil
}

// VerifyCorpus checks the reviewed path, mode, kind, and content digest.
func (expectations *Expectations) VerifyCorpus(snapshot Snapshot) error {
	if expectations.CorpusSHA256 == "" {
		return fmt.Errorf("corpus_sha256 is not frozen; run golden-update")
	}
	if len(snapshot.Entries) != expectations.EntryCount {
		return fmt.Errorf("golden entry count is %d, want %d", len(snapshot.Entries), expectations.EntryCount)
	}
	if digest := snapshot.Digest(); digest != expectations.CorpusSHA256 {
		return fmt.Errorf("golden corpus digest is %s, want %s", digest, expectations.CorpusSHA256)
	}
	return nil
}

// WriteExpectations atomically rewrites only the frozen manifest file.
func WriteExpectations(filename string, expectations *Expectations) (returnErr error) {
	expectations.Version = expectationVersion
	sort.Strings(expectations.DiagnosticZero)
	sort.Strings(expectations.FormatterFailure)
	sort.Slice(expectations.EmitFailure, func(i, j int) bool {
		return phaseExpectationLess(expectations.EmitFailure[i], expectations.EmitFailure[j])
	})
	data, err := json.MarshalIndent(expectations, "", "  ")
	if err != nil {
		return fmt.Errorf("encode expectations: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(filename)
	temp, err := os.CreateTemp(dir, ".golden-expectations-*")
	if err != nil {
		return fmt.Errorf("create expectations temporary file: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		removeErr := os.Remove(tempName)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove expectations temporary file: %w", removeErr))
		}
	}()
	err = temp.Chmod(0o644)
	if err == nil {
		_, err = temp.Write(data)
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write expectations: %w", err)
	}
	if err := os.Rename(tempName, filename); err != nil {
		return fmt.Errorf("replace expectations: %w", err)
	}
	return nil
}
