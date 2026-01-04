// Package runtimeembed provides embedded native runtime sources for LLVM builds.
package runtimeembed

import (
	"embed"
	"io/fs"
)

//go:embed native/*.c native/*.h
var nativeRuntimeFS embed.FS

// NativeRuntimeFS exposes embedded runtime sources for LLVM builds.
func NativeRuntimeFS() fs.FS {
	return nativeRuntimeFS
}
