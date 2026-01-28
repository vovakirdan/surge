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
	"sync"
	"sync/atomic"
	"time"

	"surge/internal/driver/diagnose"
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
	in           *bufio.Reader
	out          *bufio.Writer
	sendMu       sync.Mutex
	mu           sync.Mutex
	openDocs     map[string]string
	versions     map[string]int
	docSnapshots map[string]int64
	lastTouched  string
	published    map[string]struct{}

	workspaceRoot     string
	shutdownRequested bool
	debounce          time.Duration
	debounceTimer     *time.Timer
	diagCancel        context.CancelFunc
	analysisSeq       uint64
	latestSeq         uint64
	appliedSeq        uint64
	analyze           AnalyzeFunc
	analyzeFiles      AnalyzeFilesFunc
	maxDiagnostics    int
	baseCtx           context.Context
	lastSnapshot      *diagnose.AnalysisSnapshot
	lastGoodSnapshot  *diagnose.AnalysisSnapshot
	snapshotDocs      map[string]docState
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
		docSnapshots:   make(map[string]int64),
		published:      make(map[string]struct{}),
		debounce:       debounce,
		analyze:        analyzeFn,
		analyzeFiles:   analyzeFilesFn,
		maxDiagnostics: maxDiagnostics,
		inlayHints:     defaultInlayHintConfig(),
		snapshotDocs:   make(map[string]docState),
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
	case "textDocument/completion":
		return s.handleCompletion(msg)
	case "textDocument/signatureHelp":
		return s.handleSignatureHelp(msg)
	case "textDocument/inlayHint":
		return s.handleInlayHint(msg)
	case "textDocument/definition":
		return s.handleDefinition(msg)
	case "textDocument/foldingRange":
		return s.handleFoldingRange(msg)
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
			CompletionProvider: &completionOptions{
				TriggerCharacters: []string{".", ":"},
			},
			SignatureHelpProvider: &signatureHelpOptions{
				TriggerCharacters: []string{"(", ","},
			},
			FoldingRangeProvider: true,
		},
	}
	return s.sendResponse(msg.ID, result)
}

func (s *Server) handleShutdown(msg *rpcMessage) error {
	s.mu.Lock()
	s.shutdownRequested = true
	s.mu.Unlock()
	s.clearPublishedDiagnostics()
	s.clearSnapshotState()
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
	s.docSnapshots[uri]++
	s.lastTouched = uri
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
	oldSnapshot := s.docSnapshots[uri]
	newSnapshot := oldSnapshot + 1
	s.docSnapshots[uri] = newSnapshot
	s.lastTouched = uri
	trace := s.traceLSP
	s.mu.Unlock()
	if trace {
		s.logf("didChange: uri=%s version=%d snapshotID=%d->%d reason=didChange", uri, params.TextDocument.Version, oldSnapshot, newSnapshot)
	}
	s.scheduleDiagnostics()
	return nil
}

func (s *Server) handleDidSave(msg *rpcMessage) error {
	var params didSaveTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	uri := canonicalURI(params.TextDocument.URI)
	if uri == "" {
		return nil
	}
	s.mu.Lock()
	if params.Text != nil {
		s.openDocs[uri] = *params.Text
	}
	oldSnapshot := s.docSnapshots[uri]
	newSnapshot := oldSnapshot + 1
	s.docSnapshots[uri] = newSnapshot
	s.lastTouched = uri
	version := s.versions[uri]
	trace := s.traceLSP
	s.mu.Unlock()
	if trace {
		s.logf("didSave: uri=%s version=%d snapshotID=%d->%d reason=didSave", uri, version, oldSnapshot, newSnapshot)
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
	delete(s.docSnapshots, uri)
	delete(s.snapshotDocs, uri)
	if s.lastTouched == uri {
		s.lastTouched = ""
	}
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
	if list == nil {
		list = []lspDiagnostic{}
	}
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
