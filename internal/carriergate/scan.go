// Package carriergate freezes and ratchets legacy erased-carrier source shapes.
package carriergate

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

const (
	// ManifestVersion is the on-disk carrier census schema version.
	ManifestVersion = 1
	// EpicBaseCommit is the exact reviewed source baseline for Epic 23b.
	EpicBaseCommit = "7df10725e001ddf915d536aa58f880bd7e04aafd"
)

const (
	categoryLLVMWordBridge    = "llvm-erased-word-bridge"
	categoryLLVMPointerWord   = "llvm-pointer-word-ir"
	categoryLLVMCompositePtr  = "llvm-composite-to-ptr"
	categoryCompositeBox      = "composite-box-marker"
	categoryVMBoxKind         = "vm-boxed-composite-kind"
	categoryVMUniversalOwner  = "vm-universal-owner"
	categoryAsyncAny          = "vm-async-any-carrier"
	categoryNativePayloadBits = "native-payload-bits"
	categoryNativeWord        = "native-word-carrier"
	categoryNumericDrop       = "numeric-drop-dispatch"
	// categoryFrameOwner marks a suspension frame whose storage is reserved,
	// sized and released by COMPILED CODE. The frame outlives the function
	// that builds it, so its lifetime belongs to the runtime; while the
	// emitter allocates it and hands the runtime a bare address, the runtime
	// can only give it back to be released, which is why the release paths
	// are counted here alongside the allocation.
	categoryFrameOwner = "suspension-frame-owner"
	// categoryUntypedCaptureState marks a captured state described by
	// `(void* state, uint64_t state_size, uint64_t state_align)`. Two
	// integers are not a type: nothing in that triple can construct, copy or
	// destroy what the pointer addresses, which is the same erasure a typed
	// cell removes everywhere else in the blocking job.
	categoryUntypedCaptureState = "untyped-capture-state"
)

var requiredCategories = []string{
	categoryCompositeBox,
	categoryLLVMCompositePtr,
	categoryLLVMWordBridge,
	categoryLLVMPointerWord,
	categoryNativePayloadBits,
	categoryNativeWord,
	categoryNumericDrop,
	categoryFrameOwner,
	categoryUntypedCaptureState,
	categoryAsyncAny,
	categoryVMBoxKind,
	categoryVMUniversalOwner,
}

// Scope is a production-only source root scanned by the gate.
type Scope struct {
	Root       string   `json:"root"`
	Extensions []string `json:"extensions"`
	Excludes   []string `json:"excludes"`
}

var requiredScopes = []Scope{
	{Root: "internal/asyncrt", Extensions: []string{".go"}, Excludes: []string{"*_test.go", "testdata/**"}},
	{Root: "internal/backend/llvm", Extensions: []string{".go"}, Excludes: []string{"*_test.go", "testdata/**"}},
	{Root: "internal/vm", Extensions: []string{".go"}, Excludes: []string{"*_test.go", "testdata/**"}},
	{Root: "runtime/native", Extensions: []string{".c", ".h"}, Excludes: []string{}},
}

// Finding is a stable occurrence identity. Line is diagnostic-only.
type Finding struct {
	Category string `json:"category"`
	Path     string `json:"path"`
	Token    string `json:"token"`
	Evidence string `json:"evidence"`
	Ordinal  uint64 `json:"ordinal"`
	Line     int    `json:"-"`
}

type rawFinding struct {
	Finding
	offset int
}

// Scan returns all carrier findings below the fixed production scope.
func Scan(repoRoot string) ([]Finding, error) {
	return scanFS(os.DirFS(repoRoot))
}

func scanFS(rootFS fs.FS) ([]Finding, error) {
	raw := make([]rawFinding, 0, 512)
	for _, scope := range requiredScopes {
		err := fs.WalkDir(rootFS, scope.Root, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf(
					"symlink %s is not allowed in a production carrier scope; replace it with a regular file or directory",
					filePath,
				)
			}
			if entry.IsDir() || excludedPath(filePath) {
				return nil
			}
			if !hasExtension(filePath, scope.Extensions) {
				return nil
			}
			data, err := fs.ReadFile(rootFS, filePath)
			if err != nil {
				return fmt.Errorf("read %s: %w", filePath, err)
			}
			var found []rawFinding
			switch path.Ext(filePath) {
			case ".go":
				found, err = scanGoFile(filePath, data)
			case ".c", ".h":
				found = scanCFile(filePath, data)
			}
			if err != nil {
				return fmt.Errorf("scan %s: %w", filePath, err)
			}
			raw = append(raw, found...)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", scope.Root, err)
		}
	}
	return finalizeFindings(raw), nil
}

func excludedPath(filePath string) bool {
	return strings.HasSuffix(filePath, "_test.go") || strings.Contains(filePath, "/testdata/")
}

func hasExtension(filePath string, extensions []string) bool {
	ext := path.Ext(filePath)
	for _, allowed := range extensions {
		if ext == allowed {
			return true
		}
	}
	return false
}

func makeRawFinding(category, filePath, token string, data []byte, offset int) rawFinding {
	line, evidence := sourceEvidence(data, offset)
	return rawFinding{
		Finding: Finding{Category: category, Path: filePath, Token: token, Evidence: evidence, Line: line},
		offset:  offset,
	}
}

func sourceEvidence(data []byte, offset int) (line int, evidence string) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(data) {
		offset = len(data)
	}
	line = bytes.Count(data[:offset], []byte{'\n'}) + 1
	start := bytes.LastIndexByte(data[:offset], '\n') + 1
	endRel := bytes.IndexByte(data[offset:], '\n')
	end := len(data)
	if endRel >= 0 {
		end = offset + endRel
	}
	evidence = strings.TrimSpace(strings.TrimSuffix(string(data[start:end]), "\r"))
	return line, evidence
}

func finalizeFindings(raw []rawFinding) []Finding {
	sort.Slice(raw, func(i, j int) bool {
		left, right := raw[i], raw[j]
		if key := compareFindingFields(&left.Finding, &right.Finding); key != 0 {
			return key < 0
		}
		return left.offset < right.offset
	})
	findings := make([]Finding, len(raw))
	var previous Finding
	var ordinal uint64
	for i := range raw {
		current := raw[i].Finding
		if i == 0 || compareFindingFields(&previous, &current) != 0 {
			ordinal = 1
		} else {
			ordinal++
		}
		current.Ordinal = ordinal
		findings[i] = current
		previous = raw[i].Finding
	}
	return findings
}

func compareFindingFields(left, right *Finding) int {
	for _, pair := range [][2]string{
		{left.Category, right.Category}, {left.Path, right.Path},
		{left.Token, right.Token}, {left.Evidence, right.Evidence},
	} {
		if cmp := strings.Compare(pair[0], pair[1]); cmp != 0 {
			return cmp
		}
	}
	if left.Ordinal < right.Ordinal {
		return -1
	}
	if left.Ordinal > right.Ordinal {
		return 1
	}
	return 0
}
