package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var gitLocalEnvVars = map[string]struct{}{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": {},
	"GIT_COMMON_DIR":                   {},
	"GIT_DIR":                          {},
	"GIT_GRAFT_FILE":                   {},
	"GIT_IMPLICIT_WORK_TREE":           {},
	"GIT_INDEX_FILE":                   {},
	"GIT_INTERNAL_SUPER_PREFIX":        {},
	"GIT_NO_REPLACE_OBJECTS":           {},
	"GIT_OBJECT_DIRECTORY":             {},
	"GIT_PREFIX":                       {},
	"GIT_QUARANTINE_PATH":              {},
	"GIT_REPLACE_REF_BASE":             {},
	"GIT_SHALLOW_FILE":                 {},
	"GIT_WORK_TREE":                    {},
}

type moduleSyncState string

const (
	moduleSyncInstalled moduleSyncState = "installed"
	moduleSyncUpdated   moduleSyncState = "updated"
	moduleSyncUnchanged moduleSyncState = "unchanged"
)

type moduleSyncResult struct {
	State     moduleSyncState
	BeforeRev string
	AfterRev  string
}

func syncGitModule(projectRoot, name, url, dest string) (moduleSyncResult, error) {
	info, statErr := os.Stat(dest)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		if err := gitClone(projectRoot, url, dest); err != nil {
			return moduleSyncResult{}, err
		}
		if err := validateModuleRepo(name, dest); err != nil {
			if rmErr := os.RemoveAll(dest); rmErr != nil {
				return moduleSyncResult{}, fmt.Errorf("cleanup failed after error: %w", errors.Join(err, rmErr))
			}
			return moduleSyncResult{}, err
		}
		rev, revErr := gitHeadRevision(dest)
		if revErr != nil {
			return moduleSyncResult{}, revErr
		}
		return moduleSyncResult{
			State:    moduleSyncInstalled,
			AfterRev: rev,
		}, nil
	case statErr != nil:
		return moduleSyncResult{}, fmt.Errorf("failed to stat %s: %w", filepath.Join("deps", name), statErr)
	case !info.IsDir():
		return moduleSyncResult{}, fmt.Errorf("%s exists and is not a directory", filepath.Join("deps", name))
	}

	if err := ensureGitWorktree(dest); err != nil {
		return moduleSyncResult{}, err
	}
	if err := ensureOriginURL(dest, url); err != nil {
		return moduleSyncResult{}, err
	}
	if err := ensureCleanWorktree(dest); err != nil {
		return moduleSyncResult{}, err
	}
	if err := ensureAttachedBranch(dest); err != nil {
		return moduleSyncResult{}, err
	}
	if err := ensureUpstream(dest); err != nil {
		return moduleSyncResult{}, err
	}

	beforeRev, revErr := gitHeadRevision(dest)
	if revErr != nil {
		return moduleSyncResult{}, revErr
	}
	if pullErr := gitPullFFOnly(dest); pullErr != nil {
		return moduleSyncResult{}, pullErr
	}
	if validateErr := validateModuleRepo(name, dest); validateErr != nil {
		return moduleSyncResult{}, validateErr
	}
	afterRev, revErr := gitHeadRevision(dest)
	if revErr != nil {
		return moduleSyncResult{}, revErr
	}

	state := moduleSyncUpdated
	if beforeRev == afterRev {
		state = moduleSyncUnchanged
	}
	return moduleSyncResult{
		State:     state,
		BeforeRev: beforeRev,
		AfterRev:  afterRev,
	}, nil
}

func gitClone(projectRoot, url, dest string) error {
	if _, err := gitRun(projectRoot, "clone", url, dest); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

func ensureGitWorktree(repoPath string) error {
	out, err := gitRun(repoPath, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return fmt.Errorf("%s is not a git worktree", repoPath)
	}
	if strings.TrimSpace(out) != "true" {
		return fmt.Errorf("%s is not a git worktree", repoPath)
	}
	return nil
}

func ensureOriginURL(repoPath, wantURL string) error {
	gotURL, err := gitRun(repoPath, "config", "--get", "remote.origin.url")
	if err != nil {
		return fmt.Errorf("git remote.origin.url lookup failed: %w", err)
	}
	gotURL = strings.TrimSpace(gotURL)
	wantURL = strings.TrimSpace(wantURL)
	if gotURL != wantURL {
		return fmt.Errorf("remote.origin.url mismatch: got %q, want %q", gotURL, wantURL)
	}
	return nil
}

func ensureCleanWorktree(repoPath string) error {
	out, err := gitRun(repoPath, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("module repository has tracked local changes; commit or stash them before update")
	}
	return nil
}

func ensureAttachedBranch(repoPath string) error {
	if _, err := gitRun(repoPath, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
		return fmt.Errorf("module repository is in detached HEAD state; checkout a branch before update")
	}
	return nil
}

func ensureUpstream(repoPath string) error {
	if _, err := gitRun(repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		return fmt.Errorf("module repository has no upstream tracking branch; configure upstream before update")
	}
	return nil
}

func gitHeadRevision(repoPath string) (string, error) {
	out, err := gitRun(repoPath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func gitPullFFOnly(repoPath string) error {
	if _, err := gitRun(repoPath, "pull", "--ff-only"); err != nil {
		return fmt.Errorf("git pull --ff-only failed: %w", err)
	}
	return nil
}

func shortRev(rev string) string {
	rev = strings.TrimSpace(rev)
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

func gitRun(dir string, args ...string) (string, error) {
	// #nosec G204 -- exec.Command does not invoke a shell; argv entries are passed directly.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitRunEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

func gitRunEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, item := range env {
		name := item
		if idx := strings.IndexByte(item, '='); idx >= 0 {
			name = item[:idx]
		}
		if _, ok := gitLocalEnvVars[name]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
