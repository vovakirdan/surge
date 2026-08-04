package abimanifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ManifestPath is the repository-relative canonical ABI source.
const ManifestPath = "runtime/abi/typed_carrier_v2.json"

var generatedPaths = []string{
	"internal/abimanifest/typed_carrier_v2.generated.go",
	"internal/backend/llvm/typed_carrier_v2.generated.go",
	"internal/vm/typed_carrier_v2.generated.go",
	"runtime/native/rt_typed_carrier_abi.generated.c",
	"runtime/native/rt_typed_carrier_abi.generated.h",
	"runtime/native/rt_typed_carrier_abi_checks.generated.h",
}

// Generate renders every checked-in ABI view from canonical manifest content.
func Generate(manifest *Manifest, canonical []byte) (map[string][]byte, error) {
	if err := Validate(manifest); err != nil {
		return nil, err
	}
	expectedCanonical, err := CanonicalBytes(manifest)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, expectedCanonical) {
		return nil, fmt.Errorf("generator input is not canonical manifest content")
	}
	hash := ContentHash(canonical)
	view := BuildSchemaView(manifest, hash)
	outputs := make(map[string][]byte, len(generatedPaths))
	outputs[generatedPaths[0]], err = renderGoView(&view)
	if err != nil {
		return nil, err
	}
	outputs[generatedPaths[1]], err = renderLLVMView(manifest, &view)
	if err != nil {
		return nil, err
	}
	outputs[generatedPaths[2]], err = renderVMView(&view)
	if err != nil {
		return nil, err
	}
	outputs[generatedPaths[4]] = renderCHeader(manifest, hash)
	outputs[generatedPaths[5]] = renderCChecks(manifest)
	outputs[generatedPaths[3]] = renderCSource(manifest, hash)
	return outputs, nil
}

// CheckRepository verifies canonical source bytes and all generated views.
func CheckRepository(root string) error {
	manifest, canonical, err := LoadCanonical(filepath.Join(root, ManifestPath))
	if err != nil {
		return err
	}
	outputs, err := Generate(&manifest, canonical)
	if err != nil {
		return err
	}
	for _, relative := range sortedOutputPaths(outputs) {
		// #nosec G304 -- relative is selected from the closed generatedPaths list.
		actual, readErr := os.ReadFile(filepath.Join(root, relative))
		if readErr != nil {
			return fmt.Errorf("generated typed-carrier ABI view %s: %w", relative, readErr)
		}
		if !bytes.Equal(actual, outputs[relative]) {
			return fmt.Errorf("generated typed-carrier ABI view is stale: %s; run abi-manifest-gen", relative)
		}
	}
	return nil
}

// WriteRepository canonicalizes the source and atomically replaces generated
// views. This is the only write path used by the generator command.
func WriteRepository(root string) error {
	manifestPath := filepath.Join(root, ManifestPath)
	// #nosec G304 -- manifestPath is the fixed ManifestPath under the caller's repository root.
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read typed-carrier ABI manifest: %w", err)
	}
	manifest, err := Parse(raw)
	if err != nil {
		return err
	}
	canonical, err := CanonicalBytes(&manifest)
	if err != nil {
		return err
	}
	outputs, err := Generate(&manifest, canonical)
	if err != nil {
		return err
	}
	outputs[ManifestPath] = canonical
	for _, relative := range sortedOutputPaths(outputs) {
		if err := writeAtomic(filepath.Join(root, relative), outputs[relative]); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}
	return nil
}

// GeneratedPaths returns a copy of the closed generated-output inventory.
func GeneratedPaths() []string {
	return append([]string(nil), generatedPaths...)
}

func sortedOutputPaths(outputs map[string][]byte) []string {
	paths := make([]string, 0, len(outputs))
	for relative := range outputs {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	return paths
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		if cleanupErr := os.Remove(temporary); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			return errors.Join(fmt.Errorf("rename generated file: %w", err), fmt.Errorf("remove temporary file: %w", cleanupErr))
		}
		return err
	}
	return nil
}
