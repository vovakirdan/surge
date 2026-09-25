# expression kind 9 needs an origin transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 3 |
| Programs where it is the only root reason | 1 |
| Rows | 3 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

Kind 9 is `ast.ExprMap`.

## Where it is raised

- **`internal/sema/return_origin_expr.go:158`.** The expression catch-all, which has no case for a map literal.

## Corpus examples

1. **`vm_maps/map_literal_order.sg:13`**: `let m = { key("k1") => val("v1", 1), key("k2") => val("v2", 2) };`. This is 1 row, its only root.
2. **`vm_maps/map_index_get.sg:3`**: `let m = { "x" => 10 };`.

The third is `vm_maps/map_get_mut.sg:3`.

## Sound transfer and size

- **N-MAPLIT, S, prototype 43 lines, measured.** Evaluate each key and value in source order. Every key type must be crossing-inert, because the map hashes and owns its keys. The literal's value joins the values' origins, or discards their loans when the elements are free, under the G6 guard.
- **What it frees.** `map_literal_order.sg`, which then runs to its golden `.out` on both backends. It also clears the literal row in `map_index_get.sg` and `map_get_mut.sg`, which still need N-MAP-INDEX.

## Unsoundness risk

Low to medium. A value that carries a loan must keep it on the map. The prototype joins it, or refuses when it cannot discard it.

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed.
