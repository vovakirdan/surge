package lsp

import (
	"net/url"
	"path/filepath"
)

func uriToPath(uri string) string {
	if uri == "" {
		return ""
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "" && parsed.Scheme != "file" {
		return ""
	}
	path := parsed.Path
	if parsed.Scheme == "" {
		path = uri
	}
	if unescaped, err := url.PathUnescape(path); err == nil {
		path = unescaped
	}
	path = filepath.FromSlash(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

func pathToURI(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}
