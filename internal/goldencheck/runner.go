package goldencheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options configures a golden update or fail-closed check.
type Options struct {
	RepoRoot         string
	ExpectationsPath string
	Command          []string
	Env              []string
	Runs             int
	Stdout           io.Writer
	Stderr           io.Writer
}

// Check rejects a dirty corpus before running the generator, then proves that
// every serialized run preserves the reviewed snapshot exactly.
func Check(ctx context.Context, options *Options) error {
	state, err := loadState(options)
	if err != nil {
		return err
	}
	gitChanges, err := GitChanges(ctx, state.repoRoot, state.goldenRoot)
	if err != nil {
		return err
	}
	if len(gitChanges) != 0 {
		return formatGitChanges("golden preflight rejected repository state", gitChanges)
	}
	baseline, err := Scan(state.goldenRoot)
	if err != nil {
		return err
	}
	if err := state.expectations.VerifyCorpus(baseline); err != nil {
		return fmt.Errorf("golden preflight rejected frozen corpus: %w", err)
	}
	runs := options.Runs
	if runs == 0 {
		runs = 2
	}
	if runs < 1 {
		return fmt.Errorf("runs must be positive")
	}
	for run := 1; run <= runs; run++ {
		generatorErr := runGenerator(ctx, options, &state)
		post, scanErr := Scan(state.goldenRoot)
		var postErr error
		if scanErr == nil {
			if changes := Diff(baseline, post); len(changes) != 0 {
				postErr = formatSnapshotChanges(run, changes)
			}
		}
		gitChanges, gitErr := GitChanges(ctx, state.repoRoot, state.goldenRoot)
		if gitErr == nil && len(gitChanges) != 0 {
			gitErr = formatGitChanges(fmt.Sprintf("golden run %d changed repository state", run), gitChanges)
		}
		if err := errors.Join(generatorErr, scanErr, postErr, gitErr); err != nil {
			return err
		}
	}
	return nil
}

// Update runs the explicit mutator and refreshes the aggregate corpus digest.
func Update(ctx context.Context, options *Options) error {
	state, err := loadState(options)
	if err != nil {
		return err
	}
	if generatorErr := runGenerator(ctx, options, &state); generatorErr != nil {
		return generatorErr
	}
	snapshot, err := Scan(state.goldenRoot)
	if err != nil {
		return err
	}
	state.expectations.EntryCount = len(snapshot.Entries)
	state.expectations.CorpusSHA256 = snapshot.Digest()
	if err := WriteExpectations(state.expectationsPath, &state.expectations); err != nil {
		return err
	}
	return nil
}

type loadedState struct {
	repoRoot         string
	goldenRoot       string
	expectationsPath string
	expectations     Expectations
}

func loadState(options *Options) (loadedState, error) {
	repoRoot := options.RepoRoot
	if repoRoot == "" {
		repoRoot = "."
	}
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return loadedState{}, fmt.Errorf("absolute repository root: %w", err)
	}
	expectationsPath := options.ExpectationsPath
	if expectationsPath == "" {
		expectationsPath = "testdata/golden.expectations.json"
	}
	if !filepath.IsAbs(expectationsPath) {
		expectationsPath = filepath.Join(repoRoot, expectationsPath)
	}
	expectations, err := LoadExpectations(expectationsPath)
	if err != nil {
		return loadedState{}, err
	}
	return loadedState{
		repoRoot:         repoRoot,
		goldenRoot:       filepath.Join(repoRoot, filepath.FromSlash(expectations.GoldenRoot)),
		expectationsPath: expectationsPath,
		expectations:     expectations,
	}, nil
}

func runGenerator(ctx context.Context, options *Options, state *loadedState) (returnErr error) {
	command := options.Command
	if len(command) == 0 {
		command = []string{"./scripts/golden_update.sh"}
	}
	tempDir, err := os.MkdirTemp("", "surge-golden-expectations-*")
	if err != nil {
		return fmt.Errorf("create expectation transport: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove expectation transport: %w", cleanupErr))
		}
	}()
	diagFile := filepath.Join(tempDir, "diagnostic-zero.paths0")
	fmtFile := filepath.Join(tempDir, "formatter-failure.paths0")
	emitFile := filepath.Join(tempDir, "emit-failure.paths0")
	if err := writePathList(diagFile, state.expectations.DiagnosticZero); err != nil {
		return err
	}
	if err := writePathList(fmtFile, state.expectations.FormatterFailure); err != nil {
		return err
	}
	if err := writePhaseExpectations(emitFile, state.expectations.EmitFailure); err != nil {
		return err
	}
	// #nosec G204 -- this package intentionally runs the explicit CLI generator command.
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = state.repoRoot
	cmd.Env = mergeEnvironment(os.Environ(), options.Env)
	cmd.Env = mergeEnvironment(cmd.Env, []string{
		"SURGE_GOLDEN_DIAG_ZERO_EXPECTATIONS=" + diagFile,
		"SURGE_GOLDEN_FMT_FAILURE_EXPECTATIONS=" + fmtFile,
		"SURGE_GOLDEN_EMIT_FAILURE_EXPECTATIONS=" + emitFile,
	})
	cmd.Stdout = options.Stdout
	cmd.Stderr = options.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("golden generator: %w", err)
	}
	return nil
}

func mergeEnvironment(base, overrides []string) []string {
	result := append([]string(nil), base...)
	for _, override := range overrides {
		name, _, ok := strings.Cut(override, "=")
		if !ok {
			result = append(result, override)
			continue
		}
		prefix := name + "="
		replaced := false
		for i, existing := range result {
			if strings.HasPrefix(existing, prefix) {
				result[i] = override
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, override)
		}
	}
	return result
}

func writePathList(filename string, paths []string) error {
	var data strings.Builder
	for _, path := range paths {
		data.WriteString(path)
		data.WriteByte(0)
	}
	if err := os.WriteFile(filename, []byte(data.String()), 0o600); err != nil {
		return fmt.Errorf("write expectation path list: %w", err)
	}
	return nil
}

func writePhaseExpectations(filename string, expectations []PhaseExpectation) error {
	var data strings.Builder
	for _, expectation := range expectations {
		data.WriteString(expectation.Phase)
		data.WriteByte(0)
		data.WriteString(expectation.Path)
		data.WriteByte(0)
	}
	if err := os.WriteFile(filename, []byte(data.String()), 0o600); err != nil {
		return fmt.Errorf("write emit expectation list: %w", err)
	}
	return nil
}

func formatGitChanges(prefix string, changes []GitChange) error {
	var message strings.Builder
	message.WriteString(prefix)
	for _, change := range changes {
		fmt.Fprintf(&message, "\n  %s %q", change.Code, change.Path)
		if change.OrigPath != "" {
			fmt.Fprintf(&message, " (from %q)", change.OrigPath)
		}
	}
	return errors.New(message.String())
}

func formatSnapshotChanges(run int, changes []Change) error {
	var message strings.Builder
	fmt.Fprintf(&message, "golden run %d changed the frozen snapshot", run)
	for _, change := range changes {
		kind := "changed"
		if change.Before == nil {
			kind = "added"
		} else if change.After == nil {
			kind = "removed"
		}
		fmt.Fprintf(&message, "\n  %s %q", kind, change.Path)
	}
	return errors.New(message.String())
}
