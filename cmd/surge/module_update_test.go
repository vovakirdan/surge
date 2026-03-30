package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncGitModuleInstallsMissingRepo(t *testing.T) {
	requireGit(t)

	projectRoot := t.TempDir()
	remote := createModuleRemote(t)
	dest := filepath.Join(projectRoot, "deps", "sigil")

	got, err := syncGitModule(projectRoot, "sigil", remote, dest)
	if err != nil {
		t.Fatalf("syncGitModule install: %v", err)
	}
	if got.State != moduleSyncInstalled {
		t.Fatalf("state = %q, want %q", got.State, moduleSyncInstalled)
	}
	if got.AfterRev == "" {
		t.Fatalf("AfterRev is empty")
	}
	if _, err := os.Stat(filepath.Join(dest, "surge.toml")); err != nil {
		t.Fatalf("expected cloned module manifest: %v", err)
	}
}

func TestSyncGitModuleUpdatesExistingRepo(t *testing.T) {
	requireGit(t)

	projectRoot := t.TempDir()
	remote := createModuleRemote(t)
	dest := filepath.Join(projectRoot, "deps", "sigil")

	first, err := syncGitModule(projectRoot, "sigil", remote, dest)
	if err != nil {
		t.Fatalf("initial syncGitModule: %v", err)
	}
	nextRev := pushModuleCommit(t, remote, "pub fn version() -> string { return \"v2\"; }\n")

	got, err := syncGitModule(projectRoot, "sigil", remote, dest)
	if err != nil {
		t.Fatalf("update syncGitModule: %v", err)
	}
	if got.State != moduleSyncUpdated {
		t.Fatalf("state = %q, want %q", got.State, moduleSyncUpdated)
	}
	if got.BeforeRev != first.AfterRev {
		t.Fatalf("BeforeRev = %q, want %q", got.BeforeRev, first.AfterRev)
	}
	if got.AfterRev != nextRev {
		t.Fatalf("AfterRev = %q, want %q", got.AfterRev, nextRev)
	}
}

func TestSyncGitModuleRejectsTrackedLocalChanges(t *testing.T) {
	requireGit(t)

	projectRoot := t.TempDir()
	remote := createModuleRemote(t)
	dest := filepath.Join(projectRoot, "deps", "sigil")

	if _, err := syncGitModule(projectRoot, "sigil", remote, dest); err != nil {
		t.Fatalf("initial syncGitModule: %v", err)
	}
	sourcePath := filepath.Join(dest, "module.sg")
	if err := os.WriteFile(sourcePath, []byte("pub fn version() -> string { return \"dirty\"; }\n"), 0o600); err != nil {
		t.Fatalf("write dirty module file: %v", err)
	}

	_, err := syncGitModule(projectRoot, "sigil", remote, dest)
	if err == nil {
		t.Fatalf("expected dirty worktree error")
	}
	if !strings.Contains(err.Error(), "tracked local changes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func createModuleRemote(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	remote := filepath.Join(root, "sigil.git")
	mustGit(t, root, "init", "--bare", remote)

	worktree := filepath.Join(root, "seed")
	mustGit(t, root, "clone", remote, worktree)
	mustGit(t, worktree, "config", "user.email", "test@example.com")
	mustGit(t, worktree, "config", "user.name", "Test User")
	mustGit(t, worktree, "checkout", "-b", "main")

	writeFile(t, filepath.Join(worktree, "surge.toml"), `[package]
name = "sigil"
version = "0.1.0"
root = "."
`)
	writeFile(t, filepath.Join(worktree, "module.sg"), "pub fn version() -> string { return \"v1\"; }\n")

	mustGit(t, worktree, "add", "surge.toml", "module.sg")
	mustGit(t, worktree, "commit", "-m", "feat: initial module")
	mustGit(t, worktree, "push", "-u", "origin", "main")
	mustGit(t, root, "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main")
	return remote
}

func pushModuleCommit(t *testing.T, remote, source string) string {
	t.Helper()

	worktree := t.TempDir()
	mustGit(t, filepath.Dir(worktree), "clone", remote, worktree)
	mustGit(t, worktree, "config", "user.email", "test@example.com")
	mustGit(t, worktree, "config", "user.name", "Test User")

	writeFile(t, filepath.Join(worktree, "module.sg"), source)
	mustGit(t, worktree, "add", "module.sg")
	mustGit(t, worktree, "commit", "-m", "fix: update module")
	mustGit(t, worktree, "push", "origin", "HEAD")
	return strings.TrimSpace(mustGit(t, worktree, "rev-parse", "HEAD"))
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitRun(dir, args...)
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return out
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
