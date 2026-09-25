# index requires a non-scalar index transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 17 |
| Programs where it is the only root reason | 3 |
| Rows | 152 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_index.go:139`.** The index operand is not the builtin `int` (read from code). Related raises of the same text are at `return_origin_index_primitive.go:80` and `return_origin_index_store.go:135` and `:140`.
- The missing fact is the transfer of an index whose operand is a range or a map key. That runs through a selected `__index` (or `__index_set`) body instead of the canonical array element read.

## Corpus examples

1. **`sema/valid/range_literals.sg:18`**: `let x = b[r5];`. This is 1 row, its only root.
2. **`vm_maps/map_index_get.sg:4`**: `print((m["x"]) to string);`, through `Map.__index` (`core/map.sg:42-48`).

Other non-core carriers:

- `hir/indexing_ranges.sg:11`: `return b[[1..2]];`
- `vm_maps/map_get_mut.sg`, `map_growth_boundary.sg` and `map_index_set.sg`
- `stdlib/json/parser.sg:53`: `return (*data)[index];`

## Sound transfers and size

- **N-INDEX-CALL, S to M, prototype 65 lines, measured.** A nongeneric, synchronous, two-formal `__index` the checker selected, with no `&mut` formal, is evaluated as a call. Each actual passes as its storage when the checker made an implicit borrow, and otherwise as its value under the loan-discard guard G6-ii. The result comes from the callee's body summary, or from its opaque declaration promise. This frees `hir/indexing_ranges.sg` and `sema/valid/range_literals.sg`.
- **N-MAP-INDEX, M, estimate 150 to 250 lines (read from code; not prototyped).** The generic `Map<K, V>.__index` returns `&V` out of `self.get_ref(key)`, so the result is the map's storage and never the key. `__index_set` (`core/map.sg:50-53`) stores `value` into the map through `rt_map_insert`. Both need the finalized concrete use of the Map instance and a store transfer for the inserted value. This frees `map_index_get.sg`, `map_growth_boundary.sg` and `map_composite_value.sg`. `map_index_set.sg` also needs N-STORE. `map_get_mut.sg` also needs owner question 3, option B.

## Unsoundness risk

- **A body that returns a reference into `self` must keep `self`'s loans on the result.** Canary `f12` (an `__index` body that returns a reference into `self`) goes from unfinished to a precise SEM3139 under the prototype (measured). The refusal becomes named; it is not lost.
- **For Map, returning a fresh value for `m[k]` would let a `&V` outlive the next insert.** The transfer must return the map's storage.

## DEBT-365 and DEBT-368

No listed fence stands on this reason. N-INDEX-CALL reuses the G6-ii loan-discard guard, which holds P-STASH's forms (see `store-through-place.md`).

## Owner decision

None needed. For the copies, see `core-copies.md`.
