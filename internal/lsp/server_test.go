package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"surge/internal/driver/diagnose"
)

func TestPublishDiagnosticsMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sg")
	uri := pathToURI(path)
	diagFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, overlay diagnose.FileOverlay) ([]diagnose.Diagnostic, error) {
		return []diagnose.Diagnostic{
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
		Debounce: time.Hour,
		Diagnose: diagFn,
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

	server.runDiagnostics()

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
