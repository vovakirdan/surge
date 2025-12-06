package driver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/observ"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// FileClass categorizes files based on their import dependencies
type FileClass int

const (
	FileDependent        FileClass = iota // Imports project modules
	FileStdlibOnly                        // Only imports stdlib modules
	FileFullyIndependent                  // No imports
)

// parallelMetrics tracks performance metrics for parallel processing
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

// emitMetrics outputs all collected metrics at the end of processing
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

// isStdlibModule checks if a module path is a standard library module
func isStdlibModule(path string) bool {
	switch path {
	case "option", "result", "bounded", "saturating_cast", "core":
		return true
	default:
		return false
	}
}

// classifyFile determines the classification of a file based on its imports
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

func DiagnoseDirWithOptions(ctx context.Context, dir string, opts DiagnoseOptions, jobs int) (*source.FileSet, []DiagnoseDirResult, error) {
	files, err := listSGFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	fileSet := source.NewFileSetWithBase(dir)
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, p := range files {
		var id source.FileID
		id, err = fileSet.Load(p)
		if err != nil {
			loadErrors[p] = err
			continue
		}
		fileIDs[p] = id
	}

	if len(files) == 0 {
		return fileSet, nil, nil
	}

	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	// Общий in-memory кэш на прогон
	mcache := NewModuleCache(len(files) * 2)
	// Диск-кэш (под будущие экспорты/семантику). Ошибки игнорируем — не критично.
	var dcache *DiskCache
	dcache, err = OpenDiskCache("surge")
	if err != nil {
		return nil, nil, err
	}

	// Metrics for parallel processing (Phase 6)
	var metrics parallelMetrics

	results := make([]DiagnoseDirResult, len(files))
	var (
		graphReport *observ.Report
		graphPath   string
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(min(jobs, len(files)))

	for i, path := range files {
		g.Go(func(i int, path string) func() error {
			return func() error {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				// Track worker activation
				metrics.workersActive.Add(1)
				defer func() {
					metrics.workersActive.Add(-1)
					metrics.workersCompleted.Add(1)
				}()

				bag := diag.NewBag(opts.MaxDiagnostics)
				var (
					builder *ast.Builder
					astFile ast.FileID
				)

				var (
					timer         *observ.Timer
					begin         func(string) int
					end           func(int, string)
					reportTimings func()
				)
				if opts.EnableTimings {
					timer = observ.NewTimer()
					begin = func(name string) int {
						return timer.Begin(name)
					}
					end = func(idx int, note string) {
						if idx >= 0 {
							timer.End(idx, note)
						}
					}
					reportTimings = func() {
						report := timer.Report()
						results[i].Timing = &report
					}
				} else {
					begin = func(string) int { return -1 }
					end = func(int, string) {}
					reportTimings = func() {}
				}

				if loadErr, hadErr := loadErrors[path]; hadErr {
					loadIdx := begin("load_file")
					end(loadIdx, "error")
					bag.Add(&diag.Diagnostic{
						Severity: diag.SevError,
						Code:     diag.IOLoadFileError,
						Message:  "failed to load file: " + loadErr.Error(),
						Primary:  source.Span{},
					})
					results[i] = DiagnoseDirResult{Path: path, FileID: 0, Bag: bag}
					reportTimings()
					return nil
				}

				fileID, ok := fileIDs[path]
				if !ok {
					loadIdx := begin("load_file")
					end(loadIdx, "missing")
					results[i] = DiagnoseDirResult{Path: path, FileID: 0, Bag: bag}
					reportTimings()
					return nil
				}

				loadIdx := begin("load_file")
				file := fileSet.Get(fileID)
				end(loadIdx, "")

				// Предвычислим modulePath по имени файла, чтобы обратиться к кэшу до парсинга.
				modulePath := file.Path
				if base := fileSet.BaseDir(); base != "" {
					if rel, relErr := source.RelativePath(modulePath, base); relErr == nil {
						modulePath = rel
					}
				}
				if norm, normErr := project.NormalizeModulePath(modulePath); normErr == nil {
					modulePath = norm
				}

				cacheIdx := begin("cache_lookup")
				if _, br, fe, hit := mcache.Get(modulePath, file.Hash); hit {
					metrics.cacheHits.Add(1) // Track memory cache hit
					end(cacheIdx, "hit")
					results[i] = DiagnoseDirResult{
						Path:    path,
						FileID:  fileID,
						Bag:     bag,
						Builder: nil,
						ASTFile: 0,
					}
					_ = br
					_ = fe
					reportTimings()
					return nil
				}
				metrics.cacheMisses.Add(1) // Track memory cache miss
				end(cacheIdx, "miss")

				tokenIdx := begin("tokenize")
				diagnoseTokenize(file, bag)
				tokenNote := ""
				if timer != nil {
					tokenNote = fmt.Sprintf("diags=%d", bag.Len())
				}
				end(tokenIdx, tokenNote)

				var (
					symbolsRes *symbols.Result
					semaRes    *sema.Result
				)
				if opts.Stage != DiagnoseStageTokenize {
					parseIdx := begin("parse")
					builder, astFile = diagnoseParse(ctx, fileSet, file, bag)
					parseNote := ""
					if timer != nil && builder != nil && builder.Files != nil {
						fileNode := builder.Files.Get(astFile)
						if fileNode != nil {
							parseNote = fmt.Sprintf("items=%d", len(fileNode.Items))
						}
					}
					end(parseIdx, parseNote)
					if opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
						symbolIdx := begin("symbols")
						symbolsRes = diagnoseSymbols(builder, astFile, bag, modulePath, file.Path, fileSet.BaseDir(), nil)
						symbolNote := ""
						if timer != nil && symbolsRes != nil && symbolsRes.Table != nil {
							symbolNote = fmt.Sprintf("symbols=%d", symbolsRes.Table.Symbols.Len())
						}
						end(symbolIdx, symbolNote)

						semaIdx := begin("sema")
						semaRes = diagnoseSema(ctx, builder, astFile, bag, nil, symbolsRes)
						end(semaIdx, "")
					}
				}

				results[i] = DiagnoseDirResult{
					Path:    path,
					FileID:  fileID,
					Bag:     bag,
					Builder: builder,
					ASTFile: astFile,
					Symbols: symbolsRes,
					Sema:    semaRes,
				}
				reportTimings()
				return nil
			}
		}(i, path))
	}

	err = g.Wait()
	if err != nil {
		return fileSet, results, err
	}

	if opts.Stage == DiagnoseStageSyntax || opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
		baseDir := fileSet.BaseDir()
		graphPath = baseDir
		type entry struct {
			meta *project.ModuleMeta
			node dag.ModuleNode
		}
		var (
			graphTimer *observ.Timer
			beginGraph func(string) int
			endGraph   func(int, string)
		)
		if opts.EnableTimings {
			graphTimer = observ.NewTimer()
			beginGraph = func(name string) int {
				return graphTimer.Begin(name)
			}
			endGraph = func(idx int, note string) {
				if idx >= 0 {
					graphTimer.End(idx, note)
				}
			}
		} else {
			beginGraph = func(string) int { return -1 }
			endGraph = func(int, string) {}
		}
		collectIdx := beginGraph("collect_modules")
		entries := make([]*entry, 0, len(results))
		var independentCount, stdlibOnlyCount, dependentCount int
		// Собираем метаданные: либо из кэша, либо из свежего парсинга, либо fallback.
		for i := range results {
			res := &results[i]
			if res.Bag == nil || res.Builder == nil {
				continue
			}
			reporter := &diag.BagReporter{Bag: res.Bag}
			var (
				meta *project.ModuleMeta
				ok   bool
			)
			file := fileSet.Get(res.FileID)
			// Попробуем взять из in-memory кэша (по пути файла).
			modulePath := file.Path
			if baseDir != "" {
				if rel, relErr := source.RelativePath(modulePath, baseDir); relErr == nil {
					modulePath = rel
				}
			}
			if norm, normErr := project.NormalizeModulePath(modulePath); normErr == nil {
				modulePath = norm
			}

			// Try disk cache first if enabled (cross-run persistence)
			if opts.EnableDiskCache && dcache != nil && file.Hash != (project.Digest{}) {
				var payload DiskPayload
				if hit, err := dcache.Get(file.Hash, &payload); err == nil && hit {
					// Validate content hash matches
					if payload.ContentHash == file.Hash {
						if cached := diskPayloadToModule(&payload); cached != nil {
							metrics.diskHits.Add(1) // Track disk cache hit
							meta = cached
							ok = true
							// Also populate in-memory cache for faster subsequent access
							mcache.Put(meta, payload.Broken, nil)
						}
					} else {
						metrics.diskMisses.Add(1) // Hash mismatch = effective miss
					}
				} else {
					metrics.diskMisses.Add(1) // Track disk cache miss
				}
			}

			// Fallback to in-memory cache or build fresh
			if !ok {
				if m, _, _, hit := mcache.Get(modulePath, file.Hash); hit {
					meta = m
					ok = true
				} else if res.Builder != nil {
					meta, ok = buildModuleMeta(fileSet, res.Builder, []ast.FileID{res.ASTFile}, baseDir, reporter)
				}
			}
			if !ok {
				meta = fallbackModuleMeta(file, baseDir)
				// Гарантируем ContentHash (для последующих шагов)
				if meta.ContentHash == ([32]byte{}) {
					meta.ContentHash = file.Hash
				}
			}
			broken, firstErr := moduleStatus(res.Bag)

			// Classify file to determine if it needs module graph processing
			fileClass := classifyFile(res.Builder, res.ASTFile)
			switch fileClass {
			case FileFullyIndependent:
				independentCount++
				metrics.filesIndependent.Add(1) // Track independent files
			case FileStdlibOnly:
				stdlibOnlyCount++
				metrics.filesStdlibOnly.Add(1) // Track stdlib-only files
			case FileDependent:
				dependentCount++
				metrics.filesDependent.Add(1) // Track dependent files
			}

			// Only add files with project dependencies to the module graph
			// Independent and stdlib-only files don't need dependency analysis
			if fileClass == FileDependent {
				entries = append(entries, &entry{
					meta: meta,
					node: dag.ModuleNode{
						Meta:     meta,
						Reporter: reporter,
						Broken:   broken,
						FirstErr: firstErr,
					},
				})
			}

			// Положим в in-memory cache (обновление/вставка).
			mcache.Put(meta, broken, firstErr)

			// Write to disk cache if enabled (for cross-run persistence)
			// Note: Disk cache adds I/O overhead (~77% slower for fast operations)
			// Use --disk-cache flag only for large projects where cross-run caching helps
			if opts.EnableDiskCache && dcache != nil && ok && meta.ContentHash != (project.Digest{}) {
				// For now, use zero dependency hash (will be computed in module graph phase)
				payload := moduleToDiskPayload(meta, broken, project.Digest{})
				_ = dcache.Put(meta.ContentHash, payload) //nolint:errcheck // Cache is best-effort, errors are acceptable
			}
		}
		collectNote := ""
		if opts.EnableTimings {
			collectNote = fmt.Sprintf("total=%d graph=%d (indep=%d stdlib=%d)",
				independentCount+stdlibOnlyCount+dependentCount, len(entries),
				independentCount, stdlibOnlyCount)
		}
		endGraph(collectIdx, collectNote)
		var graphErr error
		if len(entries) > 0 {
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].meta.Path < entries[j].meta.Path
			})
			metas := make([]*project.ModuleMeta, 0, len(entries))
			nodes := make([]*dag.ModuleNode, 0, len(entries))
			for _, e := range entries {
				metas = append(metas, e.meta)
				nodes = append(nodes, &e.node)
			}
			buildIdx := beginGraph("build_graph")
			idx := dag.BuildIndex(metas)
			graph, slots := dag.BuildGraph(idx, nodes)
			buildNote := ""
			if opts.EnableTimings {
				buildNote = fmt.Sprintf("nodes=%d", len(metas))
			}
			endGraph(buildIdx, buildNote)
			topoIdx := beginGraph("topo")
			topo := dag.ToposortKahn(graph)
			dag.ReportCycles(idx, slots, topo)
			topoNote := ""
			if opts.EnableTimings {
				topoNote = fmt.Sprintf("order=%d", len(topo.Order))
			}
			endGraph(topoIdx, topoNote)
			hashIdx := beginGraph("hashes")
			ComputeModuleHashes(idx, graph, slots, topo)

			// Track batch parallelism metrics (Phase 6)
			if topo != nil && len(topo.Batches) > 0 {
				metrics.batchCount.Store(int64(len(topo.Batches)))
				var maxSize int64
				var totalSize int64
				for _, batch := range topo.Batches {
					size := int64(len(batch))
					totalSize += size
					if size > maxSize {
						maxSize = size
					}
				}
				metrics.batchSizeTotal.Store(totalSize)
				metrics.batchSizeMax.Store(maxSize)
			}

			for i := range slots {
				reporter, ok := slots[i].Reporter.(*diag.BagReporter)
				if !ok || reporter.Bag == nil {
					continue
				}
				brokenNow, firstErrNow := moduleStatus(reporter.Bag)
				if brokenNow {
					slots[i].Broken = true
					if slots[i].FirstErr == nil && firstErrNow != nil {
						slots[i].FirstErr = firstErrNow
					}
				}
				// инкрементально обновим кэш на основе актуальной меты (с ModuleHash)
				m := slots[i].Meta
				mcache.Put(m, slots[i].Broken, slots[i].FirstErr)
				// и положим запись в диск-кэш (ключ — ModuleHash).
				if dcache != nil && IsSHA256(m.ModuleHash) {
					if putErr := dcache.Put(m.ModuleHash, &DiskPayload{
						Schema:     1,
						Path:       m.Path,
						ModuleHash: m.ModuleHash,
					}); putErr != nil {
						graphErr = putErr
						break
					}
				}
			}
			if graphErr == nil {
				dag.ReportBrokenDeps(idx, slots)
			}
			endGraph(hashIdx, "")
		}
		if opts.EnableTimings && graphTimer != nil {
			report := graphTimer.Report()
			graphReport = &report
		}
		if graphErr != nil {
			return nil, nil, graphErr
		}
	}

	if opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
		if err := enrichModuleResults(ctx, dir, fileSet, results, opts); err != nil {
			return nil, nil, err
		}
	}

	for i := range results {
		bag := results[i].Bag
		if bag == nil {
			continue
		}
		results[i].Builder = nil
		results[i].ASTFile = 0
		if opts.IgnoreWarnings {
			bag.Filter(func(d *diag.Diagnostic) bool {
				return d.Severity != diag.SevWarning && d.Severity != diag.SevInfo
			})
		}
		if opts.WarningsAsErrors {
			bag.Transform(func(d *diag.Diagnostic) *diag.Diagnostic {
				if d.Severity == diag.SevWarning {
					d.Severity = diag.SevError
				}
				return d
			})
			bag.Sort()
		}
	}

	if opts.EnableTimings {
		for i := range results {
			res := &results[i]
			if res.Bag == nil || res.Timing == nil {
				continue
			}
			path := res.Path
			if res.FileID != 0 {
				if file := fileSet.Get(res.FileID); file != nil {
					path = file.Path
				}
			}
			appendTimingDiagnostic(res.Bag, timingPayload{
				Kind:    "file",
				Path:    path,
				TotalMS: res.Timing.TotalMS,
				Phases:  res.Timing.Phases,
			})
			res.Timing = nil
		}
		if graphReport != nil {
			path := graphPath
			if path == "" {
				path = dir
			}
			for i := range results {
				if results[i].Bag == nil {
					continue
				}
				appendTimingDiagnostic(results[i].Bag, timingPayload{
					Kind:    "module_graph",
					Path:    path,
					TotalMS: graphReport.TotalMS,
					Phases:  graphReport.Phases,
				})
				break
			}
		}
	}

	// Emit metrics summary if timing is enabled (Phase 6)
	if opts.EnableTimings {
		metricsStr := metrics.emitMetrics()
		// Add metrics as informational diagnostic to the first result
		for i := range results {
			if results[i].Bag != nil {
				results[i].Bag.Add(&diag.Diagnostic{
					Severity: diag.SevInfo,
					Code:     diag.UnknownCode,
					Message:  "Parallel processing metrics: " + metricsStr,
					Primary:  source.Span{},
				})
				break
			}
		}
	}

	return fileSet, results, nil
}

// enrichModuleResults reruns symbol/semantic resolution for directories that
// declare a multi-file module (pragma module/binary). This ensures all files in
// the module share one scope when diagnosing a directory.
func enrichModuleResults(ctx context.Context, _ string, fileSet *source.FileSet, results []DiagnoseDirResult, opts DiagnoseOptions) error {
	if fileSet == nil {
		return nil
	}

	moduleDirs := make(map[string]struct{})
	pathToIndex := make(map[string]int, len(results))

	for i := range results {
		res := &results[i]

		path := res.Path
		if path == "" && res.FileID != 0 {
			if file := fileSet.Get(res.FileID); file != nil {
				path = file.Path
			}
		}
		if path == "" {
			continue
		}

		pathKey := filepath.ToSlash(path)
		pathToIndex[pathKey] = i
		dirKey := filepath.ToSlash(filepath.Dir(pathKey))

		if res.Builder != nil && hasModulePragma(res.Builder, res.ASTFile) {
			moduleDirs[dirKey] = struct{}{}
		}
	}

	if len(moduleDirs) == 0 {
		return nil
	}

	typeInterner := types.NewInterner()
	moduleExports := make(map[string]*symbols.ModuleExports)

	dirKeys := make([]string, 0, len(moduleDirs))
	for dirKey := range moduleDirs {
		dirKeys = append(dirKeys, dirKey)
	}
	sort.Strings(dirKeys)

	for _, dirKey := range dirKeys {
		moduleRes, err := diagnoseModuleDirectory(ctx, dirKey, fileSet, opts, typeInterner, moduleExports)
		if err != nil {
			return err
		}
		for pathKey, res := range moduleRes {
			idx, ok := pathToIndex[pathKey]
			if !ok {
				continue
			}
			results[idx].Bag = res.Bag
			results[idx].FileID = res.FileID
			results[idx].ASTFile = res.ASTFile
			results[idx].Builder = res.Builder
			results[idx].Symbols = res.Symbols
			results[idx].Sema = res.Sema
		}
	}

	return nil
}

type moduleFileResult struct {
	Bag     *diag.Bag
	FileID  source.FileID
	ASTFile ast.FileID
	Symbols *symbols.Result
	Sema    *sema.Result
	Builder *ast.Builder
}

// diagnoseModuleDirectory parses and resolves a single directory that declares
// a multi-file module. It returns per-file diagnostics and semantic results so
// that directory-level diagnosis can honor shared module scope.
func diagnoseModuleDirectory(
	ctx context.Context,
	dir string,
	fileSet *source.FileSet,
	opts DiagnoseOptions,
	typeInterner *types.Interner,
	moduleExports map[string]*symbols.ModuleExports,
) (map[string]moduleFileResult, error) {
	if fileSet == nil {
		return nil, nil
	}

	bag := diag.NewBag(opts.MaxDiagnostics)
	builder, fileIDs, files, err := parseModuleDir(ctx, fileSet, dir, bag, source.NewInterner(), nil, nil)
	if err != nil {
		if errors.Is(err, errModuleNotFound) {
			return nil, nil
		}
		return nil, err
	}

	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fileSet, builder, fileIDs, fileSet.BaseDir(), reporter)
	if !ok && len(files) > 0 && files[0] != nil {
		meta = fallbackModuleMeta(files[0], fileSet.BaseDir())
	}
	if meta == nil || !meta.HasModulePragma {
		return nil, nil
	}

	rec := &moduleRecord{
		Meta:    meta,
		Bag:     bag,
		Builder: builder,
		FileIDs: fileIDs,
		Files:   files,
	}

	if typeInterner == nil {
		typeInterner = types.NewInterner()
	}
	if moduleExports == nil {
		moduleExports = make(map[string]*symbols.ModuleExports)
	}
	if exp := resolveModuleRecord(ctx, rec, fileSet.BaseDir(), moduleExports, typeInterner, opts); exp != nil {
		moduleExports[normalizeExportsKey(meta.Path)] = exp
	}

	diagsByFile := make(map[source.FileID][]*diag.Diagnostic)
	for _, d := range bag.Items() {
		diagsByFile[d.Primary.File] = append(diagsByFile[d.Primary.File], d)
	}

	out := make(map[string]moduleFileResult, len(fileIDs))
	for i, fileID := range fileIDs {
		file := files[i]
		fileBag := diag.NewBag(opts.MaxDiagnostics)
		if ds := diagsByFile[file.ID]; len(ds) > 0 {
			for _, d := range ds {
				fileBag.Add(d)
			}
		}

		var symRes *symbols.Result
		if rec.Symbols != nil {
			if sym, ok := rec.Symbols[fileID]; ok {
				symCopy := sym
				symRes = &symCopy
			}
		}
		var semRes *sema.Result
		if rec.Sema != nil {
			semRes = rec.Sema[fileID]
		}

		out[filepath.ToSlash(file.Path)] = moduleFileResult{
			Bag:     fileBag,
			FileID:  file.ID,
			ASTFile: fileID,
			Symbols: symRes,
			Sema:    semRes,
			Builder: builder,
		}
	}

	return out, nil
}

func hasModulePragma(builder *ast.Builder, fileID ast.FileID) bool {
	if builder == nil || fileID == ast.NoFileID {
		return false
	}
	node := builder.Files.Get(fileID)
	if node == nil || len(node.Pragma.Entries) == 0 {
		return false
	}
	interner := builder.StringsInterner
	for _, entry := range node.Pragma.Entries {
		name, ok := interner.Lookup(entry.Name)
		if !ok {
			continue
		}
		if name == "module" || name == "binary" {
			return true
		}
	}
	return false
}
