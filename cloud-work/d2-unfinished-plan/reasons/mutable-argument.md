# mutable argument may replace reference-bearing contents

| Measure | Value |
|---|---:|
| Programs carrying it | 11 |
| Programs where it is the only root reason | 0 |
| Rows | 188 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_calls.go:206`** and **`internal/sema/return_origin_backing_calls.go:157`.** A `&mut` argument whose referent can hold references or loans, when neither a cell transfer nor a backing transfer is proven for it. The analysis then taints the external cells. The missing fact is what the callee does to the contents of that argument.

## Corpus examples

1. **`sema/valid/json_method_jsonvalue_param.sg`**, through `stdlib/json/parser.sg:201`: `return json_error(JSON_ERR_EOF, "incomplete unicode escape", view_parser_offset(p));`, with rows at 68 sites in all. `p` is a `&mut ViewParser` or `&mut Parser` whose `data` field is a `BytesView` or a `byte[]`, so its referent carries a view or a loan (`stdlib/json/parser.sg:6-16`).
2. **The copies:** `core_stdlib/array.sg:10`, `rt_array_push(a, value);`.

## Sound transfer and size

**N-MUT-STRUCT-CELLS, M to L (estimate; not prototyped).** Give a `&mut` struct formal per-field post-state cells from the callee's body summary. `view_parser_offset` and the other helpers write only the scalar cursor field, so their summaries would show the reference field unchanged. This is one piece of the JSON program, which also needs owner question 3.

## Unsoundness risk

High if done loosely. A `&mut` struct that holds a view is exactly where a callee could store a shorter-lived one. That is the `*out =` form of P-STASH in DEBT.md:229. Per-field post-state must be exact, and it must not be a whole-struct "unchanged".

## DEBT-365 and DEBT-368

This is next to P-STASH's `*out =` form (DEBT.md:229, "Found by P1u-TC2 and not the task check's"). It is not named as that form's fence.

## Owner decision

None needed. The JSON program also depends on `owner-q3-lent-temporaries.md`. For the copies, see `core-copies.md`.
