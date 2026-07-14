# Epic 19 Task 2: Leaf Free Floor (explicit @drop) — DESIGN + the View Fork

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

## The fork (out for second opinion, then the user's semantics gate)

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
- Census harness rows: @drop of a string balances; @drop of an owned
  array balances; @drop of a view frees exactly the header; the
  resolved base-with-views semantics row; double-@drop is already a
  sema error (move tracking) — negative row confirms.

## Status

Design under review.
