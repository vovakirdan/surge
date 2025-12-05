# Parallel Compilation in Surge

This document describes the parallel processing capabilities in the Surge compiler's diagnostic system.

## Overview

The Surge compiler implements multi-level parallelism for efficient processing of large codebases:

1. **File-level parallelism** - Process multiple independent files concurrently
2. **Module-level parallelism** - Compute module hashes in parallel batches
3. **Intelligent file classification** - Optimize scheduling based on dependencies

## Architecture

### File-Level Parallelism

When analyzing a directory, Surge processes multiple `.sg` files in parallel:

```bash
surge diag testdata/ --jobs=8
```

The `--jobs` flag controls the maximum number of concurrent workers (0 = auto-detect CPU count).

**Location:** `internal/driver/parallel_diagnose.go`

**How it works:**
- Discovers all `.sg` files in the directory
- Uses `errgroup.WithContext` to spawn worker goroutines
- Each worker processes one file independently
- Respects `--jobs` limit to prevent resource exhaustion

### File Classification

Files are classified by their dependencies to optimize parallel scheduling:

- **FileFullyIndependent** - No imports, can be processed immediately
- **FileStdlibOnly** - Only imports stdlib modules (option, result, bounded)
- **FileDependent** - Imports other project modules, requires dependency resolution

This classification allows the compiler to process independent files with maximum parallelism while respecting dependency constraints for others.

**Location:** `internal/driver/parallel_diagnose.go`

### Batch Parallelism (Module Hash Computation)

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

### Why Semantic Analysis Is Not Parallelized

Semantic analysis (type checking, symbol resolution) remains sequential based on profiling data:

**Benchmark Results:**
```
Phase breakdown across test files:
  Tokenize: 11.8%
  Parse:    52.7%  ← Primary bottleneck
  Symbols:  13.0%
  Sema:     22.3%
```

**Conclusion:** Semantic analysis represents only 22.3% of total compilation time. According to Amdahl's Law, even with perfect parallelization of sema (infinite speedup), the maximum overall speedup would be ~1.27x. The primary bottleneck is parsing (52.7%), which is already parallelized at the file level.

**Additional complexity considerations:**
- Shared mutable state (symbol tables built incrementally)
- Type checker maintains global state
- Import resolution modifies shared structures
- Generic instantiation requires complete dependency types
- Circular dependency detection needs sequential traversal

Given the modest performance gain potential and significant implementation complexity, semantic analysis parallelization is not implemented.

## Caching Architecture

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

#### Parallel Metrics

The compiler tracks parallel processing metrics:
- Worker pool utilization (active/completed/errors)
- Cache hit/miss rates (memory and disk)
- File classification distribution
- Batch size statistics

These metrics are displayed at the end of each run to help understand parallel efficiency.

## Troubleshooting

### Compilation Hangs

```bash
# Reduce worker count to isolate issues
surge diag --jobs=1 testdata/

# Check for circular dependencies in output
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

# Disable disk cache (default)
surge diag testdata/

# Check cache location
echo $XDG_CACHE_HOME  # Or ~/.cache if unset
```

## Best Practices

1. **Default to memory cache:** Disk cache is experimental, use only when needed
2. **Profile before optimizing:** Use `--timings` to identify bottlenecks
3. **Test with race detector:** Always run tests with `-race` when modifying concurrent code
4. **Start with auto workers:** Use `--jobs=0` unless you have specific reasons
5. **Monitor metrics:** Check parallel efficiency at end of run
6. **Clear cache on compiler updates:** Schema version prevents incompatibility but manual clear is safer

## Implementation Summary

The parallel diagnostics implementation includes:

- **Thread-safe disk cache** with `sync.RWMutex` for concurrent access
- **Batch-parallel module hash computation** using topological sort batches
- **Intelligent file classification** for optimal scheduling
- **Comprehensive metrics** tracking worker utilization and cache effectiveness
- **Dependency-aware invalidation** using content-based hashing

Performance results:
- File-level parallelism: Near-linear speedup with CPU count
- Batch parallelism: 20% improvement in dependency-heavy workloads
- Memory cache: Zero overhead, high hit rates
- Disk cache: Currently slower due to I/O overhead, designed for future use

## Future Enhancements

Potential improvements beyond current implementation:

1. **IR Caching** - Cache intermediate representation, not just metadata
2. **Distributed Cache** - Share cache across machines
3. **Chrome Trace Format** - Export traces for visualization in chrome://tracing
4. **Incremental Compilation** - Extend disk cache for true incremental builds
5. **Parse Parallelism** - Investigate parallel parsing (current bottleneck at 52.7%)

## Code Locations

- File parallelism: `internal/driver/parallel_diagnose.go`
- Batch parallelism: `internal/driver/hashcalc.go`
- Memory cache: `internal/driver/modulecache.go`
- Disk cache: `internal/driver/dcache.go`
- CLI flags: `cmd/surge/diagnose.go`

## Conclusion

Surge's parallel processing architecture achieves practical performance improvements while maintaining code simplicity and correctness:

- **File-level parallelism:** Simple, effective, low overhead
- **Batch parallelism:** Respects dependencies, good speedup for hash computation
- **Memory cache:** Fast, zero overhead, works well
- **Disk cache:** Available but disabled by default (high overhead for current use case)

The implementation demonstrates that **targeted optimizations** (file and batch parallelism) provide good results without the complexity of parallelizing semantic analysis, which profiling showed would yield minimal benefit.
