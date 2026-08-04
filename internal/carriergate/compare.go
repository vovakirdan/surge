package carriergate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type findingKey struct {
	Category string
	Path     string
	Token    string
	Evidence string
	Ordinal  uint64
}

// Difference describes every reviewed-baseline mismatch.
type Difference struct {
	Unexpected []Finding
	Stale      []Finding
	StaleAllow []Finding
}

// Empty reports whether the exact census still matches.
func (difference Difference) Empty() bool {
	return len(difference.Unexpected) == 0 && len(difference.Stale) == 0 && len(difference.StaleAllow) == 0
}

// Compare applies the live monotonic ratchet. Legacy removals are progress;
// new identities and stale allowances are failures.
func Compare(manifest *Manifest, actual []Finding) Difference {
	return compare(manifest, actual, false)
}

// CompareExact requires every frozen base finding to remain present.
func CompareExact(manifest *Manifest, actual []Finding) Difference {
	return compare(manifest, actual, true)
}

func compare(manifest *Manifest, actual []Finding, requireLegacy bool) Difference {
	legacy := make(map[findingKey]Finding, manifest.BaselineCount)
	allowed := make(map[findingKey]Finding)
	for categoryIndex := range manifest.Categories {
		category := &manifest.Categories[categoryIndex]
		for i := range category.Legacy {
			finding := &category.Legacy[i]
			legacy[keyFor(finding)] = *finding
		}
		for i := range category.Allow {
			finding := &category.Allow[i].Finding
			allowed[keyFor(finding)] = *finding
		}
	}
	observed := make(map[findingKey]Finding, len(actual))
	difference := Difference{}
	for i := range actual {
		finding := &actual[i]
		key := keyFor(finding)
		observed[key] = *finding
		if _, known := legacy[key]; known {
			continue
		}
		if _, safe := allowed[key]; !safe {
			difference.Unexpected = append(difference.Unexpected, *finding)
		}
	}
	if requireLegacy {
		for key, finding := range legacy {
			if _, exists := observed[key]; !exists {
				difference.Stale = append(difference.Stale, finding)
			}
		}
	}
	for key, finding := range allowed {
		if _, exists := observed[key]; !exists {
			difference.StaleAllow = append(difference.StaleAllow, finding)
		}
	}
	sortFindings(difference.Unexpected)
	sortFindings(difference.Stale)
	sortFindings(difference.StaleAllow)
	return difference
}

// FormatDifference renders actionable paths and current diagnostic lines.
func FormatDifference(difference Difference) string {
	var out strings.Builder
	write := func(label string, findings []Finding) {
		for i := range findings {
			fmt.Fprintf(&out, "%s: %s\n", label, formatFinding(&findings[i]))
		}
	}
	write("unexpected", difference.Unexpected)
	write("stale legacy", difference.Stale)
	write("stale allow", difference.StaleAllow)
	return strings.TrimSpace(out.String())
}

func keyFor(finding *Finding) findingKey {
	return findingKey{
		Category: finding.Category, Path: finding.Path, Token: finding.Token,
		Evidence: finding.Evidence, Ordinal: finding.Ordinal,
	}
}

func formatFinding(finding *Finding) string {
	location := finding.Path
	if finding.Line > 0 {
		location = fmt.Sprintf("%s:%d", location, finding.Line)
	}
	return fmt.Sprintf("%s %s token=%q ordinal=%d evidence=%q", finding.Category, location, finding.Token, finding.Ordinal, finding.Evidence)
}

var makeTargetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// HasExplicitMakeTarget checks GNU Make's database, not a target exit status.
func HasExplicitMakeTarget(makefile, target string) (bool, error) {
	if !makeTargetPattern.MatchString(target) {
		return false, fmt.Errorf("invalid make target %q", target)
	}
	command := exec.Command("make", "-rRpn", "-f", filepath.Base(makefile), ":") // #nosec G204 -- target is not passed to make
	command.Dir = filepath.Dir(makefile)
	command.Env = []string{"LC_ALL=C", "PATH=" + os.Getenv("PATH")}
	database, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("query make database: %w", err)
	}
	return makeDatabaseHasTarget(database, target), nil
}

func makeDatabaseHasTarget(database []byte, target string) bool {
	inFiles := false
	for _, rawLine := range bytes.Split(database, []byte{'\n'}) {
		line := string(rawLine)
		if line == "# Files" {
			inFiles = true
			continue
		}
		if inFiles && strings.HasPrefix(line, "# files hash-table stats:") {
			break
		}
		if inFiles && (line == target+":" || strings.HasPrefix(line, target+": ")) {
			return true
		}
	}
	return false
}
