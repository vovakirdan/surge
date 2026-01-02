package hir

import (
	"context"
	"fmt"

	"surge/internal/sema"
	"surge/internal/trace"
	"surge/internal/types"
)

func BuildBorrowGraph(ctx context.Context, hfn *Func, semaRes *sema.Result) (*BorrowGraph, *MovePlan, error) {
	if hfn == nil || semaRes == nil {
		return nil, nil, nil
	}
	tracer := trace.FromContext(ctx)

	graphSpan := trace.Begin(tracer, trace.ScopePass, "hir_build_borrow_graph", 0)
	graphSpan.WithExtra("func", hfn.Name)
	bg := &BorrowGraph{
		Func:     hfn.ID,
		OutEdges: make(map[LocalID][]int),
		InEdges:  make(map[LocalID][]int),
	}

	locals := collectFuncLocals(hfn)

	// Collect borrows that belong to this function.
	//
	// Mapping note:
	// - Borrow checker tracks "locals" as binding symbols (let/param) via symbols.SymbolID.
	// - HIR LocalID is currently treated as that SymbolID (cast).
	// - Borrower ("From") is mapped via sema.Result.BorrowBindings (BorrowID -> SymbolID).
	// - Projections (field/index/deref) are not materialized in HIR BorrowEdge yet (base LocalID only).
	borrowInfos := make([]sema.BorrowInfo, 0, len(semaRes.Borrows))
	borrowInfoByID := make(map[sema.BorrowID]sema.BorrowInfo)
	for _, info := range semaRes.Borrows {
		ownerLocal := LocalID(info.Place.Base)
		var borrowerLocal LocalID
		if semaRes.BorrowBindings != nil {
			if sym := semaRes.BorrowBindings[info.ID]; sym.IsValid() {
				borrowerLocal = LocalID(sym)
			}
		}
		if _, ok := locals[ownerLocal]; !ok {
			if _, ok := locals[borrowerLocal]; !ok {
				continue
			}
		}
		if info.ID == sema.NoBorrowID {
			continue
		}
		borrowInfos = append(borrowInfos, info)
		borrowInfoByID[info.ID] = info
	}

	// Build edges from borrow checker metadata.
	for _, info := range borrowInfos {
		var from LocalID
		if semaRes.BorrowBindings != nil {
			if sym := semaRes.BorrowBindings[info.ID]; sym.IsValid() {
				from = LocalID(sym)
			}
		}
		to := LocalID(info.Place.Base)

		edge := BorrowEdge{
			From:  from,
			To:    to,
			Kind:  lowerBorrowKind(info.Kind),
			Span:  info.Span,
			Scope: ScopeID(info.Life.ToScope),
		}
		idx := len(bg.Edges)
		bg.Edges = append(bg.Edges, edge)
		if edge.From.IsValid() {
			bg.OutEdges[edge.From] = append(bg.OutEdges[edge.From], idx)
		}
		if edge.To.IsValid() {
			bg.InEdges[edge.To] = append(bg.InEdges[edge.To], idx)
		}
	}

	// Track restrictions to surface in MovePlan.
	moveRestrictions := make(map[LocalID]string)

	// Map borrow checker event log into HIR BorrowEvents.
	var nextEventID EventID = 1
	for i := range semaRes.BorrowEvents {
		ev := &semaRes.BorrowEvents[i]
		if !borrowEventBelongsToFunc(ev, locals, borrowInfoByID, semaRes) {
			continue
		}

		hEv := BorrowEvent{
			ID:    nextEventID,
			Kind:  lowerBorrowEventKind(ev.Kind),
			Span:  ev.Span,
			Scope: ScopeID(ev.Scope),
			Note:  borrowEventNote(ev),
		}
		nextEventID++

		switch ev.Kind {
		case sema.BorrowEvBorrowStart:
			hEv.Peer = LocalID(ev.Place.Base)
			if ev.Borrow != sema.NoBorrowID && semaRes.BorrowBindings != nil {
				if sym := semaRes.BorrowBindings[ev.Borrow]; sym.IsValid() {
					hEv.Local = LocalID(sym)
				}
			}
		case sema.BorrowEvBorrowEnd:
			if ev.Binding.IsValid() {
				hEv.Local = LocalID(ev.Binding)
			} else if ev.Borrow != sema.NoBorrowID && semaRes.BorrowBindings != nil {
				if sym := semaRes.BorrowBindings[ev.Borrow]; sym.IsValid() {
					hEv.Local = LocalID(sym)
				}
			}
			if ev.Place.Base.IsValid() {
				hEv.Peer = LocalID(ev.Place.Base)
			}
		case sema.BorrowEvMove, sema.BorrowEvWrite:
			hEv.Local = LocalID(ev.Place.Base)
		case sema.BorrowEvDrop, sema.BorrowEvSpawnEscape:
			if ev.Binding.IsValid() {
				hEv.Local = LocalID(ev.Binding)
			}
			// If available, attach the borrowed-from owner as the peer.
			if ev.Place.Base.IsValid() {
				hEv.Peer = LocalID(ev.Place.Base)
			} else if ev.Borrow != sema.NoBorrowID {
				if info, ok := borrowInfoByID[ev.Borrow]; ok {
					hEv.Peer = LocalID(info.Place.Base)
				}
			}
		}

		bg.Events = append(bg.Events, hEv)

		switch ev.Kind {
		case sema.BorrowEvSpawnEscape:
			if hEv.Local.IsValid() {
				if _, ok := moveRestrictions[hEv.Local]; !ok {
					moveRestrictions[hEv.Local] = "task escape"
				}
			}
		case sema.BorrowEvMove:
			if ev.Issue != sema.BorrowIssueNone {
				local := LocalID(ev.Place.Base)
				if local.IsValid() {
					if _, ok := moveRestrictions[local]; !ok {
						moveRestrictions[local] = fmt.Sprintf("move blocked by %s (B%d)", semaBorrowIssueKind(ev.Issue), ev.IssueBorrow)
					}
				}
			}
		case sema.BorrowEvWrite:
			if ev.Issue != sema.BorrowIssueNone {
				local := LocalID(ev.Place.Base)
				if local.IsValid() {
					if _, ok := moveRestrictions[local]; !ok {
						moveRestrictions[local] = fmt.Sprintf("write blocked by %s (B%d)", semaBorrowIssueKind(ev.Issue), ev.IssueBorrow)
					}
				}
			}
		}
	}

	graphSpan.WithExtra("edges", fmt.Sprint(len(bg.Edges))).
		WithExtra("events", fmt.Sprint(len(bg.Events))).
		End("")

	moveSpan := trace.Begin(tracer, trace.ScopePass, "hir_build_move_plan", 0)
	moveSpan.WithExtra("func", hfn.Name)
	mp := &MovePlan{
		Func:  hfn.ID,
		Local: make(map[LocalID]MoveInfo),
	}

	for localID, ty := range locals {
		if localID == NoLocalID {
			continue
		}
		mi := moveInfoForType(semaRes, ty)
		if why, ok := moveRestrictions[localID]; ok {
			mi.Policy = MoveForbidden
			mi.Why = why
		}
		mp.Local[localID] = mi
	}

	moveSpan.WithExtra("locals", fmt.Sprint(len(mp.Local))).End("")
	return bg, mp, nil
}

func borrowEventBelongsToFunc(ev *sema.BorrowEvent, locals map[LocalID]types.TypeID, borrowInfoByID map[sema.BorrowID]sema.BorrowInfo, semaRes *sema.Result) bool {
	if ev == nil {
		return false
	}
	if ev.Binding.IsValid() {
		if _, ok := locals[LocalID(ev.Binding)]; ok {
			return true
		}
	}
	if ev.Place.Base.IsValid() {
		if _, ok := locals[LocalID(ev.Place.Base)]; ok {
			return true
		}
	}
	if ev.Borrow != sema.NoBorrowID {
		if _, ok := borrowInfoByID[ev.Borrow]; ok {
			return true
		}
		if semaRes != nil && semaRes.BorrowBindings != nil {
			if sym := semaRes.BorrowBindings[ev.Borrow]; sym.IsValid() {
				if _, ok := locals[LocalID(sym)]; ok {
					return true
				}
			}
		}
	}
	return false
}

func lowerBorrowKind(k sema.BorrowKind) BorrowKind {
	switch k {
	case sema.BorrowShared:
		return BorrowShared
	case sema.BorrowMut:
		return BorrowMut
	default:
		return BorrowShared
	}
}

func lowerBorrowEventKind(k sema.BorrowEventKind) BorrowEventKind {
	switch k {
	case sema.BorrowEvBorrowStart:
		return EvBorrowStart
	case sema.BorrowEvBorrowEnd:
		return EvBorrowEnd
	case sema.BorrowEvMove:
		return EvMove
	case sema.BorrowEvWrite:
		return EvWrite
	case sema.BorrowEvDrop:
		return EvDrop
	case sema.BorrowEvSpawnEscape:
		return EvSpawnEscape
	default:
		return EvRead
	}
}

func borrowEventNote(ev *sema.BorrowEvent) string {
	if ev == nil {
		return ""
	}
	note := ev.Note
	if ev.Issue != sema.BorrowIssueNone {
		s := fmt.Sprintf("issue=%s", semaBorrowIssueKind(ev.Issue))
		if ev.IssueBorrow != sema.NoBorrowID {
			s += fmt.Sprintf(" borrow=B%d", ev.IssueBorrow)
		}
		if note == "" {
			note = s
		} else {
			note = note + " " + s
		}
	}
	if ev.Borrow != sema.NoBorrowID {
		tag := fmt.Sprintf("B%d", ev.Borrow)
		if note == "" {
			note = tag
		} else {
			note = note + " " + tag
		}
	}
	return note
}

func semaBorrowIssueKind(k sema.BorrowIssueKind) string {
	switch k {
	case sema.BorrowIssueConflictShared:
		return "conflict_shared"
	case sema.BorrowIssueConflictMut:
		return "conflict_mut"
	case sema.BorrowIssueFrozen:
		return "frozen"
	case sema.BorrowIssueTaken:
		return "taken"
	default:
		return "none"
	}
}

func collectFuncLocals(f *Func) map[LocalID]types.TypeID {
	out := make(map[LocalID]types.TypeID)
	if f == nil {
		return out
	}
	for _, p := range f.Params {
		if p.SymbolID.IsValid() {
			out[LocalID(p.SymbolID)] = p.Type
		}
	}
	if f.Body != nil {
		collectLocalsInBlock(out, f.Body)
	}
	return out
}

func collectLocalsInBlock(out map[LocalID]types.TypeID, b *Block) {
	if b == nil {
		return
	}
	for i := range b.Stmts {
		s := &b.Stmts[i]
		switch s.Kind {
		case StmtLet:
			data, ok := s.Data.(LetData)
			if ok && data.SymbolID.IsValid() {
				out[LocalID(data.SymbolID)] = data.Type
			}
		case StmtBlock:
			if data, ok := s.Data.(BlockStmtData); ok {
				collectLocalsInBlock(out, data.Block)
			}
		case StmtIf:
			if data, ok := s.Data.(IfStmtData); ok {
				collectLocalsInBlock(out, data.Then)
				collectLocalsInBlock(out, data.Else)
			}
		case StmtWhile:
			if data, ok := s.Data.(WhileData); ok {
				collectLocalsInBlock(out, data.Body)
			}
		case StmtFor:
			if data, ok := s.Data.(ForData); ok {
				if data.Init != nil {
					if data.Init.Kind == StmtLet {
						if letData, ok := data.Init.Data.(LetData); ok && letData.SymbolID.IsValid() {
							out[LocalID(letData.SymbolID)] = letData.Type
						}
					}
				}
				collectLocalsInBlock(out, data.Body)
			}
		}
	}
}

func moveInfoForType(semaRes *sema.Result, ty types.TypeID) MoveInfo {
	if semaRes == nil || semaRes.TypeInterner == nil || ty == types.NoTypeID {
		return MoveInfo{Policy: MoveUnknown, Why: "unknown type"}
	}
	if semaRes.IsCopyType(ty) {
		return MoveInfo{Policy: MoveCopy, Why: "copy type"}
	}
	resolved := resolveAliasHIR(semaRes.TypeInterner, ty)
	tt, ok := semaRes.TypeInterner.Lookup(resolved)
	if !ok {
		return MoveInfo{Policy: MoveUnknown, Why: "unresolved type"}
	}

	switch tt.Kind {
	case types.KindReference:
		return MoveInfo{Policy: MoveAllowed, Why: "non-copy reference"}
	case types.KindPointer, types.KindFn:
		return MoveInfo{Policy: MoveCopy, Why: "copy type"}
	case types.KindGenericParam:
		return MoveInfo{Policy: MoveUnknown, Why: "generic"}
	default:
		// Conservative v1: any non-copy value type may require drop.
		return MoveInfo{Policy: MoveNeedsDrop, Why: "non-copy (drop)"}
	}
}

func resolveAliasHIR(in *types.Interner, id types.TypeID) types.TypeID {
	if in == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		tt, ok := in.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := in.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
		seen++
	}
	return id
}
