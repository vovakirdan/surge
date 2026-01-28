package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ModuleSpec describes a dependency entry in [modules].
type ModuleSpec struct {
	Source string `toml:"source"`
	URL    string `toml:"url"`
}

// ModuleManifest describes a module's own surge.toml [package] section.
type ModuleManifest struct {
	Name string
	Root string
}

// ModuleMapping captures resolved module roots for a project manifest.
type ModuleMapping struct {
	ProjectRoot string
	Declared    map[string]ModuleSpec
	Roots       map[string]string
	Missing     map[string]string
}

var (
	// ErrPackageSectionMissing indicates that [package] is missing in a module manifest.
	ErrPackageSectionMissing = errors.New("missing [package]")
	// ErrPackageRootMissing indicates that [package].root is missing in a module manifest.
	ErrPackageRootMissing = errors.New("missing [package].root")
)

type projectModules struct {
	Modules map[string]ModuleSpec `toml:"modules"`
}

type moduleManifest struct {
	Package struct {
		Name string `toml:"name"`
		Root string `toml:"root"`
	} `toml:"package"`
}

// LoadProjectModules parses the [modules] section from a project surge.toml.
func LoadProjectModules(path string) (map[string]ModuleSpec, error) {
	var cfg projectModules
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to parse TOML: %w", path, err)
	}
	if !meta.IsDefined("modules") {
		return map[string]ModuleSpec{}, nil
	}
	if cfg.Modules == nil {
		return map[string]ModuleSpec{}, nil
	}
	return cfg.Modules, nil
}

// LoadModuleManifest parses a module's surge.toml [package] section.
func LoadModuleManifest(path string) (ModuleManifest, error) {
	var cfg moduleManifest
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return ModuleManifest{}, fmt.Errorf("%s: failed to parse TOML: %w", path, err)
	}
	if !meta.IsDefined("package") {
		return ModuleManifest{}, fmt.Errorf("%s: %w", path, ErrPackageSectionMissing)
	}
	root := strings.TrimSpace(cfg.Package.Root)
	if !meta.IsDefined("package", "root") || root == "" {
		return ModuleManifest{}, fmt.Errorf("%s: %w", path, ErrPackageRootMissing)
	}
	return ModuleManifest{
		Name: strings.TrimSpace(cfg.Package.Name),
		Root: root,
	}, nil
}

// ResolveModuleRoot resolves and validates a module root relative to the repo root.
func ResolveModuleRoot(repoRoot, root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", ErrPackageRootMissing
	}
	if filepath.IsAbs(root) {
		return "", fmt.Errorf("invalid [package].root %q: must be relative", root)
	}
	clean := filepath.Clean(filepath.FromSlash(root))
	if clean == "." {
		clean = ""
	}
	rootPath := filepath.Join(repoRoot, clean)
	if !pathWithin(repoRoot, rootPath) {
		return "", fmt.Errorf("invalid [package].root %q: escapes repository root", root)
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return "", fmt.Errorf("invalid [package].root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("invalid [package].root %q: not a directory", root)
	}
	return rootPath, nil
}

// LoadModuleMapping resolves dependency module roots for a project starting at startDir.
func LoadModuleMapping(startDir string) (*ModuleMapping, bool, error) {
	manifestPath, ok, err := FindSurgeToml(startDir)
	if err != nil || !ok {
		return nil, ok, err
	}
	modules, err := LoadProjectModules(manifestPath)
	if err != nil {
		return nil, true, err
	}
	mapping := &ModuleMapping{
		ProjectRoot: filepath.Dir(manifestPath),
		Declared:    modules,
		Roots:       make(map[string]string),
		Missing:     make(map[string]string),
	}
	for name, spec := range modules {
		if !IsValidModuleIdent(name) {
			return nil, true, fmt.Errorf("%s: invalid module name %q", manifestPath, name)
		}
		source := strings.TrimSpace(spec.Source)
		if source == "" {
			return nil, true, fmt.Errorf("%s: module %q missing source", manifestPath, name)
		}
		if source != "git" {
			return nil, true, fmt.Errorf("%s: module %q has unsupported source %q", manifestPath, name, source)
		}
		if strings.TrimSpace(spec.URL) == "" {
			return nil, true, fmt.Errorf("%s: module %q missing url", manifestPath, name)
		}
		depsDir := filepath.Join(mapping.ProjectRoot, "deps", name)
		info, statErr := os.Stat(depsDir)
		if statErr != nil || !info.IsDir() {
			mapping.Missing[name] = fmt.Sprintf("module '%s' declared in surge.toml but not installed. Run: surge module install", name)
			continue
		}
		moduleToml := filepath.Join(depsDir, "surge.toml")
		if _, err := os.Stat(moduleToml); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				mapping.Missing[name] = fmt.Sprintf("module '%s' is not a valid surge module: missing surge.toml in repository root", name)
				continue
			}
			mapping.Missing[name] = fmt.Sprintf("module '%s' is not a valid surge module: failed to stat surge.toml: %v", name, err)
			continue
		}
		manifest, err := LoadModuleManifest(moduleToml)
		if err != nil {
			switch {
			case errors.Is(err, ErrPackageSectionMissing):
				mapping.Missing[name] = fmt.Sprintf("module '%s' is not a valid surge module: missing [package] in surge.toml", name)
			case errors.Is(err, ErrPackageRootMissing):
				mapping.Missing[name] = fmt.Sprintf("module '%s' is not a valid surge module: missing [package].root in surge.toml", name)
			default:
				mapping.Missing[name] = fmt.Sprintf("module '%s' is not a valid surge module: %v", name, err)
			}
			continue
		}
		rootPath, err := ResolveModuleRoot(depsDir, manifest.Root)
		if err != nil {
			mapping.Missing[name] = fmt.Sprintf("module '%s' is not a valid surge module: %v", name, err)
			continue
		}
		mapping.Roots[name] = rootPath
	}
	return mapping, true, nil
}

// LogicalPath maps a filesystem path to a logical module path using module roots.
func (m *ModuleMapping) LogicalPath(path string) (string, bool) {
	if m == nil || len(m.Roots) == 0 {
		return "", false
	}
	clean := filepath.Clean(path)
	bestAlias := ""
	bestRoot := ""
	for alias, root := range m.Roots {
		if root == "" {
			continue
		}
		if pathWithin(root, clean) {
			if len(root) > len(bestRoot) {
				bestAlias = alias
				bestRoot = root
			}
		}
	}
	if bestAlias == "" {
		return "", false
	}
	rel, err := filepath.Rel(bestRoot, clean)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return bestAlias, true
	}
	return bestAlias + "/" + rel, true
}

func pathWithin(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}
