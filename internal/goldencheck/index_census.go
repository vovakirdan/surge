package goldencheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

// verifyIndexCensus covers entries that status can hide, including empty
// directories and absent skip-worktree files. Git stores files; their parent
// directories are the only directories a fresh checkout can reproduce.
func verifyIndexCensus(ctx context.Context, repoRoot string, snapshot Snapshot) error {
	args := []string{
		"-C", repoRoot, "--literal-pathspecs", "ls-files", "--cached", "--full-name", "-z", "--", goldenRootPath,
	}
	// #nosec G204 -- the executable and literal pathspec are fixed.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = cleanGitEnvironment(os.Environ())
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("git ls-files: %w: %s", err, bytes.TrimSpace(exitErr.Stderr))
		}
		return fmt.Errorf("git ls-files: %w", err)
	}
	indexed, err := parseIndexCensus(output)
	if err != nil {
		return err
	}
	var differences []error
	for _, entry := range snapshot.Entries {
		kind, exists := indexed[entry.Path]
		switch {
		case !exists:
			differences = append(differences, fmt.Errorf("filesystem entry not indexed: %q", entry.Path))
		case kind != entry.Kind:
			differences = append(differences, fmt.Errorf("indexed entry %q is %s, filesystem has %s", entry.Path, kind, entry.Kind))
		}
		delete(indexed, entry.Path)
	}
	missing := make([]string, 0, len(indexed))
	for filename := range indexed {
		missing = append(missing, filename)
	}
	sort.Strings(missing)
	for _, filename := range missing {
		differences = append(differences, fmt.Errorf("missing indexed entry: %q", filename))
	}
	if err := errors.Join(differences...); err != nil {
		return fmt.Errorf("golden index census: %w", err)
	}
	return nil
}

func parseIndexCensus(output []byte) (map[string]string, error) {
	indexed := make(map[string]string)
	for len(output) != 0 {
		record, rest, ok := bytes.Cut(output, []byte{0})
		if !ok {
			return nil, fmt.Errorf("git ls-files returned a non-NUL-terminated record")
		}
		output = rest
		filename, ok := strings.CutPrefix(string(record), goldenRootPath+"/")
		if !ok || filename == "" || filename == "." || filename == ".." || path.IsAbs(filename) || path.Clean(filename) != filename || strings.HasPrefix(filename, "../") {
			return nil, fmt.Errorf("git ls-files returned an invalid golden path %q", record)
		}
		if _, exists := indexed[filename]; exists {
			return nil, fmt.Errorf("git ls-files returned duplicate or conflicting path %q", record)
		}
		indexed[filename] = "file"
		indexed["."] = "directory"
		for parent := path.Dir(filename); parent != "."; parent = path.Dir(parent) {
			if indexed[parent] == "file" {
				return nil, fmt.Errorf("git ls-files returned file/directory conflict at %q", parent)
			}
			indexed[parent] = "directory"
		}
	}
	return indexed, nil
}
