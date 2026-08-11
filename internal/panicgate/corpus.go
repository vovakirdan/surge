package panicgate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Recorded is one panic report a behavioural fixture has on file.
type Recorded struct {
	Fixture string // e.g. "vm_arrays/arrays_positive_index_panics"
	Message string // normalised: the operands replaced by {}
	Code    string // "VM2105", or "" for a report with no code
}

// panicLineRe matches the first line of a panic report in a recorded .out:
// either `panic VM1101: integer overflow` or the codeless `panic: ...`.
var panicLineRe = regexp.MustCompile(`^panic(?: (VM\d+))?: (.*)$`)

// operandRe is what makes a recorded report comparable with a raise site. The
// bounds reporter formats its index and length into the text, so a fixture
// records `array index 9 out of range for length 3` while the site can only
// name the template. Both sides collapse their integers to {}.
var operandRe = regexp.MustCompile(`-?\b\d+\b`)

// NormaliseMessage collapses the integers a reporter formats into its text, so
// a template and a recorded instance of it compare equal.
func NormaliseMessage(msg string) string {
	return operandRe.ReplaceAllString(msg, "{}")
}

// ReadRecordedPanics collects every panic report the behavioural corpus has on
// file, from the fixtures that actually run and that run on BOTH backends.
//
// The both-backends condition is the point of the corpus: a report recorded on
// the VM alone says nothing about whether the native backend agrees, which is
// the disagreement this gate exists to prevent. A fixture excluded from the
// native lane by a .backends sidecar therefore does not count as coverage.
// A fixture the corpus does not execute is not coverage either, however
// complete its .out looks; DeadFixtures names those and they are dropped here
// as well as reported.
func ReadRecordedPanics(root string) ([]Recorded, error) {
	dead, err := DeadFixtures(root)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{}
	for _, d := range dead {
		skip[d.Fixture] = true
	}
	goldenDir := filepath.Join(root, "testdata", "golden")
	dirs, err := os.ReadDir(goldenDir)
	if err != nil {
		return nil, err
	}
	var out []Recorded
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		entries, readErr := os.ReadDir(filepath.Join(goldenDir, dir.Name()))
		if readErr != nil {
			return nil, readErr
		}
		for _, ent := range entries {
			name := ent.Name()
			if ent.IsDir() || !strings.HasSuffix(name, ".out") || strings.HasPrefix(name, "_") {
				continue
			}
			base := strings.TrimSuffix(name, ".out")
			if skip[dir.Name()+"/"+base] {
				continue
			}
			path := filepath.Join(goldenDir, dir.Name(), base)
			if !runsOnBothBackends(path + ".backends") {
				continue
			}
			raw, outErr := os.ReadFile(path + ".out") // #nosec G304 -- repository-owned path
			if outErr != nil {
				return nil, outErr
			}
			fixture := dir.Name() + "/" + base
			out = append(out, recordedIn(fixture, string(raw))...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Message != out[j].Message {
			return out[i].Message < out[j].Message
		}
		return out[i].Fixture < out[j].Fixture
	})
	return out, nil
}

func recordedIn(fixture, body string) []Recorded {
	var out []Recorded
	for _, line := range strings.Split(body, "\n") {
		m := panicLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		out = append(out, Recorded{Fixture: fixture, Message: NormaliseMessage(m[2]), Code: m[1]})
	}
	return out
}

// runsOnBothBackends reads a .backends sidecar. Its absence means the fixture
// runs everywhere, which is the corpus default.
func runsOnBothBackends(path string) bool {
	raw, err := os.ReadFile(path) // #nosec G304 -- repository-owned path
	if err != nil {
		return true
	}
	named := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		named[line] = true
	}
	return named["vm"] && named["llvm"]
}

// CoverageIndex maps a normalised message to the fixtures that record it.
func CoverageIndex(recorded []Recorded) map[string][]string {
	out := map[string][]string{}
	for _, r := range recorded {
		if len(out[r.Message]) > 0 && out[r.Message][len(out[r.Message])-1] == r.Fixture {
			continue
		}
		out[r.Message] = append(out[r.Message], r.Fixture)
	}
	return out
}
