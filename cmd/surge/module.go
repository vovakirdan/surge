package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"surge/internal/project"
)

var moduleCmd = &cobra.Command{
	Use:   "module",
	Short: "Manage surge modules",
}

var moduleAddCmd = &cobra.Command{
	Use:   "add <spec>",
	Short: "Add a module dependency",
	Args:  cobra.ExactArgs(1),
	RunE:  runModuleAdd,
}

var moduleInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install modules from surge.toml",
	Args:  cobra.NoArgs,
	RunE:  runModuleInstall,
}

var moduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List modules from surge.toml",
	Args:  cobra.NoArgs,
	RunE:  runModuleList,
}

var moduleRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a module dependency",
	Args:  cobra.ExactArgs(1),
	RunE:  runModuleRemove,
}

func init() {
	moduleAddCmd.Flags().String("as", "", "install module under a custom name")
	moduleAddCmd.Flags().String("toml", "", "path to surge.toml")
	moduleInstallCmd.Flags().String("toml", "", "path to surge.toml")
	moduleListCmd.Flags().String("toml", "", "path to surge.toml")
	moduleRemoveCmd.Flags().Bool("keep-files", false, "keep deps/<name> on disk")
	moduleRemoveCmd.Flags().String("toml", "", "path to surge.toml")

	moduleCmd.AddCommand(moduleAddCmd)
	moduleCmd.AddCommand(moduleInstallCmd)
	moduleCmd.AddCommand(moduleListCmd)
	moduleCmd.AddCommand(moduleRemoveCmd)
}

func runModuleAdd(cmd *cobra.Command, args []string) error {
	spec := strings.TrimSpace(args[0])
	if spec == "" {
		return fmt.Errorf("module spec is required")
	}
	asName, err := cmd.Flags().GetString("as")
	if err != nil {
		return fmt.Errorf("failed to read --as: %w", err)
	}
	tomlFlag, err := cmd.Flags().GetString("toml")
	if err != nil {
		return fmt.Errorf("failed to read --toml: %w", err)
	}
	manifestPath, projectRoot, err := resolveProjectToml("module add", tomlFlag)
	if err != nil {
		return err
	}

	urlSpec, ok := normalizeGitURL(spec)
	if !ok {
		return fmt.Errorf("official registry is not configured yet; use git URL")
	}

	name := strings.TrimSpace(asName)
	if name == "" {
		name, err = deriveModuleNameFromURL(urlSpec)
		if err != nil {
			return err
		}
	}
	if !project.IsValidModuleIdent(name) {
		return fmt.Errorf("invalid module name %q; use --as <name>", name)
	}

	if err := preflightModuleAdd(manifestPath, projectRoot, name); err != nil {
		return err
	}

	depsRoot := filepath.Join(projectRoot, "deps")
	if err := os.MkdirAll(depsRoot, 0o750); err != nil {
		return fmt.Errorf("failed to create deps directory: %w", err)
	}
	depsPath := filepath.Join(depsRoot, name)

	if err := gitClone(projectRoot, urlSpec, depsPath); err != nil {
		return err
	}
	if err := validateModuleRepo(name, depsPath); err != nil {
		if rmErr := os.RemoveAll(depsPath); rmErr != nil {
			return fmt.Errorf("cleanup failed after error: %w", errors.Join(err, rmErr))
		}
		return err
	}

	if err := updateModulesTable(manifestPath, func(mods map[string]any) error {
		if _, exists := mods[name]; exists {
			return fmt.Errorf("module %q already added in surge.toml", name)
		}
		mods[name] = map[string]any{
			"source": "git",
			"url":    urlSpec,
		}
		return nil
	}); err != nil {
		if rmErr := os.RemoveAll(depsPath); rmErr != nil {
			return fmt.Errorf("cleanup failed after error: %w", errors.Join(err, rmErr))
		}
		return err
	}

	if err := writeStdoutf("installed %s in %s\n", name, filepath.Join("deps", name)); err != nil {
		return err
	}
	if err := writeStdoutf("updated %s with [modules].%s\n", filepath.Base(manifestPath), name); err != nil {
		return err
	}
	return nil
}

func runModuleInstall(cmd *cobra.Command, _ []string) error {
	tomlFlag, err := cmd.Flags().GetString("toml")
	if err != nil {
		return fmt.Errorf("failed to read --toml: %w", err)
	}
	manifestPath, projectRoot, err := resolveProjectToml("module install", tomlFlag)
	if err != nil {
		return err
	}
	modules, err := project.LoadProjectModules(manifestPath)
	if err != nil {
		return err
	}
	if len(modules) == 0 {
		return writeStdoutln("no modules to install")
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
	skipped := 0
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
		if info, err := os.Stat(depsPath); err == nil {
			if !info.IsDir() {
				if writeErr := writeStderrf("error %s: %s exists and is not a directory\n", name, filepath.Join("deps", name)); writeErr != nil {
					return writeErr
				}
				failed++
				continue
			}
			if writeErr := writeStdoutf("skipped %s (already installed)\n", name); writeErr != nil {
				return writeErr
			}
			skipped++
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			if writeErr := writeStderrf("error %s: failed to stat deps: %v\n", name, err); writeErr != nil {
				return writeErr
			}
			failed++
			continue
		}

		if err := gitClone(projectRoot, spec.URL, depsPath); err != nil {
			if writeErr := writeStderrf("error %s: %v\n", name, err); writeErr != nil {
				return writeErr
			}
			failed++
			continue
		}
		if err := validateModuleRepo(name, depsPath); err != nil {
			if rmErr := os.RemoveAll(depsPath); rmErr != nil {
				return fmt.Errorf("cleanup failed after error: %w", errors.Join(err, rmErr))
			}
			if writeErr := writeStderrf("error %s: %v\n", name, err); writeErr != nil {
				return writeErr
			}
			failed++
			continue
		}
		if err := writeStdoutf("installed %s in %s\n", name, filepath.Join("deps", name)); err != nil {
			return err
		}
		installed++
	}

	if err := writeStdoutf("summary: installed=%d skipped=%d errors=%d\n", installed, skipped, failed); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("module install failed")
	}
	return nil
}

func runModuleList(cmd *cobra.Command, _ []string) error {
	tomlFlag, err := cmd.Flags().GetString("toml")
	if err != nil {
		return fmt.Errorf("failed to read --toml: %w", err)
	}
	manifestPath, projectRoot, err := resolveProjectToml("module list", tomlFlag)
	if err != nil {
		return err
	}
	modules, err := project.LoadProjectModules(manifestPath)
	if err != nil {
		return err
	}
	if len(modules) == 0 {
		return writeStdoutln("no modules defined")
	}

	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec := modules[name]
		installed := false
		if info, err := os.Stat(filepath.Join(projectRoot, "deps", name)); err == nil && info.IsDir() {
			installed = true
		}
		urlField := strings.TrimSpace(spec.URL)
		if urlField != "" {
			if err := writeStdoutf("%s\tsource=%s\turl=%s\tinstalled=%t\n", name, spec.Source, urlField, installed); err != nil {
				return err
			}
		} else {
			if err := writeStdoutf("%s\tsource=%s\tinstalled=%t\n", name, spec.Source, installed); err != nil {
				return err
			}
		}
	}
	return nil
}

func runModuleRemove(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if name == "" {
		return fmt.Errorf("module name is required")
	}
	keepFiles, err := cmd.Flags().GetBool("keep-files")
	if err != nil {
		return fmt.Errorf("failed to read --keep-files: %w", err)
	}
	tomlFlag, err := cmd.Flags().GetString("toml")
	if err != nil {
		return fmt.Errorf("failed to read --toml: %w", err)
	}
	manifestPath, projectRoot, err := resolveProjectToml("module remove", tomlFlag)
	if err != nil {
		return err
	}
	modules, err := project.LoadProjectModules(manifestPath)
	if err != nil {
		return err
	}
	if _, ok := modules[name]; !ok {
		return fmt.Errorf("module %q is not listed in surge.toml", name)
	}

	if err := updateModulesTable(manifestPath, func(mods map[string]any) error {
		delete(mods, name)
		return nil
	}); err != nil {
		return err
	}
	if err := writeStdoutf("removed [modules].%s from %s\n", name, filepath.Base(manifestPath)); err != nil {
		return err
	}

	if !keepFiles {
		depsPath := filepath.Join(projectRoot, "deps", name)
		if err := os.RemoveAll(depsPath); err != nil {
			return fmt.Errorf("failed to remove %s: %w", filepath.Join("deps", name), err)
		}
		if err := writeStdoutf("removed %s\n", filepath.Join("deps", name)); err != nil {
			return err
		}
	}
	return nil
}

func preflightModuleAdd(manifestPath, projectRoot, name string) error {
	shadowFile := filepath.Join(projectRoot, name+".sg")
	if info, err := os.Stat(shadowFile); err == nil {
		if info.IsDir() {
			return fmt.Errorf("module name %q conflicts with existing ./%s/; use --as <other>", name, name)
		}
		return fmt.Errorf("module name %q conflicts with existing ./%s; use --as <other>", name, name+".sg")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat %s: %w", shadowFile, err)
	}

	shadowDir := filepath.Join(projectRoot, name)
	if info, err := os.Stat(shadowDir); err == nil && info.IsDir() {
		return fmt.Errorf("module name %q conflicts with existing ./%s/; use --as <other>", name, name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat %s: %w", shadowDir, err)
	}

	depsPath := filepath.Join(projectRoot, "deps", name)
	if info, err := os.Stat(depsPath); err == nil {
		if info.IsDir() {
			return fmt.Errorf("module %q already installed at %s", name, filepath.Join("deps", name))
		}
		return fmt.Errorf("%s exists and is not a directory", filepath.Join("deps", name))
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat %s: %w", depsPath, err)
	}

	modules, err := project.LoadProjectModules(manifestPath)
	if err != nil {
		return err
	}
	if _, ok := modules[name]; ok {
		return fmt.Errorf("module %q already added in surge.toml", name)
	}
	return nil
}

func gitClone(projectRoot, url, dest string) error {
	cmd := exec.Command("git", "clone", url, dest)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

func writeStdoutf(format string, args ...any) error {
	_, err := fmt.Fprintf(os.Stdout, format, args...)
	return err
}

func writeStdoutln(args ...any) error {
	_, err := fmt.Fprintln(os.Stdout, args...)
	return err
}

func writeStderrf(format string, args ...any) error {
	_, err := fmt.Fprintf(os.Stderr, format, args...)
	return err
}
