# projected borrowed payload needs precise origin facts

| Measure | Value |
|---|---:|
| Programs carrying it | 17 |
| Programs where it is the only root reason | 1 |
| Rows | 162 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_expr.go:118`.** A member read `x.f` where `x` is borrowed and `memberBorrowsReferent` refuses the field (`internal/sema/return_origin_member.go:36-38`). That happens when the field's type is a reference, is not reference-free, or is a loan carrier such as an array or a string (read from code). The missing fact is the origin of a field read through a borrowed base when the field can carry loans.

## Corpus examples

1. **`vm_arrays/arrays_index_panic_in_method.sg:14`**: `return self.items[i];`. The receiver `self` is borrowed and `items` is an array. This is 1 row, its only root.
2. **`vm_hash/xxh64_vectors.sg`**, through `stdlib/hash/xxh64.sg:168`: `h.tail.push(value);`.

## Sound transfer and size

- **N-FIELD-BORROW, S, prototype 2 lines, measured.** Drop the loan-carrier exclusion. A loan-carrying field of a borrowed referent is a place, and its value is the referent's storage. The field's own loans are then read only through `containerLoans`, which refuses a base it cannot prove with "container loans lack a proven base" (`internal/sema/return_origin_backing.go:244`).
- **What it frees.** `arrays_index_panic_in_method.sg` alone. Together with other packets it also frees `compare_tag_ref.sg`, `stdlib_uuid_api.sg` and the four `vm_hash` programs. It also clears this reason's rows in the two `array_field_mut_ref_reborrow.sg` programs, `stdlib_hash_api.sg` and `self_mut_field_reborrow.sg`. Each of those four gets the row back when only this packet is switched off (measured).
- **What it does not answer.**
  - `p.data` read through `p: &mut Parser` (`data: byte[]`, `stdlib/json/parser.sg:74`) or through `p: &mut ViewParser` (`data: BytesView`, `:85`), passed on as an implicit borrow. Both keep the row under the prototype (measured). I did not establish why. The likely causes are the `&mut` base or the `BytesView` handle shape (read from code). This is part of the JSON work in PLAN.md, size M.
  - `foos[1].name` and similar in `vm_arrays/arrays_drop_nested.sg`, which also need the deferred non-Copy clone body.

## Unsoundness risk

Canary `f06` returns an array field through a borrowed struct. It loses this row but keeps "container loans lack a proven base" (measured), so the second fence holds. A future change to `containerLoans` must re-run `f06`.

## DEBT-365 and DEBT-368

No listed fence stands on this reason (read from DEBT.md:229).

## Owner decision

None needed.
