package goldencheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitChange is one NUL-delimited porcelain status entry.
type GitChange struct {
	Code     string
	Path     string
	OrigPath string
}

// GitChanges returns staged, unstaged, missing, untracked, and ignored paths.
func GitChanges(ctx context.Context, repoRoot, goldenRoot string) ([]GitChange, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("absolute repository root: %w", err)
	}
	goldenRoot, err = filepath.Abs(goldenRoot)
	if err != nil {
		return nil, fmt.Errorf("absolute golden root: %w", err)
	}
	rel, err := filepath.Rel(repoRoot, goldenRoot)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("golden root %q is outside repository %q", goldenRoot, repoRoot)
	}
	args := []string{
		"-C", repoRoot, "--literal-pathspecs", "status", "--porcelain=v1", "-z",
		"--untracked-files=all", "--ignored=matching", "--", filepath.ToSlash(rel),
	}
	// #nosec G204 -- the executable is fixed and the literal pathspec is validated above.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = cleanGitEnvironment(os.Environ())
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("git status: %w: %s", err, bytes.TrimSpace(exitErr.Stderr))
		}
		return nil, fmt.Errorf("git status: %w", err)
	}
	return parsePorcelain(output)
}

func cleanGitEnvironment(environment []string) []string {
	clean := make([]string, 0, len(environment))
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		if !strings.HasPrefix(name, "GIT_") {
			clean = append(clean, variable)
		}
	}
	return clean
}

func parsePorcelain(output []byte) ([]GitChange, error) {
	var changes []GitChange
	for len(output) != 0 {
		record, rest, ok := bytes.Cut(output, []byte{0})
		if !ok {
			return nil, fmt.Errorf("git status returned a non-NUL-terminated record")
		}
		output = rest
		if len(record) < 4 || record[2] != ' ' {
			return nil, fmt.Errorf("malformed git status record %q", record)
		}
		change := GitChange{Code: string(record[:2]), Path: string(record[3:])}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			original, remaining, ok := bytes.Cut(output, []byte{0})
			if !ok {
				return nil, fmt.Errorf("git status rename lacks original path")
			}
			change.OrigPath = string(original)
			output = remaining
		}
		changes = append(changes, change)
	}
	return changes, nil
}
