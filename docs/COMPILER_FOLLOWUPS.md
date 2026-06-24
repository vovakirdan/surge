# Compiler And Runtime Follow-ups

Open follow-up notes collected during stdlib work when something looked like a compiler or runtime sharp edge but did not block the current task.

## 2026-03-20

- `surge diag` and `surge build` can disagree on task-leak reporting in some async patterns.
  While iterating on `stdlib/http::serve`, a spawn pattern around accepted connections passed `diag` but failed under `build` with `task is neither awaited nor returned`.

- `string -> byte[]` conversion currently accepts owned `string`, but rejects `&string` with `SEM3015: cannot cast &string to [byte]`.
  Observed while deduplicating local `string_to_bytes(...)` helpers in `stdlib/http` and `stdlib/json/parser`.
  Current workaround is `borrowed_string.__clone() to byte[]`.

## 2026-06-24

- Array indexing requires dynamic `int`, so fixed-width loop counters must be
  converted before indexing. In a byte-copy benchmark, converting `uint64` to
  `int` inside a large per-byte loop produced millions of bigint allocations and
  could exhaust the native runtime before the benchmark completed. This does not
  affect the runtime-backed `stdlib/bytes.append_bytes_range` path, but it is a
  real sharp edge for hand-written per-byte loops.
