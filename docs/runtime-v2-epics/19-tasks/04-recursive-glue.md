# Epic 19 Task 4: Recursive Glue — DESIGN

## Increments

- **A — the two move-tracking prerequisites** (they are why the glue
  cannot ship first: without them, recursive drops double-free
  through untracked moves):
  1. Generic calls do not apply argument ownership. Root cause found:
     `type_expr_calls.go` — the monomorphic candidate branch calls
     `applyCallOwnership(sym, args)`, the generic branch performs the
     same sequence WITHOUT that one line. Fix is the missing call;
     the fallout is new (correct) use-after-move rejections wherever
     existing code moves a value through a generic call and keeps
     using it.
  2. Struct-literal fields do not observe moves
     (`ensureStructFieldType` types the value but never
     `observeMove`s it): `let t = S{f: s}; use(s)` is silently
     accepted while the aggregate owns s's handle.
- **B — struct drop glue**: a struct's `InstrDrop` frees its
  droppable fields (leaf calls field by field; nested structs
  recurse), replacing the backend's composite no-op.
- **C — droppable array elements** (`string[]`, arrays of structs):
  element-recursive freeing + census e2e at SHARDS=1/2/8 (crossing
  programs from Epics 16-18 rerun with the census gate).

## Diagnostics contract (kindness-first + the hint mandate)

The author's rule (2026-07-14): every tightening ships with an
actionable hint whenever the compiler understands the context —
"здесь можно другим путем, дружище", never bare rejection. Both new
rejections surface as `use of moved value` and MUST carry:

- a note AT THE MOVE SITE: "the value moved into this call/struct
  field here";
- a way-out hint chosen by context: callee parameter is readable →
  "if `f` only reads it, take the parameter by reference: `&T`";
  value needed afterwards → "keep a usable copy: pass
  `x.__clone()`". For struct fields: "the struct now owns this
  value; read it back through the struct, or store a clone".

The existing specialized messages (tasks → `.clone()`, far handles →
`.share()`) are the template; the generic message gains the
move-site note + hint instead of staying bare.

## Status

Increment A in progress.
