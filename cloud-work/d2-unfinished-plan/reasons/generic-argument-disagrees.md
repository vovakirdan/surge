# generic original call argument disagrees with its substituted source signature

| Measure | Value |
|---|---:|
| Programs carrying it | 9 |
| Programs where it is the only root reason | 2 |
| Rows | 25 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_generic_signature.go:189`** (`originalArgumentType`). An actual argument's type does not match the formal of the original generic signature after substitution.

There are three different missing facts behind it:

| Form | Example | Answered by |
|---|---|---|
| A nongeneric alias against its target | `sema/valid/recursive_handles.sg:33`, `let first: NodeId = len(nodes);` | N-ALIAS-ARG, measured, 20 lines |
| A union member against its union formal | `vm_compare/for_in_compare_reads_heap_free_union.sg:47`, `opts.push(Some(3:int64));` | N-TAGMEMBER-ARG, measured, 20 lines |
| An element place as a `&mut` receiver | `sema/valid/array_view_facts_a_resize_rule_withdraws.sg:27`, `xs[1].push(9);` | N-MUT-PLACE-RECV, not prototyped |

## Corpus examples

1. **`vm_compare/for_in_compare_reads_heap_free_union.sg:47`**: `opts.push(Some(3:int64));`, plus lines 48, 49 and 54. This is its only root reason.
2. **`sema/valid/array_view_facts_a_resize_rule_withdraws.sg:27`**: `xs[1].push(9);`. This is 1 row, its only root.

## Sound transfers and size

- **N-ALIAS-ARG, S, measured.** A nongeneric alias denotes its target when matching.
- **N-TAGMEMBER-ARG, S, measured.** A union member actual matches its union formal. The value keeps its payload's origins.
- **What those two free.** `for_in_compare_reads_heap_free_union.sg` alone. With other packets they also free `recursive_handles.sg`, `counted_payload_clone_and_borrow.sg` and the four `vm_hash` programs.
- **N-MUT-PLACE-RECV, S to M, estimate (not prototyped).** An element place used as a `&mut` receiver.

## Unsoundness risk and fences

DEBT.md:229 (W4-G1 paragraph): "The same reason text on calls that are not an `.await()` (`xs[1].push(9)`, `opts.push(Some(...))`, `len(nodes)`: 15 rows in 6 programs of the morning census of `b229b1f8`) is not this change's and stays, and with it P-STASH's and P-VIEW2's unfinished verdicts." P-STASH, a window into a fixed array pushed into a `&mut` container parameter, faults `panic VM3301: storage: stale reference` on D1. So these rows are part of what holds P-STASH and P-VIEW2 today.

Measured on my own reconstructions of those forms:

- **Canaries `f16` and `f17`** push `Some(window)` into a `&mut` parameter or into a local array. They lose this row under N-TAGMEMBER-ARG but keep "storage loan would be discarded by a payload-free value", the G6 guard, so they stay unfinished.
- **Canaries `f13`, `f15` and `f18`** push a bare window. They carry only the G6 row, both on the base and under the prototype.

The coordinator's own P-STASH and P-VIEW2 probes are not in the repository. N-TAGMEMBER-ARG must re-measure them before it lands.

N-MUT-PLACE-RECV is the push-into-an-element shape itself. It should wait until P-STASH has a fence of its own.

## Owner decision

Not a design question. There are two sequencing conditions for the coordinator: re-measure P-STASH and P-VIEW2 under N-TAGMEMBER-ARG, and land N-MUT-PLACE-RECV only after P-STASH has its own fence.
