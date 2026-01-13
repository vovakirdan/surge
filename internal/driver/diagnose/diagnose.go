package diagnose

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/parser"
	"surge/internal/project"
	"surge/internal/source"
)

// DiagnoseOptions configures workspace diagnostics.
type DiagnoseOptions struct {
	ProjectRoot        string
	BaseDir            string
	Stage              driver.DiagnoseStage
	MaxDiagnostics     int
	IgnoreWarnings     bool
	WarningsAsErrors   bool
	NoAlienHints       bool
	RootKind           project.ModuleKind
	EnableTimings      bool
	EnableDiskCache    bool
	DirectiveMode      parser.DirectiveMode
	DirectiveFilter    []string
	EmitHIR            bool
	EmitInstantiations bool
	Jobs               int
	Result             *WorkspaceResult
}

// FileOverlay stores in-memory file contents keyed by absolute path or file URI.
type FileOverlay struct {
	Files map[string]string
}

// Diagnostic represents a simplified diagnostic suitable for LSP mapping.
// Line/column fields are 1-based.
type Diagnostic struct {
	FilePath  string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Severity  int
	Code      string
	Message   string
}

// WorkspaceMode indicates whether diagnostics ran on a file or directory.
type WorkspaceMode uint8

const (
	// WorkspaceModeFile indicates a file-level diagnostics run.
	WorkspaceModeFile WorkspaceMode = iota
	// WorkspaceModeDir indicates a directory-level diagnostics run.
	WorkspaceModeDir
)

// WorkspaceResult optionally captures the raw driver results for CLI usage.
type WorkspaceResult struct {
	Mode       WorkspaceMode
	FileResult *driver.DiagnoseResult
	DirFileSet *source.FileSet
	DirResults []driver.DiagnoseDirResult
}

// DiagnoseWorkspace runs diagnostics for a file or directory and returns simplified diagnostics.
func DiagnoseWorkspace(ctx context.Context, opts *DiagnoseOptions, overlay FileOverlay) ([]Diagnostic, error) {
	if opts == nil {
		opts = &DiagnoseOptions{}
	}
	if strings.TrimSpace(opts.ProjectRoot) == "" {
		return nil, fmt.Errorf("project root is required")
	}

	st, err := os.Stat(opts.ProjectRoot)
	rootDir := opts.BaseDir
	if rootDir == "" {
		if err == nil && st.IsDir() {
			rootDir = opts.ProjectRoot
		} else {
			rootDir = filepath.Dir(opts.ProjectRoot)
		}
	}
	overlayMap := normalizeOverlay(overlay, rootDir)
	readFile := overlayReadFile(overlayMap, rootDir)

	driverOpts := driver.DiagnoseOptions{
		Stage:              opts.Stage,
		MaxDiagnostics:     opts.MaxDiagnostics,
		IgnoreWarnings:     opts.IgnoreWarnings,
		WarningsAsErrors:   opts.WarningsAsErrors,
		NoAlienHints:       opts.NoAlienHints,
		BaseDir:            opts.BaseDir,
		ReadFile:           readFile,
		RootKind:           opts.RootKind,
		EnableTimings:      opts.EnableTimings,
		EnableDiskCache:    opts.EnableDiskCache,
		DirectiveMode:      opts.DirectiveMode,
		DirectiveFilter:    opts.DirectiveFilter,
		EmitHIR:            opts.EmitHIR,
		EmitInstantiations: opts.EmitInstantiations,
	}

	isOverlayFile := err != nil && overlayHasPath(overlayMap, opts.ProjectRoot, rootDir)
	if err != nil && !isOverlayFile {
		return nil, err
	}

	if err == nil && st.IsDir() {
		fs, results, diagErr := driver.DiagnoseDirWithOptions(ctx, opts.ProjectRoot, &driverOpts, opts.Jobs)
		if diagErr != nil {
			return nil, diagErr
		}
		if opts.Result != nil {
			opts.Result.Mode = WorkspaceModeDir
			opts.Result.DirFileSet = fs
			opts.Result.DirResults = results
		}
		return collectDirDiagnostics(fs, results), nil
	}

	result, diagErr := driver.DiagnoseWithOptions(ctx, opts.ProjectRoot, &driverOpts)
	if diagErr != nil {
		return nil, diagErr
	}
	if opts.Result != nil {
		opts.Result.Mode = WorkspaceModeFile
		opts.Result.FileResult = result
	}
	return collectFileDiagnostics(result), nil
}

func collectFileDiagnostics(result *driver.DiagnoseResult) []Diagnostic {
	if result == nil || result.Bag == nil || result.FileSet == nil {
		return nil
	}
	fallbackPath := ""
	if result.File != nil {
		fallbackPath = result.File.Path
	}
	return collectDiagnostics(result.FileSet, result.Bag.Items(), fallbackPath)
}

func collectDirDiagnostics(fs *source.FileSet, results []driver.DiagnoseDirResult) []Diagnostic {
	if fs == nil || len(results) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0)
	for _, res := range results {
		if res.Bag == nil {
			continue
		}
		fallbackPath := res.Path
		if fallbackPath == "" && res.FileID != 0 && fs.HasFile(res.FileID) {
			if file := fs.Get(res.FileID); file != nil {
				fallbackPath = file.Path
			}
		}
		out = append(out, collectDiagnostics(fs, res.Bag.Items(), fallbackPath)...)
	}
	return out
}

func collectDiagnostics(fs *source.FileSet, items []*diag.Diagnostic, fallbackPath string) []Diagnostic {
	if fs == nil || len(items) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if diagItem, ok := toDiagnostic(fs, item, fallbackPath); ok {
			out = append(out, diagItem)
		}
	}
	return out
}

func toDiagnostic(fs *source.FileSet, item *diag.Diagnostic, fallbackPath string) (Diagnostic, bool) {
	if item == nil {
		return Diagnostic{}, false
	}
	if fs == nil {
		return Diagnostic{}, false
	}
	path := ""
	startLine, startCol, endLine, endCol := 0, 0, 0, 0
	if item.Primary.Start == 0 && item.Primary.End == 0 && item.Primary.File == 0 && fallbackPath != "" {
		path = fallbackPath
	} else if fs.HasFile(item.Primary.File) {
		file := fs.Get(item.Primary.File)
		if file != nil {
			path = file.Path
		}
		start, end := fs.Resolve(item.Primary)
		startLine = int(start.Line)
		startCol = int(start.Col)
		endLine = int(end.Line)
		endCol = int(end.Col)
	}
	if path == "" {
		return Diagnostic{}, false
	}
	code := ""
	if item.Code != diag.UnknownCode {
		code = item.Code.ID()
	}
	return Diagnostic{
		FilePath:  path,
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
		Severity:  severityToLSP(item.Severity),
		Code:      code,
		Message:   item.Message,
	}, true
}

func severityToLSP(sev diag.Severity) int {
	switch sev {
	case diag.SevError:
		return 1
	case diag.SevWarning:
		return 2
	case diag.SevInfo:
		return 3
	default:
		return 3
	}
}

func normalizeOverlay(overlay FileOverlay, baseDir string) map[string]string {
	if len(overlay.Files) == 0 {
		return nil
	}
	out := make(map[string]string, len(overlay.Files))
	for key, value := range overlay.Files {
		if norm, ok := normalizeOverlayKey(key, baseDir); ok {
			out[norm] = value
		}
	}
	return out
}

func overlayReadFile(overlay map[string]string, baseDir string) func(string) ([]byte, error) {
	if len(overlay) == 0 {
		return nil
	}
	return func(path string) ([]byte, error) {
		if norm, ok := normalizeOverlayKey(path, baseDir); ok {
			if content, ok := overlay[norm]; ok {
				return []byte(content), nil
			}
		}
		// #nosec G304 -- path comes from compiler inputs
		return os.ReadFile(path)
	}
}

func overlayHasPath(overlay map[string]string, path, baseDir string) bool {
	if len(overlay) == 0 {
		return false
	}
	if norm, ok := normalizeOverlayKey(path, baseDir); ok {
		_, ok = overlay[norm]
		return ok
	}
	return false
}

func normalizeOverlayKey(key, baseDir string) (string, bool) {
	if strings.TrimSpace(key) == "" {
		return "", false
	}
	path := key
	if strings.HasPrefix(key, "file://") {
		parsed, err := url.Parse(key)
		if err != nil {
			return "", false
		}
		path = parsed.Path
	}
	path = filepath.FromSlash(path)
	if baseDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.ToSlash(filepath.Clean(path)), true
}
