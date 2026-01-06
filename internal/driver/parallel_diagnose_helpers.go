package driver

import (
	"fmt"
	"sync/atomic"

	"surge/internal/ast"
)

// FileClass categorizes files based on their import dependencies.
type FileClass int

const (
	// FileDependent indicates the file imports project modules.
	FileDependent FileClass = iota // Imports project modules
	// FileStdlibOnly indicates the file only imports stdlib modules.
	FileStdlibOnly // Only imports stdlib modules
	// FileFullyIndependent indicates the file has no imports.
	FileFullyIndependent // No imports
)

// parallelMetrics tracks performance metrics for parallel processing.
type parallelMetrics struct {
	// Worker pool metrics
	workersActive    atomic.Int32 // Currently running workers
	workersCompleted atomic.Int64 // Total completed tasks
	workersErrors    atomic.Int64 // Total errors encountered

	// Cache metrics
	cacheHits   atomic.Int64 // Memory cache hits
	cacheMisses atomic.Int64 // Memory cache misses
	diskHits    atomic.Int64 // Disk cache hits
	diskMisses  atomic.Int64 // Disk cache misses

	// File classification metrics
	filesIndependent atomic.Int64 // Files with no imports
	filesStdlibOnly  atomic.Int64 // Files only importing stdlib
	filesDependent   atomic.Int64 // Files with project dependencies

	// Batch parallelism metrics
	batchCount     atomic.Int64 // Number of batches processed
	batchSizeTotal atomic.Int64 // Total module count across all batches
	batchSizeMax   atomic.Int64 // Largest batch size
}

// emitMetrics outputs all collected metrics at the end of processing.
func (pm *parallelMetrics) emitMetrics() string {
	// Worker stats
	completed := pm.workersCompleted.Load()
	errs := pm.workersErrors.Load()

	// Cache stats
	memHits := pm.cacheHits.Load()
	memMisses := pm.cacheMisses.Load()
	memTotal := memHits + memMisses
	memHitRate := 0.0
	if memTotal > 0 {
		memHitRate = float64(memHits) / float64(memTotal) * 100
	}

	diskHits := pm.diskHits.Load()
	diskMisses := pm.diskMisses.Load()
	diskTotal := diskHits + diskMisses
	diskHitRate := 0.0
	if diskTotal > 0 {
		diskHitRate = float64(diskHits) / float64(diskTotal) * 100
	}

	// File classification stats
	independent := pm.filesIndependent.Load()
	stdlibOnly := pm.filesStdlibOnly.Load()
	dependent := pm.filesDependent.Load()
	totalFiles := independent + stdlibOnly + dependent

	// Batch parallelism stats
	batchCount := pm.batchCount.Load()
	batchTotal := pm.batchSizeTotal.Load()
	batchMax := pm.batchSizeMax.Load()
	batchAvg := 0.0
	if batchCount > 0 {
		batchAvg = float64(batchTotal) / float64(batchCount)
	}

	return fmt.Sprintf(
		"workers: %d completed, %d errors | "+
			"cache: mem=%d/%d (%.1f%%), disk=%d/%d (%.1f%%) | "+
			"files: %d total (%d indep, %d stdlib, %d dep) | "+
			"batches: %d (avg=%.1f, max=%d)",
		completed, errs,
		memHits, memTotal, memHitRate,
		diskHits, diskTotal, diskHitRate,
		totalFiles, independent, stdlibOnly, dependent,
		batchCount, batchAvg, batchMax,
	)
}

// isStdlibModule checks if a module path is a standard library module.
func isStdlibModule(path string) bool {
	switch path {
	case "option", "result", "bounded", "saturating_cast", "core":
		return true
	default:
		return false
	}
}

// classifyFile determines the classification of a file based on its imports.
func classifyFile(builder *ast.Builder, astFile ast.FileID) FileClass {
	if builder == nil || builder.Files == nil {
		return FileDependent // Conservative default
	}

	fileNode := builder.Files.Get(astFile)
	if fileNode == nil {
		return FileDependent
	}

	hasImports := false
	hasProjectImport := false

	for _, itemID := range fileNode.Items {
		if imp, ok := builder.Items.Import(itemID); ok {
			hasImports = true
			// Get module path from first segment
			if len(imp.Module) > 0 && builder.StringsInterner != nil {
				modulePath, _ := builder.StringsInterner.Lookup(imp.Module[0])
				if !isStdlibModule(modulePath) {
					hasProjectImport = true
					break
				}
			}
		}
	}

	if !hasImports {
		return FileFullyIndependent
	}
	if hasProjectImport {
		return FileDependent
	}
	return FileStdlibOnly
}
