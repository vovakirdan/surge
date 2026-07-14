# Epic 19 Task 1: Kickoff — Free-Helper Design, Census Design, Doc Changes

Semantics approved 2026-07-14 (scope-exit drops; partial-path-move
rejection; eval-then-suppressible-drop reassignment; @raii reserved).

## Evidence Re-Pin (verified at commit `c5748852`)

Beyond the epic doc's pins, two representation facts discovered here
that shape the free helpers:

- **String literals are NOT static.** Every `mir.ConstString` use
  calls `rt_string_from_bytes(@bytes_global, len)`
  (`internal/backend/llvm/emit_term.go:307-315`) — a fresh heap
  `SurgeString{len_cp, len_bytes, data[]}` single allocation
  (`runtime/native/rt_string.c:272-296`). Consequences: (a) there is
  NO "don't free the literal" special case — every string value is
  unconditionally freeable; (b) literals allocate per use, so drops
  will reclaim real garbage immediately (literal interning is a
  recorded optimization tail, not vertical-1 scope).
- **Arrays are header + separate data + a view registry.**
  `SurgeArrayHeader{len, cap, data*}` with slices registered as views
  sharing the base's data (`runtime/native/rt_array.c:10-23`,
  `array_views` link list). Owned-array free = data + header +
  view-registry hygiene; SLICES must be reference-typed (borrows) so
  their end is `InstrEndBorrow`, never `InstrDrop` — Task 2 owns a
  row proving a slice never reaches the drop path, and the free
  helper panics on a view header (kindness: names the base/view
  confusion instead of corrupting).

## Free-Helper Design (implemented in Task 2)

- `rt_string_free(handle)`: size = `sizeof(SurgeString) + len_bytes
  + 1`, align = `alignof(SurgeString)` — computable from the header;
  one `rt_free`. Null-safe (freed/moved slots are nulled by the
  emitter).
- `rt_array_free(handle, elem_stride)`: frees data (cap * stride)
  then the header; panics on a view header (`rt_array_is_view`);
  element-recursive freeing (arrays of droppables) arrives with the
  glue in Task 4 — Task 2 covers arrays of copyables only.
- Struct fields have NO runtime helper: the compiler emits per-type
  drop glue (Task 4) that calls the leaf helpers field by field.
  Naming: `rt_string_free` / `rt_array_free` sit in the existing
  string/array modules (file caps respected).

## Census-Gate Design

`HeapStats` is already a Surge-visible surface (`heap_stats()`:
alloc_count/free_count/live_blocks/live_bytes, contract rows in
`internal/vm/runtime_v2_heap_accounting_contract_test.go`). The gate
shape: snapshot-delta — a program takes stats before and after the
scenario under test and asserts the DELTA balances (allocs == frees),
which subtracts runtime-internal allocations without an allowlist.
E2E rows print the delta and the Go side asserts it; the harness (C)
rows keep using the existing census helpers.

## ATTRIBUTES.md Changes (landed with this task)

1. `@drop` row: "Explicit drop/borrow end point" → notes it now also
   FREES non-copy owned values (real from Task 2 on).
2. `@raii` row/section: stays reserved; wording pins its future
   meaning to "user-defined scope-exit destructor hook" and states
   drops are universal and not @raii-gated.

## Baselines (Sentrux, committed tree `c5748852`)

| Scope | Quality |
| --- | --- |
| `.` (root, advisory) | 6169 |
| `internal` | 6484 |
| `runtime` | 5307 |
| `runtime/native` | 5395 |

## Open Debt Touched

None directly; RV2-DEBT-034 activates in vertical 2 (next epic), not
here. Task 2 must state its position against the rt_string/rt_array
legacy LOC ceilings (RV2-DEBT-005) when adding the helpers.
