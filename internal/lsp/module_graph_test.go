package lsp

import (
	"strings"
	"testing"

	"surge/internal/driver/diagnose"
)

func TestAnalyzeWorkspaceModuleGraph(t *testing.T) {
	t.Setenv("SURGE_STDLIB", stdlibRoot(t))

	util := strings.Join([]string{
		"pub fn greet(name: string) -> string {",
		"    return name.trim();",
		"}",
		"",
	}, "\n")
	main := strings.Join([]string{
		"import util::greet;",
		"",
		"fn main() {",
		"    let s = \" hi \";",
		"    let t = s.trim();",
		"    print(greet(t));",
		"}",
		"",
	}, "\n")

	t.Run("disk", func(t *testing.T) {
		snapshot, paths := analyzeWorkspaceSnapshot(t, map[string]string{
			"util.sg": util,
			"main.sg": main,
		}, nil)
		assertModuleGraphSnapshot(t, snapshot, paths["main.sg"], main, paths["util.sg"])
	})

	t.Run("overlay", func(t *testing.T) {
		diskMain := strings.Join([]string{
			"fn main() {",
			"    let s = \" hi \";",
			"    let t = s.trim();",
			"    print(greet(t));",
			"}",
			"",
		}, "\n")
		snapshot, paths := analyzeWorkspaceSnapshot(t, map[string]string{
			"util.sg": util,
			"main.sg": diskMain,
		}, map[string]string{
			"main.sg": main,
		})
		assertModuleGraphSnapshot(t, snapshot, paths["main.sg"], main, paths["util.sg"])
	})
}

func assertModuleGraphSnapshot(t *testing.T, snapshot *diagnose.AnalysisSnapshot, mainPath, mainSrc, utilPath string) {
	t.Helper()
	uri := pathToURI(mainPath)

	af, file := snapshotFile(snapshot, uri)
	if af == nil || file == nil || af.Symbols == nil || af.Sema == nil {
		t.Fatalf("snapshot missing analysis: af=%v file=%v symbols=%v sema=%v", af != nil, file != nil, af != nil && af.Symbols != nil, af != nil && af.Sema != nil)
	}

	greetIdx := strings.Index(mainSrc, "greet(")
	if greetIdx < 0 {
		t.Fatal("missing greet call")
	}
	locs := buildDefinition(snapshot, uri, positionForOffsetUTF16(mainSrc, greetIdx))
	if len(locs) != 1 {
		t.Fatalf("expected 1 definition location, got %d", len(locs))
	}
	if locs[0].URI != pathToURI(utilPath) {
		t.Fatalf("expected definition in util.sg, got %q", locs[0].URI)
	}

	printIdx := strings.Index(mainSrc, "print(")
	if printIdx < 0 {
		t.Fatal("missing print call")
	}
	printHover := buildHover(snapshot, uri, positionForOffsetUTF16(mainSrc, printIdx))
	if printHover == nil {
		t.Fatal("expected hover for print")
	}
	if !strings.Contains(printHover.Contents.Value, "fn print") {
		t.Fatalf("expected print signature, got %q", printHover.Contents.Value)
	}

	trimIdx := strings.Index(mainSrc, "trim()")
	if trimIdx < 0 {
		t.Fatal("missing trim call")
	}
	trimHover := buildHover(snapshot, uri, positionForOffsetUTF16(mainSrc, trimIdx))
	if trimHover == nil {
		t.Fatal("expected hover for trim call")
	}
	if !strings.Contains(trimHover.Contents.Value, "fn trim") && !strings.Contains(trimHover.Contents.Value, "Type: `string`") {
		t.Fatalf("expected trim hover details, got %q", trimHover.Contents.Value)
	}
}
