# Epic 19 Task 2: Leaf Free Floor (explicit @drop) — SHIPPED

## What shipped (implementation record)

- **Runtime**: `rt_string_free` (rt_string.c, single free sized from the
  header); `rt_array_free(handle, elem_stride, elem_align)`
  (rt_array_reclaim.c) with the corrected defer mechanics (orphan
  records hold {base, data_size, data_align}; last-view unlink frees
  storage + header + record); registry lock added to rt_array.c
  (closing the pre-existing MT race) with shared internals in
  rt_array_internal.h; debug counter
  `rt_array_debug_deferred_base_drops`.
- **Backend**: `emitInstrDrop` (emit_instr.go) — string-like places call
  rt_string_free, dynamic arrays call rt_array_free with the element
  layout, and the slot is nulled after freeing (structural double-drop
  guard; both helpers are null-safe). Composites stay no-op until the
  Task 4 glue. The debug counter emits through `emitRtUintCounter`
  (shared with rt_worker_count) because intrinsics returning Surge
  `uint` must BOX the raw u64 — a raw store reads as a bignum pointer
  and crashes.
- **Sema**: `handleDrop` now marks non-copy owned drop targets MOVED, so
  a second `@drop` (and any use after `@drop`) is `use of moved value`
  at compile time; `@drop` of a copy value stays a no-op and does not
  consume. Rows in internal/sema/drop_move_test.go.
- **E2E**: `TestRuntimeV2DropLeafReclamation` (internal/vm), 8 scenario
  functions covering the 11-row list at SURGE_THREADS=1,2; wired into
  `runtime-v2-heap-check`.

## Two discipline discoveries (they bit; they are load-bearing)

1. **rt_free re-enters the view registry.** Every `rt_free` calls
   `rt_array_forget_allocation` (rt_alloc.c), which takes the registry
   lock. Therefore NOTHING may be freed while holding that lock —
   detach under the lock, free after releasing. The first draft freed
   link/orphan records under the lock and self-deadlocked the moment a
   view drop actually ran (30s hang, zero output).
2. **The census currency for this vertical is FREES, not balance.**
   Until scope-exit synthesis (Task 3), every temporary leaks by
   design — and even `if` statements and stats-snapshot boxing
   allocate. But nothing frees spontaneously, so windows shaped
   [snapshot; @drop…; snapshot] with NO other statements inside give
   exact free_count deltas: string=1; base(no views)=2 (data+header);
   registered view=2 (link+header); base with live views=0 (counter
   +1); LAST view over a deferred base=5 (link+orphan+view header+
   base data+base header); fixed-array slice=1 (header only).
   Alloc/free BALANCE assertions arrive with Task 3/4 at program end.

## Original design + the view fork (retained)

## The view discovery (supersedes the kickoff's array paragraph)

The kickoff assumed slices are reference-typed (borrows) and never
reach the drop path. FALSE: a slice has the SAME type as an owned
array (`int[]`); view-ness is a sema-side marker only
(`arrayViewBindings` / `arrayViewExprs`,
`internal/sema/type_array_view.go:11-46`), propagated best-effort
through assignments (`:112-124`) and LOST at function boundaries — a
view passed as an `int[]` parameter is indistinguishable from an
owned array inside the callee. The resize guard (`:104-143`) blocks
mutation through views but not ownership confusion.

Therefore the RUNTIME must arbitrate array drops (it can:
`rt_array_is_view`, the `array_views` registry,
`runtime/native/rt_array.c:16-48`), with BOTH directions covered:
- view-side: dropping a view must free the view HEADER only (and
  unlink it from the registry), never the shared data;
- base-side: dropping a base must know whether live views exist over
  its data.

## Fork Resolution (second opinion, 2026-07-14; codex converged)

**V1 (defer) adopted — with a ship-blocking correction to the draft's
mechanics.** As drafted ("base header frees immediately, data
deferred") V1 is a use-after-free: the view registry's `link->base`
points at the base HEADER (`rt_array.c:16-18`), which is the only
place the deferred data pointer and its free size live — and it is
also the identity key for the last-view scan (`array_find_view`).
Freeing the header at base-drop leaves the registry dereferencing
freed memory when the last view goes to release the data. Fix: defer
the base HEADER together with the data (a small side-set of orphaned
bases awaiting free; the header has no spare bit — cap==UINT64_MAX is
already the view sentinel), free both when the last view unlinks.
`rt_array_free` must scan for remaining links itself — NOT reuse
`rt_array_forget_allocation`, which strips live views' links.

Registry facts that make the rest clean: slice-of-slice is FLATTENED
(`array_base_for_slice` re-parents every sub-view to the ultimate
base, `rt_array.c:60-68,232-243`) so `link->base` is always a true
base and there are no view chains to walk; view headers are
independent heap allocations (clean single free); fixed-array slices
register NO link (drop frees the header only).

**Census granularity bound (concrete now):** per-scope alloc/free
balance is unsound for sliced programs by construction — a view
escaping its base's scope defers BOTH frees past the scope exit. The
arc's census gate asserts at PROGRAM END (or any point with zero
outstanding views); a genuinely leaked view still leaves its base
unfreed, so the program-end gate keeps its teeth.

**Hybrid adopted:** V1 semantics + a debug-mode counter incremented at
"base dropped with live views" — tests can assert the float happened
(or did not, in cleanly nested rows) without any production panic.

**The parameter worry dissolves — the safe bound already exists.**
Surge passes plain (non-`&`) parameters as MOVES: `applyParamOwnership`
(`internal/sema/magic_ownership.go:12-38`) routes only `&`/`&mut`
types to the borrow path; everything else (`int[]` and `own T` alike)
takes `observeMove`, marking the caller's binding moved. So the
vertical-1 rule needs NO new machinery: owned/by-value params drop in
the callee (caller is moved-out and never drops); borrowed params are
never dropped (caller retains). Double-free through parameters cannot
happen — affine move tracking, not the ABI handle copy, decides who
drops.

V2 (panic) stays rejected (the sema view marker is function-boundary
blind, so the panic would be unpredictable from source — a
kindness-first violation). V3 (views as a distinct type) stays the
recorded type-system tail.

## The Original Fork (retained for the record)

What does dropping a BASE with live views do?

- **V1 — defer**: the base's data stays alive until the last view
  over it drops (registry-counted); the base header frees
  immediately. Memory-safe, no new diagnostics, census balances at
  scope end; the data's actual free point floats with view lifetime.
- **V2 — panic**: kind runtime panic ("array dropped while slices
  over it are alive; drop the slices first or restructure"). Honest
  and immediate, but valid-looking programs explode at runtime, and
  the sema marker cannot reliably pre-warn (function boundaries).
- **V3 — type-system fix**: make views a distinct type (true borrow
  or view type). The durable answer; a language-surface change that
  is its own future epic (recorded as the tail either way).

Leaning: V1 for this epic (runtime-arbitrated safety without runtime
panics on today's legal programs), V3 recorded as the type-system
tail, V2 rejected (the marker's function-boundary blindness makes the
panic unpredictable from source — fails kindness).

## Scope of Task 2 once the fork resolves

- `rt_string_free(handle)`: unconditional single free (size from
  header) — strings have no view problem (literals are heap-per-use,
  kickoff evidence).
- `rt_array_free(handle, elem_stride)`: view-aware per the resolved
  fork; arrays of copyables only (element recursion is Task 4 glue).
- Backend: `InstrDrop` emission for string/array places triggered by
  the EXISTING explicit `@drop` only; nulls the slot after freeing
  (structural double-drop guard).
- Census harness rows (the review's list, test-owned):
  1. @drop of a view frees exactly the view header + unlinks; base
     data untouched;
  2. @drop of a base with NO views frees data + header;
  3. @drop of a base WITH live views defers header+data; the last
     view drop frees both; balances at program end (float row — the
     hybrid counter observes it);
  4. slice-of-slice with all drop-order permutations balances
     (flattened links);
  5. empty slice still allocates a view header + link and must drop;
  6. fixed-array slice (no registry link) drops header only, and the
     last-view scan is confirmed absent;
  7. by-value move of an owned array into a callee: callee drops,
     caller (moved) does NOT — negative double-drop row;
  8. by-value move of a VIEW into a callee: callee frees the header
     only; base unaffected;
  9. borrowed params (`&`/`&mut`) are never dropped by the callee;
  10. double-@drop stays a sema error (negative row);
  11. REGRESSION pin for the header-defer fix: a view whose base was
      dropped-with-views must survive its own drop (fails under
      "free header immediately", passes under "defer header too").

Row 10 reality check: sema did NOT reject double-@drop before this
task (`@drop` recorded a borrow event but never consumed the binding —
only drop-AFTER-move was caught, through `typeExpr`'s use check). The
task closed the asymmetry: `handleDrop` now marks non-copy owned drop
targets moved, so drop-after-drop and any use-after-drop are
`use of moved value` at compile time, while `@drop` of a copy value
stays non-consuming (sema rows pin all three). The emitter's
null-store keeps the runtime safe even if a future surface bypasses
the sema check.

## Status

SHIPPED (see the implementation record at the top). All 11 rows plus
the moved-view-into-callee row run in `TestRuntimeV2DropLeafReclamation`
at SURGE_THREADS=1,2, wired into `runtime-v2-heap-check`.
