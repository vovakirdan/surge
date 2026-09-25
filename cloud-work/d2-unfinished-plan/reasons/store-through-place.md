# store through a place needs reference-content transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 6 |
| Programs where it is the only root reason | 0 |
| Rows | 9 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_place_store.go:27`.** An assignment into a place misses the fast path. The fast path needs both sides reference-free with no conversion. The analysis then taints the external cells.
- I did not establish why each site misses it. `self.cells[r][c] = v` in `self_mut_field_index_set.sg` stores an `int` into an `int[2][2]` field. The likely causes are the field of a `&mut self` or the fixed-array place (read from code).

## Corpus examples

1. **`sema/ownership_and_references/self_mut_field_index_set.sg:7`**: `self.cells[r][c] = v;`. Under the prototype this is its only root.
2. **`vm_maps/map_index_set.sg:4`**: `m["x"] = 10;`, through `Map.__index_set` (`core/map.sg:50-53`).

Others:

- `mir/magic_methods_repro.sg:35`: `self.values[index] = value;`
- `sema/ownership_and_references/self_mut_field_reborrow.sg:8`: `cells[r][c] = v;`
- `vm_arrays/arrays_drop_nested.sg:114`: `bar_view[0].nums[1] = 99;`
- `mir/imported_magic_methods.sg`

## Sound transfer and size

**N-STORE, M to L, estimate 200 to 400 lines (not prototyped).** A store into an element or field place of a referent moves the stored value's loans into that referent's storage, as a backing slot. Payload-free sinks keep the G6 loan-discard guard. A store through a selected `__index_set` goes through that body or through a certificate for `rt_map_insert`. Together with the Map and magic-method packets it frees `self_mut_field_index_set.sg`, `self_mut_field_reborrow.sg`, `magic_methods_repro.sg`, `imported_magic_methods.sg` and `map_index_set.sg`, and part of `arrays_drop_nested.sg`.

## Unsoundness risk

High. P-STASH's forms are stores: a window "pushed into a `&mut` container parameter, written through `*out =`" (DEBT.md:229, "Found by P1u-TC2 and not the task check's"). On D1, P-STASH faults with `panic VM3301: storage: stale reference`. A store transfer that moves a loan into caller-owned storage without checking that the loan outlives it is exactly that fault.

- The packet must keep G6.
- It must add rows for `*out = window` and for a push of a window into a `&mut` parameter.
- It must re-measure P-STASH and P-VIEW2, together with canaries `f13`, `f15` and `f18`, which today stop at G6 (measured).

## DEBT-365 and DEBT-368

P-STASH and P-VIEW2 (DEBT.md:229).

## Owner decision

Not a design question. This is a sequencing condition: land it only with P-STASH's own fence in place.
