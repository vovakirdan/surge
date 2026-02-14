package driver

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/observ"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// DiagnoseDirWithOptions runs diagnostics on all .sg files in a directory using specified options and job count.
func DiagnoseDirWithOptions(ctx context.Context, dir string, opts *DiagnoseOptions, jobs int) (*source.FileSet, []DiagnoseDirResult, error) {
	if opts == nil {
		opts = &DiagnoseOptions{}
	}
	files, err := listSGFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	// listSGFiles returns paths rooted at dir (i.e. it prefixes each file with dir when dir is relative).
	// DiagnoseFilesWithOptions treats the file list as relative-to-baseDir inputs, so we must strip the
	// dir prefix here to avoid joining baseDir twice (dir/dir/...).
	relFiles := make([]string, 0, len(files))
	for _, p := range files {
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			relFiles = append(relFiles, p)
			continue
		}
		// Defensive: keep original path if Rel escapes the root.
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			relFiles = append(relFiles, p)
			continue
		}
		relFiles = append(relFiles, rel)
	}

	return DiagnoseFilesWithOptions(ctx, dir, relFiles, opts, jobs)
}

// DiagnoseFilesWithOptions runs diagnostics for an explicit list of .sg files.
func DiagnoseFilesWithOptions(ctx context.Context, baseDir string, files []string, opts *DiagnoseOptions, jobs int) (*source.FileSet, []DiagnoseDirResult, error) {
	if opts == nil {
		opts = &DiagnoseOptions{}
	}
	var err error
	files = normalizeFileList(baseDir, files)
	if len(files) == 0 {
		return source.NewFileSetWithBase(baseDir), nil, nil
	}
	if baseDir == "" {
		baseDir = filepath.Dir(files[0])
	}
	if mapErr := ensureModuleMapping(opts, baseDir); mapErr != nil {
		return nil, nil, mapErr
	}

	fileSet := source.NewFileSetWithBase(baseDir)
	if opts.ReadFile != nil {
		fileSet.SetReadFile(opts.ReadFile)
	}
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, p := range files {
		var id source.FileID
		id, err = fileSet.Load(p)
		if err != nil {
			loadErrors[p] = err
			// Keep a placeholder so formatting can still resolve spans even when all loads fail.
			fileIDs[p] = fileSet.AddVirtual(p, nil)
			continue
		}
		fileIDs[p] = id
	}

	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	// Общий in-memory кэш на прогон
	mcache := NewModuleCache(len(files) * 2)
	var dcache *DiskCache
	// Диск-кэш (под будущие экспорты/семантику). Включается флагом --disk-cache.
	if opts.EnableDiskCache {
		dcache, err = OpenDiskCache("surge")
		if err != nil {
			return nil, nil, err
		}
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
					fileID := fileIDs[path]
					bag.Add(&diag.Diagnostic{
						Severity: diag.SevError,
						Code:     diag.IOLoadFileError,
						Message:  "failed to load file: " + loadErr.Error(),
						Primary:  source.Span{File: fileID},
					})
					results[i] = DiagnoseDirResult{Path: path, FileID: fileID, Bag: bag}
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
				modulePath := logicalPathForFile(file.Path, fileSet.BaseDir(), opts.ModuleMapping)
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
					builder, astFile = diagnoseParse(ctx, fileSet, file, bag, opts.DirectiveMode)
					parseNote := ""
					if timer != nil && builder != nil && builder.Files != nil {
						fileNode := builder.Files.Get(astFile)
						if fileNode != nil {
							parseNote = fmt.Sprintf("items=%d", len(fileNode.Items))
						}
					}
					end(parseIdx, parseNote)
					if opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
						if !opts.FullModuleGraph {
							symbolIdx := begin("symbols")
							symbolsRes = diagnoseSymbols(builder, astFile, bag, modulePath, file.Path, fileSet.BaseDir(), nil)
							symbolNote := ""
							if timer != nil && symbolsRes != nil && symbolsRes.Table != nil {
								symbolNote = fmt.Sprintf("symbols=%d", symbolsRes.Table.Symbols.Len())
							}
							end(symbolIdx, symbolNote)

							semaIdx := begin("sema")
							semaRes = diagnoseSema(ctx, builder, astFile, bag, nil, symbolsRes, !opts.NoAlienHints, nil)
							end(semaIdx, "")
						}
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

	if !opts.FullModuleGraph && (opts.Stage == DiagnoseStageSyntax || opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll) {
		baseDir = fileSet.BaseDir()
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
			modulePath := logicalPathForFile(file.Path, baseDir, opts.ModuleMapping)
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
					meta, ok = buildModuleMeta(fileSet, res.Builder, []ast.FileID{res.ASTFile}, baseDir, opts.ModuleMapping, reporter)
				}
			}
			if !ok {
				meta = fallbackModuleMeta(file, baseDir, opts.ModuleMapping)
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
			var overrides map[source.Span]string
			if opts.ModuleMapping != nil {
				records := make(map[string]*moduleRecord, len(entries))
				for _, e := range entries {
					if e.meta == nil {
						continue
					}
					records[e.meta.Path] = &moduleRecord{Meta: e.meta}
				}
				overrides = missingModuleOverrides(records, opts.ModuleMapping)
			}
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].meta.Path < entries[j].meta.Path
			})
			metas := make([]*project.ModuleMeta, 0, len(entries))
			nodes := make([]*dag.ModuleNode, 0, len(entries))
			for _, e := range entries {
				if e.node.Reporter != nil {
					e.node.Reporter = wrapMissingModuleReporter(e.node.Reporter, overrides)
				}
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
		if opts.FullModuleGraph {
			if err := resolveDirModuleGraph(ctx, fileSet, results, opts); err != nil {
				return nil, nil, err
			}
		} else {
			if err := enrichModuleResults(ctx, baseDir, fileSet, results, opts); err != nil {
				return nil, nil, err
			}
		}
	}

	for i := range results {
		bag := results[i].Bag
		if bag == nil {
			continue
		}
		if !opts.KeepArtifacts {
			results[i].Builder = nil
			results[i].ASTFile = 0
		}
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
				path = baseDir
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

func normalizeFileList(baseDir string, files []string) []string {
	if len(files) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		path := filepath.FromSlash(file)
		if !filepath.IsAbs(path) && baseDir != "" {
			path = filepath.Join(baseDir, path)
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		path = filepath.Clean(path)
		if !strings.HasSuffix(path, ".sg") {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
