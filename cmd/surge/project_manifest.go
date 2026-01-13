package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"surge/internal/project"
)

const noSurgeTomlMessage = "no surge.toml found; pass explicit module/file or create surge.toml"

type projectManifest struct {
	Path   string
	Root   string
	Config projectConfig
}

type projectConfig struct {
	Package packageConfig `toml:"package"`
	Run     runConfig     `toml:"run"`
}

type packageConfig struct {
	Name string `toml:"name"`
}

type runConfig struct {
	Main string `toml:"main"`
}

func loadProjectManifest(startDir string) (*projectManifest, bool, error) {
	manifestPath, ok, err := project.FindSurgeToml(startDir)
	if err != nil || !ok {
		return nil, ok, err
	}
	cfg, err := loadProjectConfig(manifestPath)
	if err != nil {
		return nil, true, err
	}
	return &projectManifest{
		Path:   manifestPath,
		Root:   filepath.Dir(manifestPath),
		Config: cfg,
	}, true, nil
}

func loadProjectConfig(path string) (projectConfig, error) {
	var cfg projectConfig
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return projectConfig{}, fmt.Errorf("%s: failed to parse TOML: %w", path, err)
	}
	if !meta.IsDefined("package") {
		return projectConfig{}, fmt.Errorf("%s: missing [package]", path)
	}
	if !meta.IsDefined("package", "name") || strings.TrimSpace(cfg.Package.Name) == "" {
		return projectConfig{}, fmt.Errorf("%s: missing [package].name", path)
	}
	if !meta.IsDefined("run") {
		return projectConfig{}, fmt.Errorf("%s: missing [run]", path)
	}
	if !meta.IsDefined("run", "main") || strings.TrimSpace(cfg.Run.Main) == "" {
		return projectConfig{}, fmt.Errorf("%s: missing [run].main", path)
	}
	return cfg, nil
}

func resolveProjectRunTarget(manifest *projectManifest) (string, *runDirInfo, error) {
	if manifest == nil {
		return "", nil, fmt.Errorf("missing project manifest")
	}
	mainRel := strings.TrimSpace(manifest.Config.Run.Main)
	mainPath := filepath.Join(manifest.Root, filepath.FromSlash(mainRel))
	info, err := os.Stat(mainPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("%s: [run].main path does not exist: %s", manifest.Path, mainPath)
		}
		return "", nil, fmt.Errorf("%s: failed to stat [run].main: %w", manifest.Path, err)
	}
	if info.IsDir() {
		targetPath, dirInfo, err := resolveRunTarget(mainPath)
		if err != nil {
			return "", nil, fmt.Errorf("%s: %w", manifest.Path, err)
		}
		return targetPath, dirInfo, nil
	}
	if filepath.Ext(mainPath) != ".sg" {
		return "", nil, fmt.Errorf("%s: [run].main must be a .sg file or directory", manifest.Path)
	}
	return mainPath, nil, nil
}
