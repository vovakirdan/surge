# Owner question 3: temporaries lent to a callee

Everything here was measured unless it says "(read from code)".

## Plain words

A string literal or other rvalue passed where the callee wants `&string` is materialised as a temporary that dies at the end of the statement. Return-origin admits that temporary only when the call provably keeps nothing. Today "keeps nothing" is read from the signature of a synchronous core declaration: a reference-free result with no loan-carrying part, and no `&mut` loan sink. There are two groups of rows.

- **(a) Core callees whose signature says they could keep something, although their body does not.**
  - `vm_strings/strings_std.sg:46` and `:51`: `parts_src.split(",")` and `chars_src.split("")`. The result `string[]` is a loan carrier by signature.
  - `vm_strings/strings_rope_std.sg:36`: `r.split(",")`.
  - `vm_maps/map_get_mut.sg:8`: `m.get_mut(&"x")`. The result `Option<&mut V>` is a loan carrier by signature.
- **(b) User and stdlib callees, which is residual R-c of RV2-DEBT-365.**
  - `mir/array_field_mut_ref_reborrow.sg:14-15` and `vm_arrays/array_field_mut_ref_reborrow.sg:15-16`: `add_borrower(&mut entry, &"client-a")`.
  - `sema/valid/stdlib_hash_api.sg:27-30`: `h.begin_record(&"CacheKey", 2:uint)`, `h.write_field(&"tenant")`.
  - `sema/valid/json_method_jsonvalue_param.sg`: 17 rows in `stdlib/json/parser.sg`, for example line 781 `view_consume_literal(p, "null")`, and in `stdlib/json/stringify.sg`, for example line 48 `append_string_bytes(out, "\\\"")`.

## The invariants in the way

- **`internal/driver/return_origin_string_temporary_test.go:12-17`.** "The obligation that used to stand on every such argument is answered only where the call can keep nothing: a reference-free result with no loan-carrying and no borrow-hiding part (a raw pointer, a Task or another core runtime handle), effects of the same kind, and no `&mut` loan sink."
- **The same file, lines 74-75.** Rows `core_loan_carrier_result` (`s.split("," + t)`) and `core_loan_sink_effect` (`out.append_string("g" + t)`) pin that the signature alone decides for core callees too.
- **DEBT.md:232, RV2-DEBT-368.** "A function that starts a task from its string formal shows nothing in its signature ... the task check pins only captures that have a place and a materialised temporary has none (`internal/sema/task_borrow_call.go:58-74`) ... so the unfinished row at the argument is the only compile-time fence in front of that program". The closing condition is "The task check ..., once it can publish per formal that the formal reaches no task".

## What the prototype measured for group (a)

N-CORE-TEMP lets the core body's summary answer instead of the signature. The summary must never name the slot in its result value, its post-state cells or its post-state backings. With it, `strings_std.sg` and `strings_rope_std.sg` diagnose clean, together with N-CONCAT-USE, and both run to their golden `.out` on the VM and on LLVM. `map_get_mut.sg` loses its temporary row but still needs the Map index packet. The two pinned rows above turn red, and no other driver or tripwire test changes.

## Options

| Option | Frees | Cost | Risk |
|---|---:|---|---|
| A. Wait for the task check to publish "this formal reaches no task", as DEBT-368 plans, then admit through the callee's body summary | 7, once the task check lands | task check M to L; return-origin S | None beyond DEBT-368's own plan. Nothing moves until then. |
| B. For group (a) only, let a synchronous core callee's body summary answer, flipping the two pinned rows | 3 (2 now, `map_get_mut` after the Map packet) | S, prototype 76 lines | Relies on core bodies starting no task. DEBT-368 already trusts synchronous core declarations for that, so the change is the conjunct used, not the trust. |
| C. Rewrite the call sites to bind the temporary first, as DEBT-368 suggests: `let key = "CacheKey"; h.begin_record(&key, ...)` | 7 | S: 2 golden programs, `stdlib_hash_api.sg`, 17 sites in `stdlib/json`, 4 core-callee sites in 3 golden programs | Changes stdlib and golden sources. The language is not widened. |

## The question

(a) May a synchronous core callee's body summary, rather than its signature alone, admit a lent temporary (option B, flipping `core_loan_carrier_result` and `core_loan_sink_effect`)? (b) For user and stdlib callees, should D2 wait for the task check's per-formal fact (A), or should the call sites be rewritten to bind the temporary first (C)?
