# Parallel Compilation in Surge

This document describes the parallel processing capabilities in the Surge compiler, focusing on the diagnostic system and future compilation architecture.

## Overview

The Surge compiler implements multi-level parallelism for efficient processing of large codebases:

1. **File-level parallelism** - Process multiple independent files concurrently
2. **Module-level parallelism** - Compute module hashes in parallel batches
3. **Batch parallelism** - Process dependency-free modules simultaneously

## Architecture

### Current Implementation

#### 1. File-Level Parallelism (`DiagnoseDirWithOptions`)

When analyzing a directory, Surge processes multiple `.sg` files in parallel:

```bash
surge diag testdata/ --jobs=8
```

The `--jobs` flag controls the maximum number of concurrent workers (0 = auto-detect CPU count).

**Location:** `internal/driver/parallel_diagnose.go:40-180`

**How it works:**
- Discovers all `.sg` files in the directory
- Uses `errgroup.WithContext` to spawn worker goroutines
- Each worker processes one file independently
- Respects `--jobs` limit to prevent resource exhaustion

#### 2. Batch Parallelism (Module Hash Computation)

**Location:** `internal/driver/hashcalc.go`

After building the module dependency graph, Surge uses topological sorting to identify batches of modules that can be processed in parallel:

```go
// Batches from Kahn's algorithm
// Example: [[A, B], [C], [D, E, F]]
// - Batch 0: A and B have no dependencies, compute in parallel
// - Batch 1: C depends on A or B, wait for batch 0
// - Batch 2: D, E, F depend only on earlier batches, compute in parallel
```

**What is parallelized:**
- Module content hash computation (files + dependencies)
- Module metadata extraction

**What is NOT parallelized:**
- Semantic analysis (type checking)
- Symbol resolution
- Import processing

### Memory Cache

**Location:** `internal/driver/modulecache.go`

All module analysis results are cached in memory within a single run:

```go
type ModuleCache struct {
    mu      sync.RWMutex
    modules map[project.Digest]*project.ModuleMeta
    broken  map[project.Digest]bool
    sources map[project.Digest]*ast.Builder
}
```

**Thread-safe:** Yes, protected by `sync.RWMutex`

**Lifetime:** Single compiler invocation

**Benefits:**
- Zero I/O overhead
- Fast lookups (map access)
- Shared across all workers

### Disk Cache (Experimental)

**Location:** `internal/driver/dcache.go`

The disk cache persists module metadata across compiler runs for incremental compilation.

#### Enabling Disk Cache

```bash
surge diag --disk-cache testdata/
```

**Flag:** `--disk-cache` (disabled by default)

**Cache location:** `~/.cache/surge/mods/` (auto-detected from XDG_CACHE_HOME)

#### When to Use Disk Cache

**✅ Use disk cache when:**
- Working on large projects (100+ files)
- Caching expensive analysis results (semantic analysis, IR generation)
- Performing incremental builds where most files don't change
- CI/CD pipelines where cache can be shared across runs

**❌ Don't use disk cache when:**
- Processing small projects (< 50 files)
- Operations complete in milliseconds (I/O overhead dominates)
- Working on frequently changing code
- Memory cache hit rate is already high

#### Performance Characteristics

**Current Implementation:**
- **Cache hit:** Still incurs deserialization overhead (~5-10ms)
- **Cache miss:** Incurs file read overhead (~2-5ms) before recomputation
- **Cache write:** Adds serialization + file write overhead (~10-20ms)

**Benchmark Results (stdlib/saturating_cast.sg):**
- Without cache: 57ms
- With cache (first run): 101ms (77% slower)
- With cache (second run): Still slower due to I/O

**Conclusion:** Current disk cache implementation adds overhead for fast operations. It's designed for future use when caching more expensive artifacts (full semantic analysis, IR, compiled code).

#### Cache Structure

```go
type DiskPayload struct {
    Schema          uint16           // Version for schema evolution
    Name            string           // Module name
    Path            string           // Import path
    Dir             string           // Directory path
    Kind            uint8            // ModuleKind (source/builtin/stdlib/external)
    NoStd           bool             // Module doesn't import stdlib
    HasModulePragma bool             // Has explicit module pragma
    ImportPaths     []string         // Imported module paths
    FilePaths       []string         // Source file paths
    FileHashes      []project.Digest // Individual file content hashes
    ContentHash     project.Digest   // Combined content hash
    ModuleHash      project.Digest   // Module + dependencies hash
    DependencyHash  project.Digest   // Hash of all dependency hashes
    Broken          bool             // Module has errors
}
```

**Serialization:** Uses `github.com/vmihailenco/msgpack/v5` for compact binary format

**Cache Invalidation:** Content-based hashing ensures cache is invalidated when:
- Module source files change
- Dependency modules change
- File structure changes (new imports, removed files)

## Batch Parallelism Details

### Question: "У нас нет batch parallelism на sema?"

**Answer:** No, batch parallelism currently only applies to module hash computation, NOT semantic analysis.

**Current State:**
- ✅ Module hash computation uses batches (Phase 3 from PERFORMANCE_FINAL_SUMMARY.txt)
- ❌ Semantic analysis (type checking, symbol resolution) is sequential

**Why Semantic Analysis is Sequential:**

1. **Shared Mutable State:**
   - Symbol tables are built incrementally
   - Type checker maintains global state
   - Import resolution modifies shared structures

2. **Dependency-Order Requirements:**
   - Types from imported modules must be resolved first
   - Generic instantiation requires complete dependency types
   - Circular dependency detection needs sequential traversal

3. **Complexity:**
   - Thread-safe semantic analysis requires careful synchronization
   - Risk of deadlocks with complex dependency graphs
   - Difficult to debug semantic errors in parallel execution

**Future Work:**

Parallel semantic analysis is possible but requires architectural changes:

```go
// Hypothetical batch-parallel semantic analysis
for batchIdx, batch := range topo.Batches {
    g := errgroup.Group{}
    g.SetLimit(opts.MaxWorkers)

    for _, modID := range batch {
        modID := modID
        g.Go(func() error {
            // Each module in batch has no inter-dependencies
            // Can type-check in parallel
            return typeCheckModule(modID, sharedSymbolTable)
        })
    }

    if err := g.Wait(); err != nil {
        return err
    }
}
```

**Challenges:**
- Thread-safe symbol table access
- Concurrent type inference
- Error reporting synchronization
- Generic instantiation caching

**Recommendation:** Defer parallel semantic analysis until profiling shows it's a bottleneck (currently module hash computation is fast enough).

## Performance Tuning

### Choosing Worker Count

```bash
# Auto-detect (default: runtime.NumCPU())
surge diag testdata/ --jobs=0

# Explicit worker count
surge diag testdata/ --jobs=4

# Single-threaded (for debugging)
surge diag testdata/ --jobs=1
```

**Recommendations:**
- **Small projects (< 10 files):** `--jobs=1` (parallelism overhead not worth it)
- **Medium projects (10-50 files):** `--jobs=4` (balanced parallelism)
- **Large projects (> 50 files):** `--jobs=0` (use all CPUs)
- **CI/CD:** `--jobs=0` (maximize throughput)

### Monitoring Performance

#### Enable Timing Information

```bash
surge diag --timings testdata/
```

Shows phase-level timing:
```
Phase Tokenize: 5ms
Phase Syntax:   15ms
Phase Sema:     35ms
Total:          55ms
```

#### Enable Detailed Tracing

```bash
surge diag --trace=/tmp/trace.log --trace-level=detail testdata/
```

Trace levels:
- `off` - No tracing
- `phase` - High-level phases only
- `detail` - Include module operations
- `debug` - Full diagnostic information

**Trace output includes:**
- Module batch processing
- Cache hits/misses
- Worker utilization
- Dependency graph structure

## Thread Safety

All concurrent data structures use proper synchronization:

### Memory Cache (`ModuleCache`)
```go
type ModuleCache struct {
    mu sync.RWMutex  // Protects all fields
    // ...
}

func (mc *ModuleCache) Get(hash project.Digest) (*project.ModuleMeta, bool, *ast.Builder) {
    mc.mu.RLock()         // Read lock for Get
    defer mc.mu.RUnlock()
    // ...
}

func (mc *ModuleCache) Put(meta *project.ModuleMeta, broken bool, src *ast.Builder) {
    mc.mu.Lock()          // Write lock for Put
    defer mc.mu.Unlock()
    // ...
}
```

### Disk Cache (`DiskCache`)
```go
type DiskCache struct {
    mu  sync.RWMutex  // Protects file operations
    dir string
}

// Read operations use RLock (concurrent reads OK)
// Write operations use Lock (exclusive access)
```

### Testing for Race Conditions

```bash
# Run tests with race detector
go test -race ./internal/driver/...

# Run CLI with race detector
go run -race cmd/surge/main.go diag testdata/ --jobs=8
```

## Implementation Phases (Completed)

### Phase 1: Thread Safety for DiskCache ✅
Added `sync.RWMutex` to DiskCache for concurrent access.

**Impact:** Enables safe parallel reads/writes to disk cache.

### Phase 3: Batch-Parallel Module Processing ✅
Uses topological sort batches for parallel hash computation.

**Impact:** 20% improvement in jobs=1 case.

**Location:** `internal/driver/hashcalc.go`

### Phase 5: Independent Files Detection ✅
Classifies files by dependency type for optimal scheduling.

**Impact:** Minimal (2-4% overhead) but clearer architecture.

**Location:** `internal/driver/parallel_diagnose.go`

### Phase 2+4: Disk Cache Infrastructure ✅ (Disabled by Default)
Full disk cache implementation with dependency tracking.

**Impact:** 77% slower due to I/O overhead (disabled by default, use `--disk-cache` to enable).

## Future Enhancements

### Phase 6: Observability (Pending)
- Worker pool metrics (active/completed/errors)
- Cache hit/miss rates
- Batch size distribution
- Parallel efficiency metrics

### Beyond Current Plan
1. **Parallel Semantic Analysis** - Extend batch parallelism to type checking
2. **Incremental Compilation** - Use disk cache for true incremental builds
3. **Distributed Cache** - Share cache across machines
4. **IR Caching** - Cache intermediate representation, not just metadata
5. **Chrome Trace Format** - Export traces for visualization in chrome://tracing

## Troubleshooting

### Compilation Hangs

```bash
# Enable tracing to see where it's stuck
surge diag --trace=/tmp/hang.log --trace-level=debug testdata/

# Check for circular dependencies
grep "cycle detected" /tmp/hang.log
```

### Poor Parallel Performance

```bash
# Profile with CPU profiler
surge diag --cpuprofile=/tmp/cpu.prof testdata/
go tool pprof /tmp/cpu.prof

# Check for lock contention
go tool pprof -http=:8080 /tmp/cpu.prof
# Look for "sync.(*RWMutex)" in profiles
```

### Cache Issues

```bash
# Clear disk cache
rm -rf ~/.cache/surge/mods/

# Disable disk cache
surge diag testdata/  # --disk-cache is off by default

# Check cache location
echo $XDG_CACHE_HOME  # Or ~/.cache if unset
```

## Best Practices

1. **Default to memory cache:** Disk cache is experimental, use only when needed
2. **Profile before optimizing:** Use `--timings` and `--trace` to identify bottlenecks
3. **Test with race detector:** Always run tests with `-race` when modifying concurrent code
4. **Start with auto workers:** Use `--jobs=0` unless you have specific reasons
5. **Monitor cache hit rates:** High miss rates indicate cache is ineffective
6. **Clear cache on compiler updates:** Schema version prevents incompatibility but manual clear is safer

## References

- **Parallel Diagnostics Plan:** `/Users/vladimirkirdan/.claude/plans/zesty-noodling-salamander.md`
- **Performance Summary:** `PERFORMANCE_FINAL_SUMMARY.txt`
- **Code Locations:**
  - File parallelism: `internal/driver/parallel_diagnose.go`
  - Batch parallelism: `internal/driver/hashcalc.go`
  - Memory cache: `internal/driver/modulecache.go`
  - Disk cache: `internal/driver/dcache.go`
  - CLI flags: `cmd/surge/diagnose.go`

## Conclusion

Surge's parallel processing architecture balances performance with maintainability:

- **File-level parallelism:** Simple, effective, low overhead
- **Batch parallelism:** Respects dependencies, good speedup
- **Memory cache:** Fast, zero overhead, works well
- **Disk cache:** High overhead for current use case, designed for future

The main lesson from implementation: **simple optimizations (batch parallelism) often beat complex ones (disk caching)**. Always measure before and after.
