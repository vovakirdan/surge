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
	// MigrationTracked counts the carriers this epic introduced that are still
	// present. It is reported rather than asserted on: they are known and
	// scheduled, so they are not a mismatch — but a wave that grows this number
	// should have to see it.
	MigrationTracked int
}

// Empty reports whether the exact census still matches.
//
// A tracked migration carrier does not make it non-empty. It is expected to be
// there, and it is expected to leave.
func (difference *Difference) Empty() bool {
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
	migration := make(map[findingKey]Finding)
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
		// The exact-base comparison scans the base commit, where none of these
		// existed, so it does not consider them at all.
		if requireLegacy {
			continue
		}
		for i := range category.Migration {
			finding := &category.Migration[i].Finding
			migration[keyFor(finding)] = *finding
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
		if _, tracked := migration[key]; tracked {
			difference.MigrationTracked++
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
func FormatDifference(difference *Difference) string {
	var out strings.Builder
	write := func(label string, findings []Finding) {
		for i := range findings {
			fmt.Fprintf(&out, "%s: %s\n", label, formatFinding(&findings[i]))
		}
	}
	write("unexpected", difference.Unexpected)
	write("stale legacy", difference.Stale)
	write("stale allow", difference.StaleAllow)
	// Reported even when nothing is wrong, so that growing the tracked set is
	// something a reader sees rather than something a diff hides.
	if difference.MigrationTracked > 0 {
		fmt.Fprintf(&out, "migration carriers still present: %d\n", difference.MigrationTracked)
	}
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
	// Asked before make is, because make answers a MISSING makefile with a
	// database that looks structurally complete - it prints an empty `# Files`
	// section and fails on stderr - so the probe would report "no such target"
	// for every target of a file it never read. A false negative in a gate built
	// to catch false positives is the worst possible failure here.
	if _, statErr := os.Stat(makefile); statErr != nil {
		return false, fmt.Errorf("query make database: %w", statErr)
	}
	command := exec.Command("make", "-rRpn", "-f", filepath.Base(makefile), ":") // #nosec G204 -- target is not passed to make
	command.Dir = filepath.Dir(makefile)
	command.Env = []string{"LC_ALL=C", "PATH=" + os.Getenv("PATH")}
	database, err := command.Output()
	// A NON-ZERO EXIT IS EXPECTED AND IRRELEVANT HERE. `:` is a goal that does
	// not exist, chosen so make dumps its database without running anything.
	// While the Makefile had an unconditional catch-all, every goal "succeeded"
	// and this read as exit 0; narrowing that rule (RV2-DEBT-200) gave make back
	// the ability to refuse `:` — which is the whole point of the change and
	// broke this probe, because it was reading the exit code of a question it was
	// not asking. make prints the database to stdout BEFORE failing on the goal,
	// so the answer is in `database` either way. What must not be tolerated is an
	// empty or truncated dump, which means make never got far enough to answer.
	if err != nil && !makeDatabaseIsComplete(database) {
		return false, fmt.Errorf("query make database: %w", err)
	}
	return makeDatabaseHasTarget(database, target), nil
}

// makeDatabaseIsComplete reports whether make got far enough to print the file
// section its answer lives in. It is the difference between "make refused the
// dummy goal after telling us everything" and "make died before it knew
// anything", which the exit code alone cannot distinguish.
func makeDatabaseIsComplete(database []byte) bool {
	return bytes.Contains(database, []byte("\n# Files\n"))
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
