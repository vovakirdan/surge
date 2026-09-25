# Clusters of the unfinished programs

Every number here was measured. The input is `census.json`, the user form over all files, and the tables come from `tools/clusters.py` and `tools/report.py`. Reason names are shortened to the slugs of `reasons/`. `census.md` maps each slug to its full reason text.

## Before the kind-27 drop

108 programs fall into 50 distinct root-reason sets. The largest is `on-expression-kind-27` alone, with 24 programs. The in-flight packets TC-XB and N-TASK-27S are assumed to remove every kind-27 row. The 24 programs whose only root is kind 27 then become ok, and none of them carries any other root reason. The other 84 programs carry no kind-27 row at all, so the drop changes none of their root sets.

## After the kind-27 drop

84 programs fall into 49 root-reason sets, and 35 of those sets hold a single program. Only two sets are large:

- **One cluster of 9 plus 1 is the `core_stdlib` copies.** Each copy carries 14 or 16 root reasons, and they share a single cause, described in `reasons/core-copies.md`.
- **A cluster of 6 carries only `opaque-result-classification`.** The greedy cover below shows it hides three different missing facts.

Unfinished (user form, all files): 108; freed by the kind-27 drop: 24; left: 84.

### Clusters after the kind-27 drop

| # | size | root-reason set | programs |
|---:|---:|---|---|
| 1 | 9 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration | core_stdlib/array.sg, core_stdlib/base.sg, core_stdlib/entrypoint.sg, core_stdlib/format.sg, core_stdlib/intrinsics.sg, core_stdlib/map.sg, core_stdlib/option.sg, core_stdlib/result.sg, core_stdlib/sync.sg |
| 2 | 6 | opaque-result-classification | sema/invalid/directives/time_not_directive_module/main.sg, sema/valid/directives/stdlib_benchmark_module/main.sg, sema/valid/directives/stdlib_time_directive/main.sg, sema/valid/directives/stdlib_time_import/main.sg, sema/valid/fn_type_async.sg, sema/valid/stdlib_entropy_api.sg |
| 3 | 5 | generic-opaque-use; opaque-result-classification | sema/valid/concurrency/task_clone_borrow_joined_then_returned.sg, sema/valid/concurrency/task_container_suspend_safe.sg, sema/valid/ownership/for_in_reads_then_pop_drains.sg, sema/valid/task_container_drain_with_nested_loop_break.sg, vm_async_suite/t14_loop_join.sg |
| 4 | 4 | compare-pattern | hir/compare.sg, mir/erring_option_nested_tag.sg, spec_audit/s03_compare.sg, vm_compare/compare_enum_variants.sg |
| 5 | 3 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees; index-selected-container; projected-borrowed-payload | vm_hash/stable64_frames.sg, vm_hash/stable64_primitives.sg, vm_hash/xxh64_vectors.sg |
| 6 | 3 | tuple-index-kind-12 | hir/tuples.sg, sema/valid/tuple_access.sg, vm_async_suite/t15_fairness_round_robin.sg |
| 7 | 3 | generic-tag-use | hir/option_erring.sg, mono/option_implicit_wrap.sg, sema/valid/return_type_and_sugar.sg |
| 8 | 3 | index-non-scalar | hir/indexing_ranges.sg, sema/valid/range_literals.sg, vm_maps/map_growth_boundary.sg |
| 9 | 3 | index-selected-container | vm_maps/map_composite_value.sg, vm_strings/strings_basic.sg, vm_strings/strings_rope.sg |
| 10 | 2 | task-block-capture | vm_async_suite/t22_select_wait_recv.sg, vm_async_suite/t23_select_wait_timer.sg |
| 11 | 2 | borrowed-temporary; projected-borrowed-payload | mir/array_field_mut_ref_reborrow.sg, vm_arrays/array_field_mut_ref_reborrow.sg |
| 12 | 2 | destructuring | sema/valid/tuple_destructure.sg, sema/valid/tuple_destructure_call.sg |
| 13 | 2 | generic-argument-disagrees | sema/valid/array_view_facts_a_resize_rule_withdraws.sg, vm_compare/for_in_compare_reads_heap_free_union.sg |
| 14 | 2 | selected-callable-authority | sema/valid/import_all.sg, sema/valid/module_multitest/main.sg |
| 15 | 1 | task-block-capture; default result is not proven Defaultable; generic use lacks its original typed operation | sema/valid/concurrency/task_created_in_current_scope.sg |
| 16 | 1 | an `async` or `blocking` block whose value can hold a reference, a storage loan or a task needs its payload origin | sema/valid/ret_async_body.sg |
| 17 | 1 | an `on` crossing capture that can hold a reference, a storage loan or a task needs its capture origin; index-selected-container | crossing/block02/valid/on_positive_dynamic_array_capture.sg |
| 18 | 1 | an `on` crossing reply that can hold a reference, a storage loan or a task needs its reply origin; generic result contains an unproved source | crossing/integration/valid/_integration_generic_crossing_sites.sg |
| 19 | 1 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-opaque-use; generic-use-typed-operation; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration | core_stdlib/string.sg |
| 20 | 1 | binary-callable-contract; projected-borrowed-payload | sema/valid/fixed_array_view_operator_stays_in_frame.sg |
| 21 | 1 | borrowed-temporary; call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees; index-selected-container; projected-borrowed-payload | sema/valid/stdlib_hash_api.sg |
| 22 | 1 | borrowed-temporary; map-literal-kind-9; index-non-scalar | vm_maps/map_get_mut.sg |
| 23 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority | sema/valid/clone_semantics/clone_copy_type.sg |
| 24 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority; compare-pattern; projected-borrowed-payload | sema/valid/compare_tag_ref.sg |
| 25 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority; container loans lack a proven base; index-selected-container; projected-borrowed-payload; store-through-place | mir/imported_magic_methods.sg |
| 26 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority; conversion-retains-expression; generic-index-finalized-use; implicit-borrow-temporary; index-non-scalar; index-selected-container; module member value needs its selected free-function authority; mutable-argument; projected-borrowed-payload; reference loaded through another reference needs content provenance; tag constructor lacks its original declaration target | sema/valid/json_method_jsonvalue_param.sg |
| 27 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-index-finalized-use; generic-argument-disagrees; index-selected-container; projected-borrowed-payload | vm_hash/hash64_basic.sg |
| 28 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-index-finalized-use; index-selected-container; opaque-result-classification; projected-borrowed-payload | sema/valid/stdlib_uuid_api.sg |
| 29 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-index-finalized-use; opaque-result-classification | sema/valid/stdlib_random_api.sg |
| 30 | 1 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees | vm_compare/counted_payload_clone_and_borrow.sg |
| 31 | 1 | compare has an unproved unmatched continuation | mir/compare_guard_await_release.sg |
| 32 | 1 | compare has an unproved unmatched continuation; compare-pattern | vm_compare/compare_patterns.sg |
| 33 | 1 | container loans lack a proven base; projected-borrowed-payload; store-through-place | mir/magic_methods_repro.sg |
| 34 | 1 | conversion-retains-expression; generic use has duplicate or contradictory finalized authority | sema/valid/array_helpers.sg |
| 35 | 1 | deferred clone needs its selected non-Copy body and effect transfer; generic result contains an unproved source; projected-borrowed-payload; store-through-place | vm_arrays/arrays_drop_nested.sg |
| 36 | 1 | deferred method may change reference-bearing or callable contents; index-selected-container | abi/abi_string_bytesview.sg |
| 37 | 1 | destructuring; tuple-index-kind-12 | vm_tuples/tuple_literals.sg |
| 38 | 1 | map-literal-kind-9 | vm_maps/map_literal_order.sg |
| 39 | 1 | map-literal-kind-9; index-non-scalar | vm_maps/map_index_get.sg |
| 40 | 1 | generic-argument-disagrees; generic-tag-use | sema/valid/recursive_handles.sg |
| 41 | 1 | generic-use-typed-operation; implicit-borrow-temporary | vm_strings/strings_std.sg |
| 42 | 1 | generic-use-typed-operation; implicit-borrow-temporary; index-selected-container | vm_strings/strings_rope_std.sg |
| 43 | 1 | index-non-scalar; index store requires a scalar index into its canonical container; store-through-place | vm_maps/map_index_set.sg |
| 44 | 1 | index-selected-container; projected-borrowed-payload; store-through-place | sema/ownership_and_references/self_mut_field_reborrow.sg |
| 45 | 1 | opaque-call-effects | sema/valid/clone_semantics/task_clone_uninstantiated_generic.sg |
| 46 | 1 | opaque-result-classification; selected-callable-authority | stdlib/time_duration_conversions.sg |
| 47 | 1 | projected-borrowed-payload | vm_arrays/arrays_index_panic_in_method.sg |
| 48 | 1 | projected-borrowed-payload; store-through-place | sema/ownership_and_references/self_mut_field_index_set.sg |
| 49 | 1 | storage loan would be discarded by a payload-free value | vm_intrinsics/array_range_panics.sg |

### Greedy set cover after the kind-27 drop

| step | root reason answered | carried by | frees now | freed so far | programs freed at this step |
|---:|---|---:|---:|---:|---|
| 1 | opaque-result-classification | 24 | 6 | 6 | sema/invalid/directives/time_not_directive_module/main.sg, sema/valid/directives/stdlib_benchmark_module/main.sg, sema/valid/directives/stdlib_time_directive/main.sg, sema/valid/directives/stdlib_time_import/main.sg, sema/valid/fn_type_async.sg, sema/valid/stdlib_entropy_api.sg |
| 2 | generic-opaque-use | 6 | 5 | 11 | sema/valid/concurrency/task_clone_borrow_joined_then_returned.sg, sema/valid/concurrency/task_container_suspend_safe.sg, sema/valid/ownership/for_in_reads_then_pop_drains.sg, sema/valid/task_container_drain_with_nested_loop_break.sg, vm_async_suite/t14_loop_join.sg |
| 3 | compare-pattern | 6 | 4 | 15 | hir/compare.sg, mir/erring_option_nested_tag.sg, spec_audit/s03_compare.sg, vm_compare/compare_enum_variants.sg |
| 4 | index-non-scalar | 17 | 3 | 18 | hir/indexing_ranges.sg, sema/valid/range_literals.sg, vm_maps/map_growth_boundary.sg |
| 5 | index-selected-container | 15 | 3 | 21 | vm_maps/map_composite_value.sg, vm_strings/strings_basic.sg, vm_strings/strings_rope.sg |
| 6 | selected-callable-authority | 13 | 3 | 24 | sema/valid/import_all.sg, sema/valid/module_multitest/main.sg, stdlib/time_duration_conversions.sg |
| 7 | tuple-index-kind-12 | 4 | 3 | 27 | hir/tuples.sg, sema/valid/tuple_access.sg, vm_async_suite/t15_fairness_round_robin.sg |
| 8 | generic-tag-use | 4 | 3 | 30 | hir/option_erring.sg, mono/option_implicit_wrap.sg, sema/valid/return_type_and_sugar.sg |
| 9 | generic-argument-disagrees | 9 | 3 | 33 | sema/valid/array_view_facts_a_resize_rule_withdraws.sg, sema/valid/recursive_handles.sg, vm_compare/for_in_compare_reads_heap_free_union.sg |
| 10 | destructuring | 3 | 3 | 36 | sema/valid/tuple_destructure.sg, sema/valid/tuple_destructure_call.sg, vm_tuples/tuple_literals.sg |
| 11 | map-literal-kind-9 | 3 | 2 | 38 | vm_maps/map_index_get.sg, vm_maps/map_literal_order.sg |
| 12 | task-block-capture | 3 | 2 | 40 | vm_async_suite/t22_select_wait_recv.sg, vm_async_suite/t23_select_wait_timer.sg |
| 13 | compare has an unproved unmatched continuation | 2 | 2 | 42 | mir/compare_guard_await_release.sg, vm_compare/compare_patterns.sg |
| 14 | projected-borrowed-payload | 17 | 1 | 43 | vm_arrays/arrays_index_panic_in_method.sg |
| 15 | borrowed-temporary | 4 | 3 | 46 | mir/array_field_mut_ref_reborrow.sg, vm_arrays/array_field_mut_ref_reborrow.sg, vm_maps/map_get_mut.sg |
| 16 | store-through-place | 6 | 2 | 48 | sema/ownership_and_references/self_mut_field_index_set.sg, sema/ownership_and_references/self_mut_field_reborrow.sg |
| 17 | binary-callable-contract | 11 | 1 | 49 | sema/valid/fixed_array_view_operator_stays_in_frame.sg |
| 18 | opaque-call-effects | 11 | 1 | 50 | sema/valid/clone_semantics/task_clone_uninstantiated_generic.sg |
| 19 | container loans lack a proven base | 2 | 1 | 51 | mir/magic_methods_repro.sg |
| 20 | storage loan would be discarded by a payload-free value | 1 | 1 | 52 | vm_intrinsics/array_range_panics.sg |
| 21 | index store requires a scalar index into its canonical container | 1 | 1 | 53 | vm_maps/map_index_set.sg |
| 22 | deferred method may change reference-bearing or callable contents | 1 | 1 | 54 | abi/abi_string_bytesview.sg |
| 23 | an `on` crossing capture that can hold a reference, a storage loan or a task needs its capture origin | 1 | 1 | 55 | crossing/block02/valid/on_positive_dynamic_array_capture.sg |
| 24 | an `async` or `blocking` block whose value can hold a reference, a storage loan or a task needs its payload origin | 1 | 1 | 56 | sema/valid/ret_async_body.sg |
| 25 | generic-index-finalized-use | 14 | 0 | 56 |  |
| 26 | callable-identifier-facts | 12 | 0 | 56 |  |
| 27 | conversion-retains-expression | 12 | 0 | 56 |  |
| 28 | generic use has duplicate or contradictory finalized authority | 1 | 1 | 57 | sema/valid/array_helpers.sg |
| 29 | callable-value-authority | 12 | 0 | 57 |  |
| 30 | call-exact-body | 12 | 11 | 68 | mir/imported_magic_methods.sg, sema/valid/clone_semantics/clone_copy_type.sg, sema/valid/compare_tag_ref.sg, sema/valid/stdlib_hash_api.sg, sema/valid/stdlib_random_api.sg, sema/valid/stdlib_uuid_api.sg, vm_compare/counted_payload_clone_and_borrow.sg, vm_hash/hash64_basic.sg, vm_hash/stable64_frames.sg, vm_hash/stable64_primitives.sg, vm_hash/xxh64_vectors.sg |
| 31 | mutable-argument | 11 | 0 | 68 |  |
| 32 | captured-binding | 10 | 0 | 68 |  |
| 33 | deferred-clone-live-referent | 10 | 0 | 68 |  |
| 34 | parameter-concrete-type | 10 | 0 | 68 |  |
| 35 | tag-constructor-owning-declaration | 10 | 0 | 68 |  |
| 36 | generic-use-original-declarations | 10 | 0 | 68 |  |
| 37 | range-literal-certificate | 10 | 9 | 77 | core_stdlib/array.sg, core_stdlib/base.sg, core_stdlib/entrypoint.sg, core_stdlib/format.sg, core_stdlib/intrinsics.sg, core_stdlib/map.sg, core_stdlib/option.sg, core_stdlib/result.sg, core_stdlib/sync.sg |
| 38 | generic-use-typed-operation | 3 | 1 | 78 | core_stdlib/string.sg |
| 39 | implicit-borrow-temporary | 3 | 2 | 80 | vm_strings/strings_rope_std.sg, vm_strings/strings_std.sg |
| 40 | generic result contains an unproved source | 2 | 0 | 80 |  |
| 41 | deferred clone needs its selected non-Copy body and effect transfer | 1 | 1 | 81 | vm_arrays/arrays_drop_nested.sg |
| 42 | an `on` crossing reply that can hold a reference, a storage loan or a task needs its reply origin | 1 | 1 | 82 | crossing/integration/valid/_integration_generic_crossing_sites.sg |
| 43 | default result is not proven Defaultable | 1 | 0 | 82 |  |
| 44 | generic use lacks its original typed operation | 1 | 1 | 83 | sema/valid/concurrency/task_created_in_current_scope.sg |
| 45 | tag constructor lacks its original declaration target | 1 | 0 | 83 |  |
| 46 | module member value needs its selected free-function authority | 1 | 0 | 83 |  |
| 47 | reference loaded through another reference needs content provenance | 1 | 1 | 84 | sema/valid/json_method_jsonvalue_param.sg |

## Why the greedy cover is not the plan

The greedy cover treats a reason text as one thing to answer. The census and the prototype show otherwise. One reason text is raised at several sites for different missing facts, and one fact can answer rows of several reasons.

- **Step 1 frees 6 programs, but they need three unrelated facts.** Four need a certificate for the `stdlib/time` `Duration` word. One needs the `rt_entropy_bytes` fresh-buffer certificate. One, `fn_type_async.sg`, needs a ruling on Task values leaving a frame, which is residual R-i of RV2-DEBT-365.
- **Step 2 frees 5 programs by answering `generic-opaque-use`, but the same transfer answers their `opaque-result-classification` row.** Both rows stand on `default::<T>()` with `T` a core runtime handle, at `core/option.sg:12` and `core/intrinsics.sg:777`. One fact, "a handle's default is its null sentinel", answers both.
- **`call-exact-body`, `callable-identifier-facts` and `callable-value-authority` always travel together.** All three stand on the same 51 `clone(...)` rows in the same 12 programs. 50 of those rows clone a Copy value, a byte or an integer, which the language defines as a bitwise copy with no `__clone` lookup (`docs/LANGUAGE.md:1516`). That is one missing fact seen three ways. The only exception is `clone(original)` at `mir/imported_magic_methods.sg:13`, which clones an imported type through its own `__clone` body.
- **The first 13 steps free 36 programs and the rest free 48, but 10 of those 48 are the `core_stdlib` copies.** Only a decision about the copies frees them, not a transfer.

For that reason PLAN.md orders packets by what the prototype measured. It enables one packet at a time on top of the previous ones and re-diagnoses the 84 programs.

## Measured landing order

This comes from `prototype_measurements.json`. Each step enables one more packet in a scratch prototype that was never committed, then diagnoses all 84 programs again. No step lost a program that an earlier step freed, and no program moved to `diagnostics`.

| step | packet | prototype lines | ok after step | programs that become ok |
|---:|---|---:|---:|---|
| 1 | N-TUPLE | 80 | 6 | hir/tuples.sg, sema/valid/tuple_access.sg, sema/valid/tuple_destructure.sg, sema/valid/tuple_destructure_call.sg, vm_async_suite/t15_fairness_round_robin.sg, vm_tuples/tuple_literals.sg |
| 2 | N-PATTERN | 33 | 11 | hir/compare.sg, mir/erring_option_nested_tag.sg, spec_audit/s03_compare.sg, vm_compare/compare_enum_variants.sg, vm_compare/compare_patterns.sg |
| 3 | N-DEFHANDLE | 6 | 16 | sema/valid/concurrency/task_clone_borrow_joined_then_returned.sg, sema/valid/concurrency/task_container_suspend_safe.sg, sema/valid/ownership/for_in_reads_then_pop_drains.sg, sema/valid/task_container_drain_with_nested_loop_break.sg, vm_async_suite/t14_loop_join.sg |
| 4 | N-STDLIB-TIME | 3 | 20 | sema/invalid/directives/time_not_directive_module/main.sg, sema/valid/directives/stdlib_benchmark_module/main.sg, sema/valid/directives/stdlib_time_directive/main.sg, sema/valid/directives/stdlib_time_import/main.sg |
| 5 | N-TAGCONV | 51 | 23 | hir/option_erring.sg, mono/option_implicit_wrap.sg, sema/valid/return_type_and_sugar.sg |
| 6 | N-CLONE-COPY | 32 | 24 | sema/valid/clone_semantics/clone_copy_type.sg |
| 7 | N-INDEX-VIEW | 19 | 26 | vm_strings/strings_basic.sg, vm_strings/strings_rope.sg |
| 8 | N-FIELD-BORROW | 2 | 28 | sema/valid/compare_tag_ref.sg, vm_arrays/arrays_index_panic_in_method.sg |
| 9 | N-ALIAS-ARG | 20 | 32 | sema/valid/recursive_handles.sg, vm_hash/stable64_frames.sg, vm_hash/stable64_primitives.sg, vm_hash/xxh64_vectors.sg |
| 10 | N-STDLIB-ENTROPY | 13 | 33 | sema/valid/stdlib_entropy_api.sg |
| 11 | N-DEAD-GENERIC-USE | 16 | 36 | sema/valid/stdlib_random_api.sg, sema/valid/stdlib_uuid_api.sg, vm_hash/hash64_basic.sg |
| 12 | N-CHAN-CAPTURE | 23 | 38 | vm_async_suite/t22_select_wait_recv.sg, vm_async_suite/t23_select_wait_timer.sg |
| 13 | N-IMPORT-IDENTITY | 16 | 41 | sema/valid/import_all.sg, sema/valid/module_multitest/main.sg, stdlib/time_duration_conversions.sg |
| 14 | N-TAGMEMBER-ARG | 20 | 43 | vm_compare/counted_payload_clone_and_borrow.sg, vm_compare/for_in_compare_reads_heap_free_union.sg |
| 15 | N-MAPLIT | 43 | 44 | vm_maps/map_literal_order.sg |
| 16 | N-INDEX-CALL | 65 | 46 | hir/indexing_ranges.sg, sema/valid/range_literals.sg |
| 17 | N-CONCAT-USE | 35 | 46 | none alone (enables the next step) |
| 18 | N-CORE-TEMP | 76 | 48 | vm_strings/strings_rope_std.sg, vm_strings/strings_std.sg |

`necessary_packets` in `prototype_measurements.json` records a leave-one-out run. With all 18 packets on and one packet switched off, it lists which freed programs go back to unfinished. Examples:

- **The four `vm_hash` programs** each need N-ALIAS-ARG, N-CLONE-COPY, N-INDEX-VIEW and N-FIELD-BORROW. `hash64_basic.sg` also needs N-DEAD-GENERIC-USE.
- **`strings_std.sg` and `strings_rope_std.sg`** each need N-CONCAT-USE and N-CORE-TEMP. That is why step 17 frees nothing alone.
- **`compare_tag_ref.sg`** needs N-PATTERN, N-CLONE-COPY and N-FIELD-BORROW.

## What the prototype leaves

36 programs stay unfinished. `left_after_all_packets` in `prototype_measurements.json` lists them with their remaining root reasons.

- **10** are the `core_stdlib` copies.
- **26** are single programs, each discussed in PLAN.md.
