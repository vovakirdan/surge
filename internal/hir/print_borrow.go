package hir

import (
	"fmt"
	"sort"

	"surge/internal/source"
)

func (p *Printer) printBorrowAndMovePlan(f *Func) {
	if f == nil {
		return
	}
	localNames := collectLocalNames(f)

	p.printf("\nborrow edges:\n")
	if f.Borrow == nil || len(f.Borrow.Edges) == 0 {
		p.printf("  <none>\n")
	} else {
		for _, edge := range f.Borrow.Edges {
			p.printf("  %s (%s) borrows %s", p.localLabel(edge.From, localNames), edge.Kind.String(), p.localLabel(edge.To, localNames))
			if edge.Span != (source.Span{}) {
				p.printf(" at %s", edge.Span.String())
			}
			if edge.Scope != NoScopeID {
				p.printf(" scope=S%d", edge.Scope)
			}
			p.printf("\n")
		}
	}

	p.printf("events:\n")
	if f.Borrow == nil || len(f.Borrow.Events) == 0 {
		p.printf("  <none>\n")
	} else {
		for _, ev := range f.Borrow.Events {
			p.printf("  %s %s", ev.Kind.String(), p.localLabel(ev.Local, localNames))
			if ev.Kind == EvBorrowStart && f.Borrow != nil && ev.Local.IsValid() && ev.Peer.IsValid() {
				if kind, ok := borrowKindForEvent(f.Borrow, ev); ok {
					p.printf(" -> %s (%s)", p.localLabel(ev.Peer, localNames), kind.String())
				} else {
					p.printf(" -> %s", p.localLabel(ev.Peer, localNames))
				}
			} else if ev.Peer.IsValid() {
				p.printf(" -> %s", p.localLabel(ev.Peer, localNames))
			}
			if ev.Span != (source.Span{}) {
				p.printf(" at %s", ev.Span.String())
			}
			if ev.Scope != NoScopeID {
				p.printf(" scope=S%d", ev.Scope)
			}
			if ev.Note != "" {
				p.printf(" note=%q", ev.Note)
			}
			p.printf("\n")
		}
	}

	p.printf("move plan:\n")
	if f.MovePlan == nil || len(f.MovePlan.Local) == 0 {
		p.printf("  <none>\n")
		return
	}
	ids := make([]LocalID, 0, len(f.MovePlan.Local))
	for id := range f.MovePlan.Local {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, localID := range ids {
		info := f.MovePlan.Local[localID]
		p.printf("  %s: %s", p.localLabel(localID, localNames), info.Policy.String())
		if info.Why != "" {
			p.printf(" (%s)", info.Why)
		}
		p.printf("\n")
	}
}

func collectLocalNames(f *Func) map[LocalID]string {
	names := make(map[LocalID]string)
	if f == nil {
		return names
	}
	for _, p := range f.Params {
		if p.SymbolID.IsValid() {
			names[LocalID(p.SymbolID)] = p.Name
		}
	}
	collectLocalNamesInBlock(names, f.Body)
	return names
}

func collectLocalNamesInBlock(names map[LocalID]string, b *Block) {
	if b == nil {
		return
	}
	for i := range b.Stmts {
		s := &b.Stmts[i]
		switch s.Kind {
		case StmtLet:
			data, ok := s.Data.(LetData)
			if ok && data.SymbolID.IsValid() && data.Name != "" {
				names[LocalID(data.SymbolID)] = data.Name
			}
		case StmtBlock:
			if data, ok := s.Data.(BlockStmtData); ok {
				collectLocalNamesInBlock(names, data.Block)
			}
		case StmtIf:
			if data, ok := s.Data.(IfStmtData); ok {
				collectLocalNamesInBlock(names, data.Then)
				collectLocalNamesInBlock(names, data.Else)
			}
		case StmtWhile:
			if data, ok := s.Data.(WhileData); ok {
				collectLocalNamesInBlock(names, data.Body)
			}
		case StmtFor:
			if data, ok := s.Data.(ForData); ok {
				if data.Init != nil {
					collectLocalNamesInBlock(names, &Block{Stmts: []Stmt{*data.Init}})
				}
				collectLocalNamesInBlock(names, data.Body)
			}
		}
	}
}

func (p *Printer) localLabel(id LocalID, names map[LocalID]string) string {
	if !id.IsValid() {
		return "_"
	}
	if names != nil {
		if name := names[id]; name != "" {
			return fmt.Sprintf("L%d(%s)", id, name)
		}
	}
	return fmt.Sprintf("L%d", id)
}

func borrowKindForEvent(bg *BorrowGraph, ev BorrowEvent) (BorrowKind, bool) {
	if bg == nil || ev.Kind != EvBorrowStart || !ev.Local.IsValid() || !ev.Peer.IsValid() {
		return BorrowShared, false
	}
	if bg.OutEdges != nil {
		for _, idx := range bg.OutEdges[ev.Local] {
			if idx < 0 || idx >= len(bg.Edges) {
				continue
			}
			edge := bg.Edges[idx]
			if edge.From != ev.Local || edge.To != ev.Peer {
				continue
			}
			if ev.Span != (source.Span{}) && edge.Span != ev.Span {
				continue
			}
			return edge.Kind, true
		}
	}
	for _, edge := range bg.Edges {
		if edge.From == ev.Local && edge.To == ev.Peer {
			if ev.Span != (source.Span{}) && edge.Span != ev.Span {
				continue
			}
			return edge.Kind, true
		}
	}
	return BorrowShared, false
}
