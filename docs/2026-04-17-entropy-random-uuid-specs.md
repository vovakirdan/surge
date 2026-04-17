# Entropy, Random, and UUID Platform Spec

Status: draft for review

Date: 2026-04-17

## 1. Intent

This document specifies a standalone platform feature for Surge:

1. secure host entropy access;
2. reusable randomness APIs;
3. deterministic seeded pseudo-random generation for reproducible tests;
4. RFC-compatible UUID generation and formatting.

This is intentionally not tied to any particular consumer. The feature must stand on its own as a coherent standard-library and runtime capability.

The goals are:

- clean layering;
- minimal duplication with existing `core/` and `stdlib/`;
- explicit separation between cryptographic host randomness and deterministic seeded PRNGs;
- deterministic VM record/replay behavior;
- minimal new intrinsic surface.

## 2. Existing Repo Facts

This section records the existing project facts that constrain the design.

### 2.1 Already present in `core/`

The following are already in the prelude and must be reused rather than reintroduced:

- `Error`, `ErrorLike`, `Erring<T, E>` in [core/result.sg](/home/zov/projects/surge/surge/core/result.sg).
- `byte = uint8` and the rest of the built-in numeric families in [core/intrinsics.sg](/home/zov/projects/surge/surge/core/intrinsics.sg).
- `string.bytes()` and `string.from_bytes()` in [core/string.sg](/home/zov/projects/surge/surge/core/string.sg).
- `Array<T>` and `ArrayFixed<T, N>` helpers in [core/array.sg](/home/zov/projects/surge/surge/core/array.sg).
- fixed-size array support in the language and ABI docs.

Conclusion:

- no new base error structs are required;
- no new bytes/string conversion primitives are required;
- UUID binary storage can use `byte[16]`.

### 2.2 Already present in `stdlib/`

The repo already contains stdlib modules implemented on top of runtime intrinsics:

- filesystem in [stdlib/fs/fs.sg](/home/zov/projects/surge/surge/stdlib/fs/fs.sg);
- networking in [stdlib/net/net.sg](/home/zov/projects/surge/surge/stdlib/net/net.sg);
- terminal intrinsics in [stdlib/term/intrinsics.sg](/home/zov/projects/surge/surge/stdlib/term/intrinsics.sg).

Conclusion:

- this feature does not need to pollute `core/` just because it touches the runtime;
- a stdlib module may privately declare an intrinsic and publicly wrap it with normal Surge code.

### 2.3 Important current limits

The repo does not expose a public array-data pointer API or mutable bytes view for arbitrary `byte[]` buffers. Existing low-level pointer access is centered around raw allocation, strings, and runtime internals.

Conclusion:

- the first low-level entropy primitive should return a fresh `byte[]`;
- an in-place `fill(out: &mut byte[])` API can still exist, but in v1 it should be implemented in pure Surge by copying from a freshly allocated entropy buffer;
- temporary buffers can be built with the already existing `Array::<byte>::with_len(...)` helper from [core/array.sg](/home/zov/projects/surge/surge/core/array.sg);
- we should not add a new general-purpose array-pointer intrinsic just to optimize this slice.

## 3. Scope

### In scope

- host-backed secure entropy as a stdlib capability;
- a system RNG wrapper;
- a deterministic seeded PRNG implemented in Surge;
- UUID type, parse/format, and v4 generation;
- VM record/replay support for host entropy;
- native runtime support;
- LLVM backend support;
- tests for correctness, determinism, and backend parity.

### Out of scope

- `uuid.v7()`;
- wall-clock time APIs;
- crypto hash APIs;
- random distributions beyond raw bytes and integers;
- a public mutable byte-view abstraction;
- a new generic pointer API for dynamic arrays;
- any consumer-specific integration work.

## 4. Hard Design Decisions

### 4.1 Layering

The feature is split into:

- `stdlib/entropy`
- `stdlib/random`
- `stdlib/uuid`

The dependency direction is:

`entropy` -> `random` -> `uuid`

There is no direct dependency from `uuid` to runtime intrinsics.

### 4.2 Core surface

This slice does not add new public entities to `core/`.

We reuse:

- `Error`
- `ErrorLike`
- `Erring`
- `byte`
- `ArrayFixed`
- `string.from_bytes`

This avoids duplication and keeps the language surface smaller.

### 4.3 Error model

The feature uses the existing core `Error` type instead of introducing parallel error structs like `EntropyError`, `RandomError`, or `UuidError`.

Each module defines its own error code constants, but the data carrier remains:

```surge
type Error = { message: string, code: uint };
```

This choice keeps the design consistent with existing prelude facilities and reduces needless entity growth.

### 4.4 Deterministic PRNG choice

The first public deterministic seeded PRNG is `Pcg32`.

Reasoning:

- simpler state than `xoshiro`;
- better public default than `SplitMix64`;
- easy to express in current Surge with fixed-width integers and bitwise operators;
- sufficient for reproducible tests and deterministic fixture generation.

`Pcg32` is explicitly non-cryptographic.

### 4.5 Cryptographic entropy policy

System randomness must come from host CSPRNG facilities only.

There is no fallback to:

- monotonic time;
- counters;
- PID;
- seeded PRNG;
- any other weak source.

Failure to get secure entropy is an error, not an opportunity to silently degrade.

## 5. Intrinsics vs Pure Surge

This boundary must stay explicit.

### 5.1 Runtime-backed intrinsic pieces

Only the host entropy acquisition path is intrinsic.

Conceptually, v1 adds one private stdlib intrinsic:

```surge
@intrinsic fn rt_entropy_bytes(len: uint) -> Erring<byte[], Error>;
```

This is not part of the public API. It is an implementation hook inside `stdlib/entropy`.

Backend/runtime work is required for:

- native runtime implementation;
- VM runtime implementation;
- VM record/replay logging and replay;
- LLVM builtin declaration and emission.

### 5.2 Pure Surge pieces

Everything else should be written in Surge:

- public `entropy.bytes()` wrapper;
- public `entropy.fill()` wrapper;
- `SystemRng`;
- `Pcg32`;
- integer decoding and byte packing;
- UUID parse;
- UUID format;
- UUID v4 version/variant bit setting.

This keeps the runtime surface small and the library logic testable in the language itself.

### 5.3 Why not an intrinsic `fill(out: &mut byte[])`

The repo currently does not expose a general public dynamic-array data pointer API. Adding such a primitive would widen the low-level memory surface and create a new capability far more general than this feature needs.

Therefore:

- v1 low-level entropy primitive returns a fresh `byte[]`;
- `entropy.fill(out)` is a pure wrapper that copies from a temporary entropy buffer;
- if this later proves to be a hotspot, the implementation can add a second internal optimization intrinsic without changing the public API.

## 6. Public API

### 6.1 `stdlib/entropy`

Public constants:

```surge
pub const ENTROPY_ERR_UNAVAILABLE: uint = 1;
pub const ENTROPY_ERR_BACKEND: uint = 2;
```

Public API:

```surge
pub fn bytes(len: uint) -> Erring<byte[], Error>;
pub fn fill(out: &mut byte[]) -> Erring<nothing, Error>;
```

Behavior:

- `bytes(len)` returns a freshly allocated array of cryptographically secure bytes.
- `fill(out)` overwrites the provided dynamic byte array by copying from `bytes(len(out))`.
- `len == 0` succeeds.
- both APIs may fail with `Error` carrying entropy module error codes.

There is deliberately no public API that exposes host entropy via raw pointers.

### 6.2 `stdlib/random`

Public constants:

Public contract:

```surge
pub contract RandomSource<T> {
    pub fn fill(self: &mut T, out: &mut byte[]) -> Erring<nothing, Error>;
    pub fn next_u32(self: &mut T) -> Erring<uint32, Error>;
    pub fn next_u64(self: &mut T) -> Erring<uint64, Error>;
}
```

Public types:

```surge
@copy
pub type SystemRng = {
    _private: uint8,
};

pub type Pcg32 = {
    state: uint64,
    inc: uint64,
};
```

Public functions:

```surge
pub fn system() -> SystemRng;

pub fn bytes(len: uint) -> Erring<byte[], Error>;
pub fn fill(out: &mut byte[]) -> Erring<nothing, Error>;
pub fn next_u32() -> Erring<uint32, Error>;
pub fn next_u64() -> Erring<uint64, Error>;

pub fn pcg32(seed: uint64) -> Pcg32;
pub fn pcg32_stream(seed: uint64, stream: uint64) -> Pcg32;
```

Behavior:

- `SystemRng` is a stateless wrapper over `entropy`.
- `Pcg32` is deterministic and pure.
- `Pcg32` methods are infallible in practice, but to satisfy the common contract they return `Success(...)`.
- module-level `random.bytes/fill/next_u32/next_u64` are convenience wrappers around `SystemRng`.

### 6.3 `stdlib/uuid`

Public constants:

```surge
pub const UUID_ERR_PARSE: uint = 1;
pub const UUID_ERR_RANDOM: uint = 2;
```

Public type:

```surge
pub type Uuid = {
    bytes: byte[16],
};
```

Public functions:

```surge
pub fn nil() -> Uuid;
pub fn parse(text: &string) -> Erring<Uuid, Error>;
pub fn v4() -> Erring<Uuid, Error>;
pub fn v4_from<T: random.RandomSource<T>>(rng: &mut T) -> Erring<Uuid, Error>;
```

Expected methods in `extern<Uuid>`:

```surge
pub fn to_string(self: &Uuid) -> string;
pub fn is_nil(self: &Uuid) -> bool;
```

Behavior:

- canonical text format is lowercase `8-4-4-4-12`;
- parser accepts lowercase and uppercase hex;
- `v4_from` obtains 16 random bytes through the generic RNG contract, typically by creating a temporary `Array::<byte>::with_len(16:uint)`, filling it, and copying into `byte[16]`, then sets the version and variant bits;
- `v4()` is a convenience wrapper using `SystemRng`.

This API intentionally does not add `v7`, formatting variants, or extra representation helpers in v1.

## 7. Algorithm-Level Requirements

### 7.1 `Pcg32`

The implementation must specify and freeze:

- state transition formula;
- output permutation formula;
- stream initialization rules;
- byte packing order for `fill()` and `next_u64()`.

For byte-oriented APIs, packing order is fixed to little-endian.

For `next_u64()`, the result is composed from two `next_u32()` outputs in a fixed order:

- first output becomes the low 32 bits;
- second output becomes the high 32 bits.

This must be documented and tested so behavior is stable across VM and native backends.

### 7.2 `SystemRng`

`SystemRng.next_u32()` and `SystemRng.next_u64()` decode little-endian values from secure bytes acquired through `entropy`.

This keeps the observable byte/int relationship aligned with `Pcg32.fill()` and `Pcg32.next_u64()`.

### 7.3 UUID v4

`uuid.v4_from` must:

1. obtain 16 bytes;
2. copy them into `Uuid.bytes`;
3. set the version nibble to `0100`;
4. set the RFC 4122 variant bits to `10`;
5. return the resulting UUID.

Text formatting and parsing are pure-language logic over `byte[16]`, strings, and hex helpers.

## 8. New Entities and Duplication Audit

### 8.1 New public entities

Public modules:

- `stdlib/entropy`
- `stdlib/random`
- `stdlib/uuid`

Public contract:

- `RandomSource<T>`

Public types:

- `SystemRng`
- `Pcg32`
- `Uuid`

Public function families:

- entropy helpers
- random helpers
- UUID helpers

Public constants:

- entropy error codes
- random error codes
- UUID error codes

### 8.2 New private runtime/intrinsic entities

At minimum:

- one private intrinsic symbol for host entropy bytes;
- one VM runtime interface method to obtain secure bytes;
- matching native runtime implementation;
- LLVM recognition/emission for the intrinsic.

### 8.3 Explicitly avoided duplication

This design does not introduce:

- new core error structs;
- new core string/byte conversion APIs;
- new generic array pointer APIs;
- a second “base result” abstraction;
- runtime-backed UUID intrinsics;
- runtime-backed deterministic PRNG intrinsics.

## 9. Recommended Module Layout

Recommended shape:

```text
stdlib/
  entropy/
    entropy.sg
  random/
    imports.sg
    random.sg
    system.sg
    pcg32.sg
  uuid/
    uuid.sg
```

Why:

- `entropy` is small and can stay single-file even with one private intrinsic declaration;
- `random` benefits from being split across API, system wrapper, and `Pcg32`;
- `uuid` can start in one file because parse/format/v4 still fit naturally together.

If `uuid` later grows versioned generators or binary/text adapters, it can become a multi-file module without changing the API.

## 10. Runtime and Backend Work

### 10.1 Native runtime

The native runtime must provide secure bytes from the host OS CSPRNG.

Requirements:

- exact requested length or explicit failure;
- zero-length success;
- no weak fallback;
- surface failures as `Erring<byte[], Error>` compatible results.

The exact host facility is an implementation detail. The contract is “OS-backed secure bytes”, not a specific syscall name.

### 10.2 VM runtime

The VM runtime interface must gain an entropy method conceptually equivalent to:

```go
EntropyBytes(n int) ([]byte, error)
```

Requirements:

- default runtime queries the host;
- test runtime may provide deterministic injected bytes;
- recording runtime records the exact returned bytes;
- replay runtime serves the recorded bytes and never touches the host entropy source.

### 10.3 LLVM/backend

The LLVM backend must:

- declare the intrinsic;
- recognize the symbol;
- emit the call;
- store the returned `Erring<byte[], Error>` result using normal ABI conventions.

This should follow existing runtime-intrinsic patterns, not invent a new codegen category.

## 11. Testing Strategy

### 11.1 Entropy tests

- `entropy.bytes(0)` succeeds with empty output;
- non-zero length succeeds on supported hosts;
- VM record/replay reproduces the exact byte sequence;
- replay never queries host entropy.

These tests validate semantics, not randomness quality.

### 11.2 `Pcg32` tests

- fixed-seed reference vectors for `next_u32()`;
- fixed-seed reference vectors for `next_u64()`;
- fixed-seed `fill()` byte sequences;
- backend parity between VM and native.

### 11.3 UUID tests

- parse success and failure cases;
- lowercase canonical formatting;
- parse-format roundtrip;
- `v4_from(Pcg32)` golden outputs;
- explicit checks for version and variant bits.

### 11.4 Integration tests

- `SystemRng` top-level convenience functions work;
- `uuid.v4()` works on supported hosts;
- deterministic tests can build UUIDs from seeded `Pcg32` without touching host entropy.

## 12. Implementation Order

### Slice 1: `entropy`

- add private intrinsic;
- add native runtime support;
- add VM runtime support;
- add VM record/replay support;
- add public `bytes` and pure `fill` wrappers;
- add tests.

### Slice 2: `random`

- add `SystemRng`;
- add top-level system-random convenience helpers;
- implement `Pcg32` in pure Surge;
- add deterministic reference tests.

### Slice 3: `uuid`

- add `Uuid` type;
- add parse/format;
- add `v4_from`;
- add `v4`;
- add deterministic and host-backed tests.

This order keeps the runtime substrate small and validated before building higher-level consumers on top of it.

## 13. Success Criteria

This spec is implemented successfully when all of the following are true:

- the feature exists independently of any particular consumer;
- `core/` did not gain duplicate surface unnecessarily;
- secure host entropy is available through `stdlib/entropy`;
- deterministic seeded PRNG is available through `stdlib/random`;
- UUID parse/format/v4 is available through `stdlib/uuid`;
- the only runtime-backed logic is host entropy acquisition;
- VM record/replay remains deterministic;
- backend behavior is stable and tested.

At that point the platform has the right substrate for any future consumer, including but not limited to storage systems, protocols, tests, and tooling.
