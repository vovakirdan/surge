package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestDiagnosticsClearedAfterFix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	uri := pathToURI(path)
	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %v", files)
		}
		text := overlay.Files[files[0]]
		var diags []diagnose.Diagnostic
		if strings.Contains(text, "bad") {
			diags = append(diags, diagnose.Diagnostic{
				FilePath:  files[0],
				StartLine: 1,
				StartCol:  1,
				EndLine:   1,
				EndCol:    2,
				Severity:  1,
				Code:      "SYN2205",
				Message:   "bad modifier",
			})
		}
		return &diagnose.AnalysisSnapshot{ProjectRoot: filepath.Dir(files[0])}, diags, nil
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
			Text:    "fn main() { bad }",
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

	changeParams := didChangeTextDocumentParams{
		TextDocument: versionedTextDocumentIdentifier{
			URI:     uri,
			Version: 2,
		},
		ContentChanges: []textDocumentContentChangeEvent{
			{
				Text: "fn main() { ok }",
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

	seq = atomic.LoadUint64(&server.latestSeq)
	server.runDiagnostics(seq)

	reader := bufio.NewReader(bytes.NewReader(out.Bytes()))
	firstPayload, err := readMessage(reader)
	if err != nil {
		t.Fatalf("read first publish: %v", err)
	}
	var firstMsg rpcMessage
	if unmarshalErr := json.Unmarshal(firstPayload, &firstMsg); unmarshalErr != nil {
		t.Fatalf("decode first publish: %v", unmarshalErr)
	}
	var firstParams publishDiagnosticsParams
	if unmarshalErr := json.Unmarshal(firstMsg.Params, &firstParams); unmarshalErr != nil {
		t.Fatalf("decode first params: %v", unmarshalErr)
	}
	if len(firstParams.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(firstParams.Diagnostics))
	}

	secondPayload, err := readMessage(reader)
	if err != nil {
		t.Fatalf("read second publish: %v", err)
	}
	var secondMsg rpcMessage
	if unmarshalErr := json.Unmarshal(secondPayload, &secondMsg); unmarshalErr != nil {
		t.Fatalf("decode second publish: %v", unmarshalErr)
	}
	var secondParams publishDiagnosticsParams
	if unmarshalErr := json.Unmarshal(secondMsg.Params, &secondParams); unmarshalErr != nil {
		t.Fatalf("decode second params: %v", unmarshalErr)
	}
	if len(secondParams.Diagnostics) != 0 {
		t.Fatalf("expected cleared diagnostics, got %d", len(secondParams.Diagnostics))
	}

	if got := server.currentSnapshotVersion(); got < 2 {
		t.Fatalf("expected snapshot version >= 2, got %d", got)
	}
	t.Logf("after fix: diags=%d snapshotVersion=%d", len(secondParams.Diagnostics), server.currentSnapshotVersion())
}

func TestAnalysisDiscardedOnDocStateChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	uri := pathToURI(path)
	started := make(chan struct{})
	proceed := make(chan struct{})
	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		close(started)
		<-proceed
		diags := []diagnose.Diagnostic{
			{
				FilePath:  files[0],
				StartLine: 1,
				StartCol:  1,
				EndLine:   1,
				EndCol:    2,
				Severity:  1,
				Code:      "SYN2205",
				Message:   "bad modifier",
			},
		}
		return &diagnose.AnalysisSnapshot{ProjectRoot: filepath.Dir(files[0])}, diags, nil
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
			Text:    "fn main() { bad }",
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
	done := make(chan struct{})
	go func() {
		server.runDiagnostics(seq)
		close(done)
	}()

	<-started
	server.mu.Lock()
	server.openDocs[uri] = "fn main() { ok }"
	server.versions[uri] = 2
	server.docSnapshots[uri]++
	server.mu.Unlock()
	close(proceed)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("analysis did not finish")
	}

	if out.Len() != 0 {
		t.Fatalf("expected no diagnostics published, got %d bytes", out.Len())
	}
}

func TestLatestAnalysisAppliedAfterOutOfOrderCompletion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	uri := pathToURI(path)
	var call int32
	startedFirst := make(chan struct{})
	startedSecond := make(chan struct{})
	proceedFirst := make(chan struct{})
	proceedSecond := make(chan struct{})
	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		switch atomic.AddInt32(&call, 1) {
		case 1:
			close(startedFirst)
			<-proceedFirst
		case 2:
			close(startedSecond)
			<-proceedSecond
		}
		return &diagnose.AnalysisSnapshot{ProjectRoot: filepath.Dir(files[0])}, nil, nil
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
			Text:    "fn main() { ok }",
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
	doneFirst := make(chan struct{})
	go func() {
		server.runDiagnostics(seq)
		close(doneFirst)
	}()

	<-startedFirst

	changeParams := didChangeTextDocumentParams{
		TextDocument: versionedTextDocumentIdentifier{
			URI:     uri,
			Version: 2,
		},
		ContentChanges: []textDocumentContentChangeEvent{
			{
				Text: "fn main() { ok }\n",
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

	seq = atomic.LoadUint64(&server.latestSeq)
	doneSecond := make(chan struct{})
	go func() {
		server.runDiagnostics(seq)
		close(doneSecond)
	}()

	<-startedSecond
	close(proceedSecond)

	select {
	case <-doneSecond:
	case <-time.After(time.Second):
		t.Fatal("second analysis did not finish")
	}

	close(proceedFirst)

	select {
	case <-doneFirst:
	case <-time.After(time.Second):
		t.Fatal("first analysis did not finish")
	}

	state, ok := server.snapshotDocState(uri)
	if !ok {
		t.Fatal("expected snapshot doc state")
	}
	if state.version != 2 {
		t.Fatalf("expected snapshot version 2, got %d", state.version)
	}
	if state.snapshotID != 2 {
		t.Fatalf("expected snapshotID 2, got %d", state.snapshotID)
	}
}

func TestAnalysisAppliesDespiteUnrelatedDocChange(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.sg")
	otherPath := filepath.Join(dir, "notes.txt")
	mainURI := pathToURI(mainPath)
	otherURI := pathToURI(otherPath)
	started := make(chan struct{})
	proceed := make(chan struct{})
	analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
		close(started)
		<-proceed
		return &diagnose.AnalysisSnapshot{ProjectRoot: filepath.Dir(files[0])}, nil, nil
	}

	var out bytes.Buffer
	server := NewServer(bytes.NewReader(nil), &out, ServerOptions{
		Debounce:     time.Hour,
		AnalyzeFiles: analyzeFilesFn,
	})
	server.baseCtx = context.Background()

	openMain := didOpenTextDocumentParams{
		TextDocument: textDocumentItem{
			URI:     mainURI,
			Version: 1,
			Text:    "fn main() { ok }",
		},
	}
	openMainPayload, _ := json.Marshal(openMain)
	if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openMainPayload}); err != nil {
		t.Fatalf("didOpen main: %v", err)
	}

	openOther := didOpenTextDocumentParams{
		TextDocument: textDocumentItem{
			URI:     otherURI,
			Version: 1,
			Text:    "note",
		},
	}
	openOtherPayload, _ := json.Marshal(openOther)
	if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openOtherPayload}); err != nil {
		t.Fatalf("didOpen other: %v", err)
	}

	server.mu.Lock()
	if server.debounceTimer != nil {
		server.debounceTimer.Stop()
	}
	server.mu.Unlock()

	seq := atomic.LoadUint64(&server.latestSeq)
	done := make(chan struct{})
	go func() {
		server.runDiagnostics(seq)
		close(done)
	}()

	<-started

	changeOther := didChangeTextDocumentParams{
		TextDocument: versionedTextDocumentIdentifier{
			URI:     otherURI,
			Version: 2,
		},
		ContentChanges: []textDocumentContentChangeEvent{
			{
				Text: "note updated",
			},
		},
	}
	changeOtherPayload, _ := json.Marshal(changeOther)
	if err := server.handleDidChange(&rpcMessage{Method: "textDocument/didChange", Params: changeOtherPayload}); err != nil {
		t.Fatalf("didChange other: %v", err)
	}

	server.mu.Lock()
	if server.debounceTimer != nil {
		server.debounceTimer.Stop()
	}
	server.mu.Unlock()

	close(proceed)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("analysis did not finish")
	}

	state, ok := server.snapshotDocState(mainURI)
	if !ok {
		t.Fatal("expected snapshot doc state")
	}
	if state.version != 1 {
		t.Fatalf("expected snapshot version 1, got %d", state.version)
	}
	if state.snapshotID != 1 {
		t.Fatalf("expected snapshotID 1, got %d", state.snapshotID)
	}
	if server.currentSnapshotVersion() == 0 {
		t.Fatal("expected snapshot version to advance")
	}
}

func TestFilterAnalysisDocsSkipsOtherProjectsInOpenFilesMode(t *testing.T) {
	docStates := map[string]docState{
		"file:///root/loose.sg":    {version: 1, snapshotID: 1},
		"file:///root/proj/app.sg": {version: 1, snapshotID: 1},
	}
	docPaths := map[string]string{
		"file:///root/loose.sg":    "/root/loose.sg",
		"file:///root/proj/app.sg": "/root/proj/app.sg",
	}
	docProjects := map[string]string{
		"file:///root/proj/app.sg": "/root/proj",
	}

	got := filterAnalysisDocs(docStates, docPaths, "/root", modeOpenFiles, nil, docProjects)
	if _, ok := got["file:///root/loose.sg"]; !ok {
		t.Fatalf("expected loose file to be included")
	}
	if _, ok := got["file:///root/proj/app.sg"]; ok {
		t.Fatalf("expected project file to be excluded")
	}
}

func TestFilterAnalysisDocsSkipsNestedProjectsInProjectMode(t *testing.T) {
	docStates := map[string]docState{
		"file:///root/main.sg":         {version: 1, snapshotID: 1},
		"file:///root/sub/app.sg":      {version: 1, snapshotID: 1},
		"file:///root/sub/other.sg":    {version: 1, snapshotID: 1},
		"file:///root/other/loose.sg":  {version: 1, snapshotID: 1},
	}
	docPaths := map[string]string{
		"file:///root/main.sg":         "/root/main.sg",
		"file:///root/sub/app.sg":      "/root/sub/app.sg",
		"file:///root/sub/other.sg":    "/root/sub/other.sg",
		"file:///root/other/loose.sg":  "/root/other/loose.sg",
	}
	docProjects := map[string]string{
		"file:///root/main.sg":      "/root",
		"file:///root/sub/app.sg":   "/root/sub",
		"file:///root/sub/other.sg": "/root/sub",
	}

	got := filterAnalysisDocs(docStates, docPaths, "/root", modeProjectRoot, nil, docProjects)
	if _, ok := got["file:///root/main.sg"]; !ok {
		t.Fatalf("expected root file to be included")
	}
	if _, ok := got["file:///root/sub/app.sg"]; ok {
		t.Fatalf("expected nested project file to be excluded")
	}
	if _, ok := got["file:///root/sub/other.sg"]; ok {
		t.Fatalf("expected nested project file to be excluded")
	}
	if _, ok := got["file:///root/other/loose.sg"]; !ok {
		t.Fatalf("expected loose file within root to be included")
	}
}

func TestNoStdDoesNotLeakAcrossDocs(t *testing.T) {
	dir := tempProjectDir(t)
	mainPath := filepath.Join(dir, "main.sg")
	noStdPath := filepath.Join(dir, "nostd.sg")

	mainSrc := strings.Join([]string{
		"@entrypoint",
		"fn main() -> int {",
		"    let foo = Some(1);",
		"    return 0;",
		"}",
		"",
	}, "\n")
	noStdSrc := strings.Join([]string{
		"pragma no_std",
		"fn main() -> int {",
		"    let foo: Option<int> = nothing;",
		"    return 0;",
		"}",
		"",
	}, "\n")

	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatalf("write main.sg: %v", err)
	}
	if err := os.WriteFile(noStdPath, []byte(noStdSrc), 0644); err != nil {
		t.Fatalf("write nostd.sg: %v", err)
	}

	run := func(firstPath, firstSrc, secondPath, secondSrc string) {
		var lastDiags []diagnose.Diagnostic
		analyzeFilesFn := func(ctx context.Context, opts *diagnose.DiagnoseOptions, files []string, overlay diagnose.FileOverlay) (*diagnose.AnalysisSnapshot, []diagnose.Diagnostic, error) {
			snapshot, diags, err := diagnose.AnalyzeFiles(ctx, opts, files, overlay)
			lastDiags = diags
			return snapshot, diags, err
		}
		var out bytes.Buffer
		server := NewServer(bytes.NewReader(nil), &out, ServerOptions{
			Debounce:     time.Hour,
			AnalyzeFiles: analyzeFilesFn,
		})
		server.baseCtx = context.Background()
		server.mu.Lock()
		server.workspaceRoot = dir
		server.mu.Unlock()

		openFirst := didOpenTextDocumentParams{
			TextDocument: textDocumentItem{
				URI:     pathToURI(firstPath),
				Version: 1,
				Text:    firstSrc,
			},
		}
		openFirstPayload, _ := json.Marshal(openFirst)
		if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openFirstPayload}); err != nil {
			t.Fatalf("didOpen first: %v", err)
		}

		openSecond := didOpenTextDocumentParams{
			TextDocument: textDocumentItem{
				URI:     pathToURI(secondPath),
				Version: 1,
				Text:    secondSrc,
			},
		}
		openSecondPayload, _ := json.Marshal(openSecond)
		if err := server.handleDidOpen(&rpcMessage{Method: "textDocument/didOpen", Params: openSecondPayload}); err != nil {
			t.Fatalf("didOpen second: %v", err)
		}

		server.mu.Lock()
		if server.debounceTimer != nil {
			server.debounceTimer.Stop()
		}
		server.mu.Unlock()

		seq := atomic.LoadUint64(&server.latestSeq)
		server.runDiagnostics(seq)

		if !hasDiagCode(lastDiags, noStdPath, "SEM3005") {
			t.Fatalf("expected SEM3005 for no_std file, got %s", diagSummary(lastDiags))
		}
		if hasDiagCode(lastDiags, mainPath, "SEM3005") {
			t.Fatalf("unexpected SEM3005 for main file, got %s", diagSummary(lastDiags))
		}
	}

	run(noStdPath, noStdSrc, mainPath, mainSrc)
	run(mainPath, mainSrc, noStdPath, noStdSrc)
}
