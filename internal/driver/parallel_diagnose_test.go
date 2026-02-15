package driver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/diagfmt"
	"surge/internal/source"
)

func TestDiagnoseDirWithOptions_RelativeDirDoesNotDoubleJoin(t *testing.T) {
	tmp := t.TempDir()
	projDir := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(filepath.Join(projDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Intentionally missing semicolon to force at least one diagnostic.
	badProgram := "@entrypoint\nfn main() {\n    print(\"hi\")\n}\n"

	mainPath := filepath.Join(projDir, "main.sg")
	if err := os.WriteFile(mainPath, []byte(badProgram), 0o600); err != nil {
		t.Fatalf("write main.sg: %v", err)
	}
	nestedPath := filepath.Join(projDir, "nested", "nested.sg")
	if err := os.WriteFile(nestedPath, []byte(badProgram), 0o600); err != nil {
		t.Fatalf("write nested.sg: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	relProj, err := filepath.Rel(cwd, projDir)
	if err != nil {
		t.Fatalf("rel path: %v", err)
	}
	if filepath.IsAbs(relProj) {
		t.Fatalf("expected relative dir path, got %q", relProj)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 8,
	}
	fs, results, err := DiagnoseDirWithOptions(context.Background(), relProj, &opts, 1)
	if err != nil {
		t.Fatalf("DiagnoseDirWithOptions error: %v", err)
	}
	if fs == nil {
		t.Fatalf("expected fileset")
	}
	if len(results) < 2 {
		t.Fatalf("expected >= 2 results, got %d", len(results))
	}

	found := map[string]DiagnoseDirResult{}
	for _, res := range results {
		found[res.Path] = res
	}
	for _, want := range []string{mainPath, nestedPath} {
		res, ok := found[want]
		if !ok {
			t.Fatalf("missing result for %s; got paths: %#v", want, keys(found))
		}
		if res.Bag == nil || res.Bag.Len() == 0 {
			t.Fatalf("expected diagnostics for %s", want)
		}
		if !fs.HasFile(res.FileID) {
			t.Fatalf("fileset missing file ID %d for %s", res.FileID, want)
		}
		f := fs.Get(res.FileID)
		if f.Flags&source.FileVirtual != 0 {
			t.Fatalf("expected %s to be loaded from disk, got virtual file", want)
		}

		// Directory diagnosis feeds into pretty formatting in the CLI; ensure it never panics.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("pretty formatting panicked for %s: %v", want, r)
				}
			}()
			var buf bytes.Buffer
			diagfmt.Pretty(&buf, res.Bag, fs, diagfmt.PrettyOpts{Context: 1, PathMode: diagfmt.PathModeRelative})
		}()
	}
}

func TestDiagnoseDirWithOptions_ModuleDirectoryNoCascadingSEM3005(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Skip("stdlib root not found")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	projDir := t.TempDir()
	moduleDir := filepath.Join(projDir, "m")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}

	boardPath := filepath.Join(moduleDir, "board.sg")
	boardSrc := strings.TrimSpace(`
pragma module::m;

pub type Board = { cell: Option<int> }

fn x() -> Option<int> {
	return nothing;
}
`)
	if err := os.WriteFile(boardPath, []byte(boardSrc), 0o600); err != nil {
		t.Fatalf("write board.sg: %v", err)
	}

	mainPath := filepath.Join(projDir, "main.sg")
	mainSrc := "fn main() {}\n"
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0o600); err != nil {
		t.Fatalf("write main.sg: %v", err)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 64,
	}
	single, err := DiagnoseWithOptions(context.Background(), boardPath, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if hasDiagCode(single.Bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("baseline single-file diagnostics contained SemaUnresolvedSymbol for %s: %v", boardPath, single.Bag.Items())
	}

	_, dirResults, err := DiagnoseDirWithOptions(context.Background(), projDir, &opts, 1)
	if err != nil {
		t.Fatalf("DiagnoseDirWithOptions error: %v", err)
	}

	boardRes, ok := lookupResult(dirResults, boardPath)
	if !ok {
		t.Fatalf("missing result for %s", boardPath)
	}
	if hasDiagCode(boardRes.Bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("module directory diagnostics still contains SemaUnresolvedSymbol for %s: %v", boardPath, boardRes.Bag.Items())
	}

	mainRes, ok := lookupResult(dirResults, mainPath)
	if !ok {
		t.Fatalf("missing result for %s", mainPath)
	}
	if hasDiagCode(mainRes.Bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("non-module file diagnostics unexpectedly include SemaUnresolvedSymbol for %s: %v", mainPath, mainRes.Bag.Items())
	}
}

func TestDiagnoseDirWithOptions_ModuleDirectoryExportsToNonModuleFile(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Skip("stdlib root not found")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	projDir := t.TempDir()
	moduleDir := filepath.Join(projDir, "mod")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}

	piecePath := filepath.Join(moduleDir, "piece.sg")
	pieceSrc := strings.TrimSpace(`
pragma module::mod;

pub enum Color: int = {
    White,
    Black,
}

pub type Piece = {
    color: Color,
}
`)
	if err := os.WriteFile(piecePath, []byte(pieceSrc), 0o600); err != nil {
		t.Fatalf("write piece.sg: %v", err)
	}

	boardPath := filepath.Join(moduleDir, "board.sg")
	boardSrc := strings.TrimSpace(`
pragma module::mod;

pub type Board = {
    score: int,
}
`)
	if err := os.WriteFile(boardPath, []byte(boardSrc), 0o600); err != nil {
		t.Fatalf("write board.sg: %v", err)
	}

	mainPath := filepath.Join(projDir, "main.sg")
	mainSrc := strings.TrimSpace(`
import mod::{Color, Piece, Board};

fn main() {
	let mut piece = Piece { color = Color::White };
	let mut board = Board { score = 0 };
	piece.color = Color::Black;
	board.score = 1;
	let _ = piece.color;
	let _ = board.score;
}
`)
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0o600); err != nil {
		t.Fatalf("write main.sg: %v", err)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 64,
	}
	_, dirResults, err := DiagnoseDirWithOptions(context.Background(), projDir, &opts, 1)
	if err != nil {
		t.Fatalf("DiagnoseDirWithOptions error: %v", err)
	}

	mainRes, ok := lookupResult(dirResults, mainPath)
	if !ok {
		t.Fatalf("missing result for %s", mainPath)
	}
	if hasDiagCode(mainRes.Bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("main diagnostics still contain SemaUnresolvedSymbol: %v", mainRes.Bag.Items())
	}
	if hasDiagCode(mainRes.Bag, diag.SemaModuleMemberNotFound) {
		t.Fatalf("main diagnostics still contain module member errors: %v", mainRes.Bag.Items())
	}
}

func keys[M ~map[string]V, V any](m M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasDiagCode(bag *diag.Bag, code diag.Code) bool {
	if bag == nil {
		return false
	}
	for _, d := range bag.Items() {
		if d.Code == code {
			return true
		}
	}
	return false
}

func lookupResult(results []DiagnoseDirResult, path string) (DiagnoseDirResult, bool) {
	target := filepath.Clean(path)
	for _, res := range results {
		if filepath.Clean(res.Path) == target {
			return res, true
		}
	}
	return DiagnoseDirResult{}, false
}
