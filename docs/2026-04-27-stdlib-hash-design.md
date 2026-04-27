# `stdlib/hash` Design

> **Status:** Draft design, first concrete v1 slice implemented in `hash-foundation-plan`
> **Audience:** Surge stdlib authors, compiler/runtime maintainers, and library authors
> **Purpose:** Describe the target shape of a rich, stable, non-cryptographic hashing standard library for Surge.

## 1. Why This Exists

Surge needs hashing as a first-class standard-library capability, not as a local helper for one project. `surgekv` needs deterministic key-to-shard routing, but the same primitive will be useful for caches, routing tables, bloom filters, consistent partitioning, deterministic tests, snapshots, persistent indexes, and future stdlib collections.

The current runtime has internal map key handling, but that is an implementation detail. User code needs a public API with clear guarantees. The public contract must say what is stable, what is only an implementation detail, and what belongs elsewhere.

This module is about deterministic, non-cryptographic hashing. It is not for passwords, signatures, MACs, authentication tokens, or hostile-input integrity checks. Cryptographic hashing should live later in a separate namespace such as `stdlib/crypto/hash`.

## 2. Target Outcome

At the end of this work, Surge should have a `stdlib/hash` module that supports three layers:

1. Raw algorithm hashing for bytes and strings.
2. Structured stable hashing for application values.
3. A later generic contract so user and stdlib types can define their own stable hashing behavior.

The most common user path should be simple:

```sg
import stdlib/hash as hash;

fn shard_for(key: &string, shard_count: uint) -> Option<uint> {
    let digest: hash.Hash64 = hash.stable64_string(key);
    return digest.bucket(shard_count);
}
```

More advanced users should be able to stream values into a structured hasher:

```sg
let mut h: hash.Stable64 = hash.Stable64::new();
h.write_string(&tenant);
h.write_string(&key);
h.write_u64(generation);
let digest: hash.Hash64 = h.finish();
```

Users who need a known raw byte-stream algorithm should be able to use it directly:

```sg
let raw: hash.Hash64 = hash.xxh64_string(&key, 0:uint64);
```

## 3. Design Principles

The module should optimize for stable semantics first and speed second. Performance matters, but the main value is that the same input gives the same output across VM, LLVM/native, platforms, and future releases.

The API should make ambiguity hard. Raw byte concatenation is useful, but structured data needs typed framing. Hashing `"ab" + "c"` as three raw bytes is not the same problem as hashing a pair of strings `("a", "bc")`. `Stable64` exists to make those cases distinct.

The API should be explicit about algorithms. A function named `hash()` invites users to depend on an unspecified default. Prefer names such as `xxh64_*`, `stable64_*`, and later `fnv1a64_*` if we add compatibility algorithms.

The first implementation should be pure Surge unless a runtime intrinsic is necessary. Native or VM intrinsics can come later as optimizations, but they must preserve exact output.

## 4. Module Boundary

The module should be imported as:

```sg
import stdlib/hash as hash;
```

Recommended module layout:

```text
stdlib/hash/
  hash.sg       public facade, digest types, top-level helpers
  xxh64.sg      xxHash64 implementation
  stable64.sg   structured stable encoder
```

All files in the directory should use `pragma module`.

`stdlib/hash` should not depend on `Map<K, V>` internals. Future `Map` or collection internals may use hashing internally, but user-visible stable hashing and hashmap-grade randomized hashing are separate concerns.

## 5. Digest Types

### `Hash64`

`Hash64` is the public 64-bit digest wrapper.

```sg
@copy
pub type Hash64 = {
    value: uint64,
};
```

Expected methods:

```sg
extern<Hash64> {
    pub fn as_u64(self: &Hash64) -> uint64;
    pub fn bucket(self: &Hash64, bucket_count: uint) -> Option<uint>;
    pub fn to_hex(self: &Hash64) -> string;
    pub fn __eq(self: Hash64, other: Hash64) -> bool;
    pub fn __ne(self: Hash64, other: Hash64) -> bool;
    pub fn __to(self: &Hash64, target: string) -> string;
}
```

`bucket()` returns `nothing` when `bucket_count == 0`; otherwise it returns `value % bucket_count`. This is the direct helper needed for sharding and bucket selection.

`to_hex()` is part of v1. It should return fixed-width lowercase hexadecimal text, 16 characters for `Hash64`.

### Future Digest Types

`Hash128` is not required for the first implementation, but the module should not be designed in a way that prevents it. xxHash3-128 or cryptographic digests should be added as separate types instead of overloading `Hash64`.

## 6. Raw Algorithm Layer

The primary raw algorithm should be xxHash64.

Reasons:

- It is widely known and well documented.
- It has official vectors.
- It is faster and stronger for general non-cryptographic use than FNV-1a.
- It is practical to implement in Surge using fixed-width arithmetic.
- It is appropriate for partitioning, cache keys, and tests.

Public API:

```sg
pub type Xxh64 = { ... };

extern<Xxh64> {
    pub fn new(seed: uint64) -> Xxh64;
    pub fn update_bytes(self: &mut Xxh64, bytes: &byte[]) -> nothing;
    pub fn update_string_bytes(self: &mut Xxh64, text: &string) -> nothing;
    pub fn finish(self: &Xxh64) -> Hash64;
}

pub fn xxh64_bytes(bytes: &byte[], seed: uint64) -> Hash64;
pub fn xxh64_string(text: &string, seed: uint64) -> Hash64;
```

`Xxh64` hashes exactly the byte stream provided by the caller. It adds no type tags, length prefixes, separators, or schema information.

FNV-1a is deferred. It can be added later as a tiny compatibility algorithm and teaching example, but v1 should keep the raw algorithm story focused on xxHash64.

## 7. Structured Stable Layer

`Stable64` is the main user-facing abstraction for hashing structured application data.

```sg
pub type Stable64 = {
    inner: Xxh64,
};

extern<Stable64> {
    pub fn new() -> Stable64;
    pub fn with_seed(seed: uint64) -> Stable64;

    pub fn write_bool(self: &mut Stable64, value: bool) -> nothing;
    pub fn write_byte(self: &mut Stable64, value: byte) -> nothing;

    pub fn write_u8(self: &mut Stable64, value: uint8) -> nothing;
    pub fn write_u16(self: &mut Stable64, value: uint16) -> nothing;
    pub fn write_u32(self: &mut Stable64, value: uint32) -> nothing;
    pub fn write_u64(self: &mut Stable64, value: uint64) -> nothing;

    pub fn write_i8(self: &mut Stable64, value: int8) -> nothing;
    pub fn write_i16(self: &mut Stable64, value: int16) -> nothing;
    pub fn write_i32(self: &mut Stable64, value: int32) -> nothing;
    pub fn write_i64(self: &mut Stable64, value: int64) -> nothing;

    pub fn write_bytes(self: &mut Stable64, value: &byte[]) -> nothing;
    pub fn write_string(self: &mut Stable64, value: &string) -> nothing;

    pub fn begin_list(self: &mut Stable64, len: uint) -> nothing;
    pub fn begin_record(self: &mut Stable64, name: &string, field_count: uint) -> nothing;
    pub fn write_field(self: &mut Stable64, name: &string) -> nothing;
    pub fn begin_variant(self: &mut Stable64, type_name: &string, variant_name: &string, variant_index: uint, field_count: uint) -> nothing;

    pub fn finish(self: &Stable64) -> Hash64;
}
```

Top-level helpers:

```sg
pub fn stable64_bytes(bytes: &byte[]) -> Hash64;
pub fn stable64_string(text: &string) -> Hash64;
pub fn stable64_with_seed(seed: uint64) -> Stable64;
```

`stable64_string()` should be equivalent to creating a `Stable64`, writing one string frame, and finishing it. It should not equal raw `xxh64_string()` because the stable version includes typed framing.

## 8. Stable Encoding Contract

The `Stable64` byte encoding is part of the public API. Once released, it must not change silently.

Each structured write should add:

1. a type tag;
2. a stable payload;
3. a stable length or count for variable-size structures.

Recommended v1 tags:

| Tag | Meaning |
| --- | --- |
| `0x01` | `bool false` |
| `0x02` | `bool true` |
| `0x10` | unsigned integer |
| `0x11` | signed integer |
| `0x12` | byte |
| `0x20` | byte array |
| `0x21` | string UTF-8 bytes |
| `0x30` | list frame |
| `0x31` | record frame |
| `0x32` | record field |
| `0x33` | variant frame |

Numeric payloads use little-endian byte order. Fixed-width integer frames include the numeric tag, a one-byte declared width (`1`, `2`, `4`, or `8`), then the integer payload using that many little-endian bytes. This keeps `write_u8(1)` distinct from `write_u64(1)` without spending separate tags for every width. Signed integer payloads use two's-complement bytes for their declared width. Strings use UTF-8 bytes from `string.bytes()`, not Unicode code point count.

Variable lengths and structural counts should use fixed little-endian `uint64` for v1. LEB128 is smaller, but fixed-width encoding is simpler, easier to test, and less likely to create edge-case bugs in the first release.

Dynamic `int` and `uint` are omitted from v1. Users should choose an explicit fixed-width writer such as `write_i64()` or `write_u64()`. Big-number and dynamic numeric encoding can be designed later without retrofitting v1.

## 9. Contracts And Generic Hashing

After the concrete API is stable, the module should add a structural contract:

```sg
pub contract StableHash<T> {
    pub fn stable_hash(self: &T, h: &mut Stable64) -> nothing;
}
```

Then stdlib can implement `stable_hash` methods for:

- `bool`
- fixed-width integers
- `string`
- `byte[]` if array extern ergonomics allow it cleanly
- `Hash64`
- selected stdlib types when they have a stable semantic representation

Then the module can expose:

```sg
pub fn stable_hash<T: StableHash<T>>(value: &T) -> Hash64;
```

This is intentionally not the first slice. Current stdlib experience shows that generic consumer code around mutable references is more fragile than concrete APIs. The concrete `Stable64` surface should land first.

## 10. Versioning And Compatibility

The first stable structured format should be considered `Stable64 v1`.

Potential public constants:

```sg
pub const STABLE64_VERSION: uint = 1:uint;
pub const STABLE64_SEED: uint64 = 0:uint64;
```

The unversioned names are the permanent v1 contract:

```sg
pub fn stable64_string(text: &string) -> Hash64;
pub type Stable64 = { ... };
```

If Surge later needs a different stable format, add `Stable64V2` and helpers such as `stable64_v2_string()`. Do not change v1 output for the same input.

## 11. Runtime And Backend Boundaries

The reference implementation should be pure Surge.

VM or native intrinsics can be added later for performance, but only behind identical API behavior. Any optimized backend must pass the same official vectors, streaming chunk tests, and `Stable64` framing tests.

The module should not depend on host endianness, pointer values, allocation order, map iteration order, scheduler behavior, or randomized seeds.

## 12. Testing Requirements

The finished module should have:

- semantic API smoke tests for imports and method visibility;
- official xxHash64 vectors;
- streaming chunk invariants;
- raw string and byte-array tests;
- `Hash64.bucket()` tests;
- `Stable64` primitive frame tests;
- `Stable64` structure frame tests;
- Unicode string tests;
- VM output tests;
- LLVM/native parity tests;
- golden regeneration coverage.

Representative paths:

```text
testdata/golden/sema/valid/stdlib_hash_api.sg
testdata/golden/vm_hash/hash64_basic.sg
testdata/golden/vm_hash/xxh64_vectors.sg
testdata/golden/vm_hash/stable64_primitives.sg
testdata/golden/vm_hash/stable64_frames.sg
testdata/llvm_parity/hash_xxh64.sg
testdata/llvm_parity/hash_stable64.sg
```

## 13. Documentation Requirements

`docs/STDLIB.md` and `docs/STDLIB.ru.md` should explain:

- what `stdlib/hash` is for;
- why it is not cryptographic;
- when to use raw `Xxh64`;
- when to use structured `Stable64`;
- what output stability means;
- how to pick a bucket safely;
- how to hash composite values;
- what remains future work.

The docs should include examples for sharding, cache keys, and structured record hashing.

## 14. V1 Decisions

The v1 implementation should include:

- `Hash64`
- `Hash64.as_u64()`
- `Hash64.bucket()`
- `Hash64.to_hex()`
- raw `Xxh64`
- `xxh64_bytes()`
- `xxh64_string()`
- structured `Stable64`
- `stable64_bytes()`
- `stable64_string()`
- fixed-width numeric writers
- `bool`, `byte`, `string`, and `byte[]` writers
- list, record, field, and variant frames
- permanent unversioned `stable64_*` names as the v1 contract

The v1 implementation should not include:

- FNV-1a
- generic `StableHash<T>`
- dynamic `int` and `uint` writers
- map, struct, or tag auto-hashing
- cryptographic hashing
