# conversion retains its actual expression for origin finalization

| Measure | Value |
|---|---:|
| Programs carrying it | 12 |
| Programs where it is the only root reason | 0 |
| Rows | 107 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_expr.go:136`.** A conversion `x to T` for which the checker recorded a `__to` call ("HIR lowers it as one"), when the result is not reference-free or the cast is not proven by `castProven`. The missing fact is the transfer through the selected `__to` body.

## Corpus examples

1. **`sema/valid/array_helpers.sg:13`**: `let s1: string = a to string;`. The same file has the pattern at lines 14, 15, 24, 25 and 32. Each site also carries "generic use has duplicate or contradictory finalized authority" (`internal/sema/return_origin_generics.go:166`), so two finalized uses name the same site.
2. **`sema/valid/json_method_jsonvalue_param.sg`**, through `stdlib/json/parser.sg:1080`: `let bytes: byte[] = clone(input) to byte[];`.

In the copies, see for example `core_stdlib/array.sg:134`: `let length_s: string = self.__len() to string;`.

## Sound transfer and size

- **N-CONV-GENERIC, M, estimate 100 to 200 lines (read from code; not prototyped).** Evaluate `x to T` with a selected `__to` exactly like a call to that body, substituting the finalized use's summary.
- **The duplicate-authority row must be understood first.** If the two finalized uses at one site are the conversion and an inner call that share its span, the packet must key uses by their operation, not only by site. I did not verify which case it is.
- **What it frees.** `array_helpers.sg`. It is one of several pieces the JSON program needs.

## Unsoundness risk

Medium. A conversion that returns a view of its operand must keep the operand's storage on the result. A test must pin a view-returning `__to` staying refused when it escapes.

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed. For the copies, see `core-copies.md`.
