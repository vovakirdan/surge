package driver

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
	"surge/internal/symbols"
)

func markSymbolsBuiltin(res *symbols.Result) {
	if res == nil || res.Table == nil || res.Table.Symbols == nil {
		return
	}
	count := res.Table.Symbols.Len()
	for i := 1; i <= count; i++ {
		value, convErr := safecast.Conv[uint32](i)
		if convErr != nil {
			panic(fmt.Errorf("symbol id overflow: %w", convErr))
		}
		id := symbols.SymbolID(value)
		if sym := res.Table.Symbols.Get(id); sym != nil {
			sym.Flags |= symbols.SymbolFlagBuiltin
		}
	}
}

func validateCoreModule(meta *project.ModuleMeta, file *source.File, stdlibRoot string, reporter diag.Reporter) bool {
	if meta == nil || file == nil {
		return true
	}
	if meta.Path != "core" && !strings.HasPrefix(meta.Path, "core/") {
		return true
	}
	if stdlibRoot != "" && pathWithin(stdlibRoot, file.Path) {
		return true
	}
	if reporter != nil {
		msg := fmt.Sprintf("module %q is reserved for the standard library", meta.Path)
		span := source.Span{File: file.ID}
		if b := diag.ReportError(reporter, diag.ProjInvalidModulePath, span, msg); b != nil {
			b.Emit()
		}
	}
	return false
}
