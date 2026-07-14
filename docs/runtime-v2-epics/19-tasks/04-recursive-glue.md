# Epic 19 Task 4: Recursive Drop Glue

## Increment A — SHIPPED (2026-07-14)

The move-tracking prerequisites for recursive glue, plus the fixes the
work exposed:
- argument ownership through GENERIC calls (was skipped in the
  resolver's generic branch — a value moved through a generic callee
  stayed "live" in the caller);
- struct-literal field moves (named `S{}` and anonymous `{}` forms);
- field/element-read aliasing (a binding whose initializer is a
  projection read shares its container's handle — never scope-dropped;
  leak over double free until composite drops land, i.e. B/C below);
- way-out hints on every new use-of-moved rejection (move-site note +
  "take a reference / pass a clone");
- block-expression drop scopes.
- Fixed a pre-existing shallow string `__clone` (it aliased the buffer
  → a use-after-free once drops freed the clone; now `rt_string_clone`
  deep-copies) and a tag-construction allocation undersize (payload
  store ran past the block).
- Per-arm drop synthesis replaced the partial-path-move rejection
  (SEM3136): a droppable moved on some arms drops in the arm where it
  stays live; use-after-merge stays a use-of-moved error.

## Increments B/C — DESIGN (recursive composite drops)

Today the backend frees LEAF values (string, dynamic array) but a
composite `InstrDrop` is a no-op — so a struct/tuple/union/array that
OWNS heap values leaks BOTH its box and its fields' storage.

### Representation (verified)
- Structs, tuples, unions/enums, fixed arrays are BOXED: a heap `ptr`
  from `rt_alloc(size, align)`; fields live inline at layout offsets;
  a union carries a tag + payload at `PayloadOffset`.
- Dynamic arrays: header{len, cap, data}; elements inline at
  `data + i*stride`.
- A droppable field/element of pointer-shaped type (string, array,
  nested box) is stored AS a pointer; a droppable inline field (a
  fixed array) sits directly at its offset.

### The glue: generated per-type drop functions
Emit, on demand, `@drop.type<ID>(ptr %p)` for each composite type that
(transitively) owns heap state:
- **struct / tuple**: for each field of droppable type, drop it at
  `p + offset` (load the pointer and call the leaf free / nested
  `@drop.type<field>`; a fixed-array field drops in place), then
  `rt_free(p, size, align)` frees the box.
- **union / enum**: load the tag, switch, drop the active variant's
  droppable payload (if any), then free the box. This reclaims
  `Option<string>`, `Result<T,E>`, `TaskResult<T>` — extremely common,
  so unions are IN scope.
- **fixed array `[T; N]`**: drop each of the N inline elements, then
  free the box.
- **dynamic array `T[]` with droppable T**: rides `rt_array_free`.
  Element drops must run only when the data is ACTUALLY freed (the last
  owner) — a base with live views defers its data, so its element drops
  defer with it (extend the Task-2 orphan record to carry an
  element-drop function id). The backend passes the element's drop-glue
  function pointer to a new `rt_array_free_elems(header, stride, align,
  dropfn)`; the leaf `rt_array_free` stays for copyable elements.

`InstrDrop` on a composite loads the handle and calls its
`@drop.type<ID>` (null-safe, and the slot is nulled after, like leaves).

### Recursion & cycles
Recursive types (`Node? = Option<Node>` — a linked list) drop by the
glue calling itself down the chain, following the data. Correct, but a
very deep chain recurses on the C stack. Iterative drop for deep
recursive structures is recorded as a TAIL (matches typical first
implementations; rare in practice); B/C ships the recursive form.

## Semantics to confirm (before implementation)

1. **Unions IN scope** (Option/Result/TaskResult with droppable
   payloads get tag-switch drop glue) — vs deferring them, which would
   leak every `Option<string>`. Recommend IN.
2. **Maps deferred** (heap hash structure; its own effort) — a `Map`
   with droppable K/V leaks until a later pass. Recommend defer,
   recorded as a debt.
3. **Drop order immaterial** (no user destructors; drop == free), so
   fields drop in declaration/index order. Recommend accept.
4. **Views**: array element drops defer with the data when the base has
   live views (preserving Task-2 view safety). Recommend accept.
5. **Deep-recursion tail**: ship recursive glue now, record iterative
   drop for deep structures as a future tail. Recommend accept.

## Diagnostics contract (kindness-first + the hint mandate)

Every tightening ships with an actionable hint (move-site note + a
context-chosen way out: `&T` if only read, `x.__clone()` if needed
after). Delivered in increment A.

## Increments B/C — SHIPPED (2026-07-14)

All five semantics confirmed (unions in scope; maps deferred; drop
order immaterial; view-safe element drops; recursive-now). Delivered:
`emit_drop_glue.go` generates `@drop.typeN` per composite type
(struct/tuple/union tag-switch/fixed-array), emitted after the user
functions to a fixpoint; `rt_array_free_elems` drops array elements
before freeing data, deferring element drops with the data when a base
has live views (orphan record carries the drop plan). `InstrDrop`
routes composites to their glue and dynamic-arrays-of-droppables to
rt_array_free_elems. E2e TestRuntimeV2DropComposite (struct, tuple,
Option some/none, fixed array, string array, array-of-structs, nested,
array-with-view) at SURGE_THREADS=1,2.

Two bugs the work exposed and fixed:
- union drop glue block-label ids drifted (field drops advance the temp
  counter) — captured a fixed union id before the switch;
- `BytesView.owner` was typed `string` but is a runtime-set NON-owning
  back-reference — composite drops freed the borrowed source (double
  free in the HTTP stack). Retyped `owner: *byte` (same layout, never
  read in Surge). This is the class risk of composite drops: a
  runtime-backed type whose field type lies about ownership; the full
  gate found BytesView was the only one.

Deferred tails (recorded): maps (own future redesign), iterative drop
for very deep recursive structures.

## Status

A + B + C SHIPPED. Vertical 1 (local drop emission) complete: leaves,
scope-exit synthesis, statement temporaries, per-arm synthesis, and
recursive composite glue all land. make check + runtime-v2-check green.
