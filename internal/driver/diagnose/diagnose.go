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
	"surge/internal/symbols"
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
	FullModuleGraph    bool
	Jobs               int
	Result             *WorkspaceResult
	// KeepArtifacts retains AST/symbol/semantic data for analysis snapshots.
	KeepArtifacts bool
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
	// Notes and Help are the explanatory and actionable channels. They used to
	// stop here: the language server received a message and a range and the
	// author lost everything the compiler had assembled around it.
	Notes []RelatedLocation
	Help  []RelatedLocation
	// Fixes are the edits attached to this diagnostic, positions resolved.
	//
	// Only their IDS ever reach a client. The edits stay here so that a Code
	// Action can be checked against what the server knows rather than against
	// whatever a client sends back, which is the difference between a guard and
	// a formality.
	Fixes []FixOffer
}

// FixOffer is one attached edit set with the metadata a Code Action needs.
type FixOffer struct {
	ID         string
	Title      string
	AlwaysSafe bool
	Edits      []FixEditLocation
}

// FixEditLocation is one edit in line/column form.
//
// OldText is the guard: a replace or a delete says what it expects to find, and
// an empty guard is valid only for an insertion, where the span is a point.
type FixEditLocation struct {
	FilePath  string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	NewText   string
	OldText   string
}

// RelatedLocation is one note or help entry with the position it belongs to.
type RelatedLocation struct {
	FilePath  string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
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
	Mode          WorkspaceMode
	FileResult    *driver.DiagnoseResult
	DirFileSet    *source.FileSet
	DirResults    []driver.DiagnoseDirResult
	ModuleExports map[string]*symbols.ModuleExports
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
		ModuleMapping:      nil,
		ReadFile:           readFile,
		RootKind:           opts.RootKind,
		EnableTimings:      opts.EnableTimings,
		EnableDiskCache:    opts.EnableDiskCache,
		DirectiveMode:      opts.DirectiveMode,
		DirectiveFilter:    opts.DirectiveFilter,
		EmitHIR:            opts.EmitHIR,
		EmitInstantiations: opts.EmitInstantiations,
		KeepArtifacts:      opts.KeepArtifacts,
		FullModuleGraph:    opts.FullModuleGraph,
	}
	var moduleExports map[string]*symbols.ModuleExports
	driverOpts.ExportsOut = &moduleExports

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
			opts.Result.ModuleExports = moduleExports
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
		opts.Result.ModuleExports = moduleExports
	}
	return collectFileDiagnostics(result), nil
}

// DiagnoseFiles runs diagnostics for an explicit list of files.
func DiagnoseFiles(ctx context.Context, opts *DiagnoseOptions, files []string, overlay FileOverlay) ([]Diagnostic, error) {
	if opts == nil {
		opts = &DiagnoseOptions{}
	}
	if len(files) == 0 {
		if opts.Result != nil {
			opts.Result.Mode = WorkspaceModeDir
			opts.Result.DirFileSet = source.NewFileSetWithBase(opts.BaseDir)
			opts.Result.DirResults = nil
		}
		return nil, nil
	}
	baseDir := opts.BaseDir
	if baseDir == "" {
		baseDir = filepath.Dir(files[0])
	}
	overlayMap := normalizeOverlay(overlay, baseDir)
	readFile := overlayReadFile(overlayMap, baseDir)

	driverOpts := driver.DiagnoseOptions{
		Stage:              opts.Stage,
		MaxDiagnostics:     opts.MaxDiagnostics,
		IgnoreWarnings:     opts.IgnoreWarnings,
		WarningsAsErrors:   opts.WarningsAsErrors,
		NoAlienHints:       opts.NoAlienHints,
		BaseDir:            baseDir,
		ReadFile:           readFile,
		RootKind:           opts.RootKind,
		EnableTimings:      opts.EnableTimings,
		EnableDiskCache:    opts.EnableDiskCache,
		DirectiveMode:      opts.DirectiveMode,
		DirectiveFilter:    opts.DirectiveFilter,
		EmitHIR:            opts.EmitHIR,
		EmitInstantiations: opts.EmitInstantiations,
		KeepArtifacts:      opts.KeepArtifacts,
		FullModuleGraph:    opts.FullModuleGraph,
	}
	var moduleExports map[string]*symbols.ModuleExports
	driverOpts.ExportsOut = &moduleExports

	fs, results, diagErr := driver.DiagnoseFilesWithOptions(ctx, baseDir, files, &driverOpts, opts.Jobs)
	if diagErr != nil {
		return nil, diagErr
	}
	if opts.Result != nil {
		opts.Result.Mode = WorkspaceModeDir
		opts.Result.DirFileSet = fs
		opts.Result.DirResults = results
		opts.Result.ModuleExports = moduleExports
	}
	return collectDirDiagnostics(fs, results), nil
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
	out := Diagnostic{
		FilePath:  path,
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
		Severity:  severityToLSP(item.Severity),
		Code:      code,
		Message:   item.Message,
		Notes:     relatedLocations(fs, item.Notes, path),
		Help:      relatedLocations(fs, item.Help, path),
	}
	out.Fixes = fixOffers(fs, item)
	return out, true
}

// relatedLocations resolves one auxiliary channel. An entry whose span the file
// set cannot answer for keeps the diagnostic's own path rather than being
// dropped: the sentence is still worth reading, and a note at the wrong file
// would be worse than one at the diagnostic's.
func relatedLocations(fs *source.FileSet, entries []diag.Note, fallbackPath string) []RelatedLocation {
	if len(entries) == 0 {
		return nil
	}
	out := make([]RelatedLocation, 0, len(entries))
	for _, entry := range entries {
		location := RelatedLocation{FilePath: fallbackPath, Message: entry.Msg}
		if fs.HasFile(entry.Span.File) {
			if file := fs.Get(entry.Span.File); file != nil && file.Path != "" {
				location.FilePath = file.Path
			}
			start, end := fs.Resolve(entry.Span)
			location.StartLine, location.StartCol = int(start.Line), int(start.Col)
			location.EndLine, location.EndCol = int(end.Line), int(end.Col)
		}
		out = append(out, location)
	}
	return out
}

// fixOffers materialises the attached fixes and resolves their positions.
//
// A fix that cannot be built, or that resolves to no edit, contributes nothing
// rather than an id that would later resolve to nothing.
func fixOffers(fs *source.FileSet, item *diag.Diagnostic) []FixOffer {
	if len(item.Fixes) == 0 {
		return nil
	}
	resolved, err := diag.MaterializeFixes(diag.FixBuildContext{FileSet: fs}, item.Fixes)
	if err != nil {
		return nil
	}
	out := make([]FixOffer, 0, len(resolved))
	for i, fix := range resolved {
		if fix == nil || len(fix.Edits) == 0 {
			continue
		}
		id := fix.ID
		if id == "" {
			id = fmt.Sprintf("%s-%d-%d-%d", item.Code.ID(), item.Primary.File, item.Primary.Start, i)
		}
		edits := make([]FixEditLocation, 0, len(fix.Edits))
		for _, edit := range fix.Edits {
			if !fs.HasFile(edit.Span.File) {
				edits = nil
				break
			}
			file := fs.Get(edit.Span.File)
			if file == nil || file.Path == "" {
				edits = nil
				break
			}
			start, end := fs.Resolve(edit.Span)
			edits = append(edits, FixEditLocation{
				FilePath:  file.Path,
				StartLine: int(start.Line), StartCol: int(start.Col),
				EndLine: int(end.Line), EndCol: int(end.Col),
				NewText: edit.NewText, OldText: edit.OldText,
			})
		}
		if len(edits) == 0 {
			continue
		}
		out = append(out, FixOffer{
			ID: id, Title: fix.Title,
			AlwaysSafe: fix.Applicability == diag.FixApplicabilityAlwaysSafe,
			Edits:      edits,
		})
	}
	return out
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
