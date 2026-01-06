package vm_test

import (
	"regexp"
	"strings"
)

var timingLineRE = regexp.MustCompile(`^(parsed|diagnose|built|ran|executed) [0-9]+(\.[0-9])? ms$`)

func stripTimingLines(output string) string {
	if output == "" {
		return output
	}
	hasTrailing := strings.HasSuffix(output, "\n")
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	keep := make([]string, 0, len(lines))
	for _, line := range lines {
		if timingLineRE.MatchString(strings.TrimSpace(line)) {
			continue
		}
		keep = append(keep, line)
	}
	if len(keep) == 0 {
		return ""
	}
	out := strings.Join(keep, "\n")
	if hasTrailing {
		out += "\n"
	}
	return out
}
