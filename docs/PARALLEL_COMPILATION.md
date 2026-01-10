# Parallel Diagnostics and Caching in Surge
[English](PARALLEL_COMPILATION.md) | [Russian](PARALLEL_COMPILATION.ru.md)

This document describes how the Surge compiler parallelizes **diagnostics** and
how it uses **in-memory and disk caches** for faster repeated runs.

> Scope: `surge diag` on directories. Single-file runs are mostly sequential.

---

## 1. Overview

When you run diagnostics on a directory, Surge uses **file-level parallelism**
plus **batch-parallel hash computation** for the module graph:

- **File-level parallelism:** tokenization, parsing, symbols, and sema run in
  compiler worker goroutines (Go, bounded by `--jobs`).
- **Module graph phase:** only files that import project modules are included.
- **Batch hash computation:** module hashes are computed in dependency order
  with parallel batches.

---

## 2. File-Level Parallelism

```bash
surge diag path/to/dir --jobs=8
```

- `--jobs=0` (default) uses `GOMAXPROCS(0)`.
- Each `.sg` file is analyzed independently in a worker.
- Implementation: `internal/driver/parallel_diagnose.go`.

### 2.1. File Classification

To avoid unnecessary graph work, files are classified by imports:

- **FileFullyIndependent:** no imports
- **FileStdlibOnly:** imports only stdlib modules
- **FileDependent:** imports project modules

Stdlib modules are detected by the **first path segment** of each import. The
current list is:

- `option`
- `result`
- `bounded`
- `saturating_cast`
- `core`

Only **FileDependent** files enter the module graph phase.

Implementation: `internal/driver/parallel_diagnose_helpers.go`.

---

## 3. Module Graph and Hashes

After per-file analysis, Surge builds a module dependency graph and computes
hashes in **topological batches**:

- Kahn topo sort produces batches of independent nodes.
- Batches are processed **in reverse order** (dependencies first).
- Each batch is computed in parallel using Go goroutines + `sync.WaitGroup`.

Implementation: `internal/driver/hashcalc.go`.

Hash formula:

```
ModuleHash = H(ContentHash || DepHash1 || DepHash2 ...)
```

Deps are ordered deterministically by the graph builder.

---

## 4. Caching

### 4.1. In-Memory Cache (per run)

A shared memory cache stores module metadata during a single invocation.
This is used by all workers and is protected by `sync.RWMutex`.

Implementation: `internal/driver/modulecache.go`.

### 4.2. Disk Cache (optional)

Enable with:

```bash
surge diag path/to/dir --disk-cache
```

Notes:

- Disk cache is **experimental** and currently stores **module metadata only**.
- It is used **only in directory runs** (`parallel_diagnose`).
- Entries include a schema version; mismatches are treated as cache misses.
- Cache location:
  - `${XDG_CACHE_HOME}/surge/mods/` if set
  - otherwise `~/.cache/surge/mods/`
- Files are msgpack (`.mp`) keyed by hashes.

Implementation: `internal/driver/dcache.go`.

**When disk cache helps:** large projects with expensive module graphs.
**When it hurts:** small projects where I/O dominates (cache may be slower).

---

## 5. Timings and Metrics

Enable timings for per-file and module-graph phase breakdowns:

```bash
surge diag path/to/dir --timings
```

When `--timings` is enabled, Surge also emits a **metrics summary** as an
info diagnostic, e.g.:

```
Parallel processing metrics: workers: 120 completed, 0 errors | cache: mem=12/120 (10.0%), disk=0/12 (0.0%) | files: 120 total (90 indep, 20 stdlib, 10 dep) | batches: 4 (avg=2.5, max=5)
```

Tracked metrics:

- Worker activity (active/completed/errors)
- Cache hit/miss rates (memory + disk)
- File classification distribution
- Batch counts and sizes for module hashes

---

## 6. Performance Notes

- Parsing is the dominant cost for most real codebases, and it is already
  parallelized at the file level.
- Module hash computation benefits from batch parallelism but is typically a
  smaller share of total time.
- Semantic analysis is not currently parallelized **across modules** due to
  shared symbol and import resolution.

---

## 7. Troubleshooting

**Diagnostics feel stuck or flaky:**

```bash
surge diag path/to/dir --jobs=1
```

**Clear disk cache:**

```bash
rm -rf ~/.cache/surge/mods/
```

**Reduce output noise:**

```bash
surge diag path/to/dir --no-warnings
```

---

## 8. Key Code Locations

- Parallel diagnostics: `internal/driver/parallel_diagnose.go`
- File classification: `internal/driver/parallel_diagnose_helpers.go`
- Module graph and hashes: `internal/project/dag`, `internal/driver/hashcalc.go`
- Memory cache: `internal/driver/modulecache.go`
- Disk cache: `internal/driver/dcache.go`
- CLI flags: `cmd/surge/diagnose.go`
