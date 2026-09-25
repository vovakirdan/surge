# D2 unfinished programs: plan

<!-- 10-line summary -->
1. **Totals.** At `7131fb2`, 107 of 1079 golden programs are unfinished in the harness form, and 108 with the brief's command. Removing kind 27 (N-TASK-27S and TC-XB) leaves 83, or 84.
2. **Measured.** 18 small packets, prototyped in a scratch copy and not committed (about 600 lines), make 48 of those 84 diagnose clean. They were enabled one at a time; no program was lost and none turned into a diagnostic.
3. **Top 3 packets.** N-TUPLE frees 6 programs in about 80 lines. N-PATTERN frees 5 in 33 lines, one of them in `spec_audit`. N-DEFHANDLE frees 5 in 6 lines.
4. **No decision needed for 46.** 46 of the 48 need no owner decision, though 3 of the packets need a coordinator re-measure first. `strings_std` and `strings_rope_std` also need owner question 3(a).
5. **Estimated.** 16 more programs need packets I sized but did not prototype: Map index, stores, operators, conversions, `on` capture and reply. Five of those packets wait on P-STASH's fence.
6. **Blocked on the owner.** 22 programs cannot reach zero without a decision: 10 `core_stdlib` copies (Q1), 4 Task programs (Q2), 7 lent temporaries (Q3) and 1 guard fallthrough (Q4).
7. **The questions.** Q1: diagnose the copies as core? Q2: wait for R-i and D4b on Task values? Q3: core body summaries, wait, or rewrite call sites? Q4: what a compare yields when its guarded arm fails.
8. **Found on the way.** The VM cannot build a default runtime handle (VM1999). 3 valid programs abort the analysis on an anonymous record. `t15` becomes a runtime result.
9. **Tests.** 34 driver tests already fail at `7131fb2`. The prototype changes 16 more leaves: 13 flips of pinned or control rows (2 await Q3(a)), 2 leaks still refused under another row, and 1 forged-selection hole to close.
10. **Confidence.** High for the census and the 48 measured programs. Medium that real packets match the prototype sizes. Low for the 16 estimates.

Claims are measured unless marked "(read from code)". "Measured" means a run of the compiler at `7131fb2`, or of the scratch prototype built from it. The prototype is described in words only, and its code is not committed. Per-reason detail is in `reasons/`, the numbers in `census.md`, and the grouping in `clusters.md`.

## 1. Where D2 stands

| Count | User form (the brief's command) | Harness form (`golden_update.sh`) |
|---|---:|---:|
| Unfinished, all 1079 files | 108 | 107 |
| After every kind-27 row goes | 84 | 83 |
| Of those, `core_stdlib` copies | 10 | 10 |
| Valid programs whose diagnosis aborts inside the analysis (not counted as unfinished) | 3 | 3 |

The 84 include two harness-excluded programs, `spec_audit/s03_compare.sg` and `crossing/integration/valid/_integration_generic_crossing_sites.sg`. The 83 differ only by `sema/invalid/directives/time_not_directive_module/main.sg`, which gives its expected SEM3120 in the harness form. The plan works on the 84.

## 2. Measured packets, in landing order

Each step enables one more packet in the prototype and diagnoses all 84 programs again. The table is `prototype_measurements.json`, `landing_order`. A leave-one-out run gives the packets each program needs (`necessary_packets`).

- **Sizes** are non-test Go lines of the prototype. A real packet with its test rows is likely 2 to 4 times that. S is under 100 lines, M is 100 to 300, L is over 300.
- **Order** follows programs freed per line, then dependencies. Steps 1 to 16 are independent of each other, except that some programs need several of them. Steps 17 and 18 go together.

| step | packet | answers | programs predicted ok (measured) | prerequisite or conflict | prototype lines | main risk |
|---:|---|---|---|---|---:|---|
| 1 | N-TUPLE | tuple-index-kind-12, destructuring | `hir/tuples.sg`, `sema/valid/tuple_access.sg`, `sema/valid/tuple_destructure.sg`, `sema/valid/tuple_destructure_call.sg`, `vm_async_suite/t15_fairness_round_robin.sg`, `vm_tuples/tuple_literals.sg` | none | 80 | adds `t15` to the runner-matrix watch; typing already refuses references in tuples |
| 2 | N-PATTERN | compare-pattern | `hir/compare.sg`, `mir/erring_option_nested_tag.sg`, `spec_audit/s03_compare.sg`, `vm_compare/compare_enum_variants.sg`, `vm_compare/compare_patterns.sg` | flips the sema row `enum_pattern` | 33 | a partial pattern marked covering would hide a later arm; canary `f04` becomes SEM3139 |
| 3 | N-DEFHANDLE | opaque-result-classification, generic-opaque-use (handle defaults) | `sema/valid/concurrency/task_clone_borrow_joined_then_returned.sg`, `sema/valid/concurrency/task_container_suspend_safe.sg`, `sema/valid/ownership/for_in_reads_then_pop_drains.sg`, `sema/valid/task_container_drain_with_nested_loop_break.sg`, `vm_async_suite/t14_loop_join.sg` | touch Defaultable only, never NoBorrowedState (R-i); record the VM default-handle defect first | 6 | the VM panics with VM1999 on a reached default handle (fails closed) |
| 4 | N-STDLIB-TIME | opaque-result-classification (`Duration`) | `sema/invalid/directives/time_not_directive_module/main.sg`, `sema/valid/directives/stdlib_benchmark_module/main.sg`, `sema/valid/directives/stdlib_time_directive/main.sg`, `sema/valid/directives/stdlib_time_import/main.sg` | an identity certificate, not an `@intrinsic` rule | 3 | a general `@intrinsic` rule would admit Range, Task, Channel and BytesView |
| 5 | N-TAGCONV | generic-tag-use | `hir/option_erring.sg`, `mono/option_implicit_wrap.sg`, `sema/valid/return_type_and_sugar.sg` | none | 51 | low: certifies the instance only |
| 6 | N-CLONE-COPY | call-exact-body, callable-identifier-facts, callable-value-authority | `sema/valid/clone_semantics/clone_copy_type.sg` | require no clone selection; flips `copy_number_control` | 32 | without the selection check a forged selection is accepted |
| 7 | N-INDEX-VIEW | index-selected-container | `vm_strings/strings_basic.sg`, `vm_strings/strings_rope.sg` | none | 19 | low: byte read and `own C` |
| 8 | N-FIELD-BORROW | projected-borrowed-payload | `sema/valid/compare_tag_ref.sg`, `vm_arrays/arrays_index_panic_in_method.sg` | relies on `containerLoans` refusing an unproven base (canary `f06`) | 2 | a later change to `containerLoans` reopens `f06` |
| 9 | N-ALIAS-ARG | generic-argument-disagrees (alias form) | `sema/valid/recursive_handles.sg`, `vm_hash/stable64_frames.sg`, `vm_hash/stable64_primitives.sg`, `vm_hash/xxh64_vectors.sg` | re-measure P-STASH and P-VIEW2 (same reason text) | 20 | low |
| 10 | N-STDLIB-ENTROPY | opaque-result-classification (entropy) | `sema/valid/stdlib_entropy_api.sg` | none | 13 | low; runtime freshness read from code |
| 11 | N-DEAD-GENERIC-USE | generic-index-finalized-use | `sema/valid/stdlib_random_api.sg`, `sema/valid/stdlib_uuid_api.sg`, `vm_hash/hash64_basic.sg` | none | 16 | low: the code is never emitted |
| 12 | N-CHAN-CAPTURE | task-block-capture (channel form) | `vm_async_suite/t22_select_wait_recv.sg`, `vm_async_suite/t23_select_wait_timer.sg` | coordinator sign-off: a by-value counted handle is outside R-b(body); re-measure P-PARK-BODY | 23 | the capture row is R-b(body)'s fence |
| 13 | N-IMPORT-IDENTITY | selected-callable-authority | `sema/valid/import_all.sg`, `sema/valid/module_multitest/main.sg`, `stdlib/time_duration_conversions.sg` | use the resolver's export record; exclude `core` modules (G5 tripwire) | 16 | a name match can pick the wrong export |
| 14 | N-TAGMEMBER-ARG | generic-argument-disagrees (union-member form) | `vm_compare/counted_payload_clone_and_borrow.sg`, `vm_compare/for_in_compare_reads_heap_free_union.sg` | re-measure P-STASH and P-VIEW2 before landing | 20 | canaries `f16` and `f17` keep only their G6 row |
| 15 | N-MAPLIT | map-literal-kind-9 | `vm_maps/map_literal_order.sg` | none | 43 | low |
| 16 | N-INDEX-CALL | index-non-scalar (user `__index`) | `hir/indexing_ranges.sg`, `sema/valid/range_literals.sg` | must check the selected `__index` against the original operation | 65 | the prototype accepts a forged selection; canary `f12` becomes SEM3139 |
| 17 | N-CONCAT-USE | generic-use-typed-operation | none alone | pairs with N-CORE-TEMP | 35 | low: an identity certificate |
| 18 | N-CORE-TEMP | implicit-borrow-temporary, borrowed-temporary (core callees) | `vm_strings/strings_rope_std.sg`, `vm_strings/strings_std.sg` | owner question 3(a); flips 2 pinned rows | 76 | relies on core bodies starting no task |

What the real packets must do differently from the prototype, all measured on the prototype:

- **N-CLONE-COPY** must skip a call that has a clone selection. Without that check, row `copy_result_control` loses its refusal.
- **N-IMPORT-IDENTITY** must read the resolver's export record instead of matching by name and signature, and must exclude `core` modules. Without the exclusion, `TestH2TripwireRefusedG5ModuleTimeout` fails. DEBT.md:229 names that identity as a tripwire change.
- **N-STDLIB-TIME** must be keyed by declaration identity. The prototype keyed it by source file.
- **N-INDEX-CALL** must check the selected `__index` against the original typed operation. The prototype accepts a forged selection (`TestAnalyzeTypedStringRangeIndexAuthority`).

Tests under all 18 packets with those two fixes:

Full `internal/sema` and `internal/driver` suites, base against prototype, both with a 55-minute timeout:

- **Base.** On unmodified `7131fb2`, `internal/sema` passes. `internal/driver` fails 34 top-level tests (56 entries with subtests). See section 6, item 7.
- **Under the prototype**, 16 more leaves fail. The table attributes each one by switching single packets off.

| Packet | Test leaves that change | What they mean |
|---|---|---|
| N-PATTERN | `TestReturnOriginEnumVariantTargets/enum_pattern` | documents today's refusal; an intended flip |
| N-CLONE-COPY | `TestAnalyzeSelectedDirectCloneOrigins/copy_number_control` | an intended flip. The forged-selection row `copy_result_control` stays refused. |
| N-FIELD-BORROW | `TestAnalyzeArrayPopGetMut`: `drop_last`, `first_byte`, `view_field`, `pop_views`, `reserve_field`; `TestAnalyzeMemberProjectionOrigins/loan_carrier_control` | These pin the pre-existing projection row ("BEFORE-equality" in the test). `drop_last`, `first_byte`, `reserve_field` and `read_items` become clean: they pop a byte, return `Option<byte>`, reserve capacity, or return `b.items` with `b`'s origin. `view_field` and `pop_views` stay unfinished on their other rows. The rows must be updated. |
| N-DEAD-GENERIC-USE | `TestAnalyzeArrayPopLoanFormals`: `leak_formal_inner`, `pop_inner_rt` | These are real leaks of a view of a local. They stay refused, but now by "cursor element that can hold storage loans needs its backing loan transfer" instead of the pinned pair. The rows must be re-pinned. |
| N-CONCAT-USE | `TestAnalyzeOperatorCarrierResult`: `concat_control`, `fixed_concat_control` | The test itself says the leaf "must become a plain-absence assertion"; an expected transition. |
| N-CORE-TEMP | `TestAnalyzeStringTemporaryArguments`: `core_loan_carrier_result`, `core_loan_sink_effect` | owner question 3(a) |
| N-INDEX-CALL | `TestAnalyzeTypedStringRangeOrigins/foreign_selected_range` | A pinned refusal. The foreign `__index` returns an owned string, so answering it is sound; the row must be updated. |
| N-INDEX-CALL | `TestAnalyzeTypedStringRangeIndexAuthority` | **A hole in the prototype.** A detached mutation swaps the selected index operation, and the prototype accepts it. The real packet must check the selected `__index` against the original typed operation and keep "selected string range index disagrees with its original signature". |

Other results:

- **One base failure passes under the prototype.** N-IMPORT-IDENTITY fixes `TestDiagnoseReportsWrongRelativeImportToExplicitModuleSameName`. On the base it fails with "selected callable lacks its published callable authority".
- **Tripwire and task tests pass.** That covers all 10 `TestH2Tripwire*` driver tests, all 37 `TestTaskCheck*` tests, and the 7 task, async, channel and `on` tests. Among those are `TestAnalyzeTaskAwaits` (row `borrowing_task_payload_stays_refused`, R-i's tripwire) and `TestAnalyzeTaskBlocks` (row `reference_capture_stays_refused`). All 17 VM `TestH2Tripwire*` tests pass.
- **The 15 freed programs that have a golden `.out`** match it on both backends, as their sidecars require.

## 3. Packets not prototyped (estimated from code)

| Packet | Programs it would free | Size | Prerequisite or conflict | Main risk |
|---|---|---|---|---|
| N-MAP-INDEX: generic `Map.__index` and `__index_set` (`core/map.sg:42-53`) | `vm_maps/map_index_get`, `map_growth_boundary`, `map_composite_value`. `map_index_set` also needs N-STORE; `map_get_mut` also needs N-CORE-TEMP | M | none | a fresh `m[k]` would let `&V` outlive an insert |
| N-STORE: stores into element or field places | `sema/ownership_and_references/self_mut_field_index_set`, `self_mut_field_reborrow`, `vm_maps/map_index_set` | M to L | P-STASH's own fence first | P-STASH is a store (`*out =`, a push into `&mut`) |
| N-MAGIC-METHODS: user and imported `__index`, `__index_set` and `__clone` bodies, and a field's container loans (`self.values.__range()`) | `mir/magic_methods_repro`, `mir/imported_magic_methods` | M | after N-STORE and N-INDEX-CALL | the same as N-STORE |
| N-OPERATOR-BODY: binary operators with a selected body | `sema/valid/fixed_array_view_operator_stays_in_frame` | M | none | a view built from both operands must keep both |
| N-CONV-GENERIC: `x to T` through a selected generic `__to`, and duplicate finalized uses at one site | `sema/valid/array_helpers` | M | none | a view-returning `__to` |
| N-MUT-PLACE-RECV: an element place as a `&mut` receiver (`xs[1].push(9)`) | `sema/valid/array_view_facts_a_resize_rule_withdraws` | S to M | after P-STASH's own fence; DEBT.md:229 names this exact row | this is P-STASH's push shape |
| N-DEFERRED-METHOD: `core/base.sg:85` `self.__len()` deferred with `T = BytesView` | `abi/abi_string_bytesview` | S to M | none | low |
| N-ON-CAPTURE-OWNED: an `on` capture of an owned `int[]` (`ret total(own xs);`) | `crossing/block02/valid/on_positive_dynamic_array_capture` | S to M | a run on both backends, as CLAUDE.md requires | a moved array must not keep a frame loan |
| N-ON-REPLY-GENERIC: a generic `on` reply in `route::<T>` | `crossing/integration/valid/_integration_generic_crossing_sites` (outside harness scope) | M | none | a reply payload that holds a loan |
| N-RANGE-FORMAL: a by-value `Range` argument to a user function (`sum_range(a0.__range())`) | `vm_intrinsics/array_range_panics` | M | G6 is a P-STASH fence; needs a body-summary exception for by-value formals | reopening G6 |
| N-DROP-NESTED: deferred non-Copy clone bodies (`core/array.sg:105`, `123`, `124`, `290`), a generic `to_array` result, element field reads, stores through views | `vm_arrays/arrays_drop_nested` | L | after N-STORE | the same as N-STORE |
| N-ANON-RECORD: the checker types an unannotated anonymous record literal, or the analysis refuses it by name | the 3 aborting programs in section 1 (not unfinished) | S | none | low |

## 4. Owner decisions

| # | Question | Programs | File |
|---|---|---|---|
| Q1 | Diagnose the ten `core_stdlib` copies as core (B), stop running `diag` on them (A or E), or prove them as user modules (D)? | the 10 copies | `reasons/core-copies.md` |
| Q2 | For a Task return-origin cannot see into (made through a function value, carried as a payload, captured by a block, cloned through `&Task<T>`): wait for R-i and D4b, read a new per-task fact from the task check, or refuse by a named rule? | `fn_type_async`, `ret_async_body`, `task_created_in_current_scope`, `task_clone_uninstantiated_generic` | `reasons/owner-q2-task-values.md` |
| Q3 | (a) May a synchronous core callee's body summary admit a lent temporary, flipping two pinned rows? (b) For user and stdlib callees, wait for the task check's per-formal fact, or rewrite the call sites to bind the temporary first? | (a) `strings_std`, `strings_rope_std`, `map_get_mut`; (b) both `array_field_mut_ref_reborrow`, `stdlib_hash_api`, `json_method_jsonvalue_param` | `reasons/owner-q3-lent-temporaries.md` |
| Q4 | When the only arm for a variant is guarded and the guard fails, is the compare's value the result type's default, a compile-time refusal, or a trap? | `mir/compare_guard_await_release` | `reasons/owner-q4-guard-fallthrough.md` |

These sequencing conditions need the coordinator but are not design questions:

- **N-CHAN-CAPTURE:** R-b(body)'s capture row.
- **N-ALIAS-ARG and N-TAGMEMBER-ARG:** re-measure P-STASH and P-VIEW2.
- **N-IMPORT-IDENTITY:** exclude core, per the G5 tripwire.
- **N-STORE, N-MUT-PLACE-RECV and N-RANGE-FORMAL:** land after P-STASH's own fence.

## 5. Predicted path to zero

| Stage | What lands | Unfinished left (user form, all files) | Basis |
|---|---|---:|---|
| 0 | N-TASK-27S and TC-XB (in flight) | 84 | projection over the census, as the brief assumes |
| 1 | steps 1 to 16 | 38 | measured |
| 2 | steps 17 and 18, if Q3(a) is yes | 36 | measured |
| 3 | the section-3 packets | 19 | estimated |
| 4 | Q1, option B or A | 9 | Q1 file; option B measured to leave 8 tag-constructor sites, answered by N-CORE-ROOT-TAGS |
| 5 | Q4 | 8 | estimated |
| 6 | Q3(b), by rewrite or once the task check lands | 4 | estimated. The JSON program also needs N-MUT-STRUCT-CELLS and the reference-field and module-member transfers (M to L). |
| 7 | Q2, once R-i is closed and D4b lands, or option B | 0 | estimated |

The 19 left after stage 3 are exactly the programs that cannot reach zero without an owner decision:

- 10 copies (Q1)
- 4 Task programs (Q2)
- 4 lent temporaries to user or stdlib callees (Q3(b)): both `array_field_mut_ref_reborrow`, `stdlib_hash_api` and `json_method_jsonvalue_param`
- 1 guard program (Q4)

If Q3(a) is no, `strings_std`, `strings_rope_std` and `map_get_mut` stay too, which makes 22.

## 6. Found on the way

1. **The VM cannot build a default core runtime handle.** `let mut ch: Channel<int>;`, `let mut slot: Task<int>;` and a reached `default::<Task<int>>()` (through `Option.safe()`) each panic on the VM with `panic VM1999: storage: type#N has 1 members but 0 layout offsets`. LLVM runs them. LLVM uses a null pointer for a handle's default (`internal/backend/llvm/emit_intrinsics_default.go:67-73`), while the VM builds the default from the struct layout (`internal/vm/intrinsic_default.go:145-162`); that attribution is read from code. Model, code and run disagree between the backends. This belongs in the ledger before N-DEFHANDLE lands.
2. **Three valid golden programs abort the analysis.** They fail with `return origins: expression N is not typed` (`internal/sema/return_origin_expr.go:32`) on an anonymous record literal with no annotation, `let p = { x: 1, y: 2 };`. Their committed `.diag` files are empty, so they used to pass. `scripts/golden_update.sh:202-203` rejects them today. The programs are `sema/valid/user_record_type.sg` and `sema/ownership_and_references/overload_autoref_temp_{error,move}.sg`.
3. **`t15_fairness_round_robin` becomes a runtime result.** DEBT.md:229 (ST-RUNOUT) calls it "still unfinished and not a runtime result". After N-TUPLE it builds; its VM output is byte-equal and its LLVM output is equal as a multiset, as its `.order-backends` sidecar requires.
4. **A compare whose guarded arm fails with no arm left yields `default()` silently** on both backends. The MIR lowers it as `L24 = call default()`. That is Q4.
5. **The real `core/` diagnosed as the root program by absolute path is still unfinished**: 38 rows at 8 explicit tag-constructor sites. Diagnosed by relative path, it is refused as `core namespace reserved`.
6. **`docs/RUNTIME_MODEL_EXPLAINED.md` does not exist at `7131fb2`.** Only `docs/RUNTIME_MODEL_EXPLAINED.ru.md` does. I read the Russian file.
7. **34 of the 292 `internal/driver` tests fail on unmodified `7131fb2`**, with or without `SURGE_STDLIB`.
   - **21 fail because the return-origin analysis refuses the test's own fixture program.** In 15 it is reported as unfinished; the most common rows are the three call-family reasons on `clone(...)`. In 6 it is "return-origin refusal was not represented in the returned diagnostics".
   - **6 are `TestAnalyzeTypedReturnOrigins*` tests** whose covered `core/array.sg` is not clean in their harness ("deferred clone lacks its original owning caller").
   - **7 fail on other preconditions.**

   The golden census does not count these fixtures, but they are programs the D2 gate refuses today. `internal/sema` passes.

## 7. How this was checked

- **Documents.** Runtime claims were checked against `docs/RUNTIME_V2.md`, `docs/RUNTIME_MODEL_EXPLAINED.ru.md`, `docs/runtime-v2-epics/` (with DEBT.md rows 365, 368 and 370) and `docs/RUNTIME.md`, and never against `docs/CONCURRENCY.md`.
- **Runs on both backends**, with the analysis gate bypassed in a scratch build where a program is still unfinished:
  - the channel captures behind N-CHAN-CAPTURE: probes `s01` and `s04`, plus valgrind
  - the handle defaults behind N-DEFHANDLE
  - the Task probe behind Q2, plus valgrind
  - the guard probes behind Q4
  - the 15 freed programs that have a golden `.out`
- **Read from code only:** the entropy runtime's freshness, the reasons a store misses its fast path, and every size in section 3.
- **Language never widened.** Every packet above is a transfer or a named refusal, and none of them turns a check off.
