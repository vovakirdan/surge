# Return-origin unfinished census and implementation plan

## Scope and method

* Baseline: `cafbbbe5` (`origin/validation/step7-d2-on-d1` as supplied); census branch `cloud/d2-unfinished-census-2`.
* Built with `go build ./cmd/surge`, set `SURGE_STDLIB=$PWD`, and invoked `surge diag --format short --directives=off` separately for all **1,166** `.sg` files under `core/`, `stdlib/`, `testdata/golden/`, `showcases/`, and `benchmarks/`. All invocations completed: 624 exit 0 and 542 exit 1; none timed out.
* Result: **5,116 unfinished rows in 66 invoking programs**, grouped into 44 root-cause/construct buckets. `rows.json` is the lossless row inventory. A program can import several `SourceKey`s, so `file` is the invoked program while `span` starts with the source unit that owns the row.
* Direct diagnosis of files in `core/` is rejected by the driver as “core namespace reserved”; their return-origin rows are nevertheless measured through importing valid fixtures. In particular, `core/string.sg` contributes six rows to two programs.
* “Valid golden” below means an unfinished `testdata/golden` input with an existing empty `.diag`, excluding the ten `core_stdlib` parser/snapshot fixtures: exactly **33 programs**, matching the requested 33.

## Priority order

The ordering is programs unblocked per estimated implementation effort, with the requested policy override: `core/string.sg` and the 33 valid goldens rank ahead of broad stdlib/showcase cleanup. “Touched” is not “unblocked”: only the final column counts programs whose complete row set consists of that one group.

|Rank|Group|Rows|Programs|33 valid goldens touched|Programs fully finished by this fix alone|Effort|
|---:|---|---:|---:|---:|---:|---|
|1|generic use disagrees with its original typed operation — generic call/use|9|3|2|0|S|
|2|selected callable lacks its published callable authority — selected call/operator|555|25|3|4|L|
|3|index requires a non-scalar index transfer — index expression/store|148|14|4|2|M|
|4|borrowed temporary has no proven storage owner — implicit borrow/temporary|14|7|4|3|S|
|5|an 'async' or 'blocking' block that captures a value which can hold a reference, a storage loan or a task needs its capture origin — capture/block|5|5|2|2|S|
|6|outgoing reference has unresolved or captured provenance — reference-bearing expression|1389|37|10|0|L|
|7|function result contains an unproved source — call/return result|263|31|8|0|L|
|8|callee returned an unproved source — call/return result|373|26|6|0|L|
|9|store through a place needs reference-content transfer — place store|12|9|6|0|S|
|10|projected borrowed payload needs precise origin facts — payload projection|106|9|5|1|M|
|11|generic index lacks its finalized concrete use — index expression/store|111|17|4|1|M|
|12|index requires its selected container transfer — index expression/store|12|10|4|1|S|
|13|opaque result borrowed-state classification is unsupported — expression/transfer|521|17|3|0|L|
|14|implicit borrow lacks an admitted borrow for this expression — implicit borrow/temporary|78|10|3|3|M|
|15|generic original call argument disagrees with its substituted source signature — expression/transfer|10|3|3|3|S|
|16|conversion retains its actual expression for origin finalization — conversion|124|20|2|0|M|
|17|container loans lack a proven base — expression/transfer|2|2|2|0|S|
|18|mutable argument may replace reference-bearing contents — mutable call argument|392|14|1|0|L|
|19|binary callable needs an exact origin contract — expression/transfer|144|11|1|0|M|
|20|reference loaded through another reference needs content provenance — reference dereference|4|4|1|0|S|
|21|generic result contains an unproved source — expression/transfer|12|3|1|0|S|
|22|generic use has duplicate or contradictory finalized authority — generic call/use|8|2|1|0|S|
|23|storage loan would be discarded by a payload-free value — expression/transfer|4|2|1|1|S|
|24|deferred clone needs its selected non-Copy body and effect transfer — expression/transfer|4|1|1|0|S|
|25|index store requires a scalar index into its canonical container — index expression/store|2|1|1|0|S|
|26|an 'on' crossing capture that can hold a reference, a storage loan or a task needs its capture origin — capture/block|1|1|1|0|S|
|27|call needs an exact body, canonical core contract, or opaque declaration promise — expression/transfer|1|1|1|0|S|
|28|callable identifier lacks concrete source facts — expression/transfer|1|1|1|0|S|
|29|callable value needs its concrete original type and alias authority — expression/transfer|1|1|1|0|S|
|30|deferred method may change reference-bearing or callable contents — expression/transfer|1|1|1|1|S|
|31|module member value needs its selected free-function authority — expression/transfer|1|1|1|0|S|
|32|tag constructor lacks its original declaration target — tag constructor|1|1|1|0|S|
|33|generic use lacks its exact original callable declarations — generic call/use|99|15|0|0|M|
|34|opaque call may change reference-bearing or callable contents — expression/transfer|262|11|0|0|L|
|35|range literal lacks its original builtin constructor certificate — expression/transfer|130|10|0|0|M|
|36|deferred clone requires a live local storage referent — expression/transfer|40|10|0|0|M|
|37|parameter requires concrete type or callable provenance — expression/transfer|10|10|0|0|S|
|38|captured binding requires origin finalization — capture/block|82|7|0|0|M|
|39|generic use disagrees with its finalized callee instance — generic call/use|99|6|0|0|M|
|40|tag constructor lacks its exact owning source declaration — tag constructor|81|6|0|0|M|
|41|an 'on' crossing reply that can hold a reference, a storage loan or a task needs its reply origin — expression/transfer|1|1|0|0|S|
|42|deferred clone requires its direct template shared receiver — expression/transfer|1|1|0|0|S|
|43|generic opaque use requires its type-dependent effect transfer — expression/transfer|1|1|0|0|S|
|44|tag constructor disagrees with its source payload slots — tag constructor|1|1|0|0|S|

### The 33 valid golden programs

* `testdata/golden/abi/abi_string_bytesview.sg`
* `testdata/golden/crossing/block02/valid/on_positive_dynamic_array_capture.sg`
* `testdata/golden/mir/array_field_mut_ref_reborrow.sg`
* `testdata/golden/mir/imported_magic_methods.sg`
* `testdata/golden/mir/magic_methods_repro.sg`
* `testdata/golden/sema/ownership_and_references/self_mut_field_index_set.sg`
* `testdata/golden/sema/ownership_and_references/self_mut_field_reborrow.sg`
* `testdata/golden/sema/valid/array_helpers.sg`
* `testdata/golden/sema/valid/array_view_facts_a_resize_rule_withdraws.sg`
* `testdata/golden/sema/valid/fixed_array_view_operator_stays_in_frame.sg`
* `testdata/golden/sema/valid/import_all.sg`
* `testdata/golden/sema/valid/json_method_jsonvalue_param.sg`
* `testdata/golden/sema/valid/module_multitest/main.sg`
* `testdata/golden/sema/valid/stdlib_entropy_api.sg`
* `testdata/golden/sema/valid/stdlib_hash_api.sg`
* `testdata/golden/sema/valid/stdlib_random_api.sg`
* `testdata/golden/sema/valid/stdlib_uuid_api.sg`
* `testdata/golden/stdlib/time_duration_conversions.sg`
* `testdata/golden/vm_arrays/array_field_mut_ref_reborrow.sg`
* `testdata/golden/vm_arrays/arrays_drop_nested.sg`
* `testdata/golden/vm_async_suite/t22_select_wait_recv.sg`
* `testdata/golden/vm_async_suite/t23_select_wait_timer.sg`
* `testdata/golden/vm_compare/counted_payload_clone_and_borrow.sg`
* `testdata/golden/vm_compare/for_in_compare_reads_heap_free_union.sg`
* `testdata/golden/vm_hash/hash64_basic.sg`
* `testdata/golden/vm_intrinsics/array_range_panics.sg`
* `testdata/golden/vm_maps/map_composite_value.sg`
* `testdata/golden/vm_maps/map_get_mut.sg`
* `testdata/golden/vm_maps/map_growth_boundary.sg`
* `testdata/golden/vm_maps/map_index_get.sg`
* `testdata/golden/vm_maps/map_index_set.sg`
* `testdata/golden/vm_strings/strings_rope_std.sg`
* `testdata/golden/vm_strings/strings_std.sg`

## Top five fix designs

### 1. Reconnect synthesized generic operations (`core/string.sg`)
**Rows/programs.** 9 rows / 3 programs overall; six rows originate in `core/string.sg`. **Design:** when an implicit/synthesized operation is recorded, preserve its original typed-operation identity through generic finalization, then let `checkSynthesizedGenericUses` compare the finalized use against that certificate instead of treating the rewritten expression as a different operation. Touch `return_origin_synthesized_uses.go`, the generic-use publication/index builder, and focused tests. **Estimate:** 80–140 LoC plus 6–10 tests (M). **Risk:** medium: bad aliasing between a synthesized operator and a user overload could bless the wrong effect. This is a compiler evidence-reconnection, not a language decision.

Compatibility: this implements the D2 `@return_source`/origin certificate direction in `docs/runtime-v2-epics/22-step7-execution.md` and does not change representation, scheduling, or the ownership rules in `docs/RUNTIME_V2.md` / `docs/RUNTIME_MODEL_EXPLAINED.ru.md`.

### 2. Publish exact callable authority for selected stdlib/magic calls
**Rows/programs.** 555 / 25; three of the 33 valid goldens, with many downstream rows in the stdlib API fixtures and the magic-method MIR fixtures. **Design:** extend publication indexing so selected methods/operators retain the canonical source declaration and original typed signature after import and monomorphization; consume that single certificate in `selectedCallableCandidate` rather than guessing from a physical declaration. Touch `return_origin_publication.go`, `return_origin_selected_callable.go`, declaration indexing, and import/generic tests. **Estimate:** 180–300 LoC plus 12–18 tests (L). **Risk:** medium-high: declaration aliases and imported magic methods can be ambiguous; fail closed on multiple authorities. Compiler change, no language decision.

Compatibility: canonical authority is exactly the Step-7 plan’s evidence model; it does not loosen crossing capture or runtime ownership. Keep physical/canonical identities distinct as required by the runtime-v2 epic plans.

### 3. Complete canonical index transfer (maps, strings, arrays)
**Rows/programs.** Treat the three index buckets as one implementation packet: non-scalar index 148/14, missing selected container transfer 12/10, and missing finalized generic index 111/17. It directly targets `vm_maps/*`, `vm_strings/*`, `mir/array_field_mut_ref_reborrow`, and API fixtures. **Design:** issue one finalized index certificate containing canonical container, index kind, selected get/set callable, instantiated element type, and get/set backing effect; teach `index`, `indexOperation`, and `indexStoreOperation` to consume it and transfer base/content origins. Touch `return_origin_index*.go`, generic publication, and map/string/array intrinsic tests. **Estimate:** 250–420 LoC plus 20–30 tests (L). **Risk:** high: get versus set effects and array views must not be conflated.

Compatibility: preserve the explicit buffer-alias boundary in `22-step7-execution.md`; this packet certifies existing canonical operations and must not admit the reference-bearing array view/walk forms reserved for later work. No language decision unless implementation requires aliasing a view and its base—then stop and seek the already-deferred buffer-alias ruling.

### 4. Give borrowed temporaries/reborrows a storage-owner certificate
**Rows/programs.** 14 / 7, with four valid goldens and three programs fully unblocked, including both array-field mutable-reference reborrow fixtures. **Design:** at unary borrow/reborrow creation, carry the evaluated place’s owner and lexical scope through field/index projection; for a true rvalue temporary, mint its existing temporary-frame owner. Consume this in `unary` instead of emitting “no proven storage owner.” Touch `return_origin_expr.go`, place/projection helpers, and borrow tests. **Estimate:** 90–160 LoC plus 10–14 tests (M). **Risk:** medium-high: accidentally extending a temporary beyond its frame is unsound. Compiler change for place reborrows; if the language intends rvalue-borrow lifetime extension, that subcase needs a language decision and must stay refused meanwhile.

Compatibility: aligns with `docs/RUNTIME_V2.md`’s address-stability/lifetime rule; it supplies proof rather than extending lifetime. It does not alter task/crossing rules.

### 5. Transfer safe task-block capture origins (`vm_async` t22/t23)
**Rows/programs.** 5 / 5; fixing this bucket alone finishes t22 and t23. **Design:** build the async/blocking body capture record from each capture’s proven origin, preserve reference-free/copy captures and owned task handles, and return an origin value tied to the task frame; retain the existing refusal for a borrowed capture unless the task checker proves pinning/lifetime. Touch `return_origin_task_blocks.go`, capture-record construction, and task-block tests. **Estimate:** 120–220 LoC plus 12–18 tests (M). **Risk:** high because a false certificate can make an escaping task read dead storage.

Compatibility: `docs/RUNTIME_V2.md` explicitly distinguishes local spawn (which may capture a proven borrow under carrier/lifetime constraints) from `blocking` and distributed crossings (which may not). `docs/RUNTIME_MODEL_EXPLAINED.ru.md` likewise ties captured borrows to the parent carrier. Therefore this is compiler work only for reference-free/proven-safe captures; admitting borrowed `blocking` captures or unpinned escaping local-task captures **requires a language/owner decision** and is not part of this fix.

## Complete grouped census

Counts below are raw rows and distinct invoking programs. Examples are source-span owners, not necessarily the invoking root. “Fully finished” is computed literally from `rows.json`: the listed program has no row in another group.

### G1: generic use disagrees with its original typed operation — generic call/use

* **Count:** 9 rows in 3 programs; 2 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_synthesized_uses.go:checkSynthesizedGenericUses`.
* **Minimal examples:**
  * `core/string.sg:311` — `prev + one`
  * `core/string.sg:331` — `curr + one`
  * `testdata/golden/core_stdlib/string.sg:311` — `prev + one`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G2: selected callable lacks its published callable authority — selected call/operator

* **Count:** 555 rows in 25 programs; 3 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_selected_callable.go:selectedCallableCandidate`.
* **Minimal examples:**
  * `stdlib/term/ansi.sg:50` — `xs[i]`
  * `stdlib/term/ansi.sg:84` — `tmp[i]`
  * `testdata/golden/core_stdlib/format.sg:79` — `fmt[i]`
* **Programs fully finished if only this group is fixed:** `benchmarks/native/channel_request_reply/main.sg`, `testdata/golden/sema/valid/import_all.sg`, `testdata/golden/sema/valid/module_multitest/main.sg`, `testdata/golden/stdlib/time_duration_conversions.sg`

### G3: index requires a non-scalar index transfer — index expression/store

* **Count:** 148 rows in 14 programs; 4 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_index.go:index / internal/sema/return_origin_index_primitive.go:indexOperation`.
* **Minimal examples:**
  * `testdata/golden/vm_maps/map_get_mut.sg:14` — `m["x"]`
  * `testdata/golden/vm_maps/map_index_get.sg:4` — `m["x"]`
  * `testdata/golden/vm_maps/map_index_set.sg:4` — `m["x"]`
* **Programs fully finished if only this group is fixed:** `testdata/golden/vm_maps/map_growth_boundary.sg`, `testdata/golden/vm_maps/map_index_get.sg`

### G4: borrowed temporary has no proven storage owner — implicit borrow/temporary

* **Count:** 14 rows in 7 programs; 4 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_expr.go:unary`.
* **Minimal examples:**
  * `testdata/golden/vm_maps/map_get_mut.sg:8` — `&"x"`
  * `stdlib/term/ansi.sg:127` — `&"38;5;"`
  * `stdlib/term/ansi.sg:135` — `&"48;5;"`
* **Programs fully finished if only this group is fixed:** `testdata/golden/mir/array_field_mut_ref_reborrow.sg`, `testdata/golden/sema/valid/stdlib_hash_api.sg`, `testdata/golden/vm_arrays/array_field_mut_ref_reborrow.sg`

### G5: an `async` or `blocking` block that captures a value which can hold a reference, a storage loan or a task needs its capture origin — capture/block

* **Count:** 5 rows in 5 programs; 2 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_task_blocks.go:taskBlock`.
* **Minimal examples:**
  * `testdata/golden/vm_async_suite/t22_select_wait_recv.sg:5` — `async {             sleep(5).await();             ch.send(7);             ret 0;         }`
  * `testdata/golden/vm_async_suite/t23_select_wait_timer.sg:5` — `async {             sleep(100).await();             ch.send(7);             ret 0;         }`
  * `stdlib/term/term.sg:61` — `blocking {         while true {             let ev = term_read_event();             let done = co…`
* **Programs fully finished if only this group is fixed:** `testdata/golden/vm_async_suite/t22_select_wait_recv.sg`, `testdata/golden/vm_async_suite/t23_select_wait_timer.sg`

### G6: outgoing reference has unresolved or captured provenance — reference-bearing expression

* **Count:** 1389 rows in 37 programs; 10 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_check.go:checkExpired`.
* **Minimal examples:**
  * `stdlib/json/json.sg:96` — `v`
  * `testdata/golden/sema/valid/fixed_array_view_operator_stays_in_frame.sg:41` — `v`
  * `showcases/28_generic_map_filter/main.sg:29` — `ys`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G7: function result contains an unproved source — call/return result

* **Count:** 263 rows in 31 programs; 8 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_summary.go:solveBodies`.
* **Minimal examples:**
  * `showcases/28_generic_map_filter/main.sg:9` — `-> T[]`
  * `testdata/golden/core_stdlib/map.sg:18` — `-> K[]`
  * `testdata/golden/sema/ownership_and_references/self_mut_field_index_set.sg:11` — `-> int`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G8: callee returned an unproved source — call/return result

* **Count:** 373 rows in 26 programs; 6 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_calls.go:call / internal/sema/return_origin_backing_calls.go:substituteBackingCall`.
* **Minimal examples:**
  * `testdata/golden/sema/valid/fixed_array_view_operator_stays_in_frame.sg:50` — `fresh()`
  * `testdata/golden/sema/valid/stdlib_uuid_api.sg:26` — `uuid.v4()`
  * `testdata/golden/sema/ownership_and_references/self_mut_field_index_set.sg:19` — `b.get(1, 1)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G9: store through a place needs reference-content transfer — place store

* **Count:** 12 rows in 9 programs; 6 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_place_store.go:placeStore`.
* **Minimal examples:**
  * `testdata/golden/vm_maps/map_index_set.sg:4` — `m["x"] = 10`
  * `testdata/golden/vm_maps/map_index_set.sg:5` — `m["x"] = 11`
  * `testdata/golden/sema/ownership_and_references/self_mut_field_reborrow.sg:8` — `cells[r][c] = v`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G10: projected borrowed payload needs precise origin facts — payload projection

* **Count:** 106 rows in 9 programs; 5 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_expr.go:exprCore`.
* **Minimal examples:**
  * `stdlib/json/parser.sg:85` — `p.data`
  * `stdlib/json/parser.sg:144` — `p.data`
  * `stdlib/json/parser.sg:206` — `p.data`
* **Programs fully finished if only this group is fixed:** `stdlib/fs/fs.sg`

### G11: generic index lacks its finalized concrete use — index expression/store

* **Count:** 111 rows in 17 programs; 4 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_index_primitive.go:indexOperation`.
* **Minimal examples:**
  * `stdlib/bytes/bytes.sg:127` — `data[i]`
  * `stdlib/bytes/bytes.sg:146` — `data[i]`
  * `stdlib/entropy/entropy.sg:21` — `data[i]`
* **Programs fully finished if only this group is fixed:** `testdata/golden/vm_hash/hash64_basic.sg`

### G12: index requires its selected container transfer — index expression/store

* **Count:** 12 rows in 10 programs; 4 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_index.go:index`.
* **Minimal examples:**
  * `testdata/golden/crossing/block02/valid/on_positive_dynamic_array_capture.sg:3` — `xs[0]`
  * `testdata/golden/crossing/block02/valid/on_positive_dynamic_array_capture.sg:3` — `xs[1]`
  * `testdata/golden/mir/imported_magic_methods.sg:31` — `bag[1]`
* **Programs fully finished if only this group is fixed:** `testdata/golden/vm_maps/map_composite_value.sg`

### G13: opaque result borrowed-state classification is unsupported — expression/transfer

* **Count:** 521 rows in 17 programs; 3 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_conditions.go:reportRequirements`.
* **Minimal examples:**
  * `stdlib/time/time.sg:33` — `sub`
  * `testdata/golden/core_stdlib/intrinsics.sg:254` — `new`
  * `testdata/golden/core_stdlib/intrinsics.sg:808` — `new`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G14: implicit borrow lacks an admitted borrow for this expression — implicit borrow/temporary

* **Count:** 78 rows in 10 programs; 3 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_calls.go:callArgumentOrigin`.
* **Minimal examples:**
  * `testdata/golden/vm_strings/strings_std.sg:51` — `""`
  * `benchmarks/native/byte_lines/main.sg:139` — `" "`
  * `benchmarks/native/byte_lines/main.sg:190` — `" "`
* **Programs fully finished if only this group is fixed:** `showcases/15_trim_split_join/main.sg`, `showcases/21_bigint_stress/main.sg`, `stdlib/path/path.sg`

### G15: generic original call argument disagrees with its substituted source signature — expression/transfer

* **Count:** 10 rows in 3 programs; 3 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_generic_signature.go:originalArgumentType`.
* **Minimal examples:**
  * `testdata/golden/sema/valid/array_view_facts_a_resize_rule_withdraws.sg:27` — `xs[1].push(9)`
  * `testdata/golden/vm_compare/counted_payload_clone_and_borrow.sg:44` — `opts.push(nothing)`
  * `testdata/golden/vm_compare/counted_payload_clone_and_borrow.sg:45` — `opts.push(Some(7))`
* **Programs fully finished if only this group is fixed:** `testdata/golden/sema/valid/array_view_facts_a_resize_rule_withdraws.sg`, `testdata/golden/vm_compare/counted_payload_clone_and_borrow.sg`, `testdata/golden/vm_compare/for_in_compare_reads_heap_free_union.sg`

### G16: conversion retains its actual expression for origin finalization — conversion

* **Count:** 124 rows in 20 programs; 2 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_expr.go:exprCore`.
* **Minimal examples:**
  * `testdata/golden/core_stdlib/format.sg:63` — `x to string`
  * `testdata/golden/core_stdlib/format.sg:64` — `x to string`
  * `testdata/golden/core_stdlib/format.sg:65` — `x to string`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G17: container loans lack a proven base — expression/transfer

* **Count:** 2 rows in 2 programs; 2 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_backing.go:containerLoans`.
* **Minimal examples:**
  * `testdata/golden/mir/magic_methods_repro.sg:27` — `self.values.__range()`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G18: mutable argument may replace reference-bearing contents — mutable call argument

* **Count:** 392 rows in 14 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_calls.go:call`.
* **Minimal examples:**
  * `stdlib/json/parser.sg:587` — `view_at_end(p)`
  * `stdlib/json/parser.sg:593` — `view_at_end(p)`
  * `stdlib/json/parser.sg:698` — `view_at_end(p)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G19: binary callable needs an exact origin contract — expression/transfer

* **Count:** 144 rows in 11 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_expr.go:binary`.
* **Minimal examples:**
  * `testdata/golden/sema/valid/fixed_array_view_operator_stays_in_frame.sg:23` — `a + b`
  * `testdata/golden/sema/valid/fixed_array_view_operator_stays_in_frame.sg:28` — `r + r`
  * `testdata/golden/sema/valid/fixed_array_view_operator_stays_in_frame.sg:34` — `a * b`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G20: reference loaded through another reference needs content provenance — reference dereference

* **Count:** 4 rows in 4 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_expr.go:unary`.
* **Minimal examples:**
  * `stdlib/json/parser.sg:53` — `*data`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G21: generic result contains an unproved source — expression/transfer

* **Count:** 12 rows in 3 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_declarations.go:checkGenericPromise`.
* **Minimal examples:**
  * `testdata/golden/vm_arrays/arrays_drop_nested.sg:101` — `fixed.to_array()`
  * `testdata/golden/crossing/integration/valid/_integration_generic_crossing_sites.sg:9` — `route::<int>(dst)`
  * `testdata/golden/crossing/integration/valid/_integration_generic_crossing_sites.sg:13` — `route::<bool>(dst)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G22: generic use has duplicate or contradictory finalized authority — generic call/use

* **Count:** 8 rows in 2 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_generics.go:checkGenericUses`.
* **Minimal examples:**
  * `testdata/golden/sema/valid/array_helpers.sg:13` — `a to string`
  * `testdata/golden/sema/valid/array_helpers.sg:14` — `b to string`
  * `testdata/golden/sema/valid/array_helpers.sg:15` — `c to string`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G23: storage loan would be discarded by a payload-free value — expression/transfer

* **Count:** 4 rows in 2 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_backing.go:discardLoans`.
* **Minimal examples:**
  * `testdata/golden/vm_intrinsics/array_range_panics.sg:35` — `sum_range(f.__range())`
  * `testdata/golden/vm_intrinsics/array_range_panics.sg:30` — `sum_range(a0.__range())`
  * `testdata/golden/vm_intrinsics/array_range_panics.sg:33` — `sum_range(view.__range())`
* **Programs fully finished if only this group is fixed:** `testdata/golden/vm_intrinsics/array_range_panics.sg`

### G24: deferred clone needs its selected non-Copy body and effect transfer — expression/transfer

* **Count:** 4 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_clone_authority.go:cloneOutcome`.
* **Minimal examples:**
  * `core/array.sg:123` — `clone(self[i])`
  * `core/array.sg:124` — `clone(self[j])`
  * `core/array.sg:290` — `clone(self[i])`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G25: index store requires a scalar index into its canonical container — index expression/store

* **Count:** 2 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_index_store.go:indexStoreOperation`.
* **Minimal examples:**
  * `testdata/golden/vm_maps/map_index_set.sg:4` — `m["x"]`
  * `testdata/golden/vm_maps/map_index_set.sg:5` — `m["x"]`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G26: an `on` crossing capture that can hold a reference, a storage loan or a task needs its capture origin — capture/block

* **Count:** 1 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_on_crossing.go:onCrossing`.
* **Minimal examples:**
  * `testdata/golden/crossing/block02/valid/on_positive_dynamic_array_capture.sg:9` — `xs`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G27: call needs an exact body, canonical core contract, or opaque declaration promise — expression/transfer

* **Count:** 1 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_calls.go:call`.
* **Minimal examples:**
  * `testdata/golden/mir/imported_magic_methods.sg:13` — `clone(original)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G28: callable identifier lacks concrete source facts — expression/transfer

* **Count:** 1 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_callable.go:callableIdent`.
* **Minimal examples:**
  * `testdata/golden/mir/imported_magic_methods.sg:13` — `clone`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G29: callable value needs its concrete original type and alias authority — expression/transfer

* **Count:** 1 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_callable.go:callableType`.
* **Minimal examples:**
  * `testdata/golden/mir/imported_magic_methods.sg:13` — `clone`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G30: deferred method may change reference-bearing or callable contents — expression/transfer

* **Count:** 1 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_deferred_method_authority.go:methodOutcome`.
* **Minimal examples:**
  * `core/base.sg:85` — `self.__len()`
* **Programs fully finished if only this group is fixed:** `testdata/golden/abi/abi_string_bytesview.sg`

### G31: module member value needs its selected free-function authority — expression/transfer

* **Count:** 1 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_expr.go:exprCore`.
* **Minimal examples:**
  * `testdata/golden/sema/valid/json_method_jsonvalue_param.sg:15` — `json.JsonString`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G32: tag constructor lacks its original declaration target — tag constructor

* **Count:** 1 rows in 1 programs; 1 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_tags.go:tagPayload`.
* **Minimal examples:**
  * `testdata/golden/sema/valid/json_method_jsonvalue_param.sg:15` — `json.JsonString("x")`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G33: generic use lacks its exact original callable declarations — generic call/use

* **Count:** 99 rows in 15 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_generics.go:genericUseContext`.
* **Minimal examples:**
  * `stdlib/term/ansi.sg:50` — `xs[i]`
  * `stdlib/hash/hash.sg:40` — `out[i]`
  * `stdlib/term/ansi.sg:84` — `tmp[i]`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G34: opaque call may change reference-bearing or callable contents — expression/transfer

* **Count:** 262 rows in 11 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_calls.go:call`.
* **Minimal examples:**
  * `showcases/28_generic_map_filter/main.sg:4` — `f(&x)`
  * `showcases/28_generic_map_filter/main.sg:12` — `f(&x)`
  * `testdata/golden/core_stdlib/array.sg:250` — `out.__len()`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G35: range literal lacks its original builtin constructor certificate — expression/transfer

* **Count:** 130 rows in 10 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_range.go:rangeLiteralOperation`.
* **Minimal examples:**
  * `testdata/golden/core_stdlib/string.sg:164` — `[i..i + 1]`
  * `testdata/golden/core_stdlib/string.sg:288` — `[i..i + 1]`
  * `testdata/golden/core_stdlib/string.sg:175` — `[start..idx]`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G36: deferred clone requires a live local storage referent — expression/transfer

* **Count:** 40 rows in 10 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_clone.go:cloneBindingContents`.
* **Minimal examples:**
  * `testdata/golden/core_stdlib/array.sg:123` — `clone(self[i])`
  * `testdata/golden/core_stdlib/array.sg:124` — `clone(self[j])`
  * `testdata/golden/core_stdlib/array.sg:290` — `clone(self[i])`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G37: parameter requires concrete type or callable provenance — expression/transfer

* **Count:** 10 rows in 10 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_summary.go:analyze`.
* **Minimal examples:**
  * `testdata/golden/core_stdlib/array.sg:78` — `(r: Range<T>)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G38: captured binding requires origin finalization — capture/block

* **Count:** 82 rows in 7 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_expr.go:exprCore`.
* **Minimal examples:**
  * `showcases/24_option_pipeline/main.sg:4` — `Some`
  * `stdlib/hash/hash.sg:31` — `Some`
  * `stdlib/json/parser.sg:196` — `Success`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G39: generic use disagrees with its finalized callee instance — generic call/use

* **Count:** 99 rows in 6 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_generics.go:genericInstanceKind`.
* **Minimal examples:**
  * `stdlib/json/stringify.sg:194` — `keys[i]`
  * `stdlib/json/stringify.sg:103` — `strings[j]`
  * `stdlib/json/stringify.sg:104` — `strings[j]`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G40: tag constructor lacks its exact owning source declaration — tag constructor

* **Count:** 81 rows in 6 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_tags.go:tagPayload`.
* **Minimal examples:**
  * `stdlib/json/parser.sg:388` — `Success(s)`
  * `stdlib/json/parser.sg:489` — `Success(s)`
  * `stdlib/json/parser.sg:891` — `Success(obj)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G41: an `on` crossing reply that can hold a reference, a storage loan or a task needs its reply origin — expression/transfer

* **Count:** 1 rows in 1 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_on_crossing.go:onCrossing`.
* **Minimal examples:**
  * `testdata/golden/crossing/integration/valid/_integration_generic_crossing_sites.sg:3` — `on dst {         ret default::<T>();     }`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G42: deferred clone requires its direct template shared receiver — expression/transfer

* **Count:** 1 rows in 1 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_clone.go:originalClone`.
* **Minimal examples:**
  * `showcases/28_generic_map_filter/main.sg:12` — `clone(x)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G43: generic opaque use requires its type-dependent effect transfer — expression/transfer

* **Count:** 1 rows in 1 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_declarations.go:checkGenericPromise`.
* **Minimal examples:**
  * `testdata/golden/core_stdlib/array.sg:10` — `rt_array_push(a, value)`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

### G44: tag constructor disagrees with its source payload slots — tag constructor

* **Count:** 1 rows in 1 programs; 0 of the 33 valid goldens.
* **Analysis site:** `internal/sema/return_origin_tags.go:tagPayload`.
* **Minimal examples:**
  * `showcases/24_option_pipeline/main.sg:4` — `Some(xs[idx])`
* **Programs fully finished if only this group is fixed:** _none_; every affected program also has another group.

## Sequencing and validation

Land the top five as separate compiler packets in the order above, re-running the full 1,166-file census after each. Do not interpret removal of a downstream “unproved source” row as proof by itself: every packet needs positive origin assertions and a counterfactual test that removing the new certificate restores the exact pending row. After the five, re-rank using the regenerated `rows.json`; the large outgoing-reference, callee-result, and function-result buckets are mostly terminal symptoms and should shrink when upstream certificates become available.
