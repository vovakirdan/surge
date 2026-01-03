package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path|name]",
	Short: "Initialize a new surge project",
	Long: `Initialize a new surge project by creating a project manifest (surge.toml)
and a hello-world entry point (main.sg). If [path|name] is omitted, initializes
the current directory. If a non-existing name is provided, a directory will be
created.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

// runInit initializes a Surge project at the specified target path (or the current
// working directory when no argument or "." is provided) by creating a
// surge.toml manifest and a main.sg entry file.
//
// It resolves the target path, creates the directory if it does not exist,
// derives a project name from the directory basename (falling back to
// "surge-project" for invalid names), and refuses to initialize if
// surge.toml already exists. On success it writes the manifest and entry file
// and prints the created files; it returns an error for any filesystem or
// validation failures.
func runInit(cmd *cobra.Command, args []string) error {
	// Resolve target directory
	var target string
	if len(args) == 0 || args[0] == "." {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		target = wd
	} else {
		// treat as path or name relative to cwd
		arg := args[0]
		if !filepath.IsAbs(arg) {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			target = filepath.Join(wd, arg)
		} else {
			target = arg
		}
	}

	// Ensure directory exists
	if st, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err = os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %q: %w", target, err)
			}
		} else {
			return err
		}
	} else if !st.IsDir() {
		return fmt.Errorf("%q is not a directory", target)
	}

	// Determine project name from directory basename
	name := filepath.Base(target)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "surge-project"
	}

	// Create manifest file if not exists
	manifestPath := filepath.Join(target, "surge.toml")
	if _, err := os.Stat(manifestPath); err == nil {
		return fmt.Errorf("project already initialized: %s exists", manifestPath)
	}

	manifest := buildDefaultManifest(name)
	if err := os.WriteFile(manifestPath, []byte(manifest), os.FileMode(0o600)); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	// Create main.sg if not exists
	mainPath := filepath.Join(target, "main.sg")
	createdMain := false
	if _, err := os.Stat(mainPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(mainPath, []byte(defaultMainSG()), 0o600); err != nil {
			return fmt.Errorf("failed to write main.sg: %w", err)
		}
		createdMain = true
	}

	rel := target
	if wd, err := os.Getwd(); err == nil {
		if r, err2 := filepath.Rel(wd, target); err2 == nil {
			rel = r
		}
	}
	fmt.Fprintf(os.Stdout, "Initialized surge project in %s\n", rel)
	fmt.Fprintf(os.Stdout, "  - surge.toml\n")
	if createdMain {
		fmt.Fprintf(os.Stdout, "  - main.sg\n")
	} else {
		fmt.Fprintf(os.Stdout, "  - main.sg (existing)\n")
	}
	return nil
}

// buildDefaultManifest returns a minimal TOML manifest for a Surge project using the provided package name.
// The manifest contains [package] metadata and a [run] section pointing at main.sg.
func buildDefaultManifest(name string) string {
	// Minimal TOML manifest used as a project marker.
	return fmt.Sprintf(`# Surge project manifest
[package]
name = "%s"
version = "0.1.0"

[run]
main = "main.sg"
`, name)
}

// defaultMainSG returns the default placeholder Surge program used when initializing a new project.
// The returned source includes a `hello_world` function, a `main` entry that prints its result,
// and an embedded test directive demonstrating the expected output.
func defaultMainSG() string {
	return `// Surge hello world (placeholder)
// Replace with real output once stdlib/runtime is available.

// Sure, you can run test directives!
/// test:
/// HelloWorld:
/// test.eq(hello_world(), "Hello, Surge!");
fn hello_world() -> string {
    return "Hello, Surge!";
}

fn main() {
    print(hello_world());
}
`
}
