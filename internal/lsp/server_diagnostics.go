package lsp

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"surge/internal/driver"
	"surge/internal/driver/diagnose"
	"surge/internal/parser"
	"surge/internal/project"
)

func (s *Server) scheduleDiagnostics() {
	s.mu.Lock()
	seq := atomic.AddUint64(&s.analysisSeq, 1)
	atomic.StoreUint64(&s.latestSeq, seq)
	if s.diagCancel != nil {
		s.diagCancel()
	}
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
	}
	delay := s.debounce
	s.debounceTimer = time.AfterFunc(delay, func() {
		s.runDiagnostics(seq)
	})
	s.mu.Unlock()
}

func (s *Server) runDiagnostics(seq uint64) {
	if seq == 0 || !s.isLatestSeq(seq) {
		return
	}
	s.mu.Lock()
	if len(s.openDocs) == 0 {
		s.mu.Unlock()
		s.clearPublishedDiagnostics()
		s.clearSnapshotState()
		return
	}
	if s.diagCancel != nil {
		s.diagCancel()
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	s.diagCancel = cancel
	workspaceRoot := s.workspaceRoot
	firstFile := s.preferredOpenFileLocked()
	overlay := make(map[string]string, len(s.openDocs))
	docStates := make(map[string]docState, len(s.openDocs))
	docPaths := make(map[string]string, len(s.openDocs))
	docProjects := make(map[string]string, len(s.openDocs))
	for uri, text := range s.openDocs {
		path := uriToPath(uri)
		if path == "" {
			continue
		}
		canon := canonicalPath(path)
		if canon == "" {
			continue
		}
		overlay[canon] = text
		docStates[uri] = docState{
			version:    s.versions[uri],
			snapshotID: s.docSnapshots[uri],
		}
		docPaths[uri] = canon
		if root, ok, err := project.FindProjectRoot(filepath.Dir(canon)); err == nil && ok {
			docProjects[uri] = canonicalPath(root)
		}
	}
	s.mu.Unlock()

	projectRoot, mode := detectAnalysisScope(workspaceRoot, firstFile)
	if projectRoot == "" {
		s.clearPublishedDiagnostics()
		s.clearSnapshotState()
		return
	}
	projectRoot = canonicalPath(projectRoot)
	if s.updateAnalysisScope(projectRoot, mode) {
		s.clearPublishedDiagnostics()
		s.clearSnapshotState()
	}

	opts := diagnose.DiagnoseOptions{
		ProjectRoot:    projectRoot,
		BaseDir:        projectRoot,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: s.maxDiagnostics,
		DirectiveMode:  parser.DirectiveModeOff,
	}

	var (
		snapshot *diagnose.AnalysisSnapshot
		diags    []diagnose.Diagnostic
		err      error
	)
	analysisDocs := filterAnalysisDocs(docStates, docPaths, projectRoot, mode, nil, docProjects)
	if mode == modeOpenFiles {
		openFiles, openSet, filteredOverlay := filterOpenFiles(overlay, projectRoot)
		if len(openFiles) == 0 {
			s.clearPublishedDiagnostics()
			s.clearSnapshotState()
			return
		}
		analysisDocs = filterAnalysisDocs(docStates, docPaths, projectRoot, mode, openSet, docProjects)
		s.logAnalysisStart(analysisPlan{
			seq:  seq,
			root: projectRoot,
			mode: mode,
			docs: analysisDocs,
		})
		snapshot, diags, err = s.analyzeFiles(ctx, &opts, openFiles, diagnose.FileOverlay{Files: filteredOverlay})
		diags = filterDiagnosticsForOpenFiles(diags, openSet)
	} else {
		s.logAnalysisStart(analysisPlan{
			seq:  seq,
			root: projectRoot,
			mode: mode,
			docs: analysisDocs,
		})
		snapshot, diags, err = s.analyze(ctx, &opts, diagnose.FileOverlay{Files: overlay})
	}
	plan := analysisPlan{
		seq:  seq,
		root: projectRoot,
		mode: mode,
		docs: analysisDocs,
	}
	canceled := ctx.Err() != nil
	s.logAnalysisDone(plan, snapshot, diags, err, canceled)
	if err != nil {
		s.logf("diagnostics failed: %v", err)
	}
	if canceled && s.baseCtx != nil && s.baseCtx.Err() != nil {
		s.logAnalysisDiscard(plan, "canceled")
		return
	}
	if !s.analysisAllowed(plan, true) {
		return
	}
	if err == nil && snapshot != nil {
		if s.applySnapshot(plan, snapshot) {
			s.logAnalysisApply(plan)
		}
	}
	s.publishDiagnostics(plan, diags)
}

func (s *Server) publishDiagnostics(plan analysisPlan, diags []diagnose.Diagnostic) {
	if !s.analysisAllowed(plan, false) {
		return
	}
	grouped := make(map[string][]lspDiagnostic)
	for _, d := range diags {
		uri := pathToURI(d.FilePath)
		if uri == "" {
			continue
		}
		startLine := maxZero(d.StartLine - 1)
		startCol := maxZero(d.StartCol - 1)
		endLine := maxZero(d.EndLine - 1)
		endCol := maxZero(d.EndCol - 1)
		if d.EndLine == 0 && d.EndCol == 0 {
			endLine = startLine
			endCol = startCol
		}
		grouped[uri] = append(grouped[uri], lspDiagnostic{
			Range: lspRange{
				Start: position{Line: startLine, Character: startCol},
				End:   position{Line: endLine, Character: endCol},
			},
			Severity: d.Severity,
			Code:     d.Code,
			Source:   "surge",
			Message:  d.Message,
		})
	}

	targets := make([]string, 0, len(plan.docs))
	for uri := range plan.docs {
		targets = append(targets, uri)
	}
	sort.Strings(targets)

	s.mu.Lock()
	if !s.analysisMatchesLocked(plan) {
		s.mu.Unlock()
		return
	}
	prev := s.published
	s.published = make(map[string]struct{}, len(targets))
	for _, uri := range targets {
		s.published[uri] = struct{}{}
	}
	s.mu.Unlock()

	for _, uri := range targets {
		if !s.analysisMatches(plan) {
			return
		}
		list := grouped[uri]
		if err := s.sendPublish(uri, list); err != nil {
			s.logf("failed to publish diagnostics: %v", err)
		}
		s.logPublishDiagnostics(uri, len(list))
	}

	for uri := range prev {
		if _, ok := plan.docs[uri]; ok {
			continue
		}
		if !s.analysisMatches(plan) {
			return
		}
		if err := s.sendPublish(uri, nil); err != nil {
			s.logf("failed to clear diagnostics: %v", err)
		}
		s.logPublishDiagnostics(uri, 0)
	}
}

func (s *Server) updateAnalysisScope(root string, mode analysisMode) bool {
	s.mu.Lock()
	changed := s.analysisRoot != root || s.analysisMode != mode
	if changed {
		s.analysisRoot = root
		s.analysisMode = mode
	}
	s.mu.Unlock()
	return changed
}

func (s *Server) clearPublishedDiagnostics() {
	s.mu.Lock()
	if len(s.published) == 0 {
		s.mu.Unlock()
		return
	}
	prev := s.published
	s.published = make(map[string]struct{})
	s.mu.Unlock()
	for uri := range prev {
		if err := s.sendPublish(uri, nil); err != nil {
			s.logf("failed to clear diagnostics: %v", err)
		}
	}
}

func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	candidate := filepath.FromSlash(path)
	if abs, err := filepath.Abs(candidate); err == nil {
		candidate = abs
	}
	return filepath.ToSlash(filepath.Clean(candidate))
}

func pathWithinRoot(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	root = filepath.Clean(filepath.FromSlash(root))
	path = filepath.Clean(filepath.FromSlash(path))
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	prefix := ".." + string(filepath.Separator)
	return !strings.HasPrefix(rel, prefix)
}

func filterOpenFiles(overlay map[string]string, root string) (files []string, openSet map[string]struct{}, filtered map[string]string) {
	if len(overlay) == 0 {
		return nil, nil, nil
	}
	root = canonicalPath(root)
	files = make([]string, 0, len(overlay))
	openSet = make(map[string]struct{}, len(overlay))
	filtered = make(map[string]string, len(overlay))
	for path, text := range overlay {
		canon := canonicalPath(path)
		if canon == "" {
			continue
		}
		if root != "" && !pathWithinRoot(root, canon) {
			continue
		}
		if !strings.HasSuffix(canon, ".sg") {
			continue
		}
		if _, ok := openSet[canon]; ok {
			continue
		}
		openSet[canon] = struct{}{}
		files = append(files, canon)
		filtered[canon] = text
	}
	sort.Strings(files)
	return files, openSet, filtered
}

func filterDiagnosticsForOpenFiles(diags []diagnose.Diagnostic, openSet map[string]struct{}) []diagnose.Diagnostic {
	if len(diags) == 0 || len(openSet) == 0 {
		return nil
	}
	out := make([]diagnose.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if _, ok := openSet[canonicalPath(d.FilePath)]; ok {
			out = append(out, d)
		}
	}
	return out
}

func filterAnalysisDocs(docStates map[string]docState, docPaths map[string]string, root string, mode analysisMode, openSet map[string]struct{}, docProjects map[string]string) map[string]docState {
	if len(docStates) == 0 {
		return nil
	}
	out := make(map[string]docState, len(docStates))
	root = canonicalPath(root)
	for uri, state := range docStates {
		path := docPaths[uri]
		if path == "" {
			continue
		}
		if !strings.HasSuffix(path, ".sg") {
			continue
		}
		if mode == modeOpenFiles {
			if openSet != nil {
				if _, ok := openSet[path]; !ok {
					continue
				}
			}
		} else if root != "" && !pathWithinRoot(root, path) {
			continue
		}
		if len(docProjects) > 0 {
			projectRoot := docProjects[uri]
			if mode == modeProjectRoot {
				if projectRoot != "" && root != "" && projectRoot != root {
					continue
				}
			} else {
				if projectRoot != "" && projectRoot != root {
					continue
				}
			}
		}
		out[uri] = state
	}
	return out
}

func cloneDocStates(in map[string]docState) map[string]docState {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]docState, len(in))
	for uri, state := range in {
		out[uri] = state
	}
	return out
}

type analysisMismatch struct {
	uri      string
	expected docState
	current  docState
	missing  bool
}

type analysisPlan struct {
	seq  uint64
	root string
	mode analysisMode
	docs map[string]docState
}

func (s *Server) analysisMatchesLocked(plan analysisPlan) bool {
	if plan.seq == 0 {
		return false
	}
	if plan.seq < s.appliedSeq {
		return false
	}
	if s.analysisRoot != plan.root || s.analysisMode != plan.mode {
		return false
	}
	_, mismatch := s.firstDocMismatchLocked(plan.docs)
	return !mismatch
}

func (s *Server) analysisMatches(plan analysisPlan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.analysisMatchesLocked(plan)
}

func (s *Server) analysisAllowed(plan analysisPlan, requireNew bool) bool {
	s.mu.Lock()
	trace := s.traceLSP
	if plan.seq == 0 {
		s.mu.Unlock()
		if trace {
			s.logf("discard analysis: seq=0")
		}
		return false
	}
	if requireNew {
		if plan.seq <= s.appliedSeq {
			applied := s.appliedSeq
			s.mu.Unlock()
			if trace {
				s.logf("discard analysis: seq=%d olderApplied=%d", plan.seq, applied)
			}
			return false
		}
	} else {
		if plan.seq < s.appliedSeq {
			applied := s.appliedSeq
			s.mu.Unlock()
			if trace {
				s.logf("discard analysis: seq=%d olderApplied=%d", plan.seq, applied)
			}
			return false
		}
	}
	if s.analysisRoot != plan.root || s.analysisMode != plan.mode {
		root := s.analysisRoot
		mode := s.analysisMode
		s.mu.Unlock()
		if trace {
			s.logf("discard analysis: seq=%d scopeMismatch root=%s/%d expected=%s/%d", plan.seq, root, mode, plan.root, plan.mode)
		}
		return false
	}
	mismatch, ok := s.firstDocMismatchLocked(plan.docs)
	applied := s.appliedSeq
	s.mu.Unlock()
	if !ok {
		if trace {
			latest := atomic.LoadUint64(&s.latestSeq)
			if plan.seq != latest {
				s.logf("analysis result: seq=%d latest=%d applied=%d", plan.seq, latest, applied)
			}
		}
		return true
	}
	if trace {
		if mismatch.missing {
			s.logf("discard analysis: seq=%d uri=%s resultVersion=%d resultSnapshotID=%d currentVersion=missing currentSnapshotID=missing",
				plan.seq, mismatch.uri, mismatch.expected.version, mismatch.expected.snapshotID)
		} else {
			s.logf("discard analysis: seq=%d uri=%s resultVersion=%d resultSnapshotID=%d currentVersion=%d currentSnapshotID=%d",
				plan.seq, mismatch.uri, mismatch.expected.version, mismatch.expected.snapshotID, mismatch.current.version, mismatch.current.snapshotID)
		}
	}
	return false
}

func (s *Server) firstDocMismatchLocked(expected map[string]docState) (analysisMismatch, bool) {
	if len(expected) == 0 {
		return analysisMismatch{}, false
	}
	for uri, want := range expected {
		got, ok := s.docStateLocked(uri)
		if !ok {
			return analysisMismatch{uri: uri, expected: want, missing: true}, true
		}
		if got != want {
			return analysisMismatch{uri: uri, expected: want, current: got}, true
		}
	}
	return analysisMismatch{}, false
}

func (s *Server) applySnapshot(plan analysisPlan, snapshot *diagnose.AnalysisSnapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan.seq == 0 {
		return false
	}
	if plan.seq <= s.appliedSeq {
		return false
	}
	if s.analysisRoot != plan.root || s.analysisMode != plan.mode {
		return false
	}
	if _, mismatch := s.firstDocMismatchLocked(plan.docs); mismatch {
		return false
	}
	s.lastSnapshot = snapshot
	s.lastGoodSnapshot = snapshot
	s.snapshotDocs = cloneDocStates(plan.docs)
	s.snapshotVersion++
	s.appliedSeq = plan.seq
	return true
}

func (s *Server) clearSnapshotState() {
	s.mu.Lock()
	s.lastSnapshot = nil
	s.lastGoodSnapshot = nil
	s.snapshotDocs = make(map[string]docState)
	s.mu.Unlock()
}

func (s *Server) logAnalysisStart(plan analysisPlan) {
	if !s.currentTrace() {
		return
	}
	for uri, state := range plan.docs {
		s.logf("analysis start: seq=%d uri=%s version=%d snapshotID=%d", plan.seq, uri, state.version, state.snapshotID)
	}
}

func (s *Server) logAnalysisDone(plan analysisPlan, snapshot *diagnose.AnalysisSnapshot, diags []diagnose.Diagnostic, err error, canceled bool) {
	if !s.currentTrace() {
		return
	}
	status := "ok"
	if err != nil {
		status = "error"
	}
	if snapshot == nil {
		if status == "ok" {
			status = "nosnapshot"
		} else {
			status += "+nosnapshot"
		}
	}
	if canceled {
		if status == "ok" {
			status = "canceled"
		} else {
			status += "+canceled"
		}
	}
	for uri, state := range plan.docs {
		s.logf("analysis done: seq=%d status=%s uri=%s version=%d snapshotID=%d diags=%d",
			plan.seq, status, uri, state.version, state.snapshotID, len(diags))
	}
}

func (s *Server) logAnalysisApply(plan analysisPlan) {
	if !s.currentTrace() {
		return
	}
	version := s.currentSnapshotVersion()
	for uri, state := range plan.docs {
		s.logf("analysis apply: seq=%d uri=%s snapshotVersion=%d snapshotDocVersion=%d snapshotDocSnapshotID=%d",
			plan.seq, uri, version, state.version, state.snapshotID)
	}
}

func (s *Server) logAnalysisDiscard(plan analysisPlan, reason string) {
	if !s.currentTrace() {
		return
	}
	s.logf("analysis discard: seq=%d reason=%s", plan.seq, reason)
}

func (s *Server) logPublishDiagnostics(uri string, count int) {
	if !s.currentTrace() {
		return
	}
	state, ok := s.currentDocState(uri)
	if ok {
		s.logf("publishDiagnostics: uri=%s version=%d snapshotID=%d diags=%d", uri, state.version, state.snapshotID, count)
		return
	}
	s.logf("publishDiagnostics: uri=%s version=unknown snapshotID=unknown diags=%d", uri, count)
}

func (s *Server) preferredOpenFileLocked() string {
	if s.lastTouched != "" {
		if _, ok := s.openDocs[s.lastTouched]; ok {
			if path := uriToPath(s.lastTouched); path != "" {
				return canonicalPath(path)
			}
		}
	}
	return s.firstOpenFileLocked()
}

func (s *Server) firstOpenFileLocked() string {
	best := ""
	for uri := range s.openDocs {
		path := canonicalPath(uriToPath(uri))
		if path == "" {
			continue
		}
		if best == "" || path < best {
			best = path
		}
	}
	return best
}
