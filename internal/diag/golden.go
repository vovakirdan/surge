package diag

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"surge/internal/source"
)

type goldenDiagnostic struct {
	Severity string
	Code     string
	Path     string
	Line     uint32
	Column   uint32
	Message  string
}

// FormatGoldenDiagnostics renders diagnostics into a stable, single-line-per-entry
// representation suitable for golden files. Diagnostics are filtered to drop entries
// that belong to stdlib or internal files, sorted deterministically, and returned
// as a single string (empty when nothing remains).
func FormatGoldenDiagnostics(diags []*Diagnostic, fs *source.FileSet, includeNotes bool) string {
	return formatDiagnostics(diags, fs, includeNotes, true)
}

// FormatShortDiagnostics renders diagnostics into a stable, single-line-per-entry
// representation intended for CLI short output. It includes stdlib/internal paths.
func FormatShortDiagnostics(diags []*Diagnostic, fs *source.FileSet, includeNotes bool) string {
	return formatDiagnostics(diags, fs, includeNotes, false)
}

func formatDiagnostics(diags []*Diagnostic, fs *source.FileSet, includeNotes, skipInternal bool) string {
	if fs == nil || len(diags) == 0 {
		return ""
	}

	rendered := make([]goldenDiagnostic, 0, len(diags))
	for _, d := range diags {
		rendered = appendDiagnostic(rendered, d, fs, includeNotes, skipInternal)
	}

	sort.SliceStable(rendered, func(i, j int) bool {
		di, dj := rendered[i], rendered[j]
		if di.Path != dj.Path {
			return di.Path < dj.Path
		}
		if di.Line != dj.Line {
			return di.Line < dj.Line
		}
		if di.Column != dj.Column {
			return di.Column < dj.Column
		}
		if di.Severity != dj.Severity {
			return di.Severity < dj.Severity
		}
		if di.Code != dj.Code {
			return di.Code < dj.Code
		}
		return di.Message < dj.Message
	})

	var b strings.Builder
	for i, d := range rendered {
		fmt.Fprintf(&b, "%s %s %s:%d:%d %s", d.Severity, d.Code, d.Path, d.Line, d.Column, d.Message)
		if i < len(rendered)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func appendDiagnostic(out []goldenDiagnostic, d *Diagnostic, fs *source.FileSet, includeNotes, skipInternal bool) []goldenDiagnostic {
	loc, ok := resolveSpan(fs, d.Primary)
	if ok && (!skipInternal || !shouldSkipPath(loc.Path)) {
		out = append(out, goldenDiagnostic{
			Severity: severityLabel(d.Severity),
			Code:     d.Code.ID(),
			Path:     loc.Path,
			Line:     loc.Line,
			Column:   loc.Column,
			Message:  sanitizeMessage(d.Message),
		})
	}

	if includeNotes {
		for _, note := range d.Notes {
			nloc, nok := resolveSpan(fs, note.Span)
			if !nok || (skipInternal && shouldSkipPath(nloc.Path)) {
				continue
			}
			out = append(out, goldenDiagnostic{
				Severity: "note",
				Code:     d.Code.ID(),
				Path:     nloc.Path,
				Line:     nloc.Line,
				Column:   nloc.Column,
				Message:  sanitizeMessage(note.Msg),
			})
		}
	}

	return out
}

type resolvedSpan struct {
	Path   string
	Line   uint32
	Column uint32
}

func resolveSpan(fs *source.FileSet, span source.Span) (loc resolvedSpan, ok bool) {
	defer func() {
		if recover() != nil {
			loc = resolvedSpan{}
			ok = false
		}
	}()

	file := fs.Get(span.File)
	start, _ := fs.Resolve(span)
	return resolvedSpan{
		Path:   normalizePath(file.FormatPath("relative", fs.BaseDir())),
		Line:   start.Line,
		Column: start.Col,
	}, true
}

func normalizePath(path string) string {
	p := filepath.ToSlash(path)
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	return p
}

func shouldSkipPath(path string) bool {
	if path == "" {
		return false
	}
	p := normalizePath(path)
	p = strings.TrimLeft(p, "/")
	return strings.HasPrefix(p, "stdlib/") ||
		strings.Contains(p, "/stdlib/") ||
		strings.HasPrefix(p, "internal/") ||
		strings.Contains(p, "/internal/")
}

func severityLabel(sev Severity) string {
	switch sev {
	case SevError:
		return "error"
	case SevWarning:
		return "warning"
	default:
		return "info"
	}
}

func sanitizeMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	msg = strings.ReplaceAll(msg, "\n", " ")
	return strings.TrimSpace(msg)
}
