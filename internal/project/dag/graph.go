package dag

import (
	"fmt"
	"slices"
	"strings"

	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
)

type Graph struct {
	Edges   [][]ModuleID // Edges[from] = []to
	Indeg   []int        // входящие степени для Kahn (учитывает только присутствующие модули)
	Present []bool       // признак, что модуль реально существует (а не только импортируется)
}

type ModuleNode struct {
	Meta     project.ModuleMeta
	Reporter diag.Reporter
	Broken   bool
	FirstErr *diag.Diagnostic
}

type ModuleSlot struct {
	Meta     project.ModuleMeta
	Reporter diag.Reporter
	Present  bool
	Broken   bool
	FirstErr *diag.Diagnostic
}

func BuildGraph(idx ModuleIndex, nodes []ModuleNode) (Graph, []ModuleSlot) {
	nodeCount := len(idx.IDToName)
	g := Graph{
		Edges:   make([][]ModuleID, nodeCount),
		Indeg:   make([]int, nodeCount),
		Present: make([]bool, nodeCount),
	}
	slots := make([]ModuleSlot, nodeCount)
	for i, name := range idx.IDToName {
		slots[i].Meta.Path = name
	}

	for _, node := range nodes {
		meta := node.Meta
		if meta.Path == "" {
			continue
		}
		id, ok := idx.NameToID[meta.Path]
		if !ok {
			// не должно происходить, индекс строится на тех же метаданных
			continue
		}
		slot := &slots[int(id)]
		if slot.Present {
			if node.Reporter != nil {
				notes := make([]diag.Note, 0, 1)
				if slot.Meta.Span != (source.Span{}) {
					notes = append(notes, diag.Note{
						Span: slot.Meta.Span,
						Msg:  fmt.Sprintf("previous declaration of %q", slot.Meta.Path),
					})
				}
				node.Reporter.Report(
					diag.ProjDuplicateModule,
					diag.SevError,
					meta.Span,
					fmt.Sprintf("duplicate module %q", meta.Path),
					notes,
					nil,
				)
			}
			continue
		}
		slot.Meta = meta
		slot.Reporter = node.Reporter
		slot.Present = true
		slot.Broken = node.Broken
		slot.FirstErr = node.FirstErr
		g.Present[int(id)] = true
	}

	for from := range slots {
		slot := &slots[from]
		if !slot.Present || len(slot.Meta.Imports) == 0 {
			continue
		}
		seen := make(map[ModuleID]struct{}, len(slot.Meta.Imports))
		for _, dep := range slot.Meta.Imports {
			if dep.Path == "" {
				continue
			}
			toID, ok := idx.NameToID[dep.Path]
			if !ok {
				if slot.Reporter != nil {
					slot.Reporter.Report(
						diag.ProjMissingModule,
						diag.SevError,
						dep.Span,
						fmt.Sprintf("module %q imports unknown module %q", slot.Meta.Path, dep.Path),
						nil,
						nil,
					)
				}
				continue
			}
			if ModuleID(from) == toID {
				if slot.Reporter != nil {
					slot.Reporter.Report(
						diag.ProjSelfImport,
						diag.SevError,
						dep.Span,
						fmt.Sprintf("module %q imports itself", slot.Meta.Path),
						nil,
						nil,
					)
				}
				continue
			}
			if _, dup := seen[toID]; dup {
				continue
			}
			seen[toID] = struct{}{}

			g.Edges[from] = append(g.Edges[from], toID)
			if g.Present[int(toID)] {
				g.Indeg[int(toID)]++
			} else if slot.Reporter != nil {
				slot.Reporter.Report(
					diag.ProjMissingModule,
					diag.SevError,
					dep.Span,
					fmt.Sprintf("module %q imports missing module %q", slot.Meta.Path, idx.IDToName[int(toID)]),
					nil,
					nil,
				)
			}
		}
		if len(g.Edges[from]) > 1 {
			slices.Sort(g.Edges[from])
		}
	}

	return g, slots
}

func ReportCycles(idx ModuleIndex, slots []ModuleSlot, topo Topo) {
	if !topo.Cyclic || len(topo.Cycles) == 0 {
		return
	}
	names := make([]string, 0, len(topo.Cycles))
	for _, id := range topo.Cycles {
		names = append(names, idx.IDToName[int(id)])
	}
	summary := strings.Join(names, " -> ")

	for _, id := range topo.Cycles {
		slot := slots[int(id)]
		if !slot.Present || slot.Reporter == nil {
			continue
		}
		msg := fmt.Sprintf("module %q participates in an import cycle: %s", slot.Meta.Path, summary)
		slot.Reporter.Report(diag.ProjImportCycle, diag.SevError, slot.Meta.Span, msg, nil, nil)
	}
}

func ReportBrokenDeps(idx ModuleIndex, slots []ModuleSlot) {
	for i := range slots {
		slotFrom := &slots[i]
		if !slotFrom.Present || slotFrom.Reporter == nil || len(slotFrom.Meta.Imports) == 0 {
			continue
		}
		emitted := make(map[string]struct{}, len(slotFrom.Meta.Imports))
		for _, imp := range slotFrom.Meta.Imports {
			toID, ok := idx.NameToID[imp.Path]
			if !ok {
				continue
			}
			depSlot := slots[int(toID)]
			if !depSlot.Broken {
				continue
			}
			key := imp.Path + "|" + imp.Span.String()
			if _, seen := emitted[key]; seen {
				continue
			}
			emitted[key] = struct{}{}

			notes := []diag.Note(nil)
			if depSlot.FirstErr != nil {
				notes = append(notes, diag.Note{
					Span: depSlot.FirstErr.Primary,
					Msg:  fmt.Sprintf("first error in dependency: %s", depSlot.FirstErr.Message),
				})
			}

			msg := fmt.Sprintf("dependency module %q has errors", imp.Path)
			slotFrom.Reporter.Report(diag.ProjDependencyFailed, diag.SevError, imp.Span, msg, notes, nil)
		}
	}
}
