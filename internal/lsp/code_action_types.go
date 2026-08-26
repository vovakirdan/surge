package lsp

// codeActionParams is a `textDocument/codeAction` request.
//
// The diagnostics come back from the client, carrying the opaque data this
// server stamped on them. They are treated as a LOOKUP KEY and never as a
// description of what to do.
type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        lspRange               `json:"range"`
	Context      codeActionContext      `json:"context"`
}

// codeActionOptions advertises the one kind this server offers.
type codeActionOptions struct {
	CodeActionKinds []string `json:"codeActionKinds,omitempty"`
}

type codeActionContext struct {
	Diagnostics []lspDiagnostic `json:"diagnostics"`
	Only        []string        `json:"only,omitempty"`
}

type codeAction struct {
	Title       string        `json:"title"`
	Kind        string        `json:"kind,omitempty"`
	IsPreferred bool          `json:"isPreferred,omitempty"`
	Edit        workspaceEdit `json:"edit"`
}

// workspaceEdit carries only documentChanges, never the untracked `changes`
// map: an edit that does not state which version it applies to cannot be
// rejected by a client that has moved on.
type workspaceEdit struct {
	DocumentChanges []textDocumentEdit `json:"documentChanges"`
}

type textDocumentEdit struct {
	TextDocument versionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []textEdit                      `json:"edits"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}
