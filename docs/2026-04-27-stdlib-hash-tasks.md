# `stdlib/hash` Epics And Tasks

**Goal:** Build a rich, stable, non-cryptographic hashing module for Surge stdlib.

**Approach:** Treat the work as a small stdlib product, not a one-commit helper. First lock the public contract, then implement the raw algorithm layer, structured stable layer, tests, docs, and later generic contracts.

**Primary artifacts:** `docs/2026-04-27-stdlib-hash-design.md`, `stdlib/hash/*`, `testdata/golden/*`, `testdata/llvm_parity/*`, `docs/STDLIB.md`, `docs/STDLIB.ru.md`.

---

## Current Status

Implemented in the first execution slice:

- v1 design decisions recorded in `docs/2026-04-27-stdlib-hash-design.md`;
- `stdlib/hash` module skeleton and public concrete API;
- `Hash64` wrapper, bucket helper, hex formatting, and equality operators;
- pure Surge `Xxh64` with official vectors and streaming checks;
- `Stable64` primitive frames and structural list, record, field, and variant frames;
- VM smoke programs for `Hash64`, `Xxh64`, `Stable64` primitives, and `Stable64` structures;
- generated golden outputs for the new sema and VM hash cases;
- LLVM parity programs for `Xxh64` and `Stable64`;
- parity registration in `internal/vm/llvm_parity_test.go`;
- user-facing `stdlib/hash` sections in `docs/STDLIB.md` and `docs/STDLIB.ru.md`.

Targeted verification already run:

```sh
./surge diag --format short testdata/golden/sema/valid/stdlib_hash_api.sg
./surge run --backend=vm testdata/golden/vm_hash/hash64_basic.sg
./surge run --backend=vm testdata/golden/vm_hash/xxh64_vectors.sg
./surge run --backend=vm testdata/golden/vm_hash/stable64_primitives.sg
./surge run --backend=vm testdata/golden/vm_hash/stable64_frames.sg
./surge run --backend=vm testdata/llvm_parity/hash_xxh64.sg
./surge run --backend=vm testdata/llvm_parity/hash_stable64.sg
./surge build testdata/llvm_parity/hash_xxh64.sg
target/debug/hash_xxh64
./surge build testdata/llvm_parity/hash_stable64.sg
target/debug/hash_stable64
go test ./internal/vm -run 'TestLLVMParity/hash_(xxh64|stable64)$'
SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s
git diff --check
```

Compiler guardrail:

- Fix normal implementation mistakes in this branch.
- If sema, VM, LLVM, formatting, or generated goldens show behavior that looks like a compiler/toolchain bug, stop the hash implementation slice, reduce the case, and track it separately instead of hiding it with unrelated workarounds.
- Do not rely on absent diagnostics. If invalid-looking code is accepted, capture the smallest accepted example and make it a compiler follow-up.

## Epic 1: Product Contract

Record the accepted v1 contract for `stdlib/hash`.

Status: implemented in the design doc.

Tasks:

- Keep `stable64_*` as permanent unversioned v1 names.
- Include `Hash64.to_hex()` in v1.
- Defer FNV-1a to a later compatibility slice.
- Include structural list, record, field, and variant frames in v1.
- Omit dynamic `int` and `uint` writers from v1.
- Keep generic `StableHash<T>` as a follow-up after the concrete API is stable.
- Keep the design doc focused on target state and accepted scope.

Done when:

- The design doc records the accepted v1 decisions.
- A reviewer can tell which APIs are in v1, which are later, and which are explicitly out of scope.

## Epic 2: Public Module Shape

Create the stdlib module and make its exported surface visible to the compiler.

Status: implemented, sema-smoked, and covered by generated goldens.

Tasks:

- Create `stdlib/hash/` as a multi-file module with `pragma module`.
- Add the public facade file, raw algorithm file, and stable encoder file.
- Define `Hash64`, `Xxh64`, and `Stable64`.
- Add top-level helper signatures for raw and stable hashing.
- Add a sema smoke program at `testdata/golden/sema/valid/stdlib_hash_api.sg`.
- Confirm imports, methods, and top-level helpers resolve through the module graph.

Done when:

- `./surge diag --format short testdata/golden/sema/valid/stdlib_hash_api.sg` passes.
- Generated goldens include the new sema smoke.

## Epic 3: Digest Type

Implement `Hash64` as the common digest wrapper.

Status: implemented, VM-smoked, and covered by generated goldens.

Tasks:

- Implement `Hash64.as_u64()`.
- Implement `Hash64.bucket(bucket_count)`.
- Implement equality and inequality methods.
- Implement `Hash64.to_hex()`.
- Add VM tests for zero-bucket handling, bucket math, equality, and hex formatting.

Done when:

- `testdata/golden/vm_hash/hash64_basic.sg` exits with `0` on the VM.
- The same behavior is covered by generated goldens.

## Epic 4: Raw xxHash64

Implement the raw byte-stream algorithm layer.

Status: implemented, VM-smoked, manually built for LLVM/native, registered in LLVM parity, and covered by generated goldens.

Tasks:

- Add xxHash64 constants.
- Add wrapping `uint64` arithmetic helpers using wide `uint` modulo arithmetic.
- Add rotate and little-endian byte-loading helpers.
- Implement `Xxh64::new(seed)`.
- Implement streaming `update_bytes()`.
- Implement `update_string_bytes()` using UTF-8 bytes.
- Implement `finish()`.
- Implement `xxh64_bytes()` and `xxh64_string()`.
- Add official xxHash64 vectors.
- Add chunking tests proving streaming and one-shot hashing match.

Done when:

- `testdata/golden/vm_hash/xxh64_vectors.sg` passes on VM.
- `testdata/llvm_parity/hash_xxh64.sg` passes VM/LLVM parity.

## Epic 5: Structured Stable64

Implement typed, deterministic structured hashing.

Status: implemented, VM-smoked, manually built for LLVM/native, registered in LLVM parity, and covered by generated goldens.

Tasks:

- Implement `Stable64::new()` and `Stable64::with_seed(seed)`.
- Implement stable frame writing: type tag, payload, length or count.
- Implement fixed-width boolean, byte, unsigned integer, and signed integer writers.
- Implement byte-array and string writers.
- Implement `finish()`.
- Implement `stable64_bytes()` and `stable64_string()`.
- Implement list, record, field, and variant frame helpers.
- Add tests proving structured hashing avoids raw concatenation ambiguity.
- Add Unicode string tests that prove string hashing uses UTF-8 bytes.

Done when:

- `testdata/golden/vm_hash/stable64_primitives.sg` passes on VM.
- `testdata/golden/vm_hash/stable64_frames.sg` passes on VM.
- `testdata/llvm_parity/hash_stable64.sg` passes VM/LLVM parity.

## Epic 6: Future Compatibility Algorithms

Track secondary algorithms outside the v1 implementation.

Status: deferred from v1 by design.

Tasks:

- Keep FNV-1a out of v1.
- Add a follow-up issue or task for `fnv1a64_bytes()` and `fnv1a64_string()` if compatibility or teaching examples need it.
- When added later, document FNV-1a as compatibility/simple hashing, not the recommended default.

Done when:

- The v1 implementation contains only xxHash64 and Stable64.
- The design doc says FNV-1a is deferred.

## Epic 7: Generic StableHash Contract

Add the generic user-extensibility layer after the concrete API is stable.

Status: follow-up after concrete API review.

Tasks:

- Add `StableHash<T>` contract.
- Implement stable hash methods for `bool`, fixed-width integers, `string`, `Hash64`, and other low-risk primitives.
- Decide separately how to handle dynamic `int`, `uint`, arrays, maps, structs, and tags.
- Add `stable_hash<T: StableHash<T>>(value: &T) -> Hash64`.
- Add sema and VM tests for primitive generic hashing.

Done when:

- Generic hashing works for primitive values.
- The implementation does not expose fragile generic `&mut T` behavior to user code without tests.

## Epic 8: Documentation

Document the module as part of the shipped stdlib.

Status: implemented in English and Russian stdlib docs.

Tasks:

- Add `stdlib/hash` to the module index in `docs/STDLIB.md`.
- Mirror the section in `docs/STDLIB.ru.md`.
- Document `Hash64`, raw `Xxh64`, structured `Stable64`, top-level helpers, and output stability.
- Add examples for sharding, cache keys, and structured record hashing.
- State that the module is not cryptographic.
- State what remains future work.

Done when:

- English and Russian stdlib docs describe the same user-facing surface.
- A user can pick the right API without reading implementation files.

## Epic 9: Verification And Release Readiness

Prove the module is correct across the repo-supported paths.

Status: complete for the concrete v1 slice through generated goldens, targeted VM, manual LLVM/native, targeted Go parity, full `go test ./...`, and diff hygiene checks.

Tasks:

- Regenerate goldens with `make golden-update`.
- Run direct VM smoke programs for all hash goldens.
- Run LLVM parity for hash cases.
- Run relevant module graph and VM tests.
- Run `go test ./...`.
- Run C runtime checks if runtime files change.
- Run `make check` or the repo-standard replacement if `make check` remains a placeholder.
- Review final diff for unrelated churn.

Done when:

- All targeted tests pass.
- Final diff contains only `stdlib/hash`, hash tests, parity registration, and docs.
- The implementation can be reviewed as a coherent stdlib feature.

## Suggested Commit/PR Slices

1. Design doc and epic plan.
2. Module skeleton plus API smoke.
3. `Hash64` and VM tests.
4. Raw `Xxh64` plus vectors and parity.
5. `Stable64` primitive frames plus tests.
6. Structural frames.
7. User docs and golden refresh.
8. Generic `StableHash<T>` contract in a follow-up PR.
9. Optional compatibility algorithms such as FNV-1a in a later PR if needed.
