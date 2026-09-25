# generic index lacks its finalized concrete use

| Measure | Value |
|---|---:|
| Programs carrying it | 14 |
| Programs where it is the only root reason | 0 |
| Rows | 89 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_index_primitive.go:104`** (no instantiation closure) and **`:117`** (no finalized `ConcreteInstantiationUse` at the site). The missing fact is which instance an element read in generic or stdlib code runs as.
- In every non-core program the caller is a nongeneric function that monomorphisation never emits for that program, so no finalized use exists (read from code; the prototype's condition is exactly "not in `LiveCallables`", and it frees them).

## Corpus examples

1. **`sema/valid/stdlib_random_api.sg`**, through `stdlib/random/random.sg:40`: `let b0: uint64 = clone(data[0]) to uint64;`.
2. **`vm_hash/hash64_basic.sg`**, through `stdlib/hash/xxh64.sg:59`: `return clone(bytes[index]) to uint64;`.

## Sound transfer and size

- **N-DEAD-GENERIC-USE, S, prototype 16 lines, measured.** For a nongeneric caller that is not live, take the unique original instantiation root at the site. That root must match the witness site, the caller and the template (`InstantiationGraph.Roots()`), and it is then checked like a finalized use. Two roots give a named refusal, "generic index has ambiguous original roots".
- **What it frees.** Together with other packets: `stdlib_random_api.sg`, `stdlib_uuid_api.sg` and `hash64_basic.sg`. It also clears this reason's rows in `json_method_jsonvalue_param.sg`.

## Unsoundness risk

Low. The code is never emitted, and the original root names exactly the instance the source asks for. The risk is a caller that becomes live later with a different instance. That instance would then have its own finalized use, and the prototype path would no longer apply.

Two driver leaves change: `leak_formal_inner` and `pop_inner_rt` of `TestAnalyzeArrayPopLoanFormals`. Both return a view of a local through a nested array, and they stay refused under the prototype. The refusal is now "cursor element that can hold storage loans needs its backing loan transfer" instead of the pinned "borrowed temporary" and "generic index" pair (measured). The packet must re-pin them.

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed. For the copies, see `core-copies.md`.
