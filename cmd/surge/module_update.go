package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"surge/internal/project"
)

func runModuleUpdate(cmd *cobra.Command, _ []string) error {
	tomlFlag, err := cmd.Flags().GetString("toml")
	if err != nil {
		return fmt.Errorf("failed to read --toml: %w", err)
	}
	manifestPath, projectRoot, err := resolveProjectToml("module update", tomlFlag)
	if err != nil {
		return err
	}
	modules, err := project.LoadProjectModules(manifestPath)
	if err != nil {
		return err
	}
	if len(modules) == 0 {
		return writeStdoutln("no modules to update")
	}

	depsRoot := filepath.Join(projectRoot, "deps")
	if err := os.MkdirAll(depsRoot, 0o750); err != nil {
		return fmt.Errorf("failed to create deps directory: %w", err)
	}

	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)

	installed := 0
	updated := 0
	unchanged := 0
	failed := 0

	for _, name := range names {
		spec := modules[name]
		if strings.TrimSpace(spec.Source) != "git" {
			if err := writeStderrf("error %s: unsupported source %q\n", name, spec.Source); err != nil {
				return err
			}
			failed++
			continue
		}
		if strings.TrimSpace(spec.URL) == "" {
			if err := writeStderrf("error %s: missing url\n", name); err != nil {
				return err
			}
			failed++
			continue
		}

		depsPath := filepath.Join(depsRoot, name)
		result, err := syncGitModule(projectRoot, name, spec.URL, depsPath)
		if err != nil {
			if writeErr := writeStderrf("error %s: %v\n", name, err); writeErr != nil {
				return writeErr
			}
			failed++
			continue
		}

		switch result.State {
		case moduleSyncInstalled:
			if err := writeStdoutf("installed %s in %s\n", name, filepath.Join("deps", name)); err != nil {
				return err
			}
			installed++
		case moduleSyncUpdated:
			if err := writeStdoutf("updated %s (%s -> %s)\n", name, shortRev(result.BeforeRev), shortRev(result.AfterRev)); err != nil {
				return err
			}
			updated++
		case moduleSyncUnchanged:
			if err := writeStdoutf("unchanged %s (%s)\n", name, shortRev(result.AfterRev)); err != nil {
				return err
			}
			unchanged++
		default:
			if err := writeStderrf("error %s: unexpected sync state %q\n", name, result.State); err != nil {
				return err
			}
			failed++
		}
	}

	if err := writeStdoutf("summary: installed=%d updated=%d unchanged=%d errors=%d\n", installed, updated, unchanged, failed); err != nil {
		return err
	}
	if failed > 0 {
		return errors.New("module update failed")
	}
	return nil
}
