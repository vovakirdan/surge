package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"surge/internal/driver/diagnose"
)

func TestPublishDiagnosticsMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sg")
	uri := pathToURI(path)
	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		return nil, []diagnose.Diagnostic{
			{
				FilePath:  path,
				StartLine: 2,
				StartCol:  3,
				EndLine:   2,
				EndCol:    6,
				Severity:  1,
				Code:      "SYN2001",
				Message:   "boom",
			},
		}, nil
	}

	var out bytes.Buffer
	server := NewServer(bytes.NewReader(nil), &out, ServerOptions{
		Debounce:     time.Hour,
		AnalyzeFiles: analyzeFilesFn,
	})
	server.baseCtx = context.Background()

	openParams := didOpenTextDocumentParams{
		TextDocument: textDocumentItem{
			URI:     uri,
			Version: 1,
			Text:    "one\ntwo\n",
		},
	}
	openPayload, _ := json.Marshal(openParams)
	if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openPayload}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	changeParams := didChangeTextDocumentParams{
		TextDocument: versionedTextDocumentIdentifier{
			URI:     uri,
			Version: 2,
		},
		ContentChanges: []textDocumentContentChangeEvent{
			{
				Range: &lspRange{
					Start: position{Line: 0, Character: 0},
					End:   position{Line: 0, Character: 0},
				},
				Text: "// ",
			},
		},
	}
	changePayload, _ := json.Marshal(changeParams)
	if err := server.handleDidChange(&rpcMessage{Method: "textDocument/didChange", Params: changePayload}); err != nil {
		t.Fatalf("didChange: %v", err)
	}

	server.mu.Lock()
	if server.debounceTimer != nil {
		server.debounceTimer.Stop()
	}
	server.mu.Unlock()

	seq := atomic.LoadUint64(&server.latestSeq)
	server.runDiagnostics(seq)

	reader := bufio.NewReader(bytes.NewReader(out.Bytes()))
	payload, err := readMessage(reader)
	if err != nil {
		t.Fatalf("read publish: %v", err)
	}
	var msg rpcMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode publish: %v", err)
	}
	if msg.Method != "textDocument/publishDiagnostics" {
		t.Fatalf("expected publishDiagnostics, got %q", msg.Method)
	}
	var params publishDiagnosticsParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.URI != uri {
		t.Fatalf("expected uri %q, got %q", uri, params.URI)
	}
	if len(params.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(params.Diagnostics))
	}
	got := params.Diagnostics[0]
	if got.Range.Start.Line != 1 || got.Range.Start.Character != 2 {
		t.Fatalf("unexpected start range: %+v", got.Range.Start)
	}
	if got.Range.End.Line != 1 || got.Range.End.Character != 5 {
		t.Fatalf("unexpected end range: %+v", got.Range.End)
	}
	if got.Message != "boom" {
		t.Fatalf("unexpected message: %q", got.Message)
	}
}

func TestSnapshotRetentionOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sg")
	uri := pathToURI(path)
	snapshot := &diagnose.AnalysisSnapshot{ProjectRoot: filepath.Dir(path)}

	call := 0
	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		call++
		if call == 1 {
			return snapshot, nil, nil
		}
		return nil, nil, errors.New("boom")
	}

	var out bytes.Buffer
	server := NewServer(bytes.NewReader(nil), &out, ServerOptions{
		Debounce:     time.Hour,
		AnalyzeFiles: analyzeFilesFn,
	})
	server.baseCtx = context.Background()

	openParams := didOpenTextDocumentParams{
		TextDocument: textDocumentItem{
			URI:     uri,
			Version: 1,
			Text:    "fn main() {}",
		},
	}
	openPayload, _ := json.Marshal(openParams)
	if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openPayload}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	seq := atomic.LoadUint64(&server.latestSeq)
	server.runDiagnostics(seq)
	if got := server.currentSnapshot(); got != snapshot {
		t.Fatal("expected snapshot after first analysis")
	}

	server.scheduleDiagnostics()
	server.mu.Lock()
	if server.debounceTimer != nil {
		server.debounceTimer.Stop()
	}
	server.mu.Unlock()
	seq = atomic.LoadUint64(&server.latestSeq)
	server.runDiagnostics(seq)
	if got := server.currentSnapshot(); got != snapshot {
		t.Fatal("expected last good snapshot after failure")
	}
}

func TestOpenFilesModeFiltersDiagnostics(t *testing.T) {
	dir := t.TempDir()
	openPath := filepath.Join(dir, "main.sg")
	otherPath := filepath.Join(dir, "other.sg")
	if err := os.WriteFile(openPath, []byte("fn main() {}"), 0644); err != nil {
		t.Fatalf("write open file: %v", err)
	}
	if err := os.WriteFile(otherPath, []byte("fn other() {}"), 0644); err != nil {
		t.Fatalf("write other file: %v", err)
	}
	openURI := pathToURI(openPath)

	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		if len(files) != 1 || canonicalPath(files[0]) != canonicalPath(openPath) {
			t.Fatalf("expected open file only, got %v", files)
		}
		return nil, []diagnose.Diagnostic{
			{
				FilePath:  openPath,
				StartLine: 1,
				StartCol:  1,
				EndLine:   1,
				EndCol:    3,
				Severity:  1,
				Code:      "SYN2001",
				Message:   "open",
			},
			{
				FilePath:  otherPath,
				StartLine: 1,
				StartCol:  1,
				EndLine:   1,
				EndCol:    3,
				Severity:  1,
				Code:      "SYN2001",
				Message:   "other",
			},
		}, nil
	}

	var out bytes.Buffer
	server := NewServer(bytes.NewReader(nil), &out, ServerOptions{
		Debounce:     time.Hour,
		AnalyzeFiles: analyzeFilesFn,
	})
	server.baseCtx = context.Background()

	openParams := didOpenTextDocumentParams{
		TextDocument: textDocumentItem{
			URI:     openURI,
			Version: 1,
			Text:    "fn main() {}",
		},
	}
	openPayload, _ := json.Marshal(openParams)
	if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openPayload}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	server.mu.Lock()
	if server.debounceTimer != nil {
		server.debounceTimer.Stop()
	}
	server.mu.Unlock()

	seq := atomic.LoadUint64(&server.latestSeq)
	server.runDiagnostics(seq)

	reader := bufio.NewReader(bytes.NewReader(out.Bytes()))
	payload, err := readMessage(reader)
	if err != nil {
		t.Fatalf("read publish: %v", err)
	}
	var msg rpcMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode publish: %v", err)
	}
	var params publishDiagnosticsParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.URI != openURI {
		t.Fatalf("expected uri %q, got %q", openURI, params.URI)
	}
	if len(params.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(params.Diagnostics))
	}
	if _, err := readMessage(reader); err == nil {
		t.Fatalf("expected single publish")
	}
}

func TestDiagnosticsClearedOnScopeChange(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(filePath, []byte("fn main() {}"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	uri := pathToURI(filePath)

	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		return nil, nil, nil
	}

	var out bytes.Buffer
	server := NewServer(bytes.NewReader(nil), &out, ServerOptions{
		Debounce:     time.Hour,
		AnalyzeFiles: analyzeFilesFn,
	})
	server.baseCtx = context.Background()

	server.mu.Lock()
	server.published[uri] = struct{}{}
	server.analysisRoot = filepath.Join(dir, "other")
	server.analysisMode = modeProjectRoot
	server.mu.Unlock()

	openParams := didOpenTextDocumentParams{
		TextDocument: textDocumentItem{
			URI:     uri,
			Version: 1,
			Text:    "fn main() {}",
		},
	}
	openPayload, _ := json.Marshal(openParams)
	if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openPayload}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	server.mu.Lock()
	if server.debounceTimer != nil {
		server.debounceTimer.Stop()
	}
	server.mu.Unlock()

	seq := atomic.LoadUint64(&server.latestSeq)
	server.runDiagnostics(seq)

	reader := bufio.NewReader(bytes.NewReader(out.Bytes()))
	payload, err := readMessage(reader)
	if err != nil {
		t.Fatalf("read publish: %v", err)
	}
	var msg rpcMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode publish: %v", err)
	}
	var params publishDiagnosticsParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.URI != uri {
		t.Fatalf("expected uri %q, got %q", uri, params.URI)
	}
	if len(params.Diagnostics) != 0 {
		t.Fatalf("expected cleared diagnostics, got %d", len(params.Diagnostics))
	}
}
