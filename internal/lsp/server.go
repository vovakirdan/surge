package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"surge/internal/driver"
	"surge/internal/driver/diagnose"
	"surge/internal/parser"
)

var (
	// ErrExit signals a graceful shutdown after receiving "exit".
	ErrExit = errors.New("lsp exit")
	// ErrExitWithoutShutdown signals an "exit" without a preceding "shutdown".
	ErrExitWithoutShutdown = errors.New("lsp exit without shutdown")
)

// AnalyzeFunc runs workspace diagnostics and returns an analysis snapshot.
type AnalyzeFunc func(ctx context.Context, opts *diagnose.DiagnoseOptions, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error)

// AnalyzeFilesFunc runs diagnostics for a fixed file set and returns an analysis snapshot.
type AnalyzeFilesFunc func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error)

// ServerOptions configures LSP server behavior.
type ServerOptions struct {
	Debounce       time.Duration
	Analyze        AnalyzeFunc
	AnalyzeFiles   AnalyzeFilesFunc
	MaxDiagnostics int
}

// Server handles stdio JSON-RPC for the Surge LSP.
type Server struct {
	in        *bufio.Reader
	out       *bufio.Writer
	sendMu    sync.Mutex
	mu        sync.Mutex
	openDocs  map[string]string
	versions  map[string]int
	published map[string]struct{}

	workspaceRoot     string
	shutdownRequested bool
	debounce          time.Duration
	debounceTimer     *time.Timer
	diagCancel        context.CancelFunc
	analysisSeq       uint64
	latestSeq         uint64
	analyze           AnalyzeFunc
	analyzeFiles      AnalyzeFilesFunc
	maxDiagnostics    int
	baseCtx           context.Context
	lastSnapshot      *diagnose.AnalysisSnapshot
	lastGoodSnapshot  *diagnose.AnalysisSnapshot
	snapshotVersion   int64
	inlayHints        inlayHintConfig
	traceLSP          bool
	analysisMode      analysisMode
	analysisRoot      string
}

// NewServer constructs a new LSP server.
func NewServer(in io.Reader, out io.Writer, opts ServerOptions) *Server {
	debounce := opts.Debounce
	if debounce <= 0 {
		debounce = 300 * time.Millisecond
	}
	analyzeFn := opts.Analyze
	if analyzeFn == nil {
		analyzeFn = diagnose.AnalyzeWorkspace
	}
	analyzeFilesFn := opts.AnalyzeFiles
	if analyzeFilesFn == nil {
		analyzeFilesFn = diagnose.AnalyzeFiles
	}
	maxDiagnostics := opts.MaxDiagnostics
	if maxDiagnostics <= 0 {
		maxDiagnostics = 100
	}
	return &Server{
		in:             bufio.NewReader(in),
		out:            bufio.NewWriter(out),
		openDocs:       make(map[string]string),
		versions:       make(map[string]int),
		published:      make(map[string]struct{}),
		debounce:       debounce,
		analyze:        analyzeFn,
		analyzeFiles:   analyzeFilesFn,
		maxDiagnostics: maxDiagnostics,
		inlayHints:     defaultInlayHintConfig(),
	}
}

// Run serves LSP requests until shutdown.
func (s *Server) Run(ctx context.Context) error {
	s.baseCtx = ctx
	for {
		payload, err := readMessage(s.in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var msg rpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			s.logf("failed to parse message: %v", err)
			continue
		}
		if msg.Method == "" {
			continue
		}
		if err := s.handleMessage(&msg); err != nil {
			if errors.Is(err, ErrExit) || errors.Is(err, ErrExitWithoutShutdown) {
				return err
			}
			return err
		}
	}
}

func (s *Server) handleMessage(msg *rpcMessage) error {
	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "initialized":
		return nil
	case "shutdown":
		return s.handleShutdown(msg)
	case "exit":
		if s.shutdownRequested {
			return ErrExit
		}
		return ErrExitWithoutShutdown
	case "workspace/didChangeConfiguration":
		return s.handleDidChangeConfiguration(msg)
	case "textDocument/didOpen":
		return s.handleDidOpen(msg)
	case "textDocument/didChange":
		return s.handleDidChange(msg)
	case "textDocument/didSave":
		return s.handleDidSave(msg)
	case "textDocument/didClose":
		return s.handleDidClose(msg)
	case "textDocument/hover":
		return s.handleHover(msg)
	case "textDocument/inlayHint":
		return s.handleInlayHint(msg)
	case "textDocument/definition":
		return s.handleDefinition(msg)
	default:
		if len(msg.ID) > 0 {
			return s.sendError(msg.ID, -32601, "method not found")
		}
		return nil
	}
}

func (s *Server) handleInitialize(msg *rpcMessage) error {
	var params initializeParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	root := ""
	if params.RootURI != "" {
		root = uriToPath(params.RootURI)
	}
	if root == "" && params.RootPath != "" {
		root = params.RootPath
	}
	if root == "" && len(params.WorkspaceFolders) > 0 {
		root = uriToPath(params.WorkspaceFolders[0].URI)
	}
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}
	s.mu.Lock()
	s.workspaceRoot = root
	s.mu.Unlock()

	result := initializeResult{
		Capabilities: serverCapabilities{
			TextDocumentSync: textDocumentSyncOptions{
				OpenClose: true,
				Change:    2,
				Save: saveOptions{
					IncludeText: true,
				},
			},
			HoverProvider:      true,
			DefinitionProvider: true,
			InlayHintProvider:  &inlayHintOptions{},
		},
	}
	return s.sendResponse(msg.ID, result)
}

func (s *Server) handleShutdown(msg *rpcMessage) error {
	s.mu.Lock()
	s.shutdownRequested = true
	s.mu.Unlock()
	s.clearPublishedDiagnostics()
	return s.sendResponse(msg.ID, nil)
}

func (s *Server) handleDidOpen(msg *rpcMessage) error {
	var params didOpenTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	uri := canonicalURI(params.TextDocument.URI)
	if uri == "" {
		return nil
	}
	s.mu.Lock()
	s.openDocs[uri] = params.TextDocument.Text
	s.versions[uri] = params.TextDocument.Version
	s.mu.Unlock()
	s.scheduleDiagnostics()
	return nil
}

func (s *Server) handleDidChange(msg *rpcMessage) error {
	var params didChangeTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	uri := canonicalURI(params.TextDocument.URI)
	if uri == "" {
		return nil
	}
	s.mu.Lock()
	text := s.openDocs[uri]
	text = applyChanges(text, params.ContentChanges)
	s.openDocs[uri] = text
	s.versions[uri] = params.TextDocument.Version
	s.mu.Unlock()
	s.scheduleDiagnostics()
	return nil
}

func (s *Server) handleDidSave(msg *rpcMessage) error {
	var params didSaveTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	if params.Text != nil {
		uri := canonicalURI(params.TextDocument.URI)
		if uri == "" {
			return nil
		}
		s.mu.Lock()
		s.openDocs[uri] = *params.Text
		s.mu.Unlock()
	}
	s.scheduleDiagnostics()
	return nil
}

func (s *Server) handleDidClose(msg *rpcMessage) error {
	var params didCloseTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	uri := canonicalURI(params.TextDocument.URI)
	if uri == "" {
		return nil
	}
	s.mu.Lock()
	delete(s.openDocs, uri)
	delete(s.versions, uri)
	_, hadDiagnostics := s.published[uri]
	delete(s.published, uri)
	s.mu.Unlock()
	if hadDiagnostics {
		if err := s.sendPublish(uri, nil); err != nil {
			s.logf("failed to clear diagnostics: %v", err)
		}
	}
	s.scheduleDiagnostics()
	return nil
}

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
		return
	}
	if s.diagCancel != nil {
		s.diagCancel()
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	s.diagCancel = cancel
	workspaceRoot := s.workspaceRoot
	firstFile := s.firstOpenFileLocked()
	overlay := make(map[string]string, len(s.openDocs))
	for uri, text := range s.openDocs {
		if path := uriToPath(uri); path != "" {
			overlay[canonicalPath(path)] = text
		}
	}
	s.mu.Unlock()

	projectRoot, mode := detectAnalysisScope(workspaceRoot, firstFile)
	if projectRoot == "" {
		s.clearPublishedDiagnostics()
		return
	}
	projectRoot = canonicalPath(projectRoot)
	if s.updateAnalysisScope(projectRoot, mode) {
		s.clearPublishedDiagnostics()
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
	if mode == modeOpenFiles {
		openFiles, openSet, filteredOverlay := filterOpenFiles(overlay, projectRoot)
		if len(openFiles) == 0 {
			s.clearPublishedDiagnostics()
			return
		}
		snapshot, diags, err = s.analyzeFiles(ctx, &opts, openFiles, diagnose.FileOverlay{Files: filteredOverlay})
		diags = filterDiagnosticsForOpenFiles(diags, openSet)
	} else {
		snapshot, diags, err = s.analyze(ctx, &opts, diagnose.FileOverlay{Files: overlay})
	}
	if err != nil {
		s.logf("diagnostics failed: %v", err)
	}
	if ctx.Err() != nil {
		return
	}
	if err == nil && snapshot != nil && s.isLatestSeq(seq) {
		s.mu.Lock()
		if s.isLatestSeq(seq) {
			s.lastSnapshot = snapshot
			s.lastGoodSnapshot = snapshot
			s.snapshotVersion++
		}
		s.mu.Unlock()
	}
	s.publishDiagnostics(seq, diags)
}

func (s *Server) publishDiagnostics(seq uint64, diags []diagnose.Diagnostic) {
	if !s.isLatestSeq(seq) {
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

	s.mu.Lock()
	if !s.isLatestSeq(seq) {
		s.mu.Unlock()
		return
	}
	prev := s.published
	s.published = make(map[string]struct{}, len(grouped))
	for uri := range grouped {
		s.published[uri] = struct{}{}
	}
	s.mu.Unlock()

	for uri, list := range grouped {
		if err := s.sendPublish(uri, list); err != nil {
			s.logf("failed to publish diagnostics: %v", err)
		}
	}
	for uri := range prev {
		if _, ok := grouped[uri]; ok {
			continue
		}
		if err := s.sendPublish(uri, nil); err != nil {
			s.logf("failed to clear diagnostics: %v", err)
		}
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

func (s *Server) firstOpenFileLocked() string {
	for uri := range s.openDocs {
		if path := uriToPath(uri); path != "" {
			return canonicalPath(path)
		}
	}
	return ""
}

func (s *Server) sendResponse(id json.RawMessage, result any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  result,
	}
	return s.send(msg)
}

func (s *Server) sendError(id json.RawMessage, code int, message string) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": rpcError{
			Code:    code,
			Message: message,
		},
	}
	return s.send(msg)
}

func (s *Server) sendPublish(uri string, list []lspDiagnostic) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/publishDiagnostics",
		"params": publishDiagnosticsParams{
			URI:         uri,
			Diagnostics: list,
		},
	}
	return s.send(msg)
}

func (s *Server) send(msg any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := writeMessage(s.out, payload); err != nil {
		return err
	}
	return s.out.Flush()
}

func (s *Server) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "lsp: "+format+"\n", args...)
}

func (s *Server) isLatestSeq(seq uint64) bool {
	if seq == 0 {
		return false
	}
	return seq == atomic.LoadUint64(&s.latestSeq)
}

func maxZero(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
