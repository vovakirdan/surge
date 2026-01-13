package lsp

import "encoding/json"

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	RootURI          string            `json:"rootUri,omitempty"`
	RootPath         string            `json:"rootPath,omitempty"`
	WorkspaceFolders []workspaceFolder `json:"workspaceFolders,omitempty"`
}

type workspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type textDocumentContentChangeEvent struct {
	Range *lspRange `json:"range,omitempty"`
	Text  string    `json:"text"`
}

type didOpenTextDocumentParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeTextDocumentParams struct {
	TextDocument   versionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []textDocumentContentChangeEvent `json:"contentChanges"`
}

type didSaveTextDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

type didCloseTextDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type textDocumentSyncOptions struct {
	OpenClose bool        `json:"openClose"`
	Change    int         `json:"change"`
	Save      saveOptions `json:"save,omitempty"`
}

type saveOptions struct {
	IncludeText bool `json:"includeText,omitempty"`
}

type serverCapabilities struct {
	TextDocumentSync textDocumentSyncOptions `json:"textDocumentSync"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
}

type publishDiagnosticsParams struct {
	URI         string          `json:"uri"`
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}

type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity,omitempty"`
	Code     string   `json:"code,omitempty"`
	Source   string   `json:"source,omitempty"`
	Message  string   `json:"message"`
}
