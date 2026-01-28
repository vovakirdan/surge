package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"surge/internal/project"
)

func resolveProjectToml(action, tomlFlag string) (manifestPath, projectRoot string, err error) {
	if tomlFlag != "" {
		info, statErr := os.Stat(tomlFlag)
		if statErr == nil && !info.IsDir() {
			abs := tomlFlag
			if resolved, absErr := filepath.Abs(tomlFlag); absErr == nil {
				abs = resolved
			}
			return abs, filepath.Dir(abs), nil
		}
		return "", "", fmt.Errorf("cannot run surge %s : no surge.toml found. Consider --toml <path> or run it in right directory", action)
	}
	manifest, ok, err := project.FindSurgeToml(".")
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", fmt.Errorf("cannot run surge %s : no surge.toml found. Consider --toml <path> or run it in right directory", action)
	}
	return manifest, filepath.Dir(manifest), nil
}

func normalizeGitURL(spec string) (string, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	if strings.Contains(spec, "://") || strings.HasPrefix(spec, "git@") {
		return spec, true
	}
	if strings.HasPrefix(spec, "github.com/") || strings.HasPrefix(spec, "www.github.com/") {
		return "https://" + spec, true
	}
	return "", false
}

func deriveModuleNameFromURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty module URL")
	}
	if strings.HasPrefix(raw, "git@") {
		if idx := strings.Index(raw, ":"); idx != -1 && idx+1 < len(raw) {
			raw = raw[idx+1:]
		}
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Path != "" {
		raw = parsed.Path
	}
	raw = strings.TrimSuffix(raw, "/")
	name := path.Base(raw)
	name = strings.TrimSuffix(name, ".git")
	if name == "" || name == "." || name == "/" {
		return "", fmt.Errorf("unable to derive module name from %q", raw)
	}
	return name, nil
}

func readTomlFile(tomlPath string) (map[string]any, error) {
	var data map[string]any
	if _, err := toml.DecodeFile(tomlPath, &data); err != nil {
		return nil, fmt.Errorf("%s: failed to parse TOML: %w", tomlPath, err)
	}
	if data == nil {
		data = make(map[string]any)
	}
	return data, nil
}

func writeTomlFile(tomlPath string, data map[string]any) error {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("%s: failed to encode TOML: %w", tomlPath, err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(tomlPath); err == nil {
		mode = info.Mode()
	}
	if err := os.WriteFile(tomlPath, buf.Bytes(), mode); err != nil {
		return fmt.Errorf("failed to write %s: %w", tomlPath, err)
	}
	return nil
}

func updateModulesTable(tomlPath string, update func(map[string]any) error) error {
	data, err := readTomlFile(tomlPath)
	if err != nil {
		return err
	}
	mods := map[string]any{}
	if raw, ok := data["modules"]; ok {
		var ok bool
		mods, ok = raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: invalid [modules] section", tomlPath)
		}
	}
	if err := update(mods); err != nil {
		return err
	}
	if len(mods) == 0 {
		delete(data, "modules")
	} else {
		data["modules"] = mods
	}
	return writeTomlFile(tomlPath, data)
}

func validateModuleRepo(name, repoPath string) error {
	moduleToml := filepath.Join(repoPath, "surge.toml")
	if _, err := os.Stat(moduleToml); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s is not a valid surge module: missing surge.toml in repository root", name)
		}
		return fmt.Errorf("%s is not a valid surge module: failed to stat surge.toml: %w", name, err)
	}
	manifest, err := project.LoadModuleManifest(moduleToml)
	if err != nil {
		switch {
		case errors.Is(err, project.ErrPackageSectionMissing):
			return fmt.Errorf("%s is not a valid surge module: missing [package] in surge.toml", name)
		case errors.Is(err, project.ErrPackageRootMissing):
			return fmt.Errorf("%s is not a valid surge module: missing [package].root in surge.toml", name)
		default:
			return fmt.Errorf("%s is not a valid surge module: %w", name, err)
		}
	}
	if _, err := project.ResolveModuleRoot(repoPath, manifest.Root); err != nil {
		return fmt.Errorf("%s is not a valid surge module: %w", name, err)
	}
	return nil
}
