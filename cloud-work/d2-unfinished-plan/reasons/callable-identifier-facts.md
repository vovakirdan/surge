# callable identifier lacks concrete source facts

| Measure | Value |
|---|---:|
| Programs carrying it | 12 |
| Programs where it is the only root reason | 0 |
| Rows | 51 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

This reason is one of three that always stand together on the same 51 `clone(...)` rows in 12 programs.

- **`internal/sema/return_origin_calls.go:103`**: "call needs an exact body, canonical core contract, or opaque declaration promise".
- **`internal/sema/return_origin_callable.go:87`**: "callable identifier lacks concrete source facts". The identifier `clone` is itself evaluated.
- **`internal/sema/return_origin_callable.go:59`**: "callable value needs its concrete original type and alias authority".

For a Copy argument the checker records no clone selection, so `clone` resolves to the builtin generic function with nothing behind it (read from code). 50 of the 51 rows clone a byte or an integer. The language defines that as a bitwise copy with no `__clone` lookup (`docs/LANGUAGE.md:1516`). The missing fact is that a Copy `clone` is a copy.

## Corpus examples

1. **`sema/valid/clone_semantics/clone_copy_type.sg:4`**: `let y: int32 = clone(&x);  // ok, just copy`. This program carries only the three call-family reasons.
2. **`sema/valid/compare_tag_ref.sg:13`**: `VInt(x) => clone(x);`.

## Sound transfer and size

- **N-CLONE-COPY, S, prototype about 32 lines, measured.** The pattern is `clone(x)`: one positional argument, the target is the builtin `clone`, the result type is Copy, and the call has no entry in `CloneSymbols`. It is treated as a copy: the argument is evaluated, and the result is fresh when it is reference-free and not a loan carrier. Otherwise the refusal is named: "copy clone of a reference-bearing value needs its referent's contents".
- **What it frees.** `clone_copy_type.sg` alone. With other packets it also frees `compare_tag_ref.sg`, `counted_payload_clone_and_borrow.sg`, `stdlib_random_api.sg`, `stdlib_uuid_api.sg` and the four `vm_hash` programs.
- **One carrier is left:** `mir/imported_magic_methods.sg:13`, `let cloned: repro.Box = clone(original);`. That clones an imported non-Copy type through its own `__clone` body and belongs to the magic-methods work in PLAN.md.

## Unsoundness risk

- **The no-selection condition is required.** Without it the prototype accepted a forged selection: row `copy_result_control` of `TestAnalyzeSelectedDirectCloneOrigins` lost its refusal. With it that row stays refused.
- **Only `copy_number_control` flips.** That row documents today's refusal of a Copy clone, and the packet must turn it into a cleared row (measured).

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed.
