# Entropy / Random / UUID Task Breakdown

Status: draft for execution

Date: 2026-04-17

This document turns the platform spec into an execution sequence. It is intentionally file-oriented and optimized for implementation handoff.

## 1. Execution Strategy

The slice should be built in this order:

1. freeze the intrinsic contract and declaration strategy;
2. land backend/runtime support;
3. expose the smallest stdlib surface: `entropy`;
4. build reusable randomness APIs;
5. build UUID on top;
6. finish with cross-backend tests and docs.

The sequencing matters because the repo does not use automatic runtime intrinsic discovery. VM dispatch, LLVM lowering, and native runtime symbols all need the same spelling and return-shape assumptions.

## 2. Proposed File Layout

Unless TASK-01 proves otherwise, the target layout is:

```text
stdlib/
  entropy/
    entropy.sg
  random/
    random.sg
    pcg32.sg
  uuid/
    uuid.sg
```

If the private intrinsic needs physical separation for readability, add:

```text
stdlib/entropy/intrinsics.sg
```

Do not add new public files in `core/` unless intrinsic declaration constraints force it.

## 3. Task Set

### TASK-01 — Freeze Symbol Strategy

- Goal:
  - decide the exact intrinsic name visible in Surge code and the exact runtime symbol called by LLVM/native.
- Primary files to inspect or edit:
  - [internal/vm/intrinsic.go](/home/zov/projects/surge/surge/internal/vm/intrinsic.go)
  - [internal/backend/llvm/emit_intrinsics_runtime.go](/home/zov/projects/surge/surge/internal/backend/llvm/emit_intrinsics_runtime.go)
  - [internal/backend/llvm/builtins.go](/home/zov/projects/surge/surge/internal/backend/llvm/builtins.go)
  - [core/intrinsics.sg](/home/zov/projects/surge/surge/core/intrinsics.sg) only if needed
  - [docs/2026-04-17-entropy-random-uuid-specs.md](/home/zov/projects/surge/surge/docs/2026-04-17-entropy-random-uuid-specs.md)
- Decisions to lock:
  - private intrinsic name in Surge;
  - runtime symbol name in C;
  - whether the intrinsic declaration lives in stdlib or must live in `core/`;
  - final stdlib directory/file layout.
- Done when:
  - there is no unresolved ambiguity about symbol spelling or declaration site.

### TASK-02 — Native Runtime Entropy Implementation

- Goal:
  - add a host-backed secure entropy function on the native runtime side.
- Primary files:
  - [runtime/native/rt.h](/home/zov/projects/surge/surge/runtime/native/rt.h)
  - one new or existing `runtime/native/rt_*.c`
- Required behavior:
  - zero-length requests succeed;
  - successful calls return a `byte[]` result object compatible with stdlib expectations;
  - failures return an `Error` result object;
  - no fallback to time, counters, or seeded PRNG.
- Review points:
  - supported host APIs;
  - memory ownership of returned arrays and error objects;
  - parity with VM result shape.

### TASK-03 — VM Runtime Interface and Default Runtime Support

- Goal:
  - extend VM runtime interfaces with entropy support.
- Primary files:
  - [internal/vm/runtime.go](/home/zov/projects/surge/surge/internal/vm/runtime.go)
  - [internal/vm/intrinsic.go](/home/zov/projects/surge/surge/internal/vm/intrinsic.go)
  - one new or existing `internal/vm/intrinsic_*.go`
- Required behavior:
  - `DefaultRuntime` fetches real entropy from host Go APIs;
  - `TestRuntime` has controllable entropy behavior for tests;
  - the VM intrinsic handler validates argument count and types and writes the correct result value.
- Review points:
  - runtime interface growth stays minimal;
  - VM error behavior matches native error behavior closely enough for stdlib parity.

### TASK-04 — Replay Logging for Entropy

- Goal:
  - make entropy deterministic under record/replay.
- Primary files:
  - [internal/vm/runtime.go](/home/zov/projects/surge/surge/internal/vm/runtime.go)
  - [internal/vm/record.go](/home/zov/projects/surge/surge/internal/vm/record.go)
  - [internal/vm/replay.go](/home/zov/projects/surge/surge/internal/vm/replay.go)
  - [internal/vm/logfmt.go](/home/zov/projects/surge/surge/internal/vm/logfmt.go)
- Required behavior:
  - recording stores the exact bytes returned by the runtime;
  - replay returns recorded bytes without hitting the host RNG;
  - malformed replay data fails loudly.
- Review points:
  - choose one stable log encoding for byte arrays;
  - avoid hidden dependence on host availability during replay.

### TASK-05 — LLVM Builtin and Lowering

- Goal:
  - route the new intrinsic through LLVM codegen.
- Primary files:
  - [internal/backend/llvm/builtins.go](/home/zov/projects/surge/surge/internal/backend/llvm/builtins.go)
  - [internal/backend/llvm/emit_intrinsics_runtime.go](/home/zov/projects/surge/surge/internal/backend/llvm/emit_intrinsics_runtime.go)
- Required behavior:
  - runtime declaration exists;
  - intrinsic recognition maps the Surge-side symbol to the native runtime call;
  - result handling matches the runtime object conventions already used by other stdlib-backed features.
- Review points:
  - keep naming logic consistent with existing `term_*` and `rt_fs_*` handling;
  - avoid introducing a second naming convention for the same intrinsic.

### TASK-06 — `stdlib/entropy`

- Goal:
  - expose the minimal public entropy API.
- Primary files:
  - `stdlib/entropy/entropy.sg`
  - optional `stdlib/entropy/intrinsics.sg`
- Public surface:
  - `pub const ENTROPY_ERR_UNAVAILABLE`
  - `pub const ENTROPY_ERR_BACKEND`
  - `pub fn bytes(len: uint) -> Erring<byte[], Error>`
  - `pub fn fill(out: &mut byte[]) -> Erring<nothing, Error>`
- Implementation notes:
  - prefer a tiny wrapper over the runtime primitive;
  - `fill(out)` may allocate a temp buffer and copy;
  - do not invent new error structs.
- Review points:
  - zero-length behavior;
  - copy semantics in `fill(out)`;
  - public naming and module comments.

### TASK-07 — `stdlib/random` Base Contract

- Goal:
  - establish reusable randomness APIs and host-backed RNG.
- Primary files:
  - `stdlib/random/random.sg`
  - optional `stdlib/random/system.sg`
- Public surface:
  - `pub contract RandomSource<T>`
  - `pub type SystemRng`
  - `pub fn system() -> SystemRng`
  - convenience wrappers for `bytes`, `fill`, `next_u32`, `next_u64`
- Implementation notes:
  - `SystemRng` delegates to `entropy`;
  - integer decoding must be explicitly little-endian;
  - keep contract generic so `uuid` can consume deterministic and nondeterministic sources equally.
- Review points:
  - method naming stays aligned with spec;
  - no cryptographic claims are attached to deterministic sources.

### TASK-08 — `stdlib/random.Pcg32`

- Goal:
  - add deterministic seeded randomness for tests and fixtures.
- Primary files:
  - `stdlib/random/pcg32.sg`
  - `stdlib/random/random.sg` if constructors live there
- Public surface:
  - `pub type Pcg32`
  - `pub fn pcg32(seed: uint64) -> Pcg32`
  - `pub fn pcg32_stream(seed: uint64, stream: uint64) -> Pcg32`
- Implementation notes:
  - freeze constants and step formula in code comments;
  - `next_u64()` must be defined in terms of two `next_u32()` calls in the order chosen by the spec.
- Review points:
  - sequence stability becomes a compatibility promise;
  - keep it visibly non-cryptographic.

### TASK-09 — `stdlib/uuid`

- Goal:
  - deliver UUID as a pure stdlib layer over `random`.
- Primary files:
  - `stdlib/uuid/uuid.sg`
- Public surface:
  - `pub type Uuid = { bytes: byte[16] }`
  - `pub fn nil() -> Uuid`
  - `pub fn parse(s: &string) -> Erring<Uuid, Error>`
  - `pub fn v4() -> Erring<Uuid, Error>`
  - `pub fn v4_from<T: random.RandomSource<T>>(rng: &mut T) -> Erring<Uuid, Error>`
  - `extern<Uuid>` helpers such as `to_string()` and `is_nil()`
- Implementation notes:
  - canonical lowercase string formatting;
  - strict parse path;
  - set version and variant bits after filling random bytes.
- Review points:
  - parser strictness;
  - hex encoding/decoding helpers;
  - correctness of version and variant bits.

### TASK-10 — VM and LLVM Program-Level Tests

- Goal:
  - validate the public APIs as user code, not only as internals.
- Primary files:
  - one or more `internal/vm/*_test.go`
  - possibly `testdata/golden/...` fixtures
- Coverage targets:
  - `entropy.bytes(0)` success;
  - `entropy.fill()` overwrites a buffer;
  - `SystemRng.next_u32/next_u64()` pack bytes correctly;
  - `Pcg32` fixed-seed vector;
  - `uuid.parse` round-trip;
  - `uuid.v4()` sets correct bits.
- Review points:
  - prefer at least one test that imports all three modules in one program;
  - include parity coverage for VM and LLVM/native.

### TASK-11 — Hardening and Replay Regression

- Goal:
  - close edge cases and prove entropy-consuming programs replay deterministically.
- Primary files:
  - [internal/vm/vm_replay_test.go](/home/zov/projects/surge/surge/internal/vm/vm_replay_test.go)
  - [internal/vm/vm_golden_update_determinism_test.go](/home/zov/projects/surge/surge/internal/vm/vm_golden_update_determinism_test.go) if relevant
- Coverage targets:
  - program that calls `entropy.bytes()`;
  - program that calls `uuid.v4()` and produces the same output under replay;
  - malformed replay payload for entropy fails with a clear replay-format error.
  - zero-length and strict-parse edge cases stay covered.
- Review points:
  - replay should assert on logged bytes, not just output text.
  - edge-case failures should localize whether the issue is in stdlib, replay, or backend error mapping.

### TASK-12 — Public Docs

- Goal:
  - describe the new modules and runtime behavior.
- Primary files:
  - [docs/MODULES.md](/home/zov/projects/surge/surge/docs/MODULES.md)
  - [docs/MODULES.ru.md](/home/zov/projects/surge/surge/docs/MODULES.ru.md)
  - [docs/RUNTIME.md](/home/zov/projects/surge/surge/docs/RUNTIME.md)
  - [docs/RUNTIME.ru.md](/home/zov/projects/surge/surge/docs/RUNTIME.ru.md)
- Documentation requirements:
  - explain the split between `entropy`, `random`, and `uuid`;
  - explain that `SystemRng` is host-backed and `Pcg32` is deterministic;
  - note replay semantics for entropy;
  - do not mention deferred APIs as if they exist.

## 4. Parallelization Map

- Safe after TASK-01:
  - TASK-02
  - TASK-03
  - TASK-06 draft skeleton work
- Safe after runtime contract is stable:
  - TASK-04
  - TASK-05
  - TASK-06
- Safe after `stdlib/random` contract exists:
  - TASK-08
  - TASK-09
- Best left late:
  - TASK-10
  - TASK-11
  - TASK-12

## 5. Exit Criteria

The slice is ready to call implemented when all of the following are true:

- user code can import the new modules and run on VM and LLVM/native;
- host entropy works without fallback;
- replay reproduces entropy-consuming executions deterministically;
- deterministic `Pcg32` outputs are stable under fixed seeds;
- UUID parse/format/v4 behavior is covered by tests;
- docs describe the actual shipped surface and its limits.
