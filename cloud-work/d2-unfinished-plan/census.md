# D2 unfinished census at 7131fb2

Every claim on this page was measured by running the compiler unless it says "(read from code)".

## What was run

- **Compiler.** `go build -o /tmp/surge ./cmd/surge` at `7131fb2e3d6e` on `validation/step7-d2-on-d1`. The remote branch head was still `7131fb2` on 2026-09-25.
- **Programs.** Every `.sg` file under `testdata/golden`, 1079 files, listed with `find testdata/golden -type f -name '*.sg' | LC_ALL=C sort`.
- **Two command forms**, each with a 120 s per-file timeout, 8 files in parallel (`tools/run_one.sh`, `tools/run_census.sh`). No file timed out, and every exit status was 0 or 1.
  - The **user form** is the command the brief names: `SURGE_STDLIB=$PWD /tmp/surge diag <file>`.
  - The **harness form** is what `scripts/golden_update.sh:185` runs: `surge diag --format short --directives=<off|collect> <file>`, with `collect` for paths containing `/directives/` (`scripts/golden_update.sh:179`).
- **Multi-file packages** are handled the way the harness handles them. Each `.sg` file is its own entry point (`scripts/golden_update.sh:258-268`), and the compiler resolves the module directory from it. Files whose name starts with `_` are helper modules the harness skips. They are still diagnosed and counted under "all files".
- **Harness scope** is the set the harness diagnoses. It leaves out `spec_audit/`, `crossing/crosses_deferred/` and underscore files, which is 79 files.
- **Classes.** `ok` means exit 0. `unfinished` means the output carries `return-origin analysis unfinished: [...]` (`internal/driver/return_origin_finalization.go:30-37`). `diagnostics` means exit 1 with at least one ERROR code. `other` is anything else.
- **Derived reasons.** Three reasons only carry forward a source raised somewhere else, so they are never counted as root reasons: "function result contains an unproved source", "callee returned an unproved source" and "outgoing reference has unresolved or captured provenance".
- **Row locations.** Each row's span is a byte range in its `SourceKey` file. `census.json` turns that into line and column.
- **Determinism.** Two independent full runs gave the same class and the same root-reason set for all 1079 programs.

## Findings

1. **The owner's figures are the harness form.** The harness form finds 107 unfinished programs over all files, and 83 after every kind-27 row is removed. DEBT.md:229 records "golden ok 505 -> 517 and unfinished 119 -> 107"; it counts 1097 files by its own method, while this census lists 1079 `.sg` files and finds 518 ok. The brief's figure after kind 27 is 83. The user form finds 108 and 84.
2. **The forms disagree on three programs, all under `sema/invalid/directives/`.** The user form runs with directives off. `time_not_directive_module/main.sg` is unfinished in the user form and gives its expected SEM3120 in the harness form. `not_directive_module/main.sg` and `unknown_namespace.sg` are ok in the user form and give their expected diagnostics in the harness form.
3. **Three valid programs abort inside the analysis, which is worse than unfinished.** They fail with `return origins: expression N is not typed`, raised at `internal/sema/return_origin_expr.go:32`. They are `sema/valid/user_record_type.sg` and `sema/ownership_and_references/overload_autoref_temp_{error,move}.sg`. The smallest trigger is `fn main() { let p = { x: 1, y: 2 }; }`, an anonymous record literal with no annotation. The checker records no type for it, so the harness's "diagnostics failed for valid case" check (`scripts/golden_update.sh:202-203`) fails on all three. Their committed `.diag` files are empty, so all three used to pass clean. PLAN.md lists this as its own packet.
4. **Nine unfinished programs are outside harness scope.** Seven carry only kind 27. `crossing/integration/valid/_integration_generic_crossing_sites.sg` carries an `on` reply row. `spec_audit/s03_compare.sg` carries a compare-pattern row. The D2 criterion covers every program under `testdata/golden`, so this plan keeps them.
5. **Ten unfinished programs are the copies of `core/` in `testdata/golden/core_stdlib/`.** Each copy is a `pragma module, no_std;` directory module, analysed as user code, with 301 to 310 rows and 14 or 16 root reasons. The harness refreshes the copy from `core/` before diagnosing (`scripts/golden_update.sh:255-256`). A fresh copy gives the same 305 rows and 14 root reasons on `base.sg`, so the copy being stale is not the cause. See `reasons/core-copies.md`.

## Totals

| Form | Scope | ok | unfinished | diagnostics | other | total |
|---|---|---:|---:|---:|---:|---:|
| user form | all files | 520 | 108 | 448 | 3 | 1079 |
| user form | harness scope | 476 | 99 | 422 | 3 | 1000 |
| harness form | all files | 518 | 107 | 451 | 3 | 1079 |
| harness form | harness scope | 474 | 98 | 425 | 3 | 1000 |

| Form | Scope | unfinished now | freed if every kind-27 row goes | unfinished after |
|---|---|---:|---:|---:|
| user form | all files | 108 | 24 | 84 |
| user form | harness scope | 99 | 17 | 82 |
| harness form | all files | 107 | 24 | 83 |
| harness form | harness scope | 98 | 17 | 81 |

### Root reasons

| Root reason | programs carrying it | programs where it is the only root | rows |
|---|---:|---:|---:|
| [expression kind 27 needs an origin transfer](reasons/on-expression-kind-27.md) | 24 | 24 | 24 |
| [opaque result borrowed-state classification is unsupported](reasons/opaque-result-classification.md) | 24 | 6 | 509 |
| [index requires a non-scalar index transfer](reasons/index-non-scalar.md) | 17 | 3 | 152 |
| [projected borrowed payload needs precise origin facts](reasons/projected-borrowed-payload.md) | 17 | 1 | 162 |
| [index requires its selected container transfer](reasons/index-selected-container.md) | 15 | 3 | 36 |
| [generic index lacks its finalized concrete use](reasons/generic-index-finalized-use.md) | 14 | 0 | 89 |
| [selected callable lacks its published callable authority](reasons/selected-callable-authority.md) | 13 | 2 | 271 |
| [call needs an exact body, canonical core contract, or opaque declaration promise](reasons/call-exact-body.md) | 12 | 0 | 51 |
| [callable identifier lacks concrete source facts](reasons/callable-identifier-facts.md) | 12 | 0 | 51 |
| [callable value needs its concrete original type and alias authority](reasons/callable-value-authority.md) | 12 | 0 | 51 |
| [conversion retains its actual expression for origin finalization](reasons/conversion-retains-expression.md) | 12 | 0 | 107 |
| [binary callable needs an exact origin contract](reasons/binary-callable-contract.md) | 11 | 0 | 144 |
| [mutable argument may replace reference-bearing contents](reasons/mutable-argument.md) | 11 | 0 | 188 |
| [opaque call may change reference-bearing or callable contents](reasons/opaque-call-effects.md) | 11 | 1 | 261 |
| [captured binding requires origin finalization](reasons/captured-binding.md) | 10 | 0 | 80 |
| [deferred clone requires a live local storage referent](reasons/deferred-clone-live-referent.md) | 10 | 0 | 40 |
| [generic use lacks its exact original callable declarations](reasons/generic-use-original-declarations.md) | 10 | 0 | 15 |
| [parameter requires concrete type or callable provenance](reasons/parameter-concrete-type.md) | 10 | 0 | 10 |
| [range literal lacks its original builtin constructor certificate](reasons/range-literal-certificate.md) | 10 | 0 | 130 |
| [tag constructor lacks its exact owning source declaration](reasons/tag-constructor-owning-declaration.md) | 10 | 0 | 80 |
| [generic original call argument disagrees with its substituted source signature](reasons/generic-argument-disagrees.md) | 9 | 2 | 25 |
| [compare pattern needs a precise matching transfer](reasons/compare-pattern.md) | 6 | 4 | 30 |
| [generic opaque use requires its type-dependent effect transfer](reasons/generic-opaque-use.md) | 6 | 0 | 6 |
| [store through a place needs reference-content transfer](reasons/store-through-place.md) | 6 | 0 | 9 |
| [borrowed temporary has no proven storage owner](reasons/borrowed-temporary.md) | 4 | 0 | 8 |
| [expression kind 12 needs an origin transfer](reasons/tuple-index-kind-12.md) | 4 | 3 | 9 |
| [generic tag use disagrees with its original typed call](reasons/generic-tag-use.md) | 4 | 3 | 9 |
| [an `async` or `blocking` block that captures a value which can hold a reference, a storage loan or a task needs its capture origin](reasons/task-block-capture.md) | 3 | 2 | 3 |
| [destructuring needs projected origin facts](reasons/destructuring.md) | 3 | 2 | 5 |
| [expression kind 9 needs an origin transfer](reasons/map-literal-kind-9.md) | 3 | 1 | 3 |
| [generic use disagrees with its original typed operation](reasons/generic-use-typed-operation.md) | 3 | 0 | 9 |
| [implicit borrow lacks an admitted borrow for this expression](reasons/implicit-borrow-temporary.md) | 3 | 0 | 20 |
| compare has an unproved unmatched continuation | 2 | 1 | 4 |
| container loans lack a proven base | 2 | 0 | 2 |
| generic result contains an unproved source | 2 | 0 | 10 |
| an `async` or `blocking` block whose value can hold a reference, a storage loan or a task needs its payload origin | 1 | 1 | 1 |
| an `on` crossing capture that can hold a reference, a storage loan or a task needs its capture origin | 1 | 0 | 1 |
| an `on` crossing reply that can hold a reference, a storage loan or a task needs its reply origin | 1 | 0 | 1 |
| default result is not proven Defaultable | 1 | 0 | 1 |
| deferred clone needs its selected non-Copy body and effect transfer | 1 | 0 | 4 |
| deferred method may change reference-bearing or callable contents | 1 | 0 | 1 |
| generic use has duplicate or contradictory finalized authority | 1 | 0 | 6 |
| generic use lacks its original typed operation | 1 | 0 | 1 |
| index store requires a scalar index into its canonical container | 1 | 0 | 2 |
| module member value needs its selected free-function authority | 1 | 0 | 1 |
| reference loaded through another reference needs content provenance | 1 | 0 | 1 |
| storage loan would be discarded by a payload-free value | 1 | 1 | 3 |
| tag constructor lacks its original declaration target | 1 | 0 | 1 |

### Derived reasons (propagate a source raised elsewhere; never counted as roots)

| Derived reason | programs carrying it | rows |
|---|---:|---:|
| outgoing reference has unresolved or captured provenance | 61 | 1043 |
| function result contains an unproved source | 53 | 323 |
| callee returned an unproved source | 31 | 302 |

### Unfinished programs (user form)

| Program | harness form | excluded from harness | rows | root reasons |
|---|---|---|---:|---|
| abi/abi_string_bytesview.sg | unfinished |  | 3 | deferred method may change reference-bearing or callable contents; index-selected-container |
| core_stdlib/array.sg | unfinished |  | 310 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/base.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/entrypoint.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/format.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/intrinsics.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/map.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/option.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/result.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/string.sg | unfinished |  | 301 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-opaque-use; generic-use-typed-operation; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| core_stdlib/sync.sg | unfinished |  | 305 | binary-callable-contract; captured-binding; conversion-retains-expression; deferred-clone-live-referent; generic-index-finalized-use; generic-use-original-declarations; index-non-scalar; mutable-argument; opaque-call-effects; opaque-result-classification; parameter-concrete-type; range-literal-certificate; selected-callable-authority; tag-constructor-owning-declaration |
| crossing/block02/valid/on_positive_dynamic_array_capture.sg | unfinished |  | 3 | an `on` crossing capture that can hold a reference, a storage loan or a task needs its capture origin; index-selected-container |
| crossing/block03/invalid/_spawn_on_negative_backend_unavailable.sg | unfinished | underscore helper | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_async_crosses.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_copy_capture.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_copy_composite_capture.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_copy_composite_result.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_distributed.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_far_handle_capture.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_placement_capture.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_placement_var.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_pool.sg | unfinished |  | 1 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_ret_nothing.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_return_far_task.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_route_fn.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_shard.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block03/valid/spawn_on_positive_shard_movable_capture.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block04/valid/capture_positive_copy_spawn_on.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block04/valid/capture_positive_shard_movable_spawn_on.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/block04/valid/effect_positive_spawn_on_inferred.sg | unfinished |  | 3 | on-expression-kind-27 |
| crossing/crosses_deferred/_spawn_on_negative_crosses_call_propagation.sg | unfinished | crosses_deferred | 6 | on-expression-kind-27 |
| crossing/crosses_deferred/_spawn_on_negative_without_crosses.sg | unfinished | crosses_deferred | 3 | on-expression-kind-27 |
| crossing/crosses_deferred/block04/invalid/_crosses_negative_spawn_on_missing_crosses.sg | unfinished | crosses_deferred | 3 | on-expression-kind-27 |
| crossing/integration/valid/_integration_combined_on_spawn_on.sg | unfinished | underscore helper | 3 | on-expression-kind-27 |
| crossing/integration/valid/_integration_generic_crossing_sites.sg | unfinished | underscore helper | 11 | an `on` crossing reply that can hold a reference, a storage loan or a task needs its reply origin; generic result contains an unproved source |
| crossing/integration/valid/_integration_spawn_on_then_await.sg | unfinished | underscore helper | 1 | on-expression-kind-27 |
| crossing/integration/valid/_probe_far_channel_on_and_spawn_on.sg | unfinished | underscore helper | 3 | on-expression-kind-27 |
| hir/compare.sg | unfinished |  | 1 | compare-pattern |
| hir/indexing_ranges.sg | unfinished |  | 3 | index-non-scalar |
| hir/option_erring.sg | unfinished |  | 1 | generic-tag-use |
| hir/tuples.sg | unfinished |  | 3 | tuple-index-kind-12 |
| mir/array_field_mut_ref_reborrow.sg | unfinished |  | 4 | borrowed-temporary; projected-borrowed-payload |
| mir/compare_guard_await_release.sg | unfinished |  | 3 | compare has an unproved unmatched continuation |
| mir/erring_option_nested_tag.sg | unfinished |  | 2 | compare-pattern |
| mir/imported_magic_methods.sg | unfinished |  | 14 | call-exact-body; callable-identifier-facts; callable-value-authority; container loans lack a proven base; index-selected-container; projected-borrowed-payload; store-through-place |
| mir/magic_methods_repro.sg | unfinished |  | 9 | container loans lack a proven base; projected-borrowed-payload; store-through-place |
| mono/option_implicit_wrap.sg | unfinished |  | 2 | generic-tag-use |
| sema/invalid/directives/time_not_directive_module/main.sg | diagnostics |  | 6 | opaque-result-classification |
| sema/ownership_and_references/self_mut_field_index_set.sg | unfinished |  | 9 | projected-borrowed-payload; store-through-place |
| sema/ownership_and_references/self_mut_field_reborrow.sg | unfinished |  | 4 | index-selected-container; projected-borrowed-payload; store-through-place |
| sema/valid/array_helpers.sg | unfinished |  | 12 | conversion-retains-expression; generic use has duplicate or contradictory finalized authority |
| sema/valid/array_view_facts_a_resize_rule_withdraws.sg | unfinished |  | 1 | generic-argument-disagrees |
| sema/valid/clone_semantics/clone_copy_type.sg | unfinished |  | 6 | call-exact-body; callable-identifier-facts; callable-value-authority |
| sema/valid/clone_semantics/task_clone_uninstantiated_generic.sg | unfinished |  | 1 | opaque-call-effects |
| sema/valid/compare_tag_ref.sg | unfinished |  | 9 | call-exact-body; callable-identifier-facts; callable-value-authority; compare-pattern; projected-borrowed-payload |
| sema/valid/concurrency/task_clone_borrow_joined_then_returned.sg | unfinished |  | 4 | generic-opaque-use; opaque-result-classification |
| sema/valid/concurrency/task_container_suspend_safe.sg | unfinished |  | 4 | generic-opaque-use; opaque-result-classification |
| sema/valid/concurrency/task_created_in_current_scope.sg | unfinished |  | 4 | task-block-capture; default result is not proven Defaultable; generic use lacks its original typed operation |
| sema/valid/directives/stdlib_benchmark_module/main.sg | unfinished |  | 12 | opaque-result-classification |
| sema/valid/directives/stdlib_time_directive/main.sg | unfinished |  | 12 | opaque-result-classification |
| sema/valid/directives/stdlib_time_import/main.sg | unfinished |  | 10 | opaque-result-classification |
| sema/valid/fixed_array_view_operator_stays_in_frame.sg | unfinished |  | 25 | binary-callable-contract; projected-borrowed-payload |
| sema/valid/fn_type_async.sg | unfinished |  | 4 | opaque-result-classification |
| sema/valid/import_all.sg | unfinished |  | 1 | selected-callable-authority |
| sema/valid/json_method_jsonvalue_param.sg | unfinished |  | 267 | call-exact-body; callable-identifier-facts; callable-value-authority; conversion-retains-expression; generic-index-finalized-use; implicit-borrow-temporary; index-non-scalar; index-selected-container; module member value needs its selected free-function authority; mutable-argument; projected-borrowed-payload; reference loaded through another reference needs content provenance; tag constructor lacks its original declaration target |
| sema/valid/module_multitest/main.sg | unfinished |  | 1 | selected-callable-authority |
| sema/valid/ownership/for_in_reads_then_pop_drains.sg | unfinished |  | 10 | generic-opaque-use; opaque-result-classification |
| sema/valid/range_literals.sg | unfinished |  | 1 | index-non-scalar |
| sema/valid/recursive_handles.sg | unfinished |  | 6 | generic-argument-disagrees; generic-tag-use |
| sema/valid/ret_async_body.sg | unfinished |  | 3 | an `async` or `blocking` block whose value can hold a reference, a storage loan or a task needs its payload origin |
| sema/valid/return_type_and_sugar.sg | unfinished |  | 3 | generic-tag-use |
| sema/valid/stdlib_entropy_api.sg | unfinished |  | 19 | opaque-result-classification |
| sema/valid/stdlib_hash_api.sg | unfinished |  | 35 | borrowed-temporary; call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees; index-selected-container; projected-borrowed-payload |
| sema/valid/stdlib_random_api.sg | unfinished |  | 125 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-index-finalized-use; opaque-result-classification |
| sema/valid/stdlib_uuid_api.sg | unfinished |  | 131 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-index-finalized-use; index-selected-container; opaque-result-classification; projected-borrowed-payload |
| sema/valid/task_container_drain_with_nested_loop_break.sg | unfinished |  | 30 | generic-opaque-use; opaque-result-classification |
| sema/valid/tuple_access.sg | unfinished |  | 9 | tuple-index-kind-12 |
| sema/valid/tuple_destructure.sg | unfinished |  | 2 | destructuring |
| sema/valid/tuple_destructure_call.sg | unfinished |  | 1 | destructuring |
| spec_audit/s03_compare.sg | unfinished | spec_audit | 2 | compare-pattern |
| stdlib/time_duration_conversions.sg | unfinished |  | 19 | opaque-result-classification; selected-callable-authority |
| vm_arrays/array_field_mut_ref_reborrow.sg | unfinished |  | 4 | borrowed-temporary; projected-borrowed-payload |
| vm_arrays/arrays_drop_nested.sg | unfinished |  | 51 | deferred clone needs its selected non-Copy body and effect transfer; generic result contains an unproved source; projected-borrowed-payload; store-through-place |
| vm_arrays/arrays_index_panic_in_method.sg | unfinished |  | 7 | projected-borrowed-payload |
| vm_async_suite/t14_loop_join.sg | unfinished |  | 6 | generic-opaque-use; opaque-result-classification |
| vm_async_suite/t15_fairness_round_robin.sg | unfinished |  | 2 | tuple-index-kind-12 |
| vm_async_suite/t22_select_wait_recv.sg | unfinished |  | 1 | task-block-capture |
| vm_async_suite/t23_select_wait_timer.sg | unfinished |  | 1 | task-block-capture |
| vm_compare/compare_enum_variants.sg | unfinished |  | 2 | compare-pattern |
| vm_compare/compare_patterns.sg | unfinished |  | 55 | compare has an unproved unmatched continuation; compare-pattern |
| vm_compare/counted_payload_clone_and_borrow.sg | unfinished |  | 6 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees |
| vm_compare/for_in_compare_reads_heap_free_union.sg | unfinished |  | 6 | generic-argument-disagrees |
| vm_hash/hash64_basic.sg | unfinished |  | 35 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-index-finalized-use; generic-argument-disagrees; index-selected-container; projected-borrowed-payload |
| vm_hash/stable64_frames.sg | unfinished |  | 37 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees; index-selected-container; projected-borrowed-payload |
| vm_hash/stable64_primitives.sg | unfinished |  | 37 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees; index-selected-container; projected-borrowed-payload |
| vm_hash/xxh64_vectors.sg | unfinished |  | 32 | call-exact-body; callable-identifier-facts; callable-value-authority; generic-argument-disagrees; index-selected-container; projected-borrowed-payload |
| vm_intrinsics/array_range_panics.sg | unfinished |  | 3 | storage loan would be discarded by a payload-free value |
| vm_maps/map_composite_value.sg | unfinished |  | 1 | index-selected-container |
| vm_maps/map_get_mut.sg | unfinished |  | 8 | borrowed-temporary; map-literal-kind-9; index-non-scalar |
| vm_maps/map_growth_boundary.sg | unfinished |  | 3 | index-non-scalar |
| vm_maps/map_index_get.sg | unfinished |  | 2 | map-literal-kind-9; index-non-scalar |
| vm_maps/map_index_set.sg | unfinished |  | 7 | index-non-scalar; index store requires a scalar index into its canonical container; store-through-place |
| vm_maps/map_literal_order.sg | unfinished |  | 1 | map-literal-kind-9 |
| vm_strings/strings_basic.sg | unfinished |  | 1 | index-selected-container |
| vm_strings/strings_rope.sg | unfinished |  | 2 | index-selected-container |
| vm_strings/strings_rope_std.sg | unfinished |  | 6 | generic-use-typed-operation; implicit-borrow-temporary; index-selected-container |
| vm_strings/strings_std.sg | unfinished |  | 5 | generic-use-typed-operation; implicit-borrow-temporary |
| vm_tuples/tuple_literals.sg | unfinished |  | 5 | destructuring; tuple-index-kind-12 |

## Files

- **`census.json`** is the full record, one line per list element. `unfinished_programs[*].rows` holds every row with its source key, span, line, column, reason and source text.
- **`clusters.json`** is the output of `tools/clusters.py`.
- **`tools/`** holds the runner, classifier, cluster script and table generator. `VERIFY.md` says how to rebuild everything from scratch.
