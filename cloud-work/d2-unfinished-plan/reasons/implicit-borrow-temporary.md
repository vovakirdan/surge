# implicit borrow lacks an admitted borrow for this expression

| Measure | Value |
|---|---:|
| Programs carrying it | 3 |
| Programs where it is the only root reason | 0 |
| Rows | 20 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_calls.go:355`.** A string temporary passed to a `&string` formal, with no borrow record, where the call might keep it. The admitted case is at `:352-354`: a statement temporary under a synchronous core declaration whose signature can keep nothing. The missing fact is that the callee keeps nothing and reaches no task through that formal.

## Corpus examples

1. **`vm_strings/strings_std.sg:46`**: `let parts = parts_src.split(",");`, plus `:51` `chars_src.split("")`. The callee is core, but its `string[]` result is a loan carrier by signature.
2. **`sema/valid/json_method_jsonvalue_param.sg`**, through `stdlib/json/parser.sg:781`: `if view_consume_literal(p, "null") {`, a stdlib callee. There are 17 sites in `parser.sg` and `stringify.sg`.

The third is `vm_strings/strings_rope_std.sg:36`.

## Transfer and size

- **Core callees:** N-CORE-TEMP (measured, 76 lines). It flips two pinned rows. This is owner question 3, option B.
- **User and stdlib callees:** owner question 3, option A (wait for the task check's per-formal fact) or option C (rewrite the call sites).

## Unsoundness risk

A callee that starts a task over its `&string` formal shows nothing in its signature (DEBT.md:232). The task check cannot pin a temporary, because it has no place. This row is the only fence of R-c.

## DEBT-365 and DEBT-368

R-c, and RV2-DEBT-368 itself.

## Owner decision

Yes. See `owner-q3-lent-temporaries.md`.
