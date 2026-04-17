# Entropy / Random / UUID Implementation Plan

## 1. Planning Inputs

- PRD: not applicable; this slice is driven by a platform spec rather than a product PRD.
- Stack: Surge stdlib, compiler frontend, VM runtime, LLVM backend, native runtime in C, Go test harness.
- Architecture: [2026-04-17-entropy-random-uuid-specs.md](/home/zov/projects/surge/surge/docs/2026-04-17-entropy-random-uuid-specs.md).
- Project rules: preserve existing `core/` public surface unless implementation constraints force a documented exception; keep runtime-backed surface minimal; prefer library logic in Surge over host intrinsics.
- Current repo state: `stdlib` already contains runtime-backed modules, VM replay machinery exists, LLVM backend uses explicit builtin registration and intrinsic dispatch, native runtime exposes functions through `runtime/native/rt.h`.
- Conversation-specific decisions:
  - the feature is standalone and not tied to any consumer;
  - no weak randomness fallback is allowed;
  - deterministic seeded PRNG ships in v1;
  - first seeded PRNG is `Pcg32`;
  - first UUID slice is RFC-compatible `v4`, parse, format, and nil UUID;
  - `uuid.v7()` is deferred;
  - `entropy.fill(out)` is public, but v1 may implement it as a pure wrapper over fresh entropy bytes.

## 2. Delivery Context

- System shape: one new capability family spanning four layers:
  - stdlib modules: `entropy`, `random`, `uuid`;
  - VM runtime and record/replay;
  - LLVM builtin registration and intrinsic lowering;
  - native runtime host-entropy bridge.
- First usable slice: host entropy works in VM/native/LLVM and is exposed as `stdlib/entropy.bytes(len)` plus `fill(out)`.
- Team or agent execution model: one engineer can execute serially, but backend wiring and stdlib work can split into parallel workstreams after the intrinsic contract is frozen.
- Delivery constraints:
  - preserve determinism in VM replay mode;
  - avoid expanding `core/` without a clear need;
  - do not add general pointer or mutable-byte-view APIs;
  - keep public API names consistent with existing naming in the repo;
  - keep behavior aligned across VM and LLVM/native execution.
- Quality bar for the slice:
  - same observable behavior on VM and LLVM/native;
  - deterministic replay for entropy-consuming programs;
  - no cryptographic downgrade path;
  - stdlib API is documented and directly usable without backend knowledge;
  - tests include algorithm vectors, error paths, and backend parity.

## 3. MVP Scope Framing

- Must-have capabilities:
  - secure host entropy retrieval;
  - public `entropy.bytes()` and `entropy.fill()`;
  - `random.RandomSource<T>`;
  - `random.SystemRng`;
  - deterministic `random.Pcg32`;
  - `uuid.Uuid`, `uuid.nil()`, `uuid.parse()`, `uuid.v4()`, `uuid.v4_from(...)`, formatting helpers;
  - VM record/replay coverage for entropy.
- Deferred capabilities:
  - `uuid.v7()`;
  - distributions and helpers like ranged integers or shuffles;
  - public wall-clock API;
  - optimized in-place entropy intrinsic;
  - additional deterministic PRNG families.
- Core assumptions:
  - existing module layout supports new stdlib subdirectories;
  - one private runtime-backed intrinsic is sufficient;
  - `Error` remains the common error carrier;
  - array allocation and byte copying are adequate for v1 performance.

## 4. Task Decomposition Principles

- How tasks were grouped:
  - first by cross-layer contract freeze;
  - then by backend/runtime enablement;
  - then by stdlib modules;
  - finally by tests and public docs.
- Granularity standard:
  - each task should either freeze a contract or land a coherent vertical slice that can be validated independently.
- Parallelism standard:
  - no backend work starts until the intrinsic contract and symbol strategy are frozen;
  - once frozen, native runtime, VM runtime, LLVM lowering, and pure stdlib tasks can run in parallel with low merge risk.
- Validation standard:
  - every task must name the concrete tests or command paths that prove it is complete;
  - backend work is not done until there is at least one program-level test that crosses the new surface.

## 5. Task List

### 5.1 Foundation Tasks

#### TASK-01 — Freeze Intrinsic Contract and Module Layout

- Type: architecture / compiler integration
- Goal: lock down the exact intrinsic name, declaration location, and stdlib file layout before implementation branches diverge.
- Scope:
  - confirm whether the entropy intrinsic can remain private to `stdlib/entropy` or must be declared in `core/intrinsics.sg`;
  - fix the final symbol spelling used by VM and LLVM dispatch;
  - choose final stdlib file layout for `entropy`, `random`, and `uuid`.
- Non-goals:
  - implementing host entropy itself;
  - writing full stdlib logic.
- Inputs / dependencies:
  - [2026-04-17-entropy-random-uuid-specs.md](/home/zov/projects/surge/surge/docs/2026-04-17-entropy-random-uuid-specs.md);
  - current intrinsic handling in [internal/vm/intrinsic.go](/home/zov/projects/surge/surge/internal/vm/intrinsic.go);
  - current LLVM intrinsic lowering in [internal/backend/llvm/emit_intrinsics_runtime.go](/home/zov/projects/surge/surge/internal/backend/llvm/emit_intrinsics_runtime.go).
- Outputs:
  - final contract notes embedded into implementation docs;
  - if required, a small spec adjustment documenting the chosen declaration strategy.
- Interfaces / contracts touched:
  - intrinsic name and declaration site;
  - module paths and import expectations.
- Files or areas likely affected:
  - [docs/2026-04-17-entropy-random-uuid-specs.md](/home/zov/projects/surge/surge/docs/2026-04-17-entropy-random-uuid-specs.md);
  - [core/intrinsics.sg](/home/zov/projects/surge/surge/core/intrinsics.sg) only if the repo architecture forces it.
- Definition of Done:
  - there is no remaining ambiguity about symbol name, declaration placement, or stdlib layout.
- Suggested validation:
  - manual review against existing `term` and `fs` intrinsic patterns.
- Parallelizable: no
- Can be delegated to subagent: no
- Priority: P0
- Risks / notes:
  - this is the main hidden coupling point between the spec and the actual compiler/runtime wiring.

### 5.2 Capability or Vertical Slice Tasks

#### TASK-02 — Add Native Runtime Host Entropy Primitive

- Type: runtime / C
- Goal: expose secure host entropy from the native runtime without any weak fallback path.
- Scope:
  - add a new runtime function to [runtime/native/rt.h](/home/zov/projects/surge/surge/runtime/native/rt.h);
  - implement host entropy acquisition in a new runtime C unit or the nearest appropriate runtime file;
  - return an error-shaped runtime object on failure instead of degrading.
- Non-goals:
  - deterministic PRNG logic;
  - UUID formatting or parsing.
- Inputs / dependencies:
  - TASK-01 contract freeze;
  - existing runtime return conventions used by `rt_fs_*` and terminal helpers.
- Outputs:
  - native runtime symbol available to LLVM-generated programs.
- Interfaces / contracts touched:
  - native runtime ABI surface;
  - result object layout conventions for stdlib-facing runtime calls.
- Files or areas likely affected:
  - [runtime/native/rt.h](/home/zov/projects/surge/surge/runtime/native/rt.h);
  - one new or existing `runtime/native/rt_*.c` implementation file;
  - build wiring if the new file is separate.
- Definition of Done:
  - native runtime can produce secure bytes or an explicit error object;
  - zero-length request succeeds.
- Suggested validation:
  - targeted native/runtime unit coverage if present;
  - end-to-end program test through LLVM/native path once TASK-05 and TASK-06 land.
- Parallelizable: yes, after TASK-01
- Can be delegated to subagent: yes
- Priority: P0
- Risks / notes:
  - platform-specific entropy source selection needs careful review;
  - error-path behavior must match VM behavior closely enough for stdlib parity.

#### TASK-03 — Add VM Runtime Entropy API and Intrinsic Dispatch

- Type: runtime / VM
- Goal: make entropy available in VM execution before replay concerns are layered in.
- Scope:
  - extend the VM `Runtime` interface with an entropy method;
  - implement it in `DefaultRuntime` and `TestRuntime`;
  - add VM intrinsic handling for the new call shape.
- Non-goals:
  - replay logging;
  - LLVM lowering;
  - stdlib wrappers.
- Inputs / dependencies:
  - TASK-01 contract freeze.
- Outputs:
  - VM runtime method for entropy bytes;
  - VM intrinsic dispatch path for the new symbol.
- Interfaces / contracts touched:
  - [internal/vm/runtime.go](/home/zov/projects/surge/surge/internal/vm/runtime.go);
  - [internal/vm/intrinsic.go](/home/zov/projects/surge/surge/internal/vm/intrinsic.go).
- Files or areas likely affected:
  - [internal/vm/runtime.go](/home/zov/projects/surge/surge/internal/vm/runtime.go);
  - [internal/vm/intrinsic.go](/home/zov/projects/surge/surge/internal/vm/intrinsic.go);
  - one new `internal/vm/intrinsic_*.go` helper or the closest intrinsic file.
- Definition of Done:
  - VM can execute the entropy intrinsic in normal execution mode;
  - `DefaultRuntime` and `TestRuntime` expose the minimal new API cleanly.
- Suggested validation:
  - focused VM runtime tests for success, zero-length, and error behavior.
- Parallelizable: yes, after TASK-01
- Can be delegated to subagent: yes
- Priority: P0
- Risks / notes:
  - runtime interface growth should stay minimal and not leak consumer-specific semantics.

#### TASK-04 — Add Replay Logging for Entropy

- Type: runtime / determinism
- Goal: preserve deterministic record/replay behavior for entropy-consuming programs.
- Scope:
  - implement entropy support in `RecordingRuntime` and replay runtime paths;
  - record exact entropy bytes in logs and replay them byte-for-byte;
  - add or reuse explicit log formatting helpers for byte payloads.
- Non-goals:
  - VM intrinsic dispatch itself;
  - LLVM lowering;
  - stdlib wrappers.
- Inputs / dependencies:
  - TASK-03;
  - existing replay patterns for `monotonic_now`, `term_size`, and `term_read_event`.
- Outputs:
  - recorder/replayer support for the new intrinsic.
- Interfaces / contracts touched:
  - replay log shape in [internal/vm/record.go](/home/zov/projects/surge/surge/internal/vm/record.go) and [internal/vm/replay.go](/home/zov/projects/surge/surge/internal/vm/replay.go);
  - log helpers in [internal/vm/logfmt.go](/home/zov/projects/surge/surge/internal/vm/logfmt.go).
- Files or areas likely affected:
  - [internal/vm/runtime.go](/home/zov/projects/surge/surge/internal/vm/runtime.go);
  - [internal/vm/record.go](/home/zov/projects/surge/surge/internal/vm/record.go);
  - [internal/vm/replay.go](/home/zov/projects/surge/surge/internal/vm/replay.go);
  - [internal/vm/logfmt.go](/home/zov/projects/surge/surge/internal/vm/logfmt.go).
- Definition of Done:
  - recording mode logs bytes deterministically;
  - replay mode consumes logged bytes and does not consult host randomness;
  - malformed replay data fails loudly.
- Suggested validation:
  - replay-focused regression test proving byte-for-byte reproduction.
- Parallelizable: yes, after TASK-03
- Can be delegated to subagent: yes
- Priority: P0
- Risks / notes:
  - replay log encoding for `byte[]` needs to be explicit and stable.

#### TASK-05 — Add LLVM Builtin Registration and Intrinsic Lowering

- Type: compiler backend / LLVM
- Goal: lower the entropy intrinsic correctly in LLVM-generated code.
- Scope:
  - add builtin declaration for the native runtime symbol;
  - teach runtime intrinsic lowering to recognize the Surge intrinsic and emit the native call;
  - route any result object handling through existing conventions.
- Non-goals:
  - VM behavior;
  - stdlib algorithm logic.
- Inputs / dependencies:
  - TASK-01 contract freeze;
  - TASK-02 native runtime symbol.
- Outputs:
  - LLVM backend knows how to emit entropy calls.
- Interfaces / contracts touched:
  - builtin signature map;
  - intrinsic dispatch in runtime lowering.
- Files or areas likely affected:
  - [internal/backend/llvm/builtins.go](/home/zov/projects/surge/surge/internal/backend/llvm/builtins.go);
  - [internal/backend/llvm/emit_intrinsics_runtime.go](/home/zov/projects/surge/surge/internal/backend/llvm/emit_intrinsics_runtime.go);
  - possibly one additional `emit_intrinsics_*.go` file if the logic is split out.
- Definition of Done:
  - LLVM path builds and emits a correct call sequence for the new intrinsic.
- Suggested validation:
  - LLVM parity or smoke test using a tiny program that calls `entropy.bytes()`;
  - backend-specific regression if a dedicated intrinsic test exists.
- Parallelizable: yes, after TASK-01
- Can be delegated to subagent: yes
- Priority: P0
- Risks / notes:
  - name matching currently mixes raw runtime names and friendly intrinsic names; this is why TASK-01 is mandatory.

### 5.3 Component or Surface Tasks

#### TASK-06 — Implement `stdlib/entropy`

- Type: stdlib / API
- Goal: provide the smallest public entropy surface on top of the runtime primitive.
- Scope:
  - create the new stdlib module;
  - expose `bytes(len)` and `fill(out)`;
  - define module-local error code constants;
  - keep `fill(out)` as a pure wrapper unless implementation evidence justifies more.
- Non-goals:
  - `RandomSource<T>`;
  - deterministic PRNG;
  - UUID logic.
- Inputs / dependencies:
  - TASK-01 contract freeze;
  - TASK-03 VM intrinsic support or TASK-05 LLVM lowering for end-to-end validation.
- Outputs:
  - public entropy module usable from Surge code.
- Interfaces / contracts touched:
  - `Erring<byte[], Error>`;
  - public entropy error code constants.
- Files or areas likely affected:
  - `stdlib/entropy/entropy.sg` or the final layout chosen in TASK-01;
  - optional `stdlib/entropy/intrinsics.sg` if the private intrinsic is split out;
  - test programs exercising the chosen public module path.
- Definition of Done:
  - zero-length success path exists;
  - error codes are stable and documented;
  - `fill(out)` preserves output length and overwrites the buffer.
- Suggested validation:
  - new stdlib-facing VM tests;
  - one program-level test covering both `bytes()` and `fill()` and proving the intended import path.
- Parallelizable: yes, after TASK-01
- Can be delegated to subagent: yes
- Priority: P0
- Risks / notes:
  - the final declaration site of the intrinsic depends on TASK-01.

#### TASK-07 — Implement `random.RandomSource<T>` and `SystemRng`

- Type: stdlib / API
- Goal: define the reusable randomness contract and host-backed RNG implementation.
- Scope:
  - introduce `RandomSource<T>`;
  - add `SystemRng`;
  - implement `fill`, `next_u32`, and `next_u64`;
  - add module-level convenience wrappers that delegate to `SystemRng`.
- Non-goals:
  - deterministic PRNG implementation;
  - UUID API.
- Inputs / dependencies:
  - TASK-06 `stdlib/entropy`.
- Outputs:
  - reusable RNG contract consumed by `uuid` and future stdlib features.
- Interfaces / contracts touched:
  - `RandomSource<T>`;
  - `SystemRng`;
  - byte-to-integer decoding rules.
- Files or areas likely affected:
  - `stdlib/random/random.sg`;
  - optional `stdlib/random/system.sg`;
  - optional `stdlib/random/imports.sg`.
- Definition of Done:
  - `SystemRng` fully satisfies the public contract;
  - integer assembly order is fixed and documented in code comments where needed.
- Suggested validation:
  - focused stdlib tests comparing byte packing with expected integer values.
- Parallelizable: yes, after TASK-06
- Can be delegated to subagent: yes
- Priority: P1
- Risks / notes:
  - little-endian byte interpretation must stay consistent with the spec and tests.

#### TASK-08 — Implement Deterministic `random.Pcg32`

- Type: stdlib / algorithm
- Goal: provide a stable seeded PRNG for reproducible tests and fixtures.
- Scope:
  - add `Pcg32` state type;
  - implement constructors;
  - implement `fill`, `next_u32`, and derived `next_u64`;
  - freeze algorithm constants and state transition rules.
- Non-goals:
  - cryptographic randomness;
  - additional PRNG families.
- Inputs / dependencies:
  - TASK-07 contract definitions.
- Outputs:
  - deterministic RNG usable through the same `RandomSource<T>` contract.
- Interfaces / contracts touched:
  - `Pcg32` public API;
  - deterministic algorithm constants.
- Files or areas likely affected:
  - `stdlib/random/pcg32.sg`;
  - `stdlib/random/random.sg` if contract and constructors live there.
- Definition of Done:
  - fixed seeds produce fixed outputs across executions and backends.
- Suggested validation:
  - reference vector tests;
  - parity test that the same seed yields the same bytes on VM and LLVM/native.
- Parallelizable: yes, after TASK-07
- Can be delegated to subagent: yes
- Priority: P1
- Risks / notes:
  - once public, the sequence effectively becomes part of the platform contract.

#### TASK-09 — Implement `stdlib/uuid`

- Type: stdlib / API
- Goal: provide a compact UUID surface that is fully layered on top of `random`.
- Scope:
  - add `Uuid` binary type;
  - implement `nil`, parse, string formatting, `is_nil`;
  - implement `v4()` via `SystemRng`;
  - implement `v4_from<T: RandomSource<T>>`.
- Non-goals:
  - `v7`;
  - hostname, time-based, or namespace UUID variants.
- Inputs / dependencies:
  - TASK-07;
  - TASK-08 for deterministic test paths.
- Outputs:
  - public UUID module with parse/format/generation.
- Interfaces / contracts touched:
  - `Uuid` public type;
  - UUID parse and formatting rules;
  - version and variant bit handling.
- Files or areas likely affected:
  - `stdlib/uuid/uuid.sg`.
- Definition of Done:
  - parse accepts canonical RFC-style textual form;
  - formatter emits canonical lowercase text;
  - `v4()` sets correct version and variant bits.
- Suggested validation:
  - parse/format round-trip tests;
  - invalid-input tests;
  - deterministic `v4_from(Pcg32)` golden-style vectors.
- Parallelizable: yes, after TASK-07
- Can be delegated to subagent: yes
- Priority: P1
- Risks / notes:
  - parse behavior must be strict enough to avoid later compatibility drift.

### 5.4 Testing, Hardening, and Release-Readiness Tasks

#### TASK-10 — Add Cross-Backend Program-Level Tests

- Type: tests / integration
- Goal: prove the public APIs behave correctly as imported user code on VM and LLVM/native.
- Scope:
  - add focused program-level tests for `entropy`, `SystemRng`, `Pcg32`, and UUID;
  - add parity or smoke coverage for LLVM/native;
  - verify the chosen public module paths compile cleanly.
- Non-goals:
  - replay-specific regression depth;
  - public docs.
- Inputs / dependencies:
  - TASK-03 through TASK-09.
- Outputs:
  - regression coverage that exercises the user-visible surface end-to-end.
- Interfaces / contracts touched:
  - public stdlib APIs;
  - module import paths.
- Files or areas likely affected:
  - [internal/vm/llvm_parity_test.go](/home/zov/projects/surge/surge/internal/vm/llvm_parity_test.go);
  - [internal/vm/llvm_smoke_test.go](/home/zov/projects/surge/surge/internal/vm/llvm_smoke_test.go);
  - one or more new targeted `internal/vm/*_test.go` files;
  - possibly new `testdata/golden/...` fixtures if golden tests are the best fit for the public API surface.
- Definition of Done:
  - failures clearly localize whether a regression is in stdlib, runtime, or backend lowering.
- Suggested validation:
  - full affected Go test packages;
  - targeted parity test invocation.
- Parallelizable: mostly no, until core implementation tasks land
- Can be delegated to subagent: yes
- Priority: P0
- Risks / notes:
  - test quality is the main defense against silent behavior drift.

#### TASK-11 — Harden Edge Cases and Replay Regression

- Type: hardening
- Goal: close edge cases and prove replay determinism for entropy-consuming programs.
- Scope:
  - zero-length entropy;
  - large buffer handling sanity;
  - strict UUID parse rejection cases;
  - deterministic PRNG seed edge cases;
  - backend error mapping consistency;
  - replay regression tests for `entropy.bytes()` and `uuid.v4()`.
- Non-goals:
  - new functionality beyond the approved scope.
- Inputs / dependencies:
  - TASK-04;
  - TASK-06 through TASK-10.
- Outputs:
  - explicit edge-case handling and replay-focused tests.
- Interfaces / contracts touched:
  - `Error.code` stability and messaging;
  - parse/format semantics;
  - replay log serialization.
- Files or areas likely affected:
  - stdlib module files;
  - [internal/vm/vm_replay_test.go](/home/zov/projects/surge/surge/internal/vm/vm_replay_test.go);
  - [internal/vm/vm_golden_update_determinism_test.go](/home/zov/projects/surge/surge/internal/vm/vm_golden_update_determinism_test.go);
  - targeted tests.
- Definition of Done:
  - known edge cases are covered either in code or tests, ideally both;
  - replay proves host entropy is not consulted once bytes are recorded.
- Suggested validation:
  - targeted replay and edge-case tests for each family.
- Parallelizable: partial
- Can be delegated to subagent: yes
- Priority: P1
- Risks / notes:
  - backend-specific failure mapping and replay payload encoding are the most likely sources of subtle inconsistency.

### 5.5 Documentation and Developer-Experience Tasks

#### TASK-12 — Document the New Stdlib and Runtime Behavior

- Type: docs
- Goal: document the public APIs and the runtime caveats introduced by this slice.
- Scope:
  - add or update docs describing module purpose and public API;
  - document replay semantics for host entropy;
  - mention cryptographic-vs-deterministic randomness split.
- Non-goals:
  - writing tutorials for deferred features.
- Inputs / dependencies:
  - final APIs from TASK-06 through TASK-09.
- Outputs:
  - user-facing docs aligned with shipped behavior.
- Interfaces / contracts touched:
  - module docs and runtime docs.
- Files or areas likely affected:
  - [docs/MODULES.md](/home/zov/projects/surge/surge/docs/MODULES.md);
  - [docs/MODULES.ru.md](/home/zov/projects/surge/surge/docs/MODULES.ru.md);
  - [docs/RUNTIME.md](/home/zov/projects/surge/surge/docs/RUNTIME.md);
  - [docs/RUNTIME.ru.md](/home/zov/projects/surge/surge/docs/RUNTIME.ru.md);
  - possibly [docs/LANGUAGE.md](/home/zov/projects/surge/surge/docs/LANGUAGE.md) only if examples or standard-library references need updating.
- Definition of Done:
  - public docs do not mention behavior that differs from the shipped implementation.
- Suggested validation:
  - doc review against tests and shipped APIs.
- Parallelizable: yes, once APIs stop moving
- Can be delegated to subagent: yes
- Priority: P2
- Risks / notes:
  - documentation must not overpromise `uuid.v7()` or optimized fill paths.

## 6. Dependency Graph and Execution Order

### 6.1 Critical Path

1. TASK-01 — freeze intrinsic contract and module layout.
2. TASK-02 / TASK-03 / TASK-04 / TASK-05 — platform enablement once the contract is frozen.
3. TASK-06 — `stdlib/entropy`.
4. TASK-07 — `random.RandomSource<T>` and `SystemRng`.
5. TASK-08 / TASK-09 — deterministic PRNG and UUID layer.
6. TASK-10 / TASK-11 / TASK-12 — regression coverage, hardening, docs.

### 6.2 Parallel Workstreams

- Workstream A: native runtime entropy plumbing.
- Workstream B: VM runtime, recorder, replay, and intrinsic dispatch.
- Workstream C: LLVM builtin and lowering.
- Workstream D: pure Surge stdlib work once `entropy` is usable.

### 6.3 Early Integration Points

- intrinsic declaration site decision from TASK-01;
- replay log encoding for entropy bytes;
- first program that imports `entropy` and runs on both VM and LLVM/native.

### 6.4 Risky Tasks

- TASK-01 because it can force a spec correction if private stdlib intrinsics are more constrained than expected;
- TASK-04 because replay determinism is easy to accidentally weaken;
- TASK-05 because LLVM lowering depends on exact symbol naming and result conventions;
- TASK-09 because UUID parse strictness becomes long-lived public behavior.

## 7. Recommended Roadmap

### 7.1 Phases

- Phase 1: contract freeze and backend enablement.
- Phase 2: `entropy` and `SystemRng`.
- Phase 3: `Pcg32` and `uuid`.
- Phase 4: integration tests, edge cases, and docs.

### 7.2 Workstreams

- Runtime workstream: TASK-02, TASK-03, and TASK-04.
- Backend workstream: TASK-05.
- Stdlib workstream: TASK-06 through TASK-09.
- Confidence workstream: TASK-10 through TASK-12.

### 7.3 Recommended Next 5 Tasks

1. TASK-01 because it removes the only remaining architectural ambiguity around intrinsic placement and symbol naming.
   It unblocks every backend and stdlib task.
   It should not run in parallel with other implementation tasks.
2. TASK-03 because VM execution support is needed before replay and before stdlib calls can be exercised in the interpreter.
   It unblocks TASK-04 and early `entropy` validation.
   It can run in parallel with TASK-02 after TASK-01.
3. TASK-02 because LLVM/native work cannot complete until the runtime symbol exists and is shaped correctly.
   It unblocks TASK-05 and final native parity.
   It can run in parallel with TASK-03 after TASK-01.
4. TASK-04 because replay semantics are part of the platform contract, not an afterthought.
   It unblocks deterministic regression coverage for entropy and UUID generation.
   It can run in parallel with TASK-05 once TASK-03 is stable.
5. TASK-05 because backend wiring is the last blocker before stdlib calls can be validated end-to-end outside the VM.
   It unblocks cross-backend parity testing and final confidence in `entropy`.
   It can run in parallel with the first pass of TASK-06 if the runtime contract is already stable.

## 8. Task Readiness Notes

- Fully ready to start:
  - TASK-01 immediately;
  - TASK-02, TASK-03, and TASK-05 once TASK-01 freezes the intrinsic contract;
  - TASK-04 once TASK-03 has a stable runtime call path;
  - TASK-06 once at least one backend path is callable in tests.
- Still dependent on one explicit answer or spike:
  - the exact declaration site for the private intrinsic;
  - whether module layout needs split files from day one or can start flat and split later.
- Validations that should exist before parallel execution expands:
  - one focused intrinsic test in VM;
  - one LLVM/native smoke path through `entropy.bytes()`;
  - one replay test that proves host entropy is not consulted during replay.

## 9. Assumptions

- secure host entropy is available on the supported target environments of the repo;
- existing runtime object conventions are sufficient for returning either `byte[]` success or `Error` failure;
- new stdlib modules can be added without changing language semantics;
- `Pcg32` implemented in pure Surge is performant enough for its intended scope.

## 10. Open Questions and Risks

- Can the new intrinsic stay private to `stdlib/entropy`, or do current compiler assumptions require a declaration in `core/intrinsics.sg`?
- Which exact runtime object-construction path should the native entropy function use for `Erring<byte[], Error>` values?
- Does replay logging already have a canonical byte-array encoding, or does entropy need a dedicated log helper?
- Do docs need a small note about supported host platforms for secure entropy, or is that already covered by existing runtime documentation?
