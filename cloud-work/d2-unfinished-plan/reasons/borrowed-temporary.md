# borrowed temporary has no proven storage owner

| Measure | Value |
|---|---:|
| Programs carrying it | 4 |
| Programs where it is the only root reason | 0 |
| Rows | 8 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_expr.go:183`.** An explicit `&` or `&mut` of an rvalue, whose storage is no proven place. The missing fact is whether the callee that receives the borrow can keep it past the statement that frees the temporary, including through a task it starts.

## Corpus examples

1. **`vm_arrays/array_field_mut_ref_reborrow.sg:15`**: `add_borrower(&mut entry, &"client-a");`, and line 16, into a user function. The `mir/` twin has the same row at lines 14-15.
2. **`sema/valid/stdlib_hash_api.sg:27`**: `h.begin_record(&"CacheKey", 2:uint);`, and lines 28 and 30, into stdlib hasher methods.

The fourth carrier is `vm_maps/map_get_mut.sg:8`, `let v = m.get_mut(&"x");`, into a core Map method.

## Transfer and size

This is the explicit-borrow form of the lent temporary in `implicit-borrow-temporary.md`, and it has the same answer.

- **Core callee (`map_get_mut.sg`).** N-CORE-TEMP (measured, 76 lines) treats `&<literal>` as a temporary this call cannot keep when the core body's summary never names that slot. `map_get_mut.sg` then loses this row and needs only the Map index packet.
- **User and stdlib callees.** Owner question 3, option A or C.

## Unsoundness risk

The callee may start a task over the formal and outlive the statement. That is R-c in DEBT.md:229, and its only compile-time fence is this row and DEBT-368's.

## DEBT-365 and DEBT-368

R-c, RV2-DEBT-368.

## Owner decision

Yes. See `owner-q3-lent-temporaries.md`.
