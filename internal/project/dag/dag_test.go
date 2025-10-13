package dag

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
)

func idsToNames(idx ModuleIndex, ids []ModuleID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = idx.IDToName[int(id)]
	}
	return out
}

func batchesToNames(idx ModuleIndex, batches [][]ModuleID) [][]string {
	out := make([][]string, len(batches))
	for i, batch := range batches {
		out[i] = idsToNames(idx, batch)
	}
	return out
}

func TestBuildIndexIncludesImports(t *testing.T) {
	metas := []project.ModuleMeta{
		{
			Path: "core/main",
			Imports: []project.ImportMeta{
				{Path: "lib/math"},
				{Path: "lib/util"},
			},
		},
		{Path: "lib/util"},
	}

	idx := BuildIndex(metas)

	if len(idx.IDToName) != 3 {
		t.Fatalf("unexpected module count: %d", len(idx.IDToName))
	}

	wantNames := []string{"core/main", "lib/math", "lib/util"}
	for i, want := range wantNames {
		if got := idx.IDToName[i]; got != want {
			t.Fatalf("idx.IDToName[%d] = %q, want %q", i, got, want)
		}
		if id, ok := idx.NameToID[want]; !ok || int(id) != i {
			t.Fatalf("idx.NameToID[%q] = %v, want %d", want, id, i)
		}
	}
}

func TestBuildGraphReportsMissingModules(t *testing.T) {
	appSpan := source.Span{File: 1, Start: 0, End: 10}
	coreSpan := source.Span{File: 2, Start: 0, End: 8}
	utilImportSpan := source.Span{File: 1, Start: 5, End: 8}

	appMeta := project.ModuleMeta{
		Path: "app",
		Span: appSpan,
		Imports: []project.ImportMeta{
			{Path: "core", Span: source.Span{File: 1, Start: 1, End: 4}},
			{Path: "util", Span: utilImportSpan},
		},
	}
	coreMeta := project.ModuleMeta{
		Path: "core",
		Span: coreSpan,
		Imports: []project.ImportMeta{
			{Path: "util", Span: source.Span{File: 2, Start: 2, End: 5}},
		},
	}

	bagApp := diag.NewBag(10)
	bagCore := diag.NewBag(10)

	nodes := []ModuleNode{
		{Meta: appMeta, Reporter: &diag.BagReporter{Bag: bagApp}},
		{Meta: coreMeta, Reporter: &diag.BagReporter{Bag: bagCore}},
	}
	idx := BuildIndex([]project.ModuleMeta{appMeta, coreMeta})
	graph, _ := BuildGraph(idx, nodes)

	appID := idx.NameToID["app"]
	coreID := idx.NameToID["core"]
	utilID := idx.NameToID["util"]

	appDeps := graph.Edges[int(appID)]
	if len(appDeps) != 2 || appDeps[0] != coreID || appDeps[1] != utilID {
		t.Fatalf("app deps = %v, want [%v %v]", appDeps, coreID, utilID)
	}

	coreDeps := graph.Edges[int(coreID)]
	if len(coreDeps) != 1 || coreDeps[0] != utilID {
		t.Fatalf("core deps = %v, want [%v]", coreDeps, utilID)
	}

	if !graph.Present[int(appID)] || !graph.Present[int(coreID)] || graph.Present[int(utilID)] {
		t.Fatalf("unexpected Present flags: %v", graph.Present)
	}

	if bagApp.Len() != 1 {
		t.Fatalf("app diagnostics = %d, want 1", bagApp.Len())
	}
	if bagApp.Items()[0].Code != diag.ProjMissingModule {
		t.Fatalf("app diag code = %v, want %v", bagApp.Items()[0].Code, diag.ProjMissingModule)
	}

	if bagCore.Len() != 1 {
		t.Fatalf("core diagnostics = %d, want 1", bagCore.Len())
	}
	if bagCore.Items()[0].Code != diag.ProjMissingModule {
		t.Fatalf("core diag code = %v, want %v", bagCore.Items()[0].Code, diag.ProjMissingModule)
	}
}

func TestBuildGraphDuplicateModules(t *testing.T) {
	spanA := source.Span{File: 1, Start: 0, End: 5}
	spanB := source.Span{File: 2, Start: 0, End: 5}

	metaA := project.ModuleMeta{Path: "dup/mod", Span: spanA}
	metaB := project.ModuleMeta{Path: "dup/mod", Span: spanB}

	bagA := diag.NewBag(10)
	bagB := diag.NewBag(10)

	nodes := []ModuleNode{
		{Meta: metaA, Reporter: &diag.BagReporter{Bag: bagA}},
		{Meta: metaB, Reporter: &diag.BagReporter{Bag: bagB}},
	}

	idx := BuildIndex([]project.ModuleMeta{metaA, metaB})
	graph, slots := BuildGraph(idx, nodes)

	if !graph.Present[idx.NameToID["dup/mod"]] {
		t.Fatalf("expected module to be present")
	}

	if bagA.Len() != 0 {
		t.Fatalf("unexpected diagnostics for first module: %v", bagA.Items())
	}
	if bagB.Len() != 1 {
		t.Fatalf("expected one diagnostic for duplicate, got %d", bagB.Len())
	}
	if bagB.Items()[0].Code != diag.ProjDuplicateModule {
		t.Fatalf("duplicate code = %v, want %v", bagB.Items()[0].Code, diag.ProjDuplicateModule)
	}

	// ensure slots keep original metadata
	slot := slots[int(idx.NameToID["dup/mod"])]
	if !slot.Present || slot.Meta.Span != spanA {
		t.Fatalf("expected slot to hold first module metadata")
	}
}

func TestToposortKahnBatches(t *testing.T) {
	metas := []project.ModuleMeta{
		{Path: "b", Imports: []project.ImportMeta{{Path: "c"}}},
		{Path: "a"},
		{Path: "c"},
	}

	nodes := []ModuleNode{
		{Meta: metas[0]},
		{Meta: metas[1]},
		{Meta: metas[2]},
	}

	idx := BuildIndex(metas)
	graph, _ := BuildGraph(idx, nodes)

	topo := ToposortKahn(graph)
	if topo.Cyclic {
		t.Fatalf("expected acyclic graph")
	}

	orderNames := idsToNames(idx, topo.Order)
	if len(orderNames) != 3 {
		t.Fatalf("order len = %d, want 3", len(orderNames))
	}
	wantOrder := []string{"a", "b", "c"}
	for i, want := range wantOrder {
		if orderNames[i] != want {
			t.Fatalf("order[%d] = %q, want %q", i, orderNames[i], want)
		}
	}

	batches := batchesToNames(idx, topo.Batches)
	wantBatches := [][]string{{"a", "b"}, {"c"}}
	if len(batches) != len(wantBatches) {
		t.Fatalf("batches len = %d, want %d", len(batches), len(wantBatches))
	}
	for i := range wantBatches {
		if len(batches[i]) != len(wantBatches[i]) {
			t.Fatalf("batch[%d] len = %d, want %d", i, len(batches[i]), len(wantBatches[i]))
		}
		for j, want := range wantBatches[i] {
			if batches[i][j] != want {
				t.Fatalf("batch[%d][%d] = %q, want %q", i, j, batches[i][j], want)
			}
		}
	}
}

func TestReportCycles(t *testing.T) {
	spanA := source.Span{File: 1, Start: 0, End: 4}
	spanB := source.Span{File: 2, Start: 0, End: 4}

	metaA := project.ModuleMeta{
		Path: "a",
		Span: spanA,
		Imports: []project.ImportMeta{
			{Path: "b", Span: spanA},
		},
	}
	metaB := project.ModuleMeta{
		Path: "b",
		Span: spanB,
		Imports: []project.ImportMeta{
			{Path: "a", Span: spanB},
		},
	}

	bagA := diag.NewBag(10)
	bagB := diag.NewBag(10)

	nodes := []ModuleNode{
		{Meta: metaA, Reporter: &diag.BagReporter{Bag: bagA}},
		{Meta: metaB, Reporter: &diag.BagReporter{Bag: bagB}},
	}

	idx := BuildIndex([]project.ModuleMeta{metaA, metaB})
	graph, slots := BuildGraph(idx, nodes)

	topo := ToposortKahn(graph)
	if !topo.Cyclic || len(topo.Cycles) != 2 {
		t.Fatalf("expected cycle with two modules, got %+v", topo)
	}

	ReportCycles(idx, slots, topo)

	if bagA.Len() != 1 || bagA.Items()[0].Code != diag.ProjImportCycle {
		t.Fatalf("module a diagnostics = %v", bagA.Items())
	}
	if bagB.Len() != 1 || bagB.Items()[0].Code != diag.ProjImportCycle {
		t.Fatalf("module b diagnostics = %v", bagB.Items())
	}
}
