package diagnose

import (
	"context"
	"path/filepath"

	"surge/internal/ast"
	"surge/internal/lexer"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/token"
)

// AnalysisSnapshot captures analysis artefacts for LSP queries.
type AnalysisSnapshot struct {
	ProjectRoot   string
	FileSet       *source.FileSet
	Files         map[string]*AnalysisFile
	Diagnostics   []Diagnostic
	ModuleExports map[string]*symbols.ModuleExports
}

// AnalysisFile bundles per-file analysis outputs.
type AnalysisFile struct {
	Path    string
	FileID  source.FileID
	ASTFile ast.FileID
	Builder *ast.Builder
	Symbols *symbols.Result
	Sema    *sema.Result
	Tokens  []token.Token
}

// AnalyzeWorkspace runs diagnostics and returns a reusable analysis snapshot.
func AnalyzeWorkspace(ctx context.Context, opts *DiagnoseOptions, overlay FileOverlay) (*AnalysisSnapshot, []Diagnostic, error) {
	var runOpts DiagnoseOptions
	if opts != nil {
		runOpts = *opts
	}
	runOpts.KeepArtifacts = true
	runOpts.FullModuleGraph = true
	workspace := WorkspaceResult{}
	runOpts.Result = &workspace
	diags, err := DiagnoseWorkspace(ctx, &runOpts, overlay)
	snapshot := buildSnapshot(&runOpts, &workspace, diags)
	return snapshot, diags, err
}

// AnalyzeFiles runs diagnostics for an explicit file set and returns a reusable analysis snapshot.
func AnalyzeFiles(ctx context.Context, opts *DiagnoseOptions, files []string, overlay FileOverlay) (*AnalysisSnapshot, []Diagnostic, error) {
	var runOpts DiagnoseOptions
	if opts != nil {
		runOpts = *opts
	}
	runOpts.KeepArtifacts = true
	runOpts.FullModuleGraph = true
	workspace := WorkspaceResult{}
	runOpts.Result = &workspace
	diags, err := DiagnoseFiles(ctx, &runOpts, files, overlay)
	snapshot := buildSnapshot(&runOpts, &workspace, diags)
	return snapshot, diags, err
}

func buildSnapshot(opts *DiagnoseOptions, workspace *WorkspaceResult, diags []Diagnostic) *AnalysisSnapshot {
	if workspace == nil {
		return nil
	}
	snapshot := &AnalysisSnapshot{
		Files:       make(map[string]*AnalysisFile),
		Diagnostics: diags,
	}
	if opts != nil {
		snapshot.ProjectRoot = opts.ProjectRoot
	}
	if workspace.ModuleExports != nil {
		snapshot.ModuleExports = workspace.ModuleExports
	}

	switch workspace.Mode {
	case WorkspaceModeFile:
		if workspace.FileResult == nil || workspace.FileResult.FileSet == nil {
			return nil
		}
		snapshot.FileSet = workspace.FileResult.FileSet
		file := workspace.FileResult.File
		path := ""
		fileID := source.FileID(0)
		if file != nil {
			path = file.Path
			fileID = file.ID
		}
		if key := snapshotPathKey(path); key != "" {
			af := &AnalysisFile{
				Path:    path,
				FileID:  fileID,
				ASTFile: workspace.FileResult.FileID,
				Builder: workspace.FileResult.Builder,
				Symbols: workspace.FileResult.Symbols,
				Sema:    workspace.FileResult.Sema,
			}
			if file != nil {
				af.Tokens = lexTokens(file)
			}
			snapshot.Files[key] = af
		}
	case WorkspaceModeDir:
		if workspace.DirFileSet == nil {
			return nil
		}
		snapshot.FileSet = workspace.DirFileSet
		for _, res := range workspace.DirResults {
			path := res.Path
			fileID := res.FileID
			var file *source.File
			if snapshot.FileSet != nil {
				if snapshot.FileSet.HasFile(res.FileID) {
					file = snapshot.FileSet.Get(res.FileID)
				} else if res.Path != "" {
					if latestID, ok := snapshot.FileSet.GetLatest(res.Path); ok {
						file = snapshot.FileSet.Get(latestID)
					}
				}
				if file != nil {
					path = file.Path
					fileID = file.ID
				}
			}
			key := snapshotPathKey(path)
			if key == "" {
				continue
			}
			af := &AnalysisFile{
				Path:    path,
				FileID:  fileID,
				ASTFile: res.ASTFile,
				Builder: res.Builder,
				Symbols: res.Symbols,
				Sema:    res.Sema,
			}
			if file != nil {
				af.Tokens = lexTokens(file)
			}
			snapshot.Files[key] = af
		}
	default:
		return nil
	}

	return snapshot
}

func snapshotPathKey(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := source.AbsolutePath(path); err == nil {
		return abs
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func lexTokens(file *source.File) []token.Token {
	if file == nil {
		return nil
	}
	lx := lexer.New(file, lexer.Options{})
	out := make([]token.Token, 0, len(file.LineIdx))
	for {
		tok := lx.Next()
		if tok.Kind.IsEOF() {
			break
		}
		out = append(out, tok)
	}
	return out
}
