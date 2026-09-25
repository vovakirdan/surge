# index requires its selected container transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 15 |
| Programs where it is the only root reason | 3 |
| Rows | 36 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_index.go:143`.** `returnOriginIndexContainer` does not recognise the indexed type as a canonical container. That happens for a `BytesView`, an `own C`, a Map, or a user type with its own `__index` (read from code). The missing fact is which container transfer the checker's selected operation implies.

## Corpus examples

1. **`vm_strings/strings_basic.sg:40`**: `let b0: uint = bv[0] to uint;`, with `bv` a `BytesView`. This is 1 row, its only root.
2. **`vm_maps/map_composite_value.sg:19`**: `print((m[one].a) to string);`. This is 1 row, its only root.

## Sound transfer and size

- **N-INDEX-VIEW, S, prototype 19 lines, measured.** An index into a `BytesView` whose selected reader is the certified body-less reader (`returnOriginBytesViewReader`) yields a fresh byte. An `own C` target is resolved as `C`, because an owned container holds its elements by value. This frees `strings_basic.sg` and `strings_rope.sg`. Together with other packets it also frees `strings_rope_std.sg`, `stdlib_uuid_api.sg` and the four `vm_hash` programs.
- **What is left after the prototype.**
  - `map_composite_value.sg` needs N-MAP-INDEX (see `index-non-scalar.md`).
  - `mir/imported_magic_methods.sg:31-32` (`bag[1] = 9`, `bag[1] != 9`) needs a user `__index`/`__index_set` pair on an imported type. That extends N-INDEX-CALL to imported, `__index_set` and generic bodies, size M.
  - `self_mut_field_reborrow.sg:8` needs N-STORE.

## Unsoundness risk

Low for the byte read, whose element is a scalar. For `own C`, the element is read out of a container the frame owns, so the usual element rules apply.

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed.
