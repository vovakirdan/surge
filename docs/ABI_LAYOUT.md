# ABI Layout (v1) — `x86_64-linux-gnu`

This document defines the **v1 ABI layout contract** for Surge on the single supported target:

- Target triple: `x86_64-linux-gnu`
- Pointer size/alignment: `8` / `8` bytes

The VM may store values differently at runtime, but:

- `size_of<T>()` and `align_of<T>()` must return **ABI-correct byte values** for this target.
- Future backends (e.g. LLVM) must use the same rules.

## Conventions

- **Handle** means a pointer-sized value that refers to a heap object in the VM address space.
- Numeric handle values are **not** ABI-stable and must not appear in goldens.
- All sizes/alignments are derived from the LayoutEngine rules below.

## Scalars

- `bool`: size `1`, align `1`
- `int8/uint8`: `1/1`
- `int16/uint16`: `2/2`
- `int32/uint32`: `4/4`
- `int64/uint64`: `8/8`
- Pointers (`*T`, `&T`, `&mut T`, function pointers): `8/8`

### Dynamic-sized numeric objects (v1 contract)

`int`, `uint`, `float` are treated as **pointer-like handles** for ABI layout queries:

- `int`: `8/8`
- `uint`: `8/8`
- `float`: `8/8`

## Handle-backed values

The following surface types are handle-sized in the v1 ABI:

- `string`
- `Array<T>` (dynamic)
- `Range<T>`

`size_of`/`align_of` for these types are `8/8` on this target.

## String ABI

### Value representation

- `string` is a value that refers to a heap object via a **handle**.
- Handle values are observable only for identity/lifetime; their numeric value is not stable.

### Length contract

- `len(s)` / `rt_string_len(&s)` return **Unicode code point count**.
- `rt_string_len_bytes(&s)` returns **UTF-8 byte length**.

### Pointer contract

- `rt_string_ptr(&s)` returns a pointer to **contiguous UTF-8 bytes**.
- If `s` is a rope/slice, the runtime may **materialize (flatten)** to a cached buffer.
- After materialization, the returned pointer remains valid at least as long as the owning
  `string` value is alive (RC/lifetime rules).

### Normalization

- String constructors normalize to **NFC** (e.g., `rt_string_from_bytes`,
  `rt_string_from_utf16`). Other operations preserve this invariant.

### Stability and determinism

- Content and lengths are stable across runs.
- Pointer numeric values are **not** part of the ABI contract and must not appear in goldens.

**Tests:** `testdata/golden/abi/abi_string_bytesview.sg`, `internal/vm/vm_abi_layout_test.go` (sizes + snapshot)

## BytesView ABI

`BytesView` is defined in `core/intrinsics.sg` and its field order is ABI-stable.

### Layout

Fields in order:

1. `owner: string`
2. `ptr: *byte`
3. `len: uint`

Layout uses standard struct rules (see “Structs and field offsets”).

### Lifetime and pointer semantics

- `owner` keeps bytes alive (strong reference).
- `ptr` points to the owner’s contiguous UTF-8 bytes.
- `len == rt_string_len_bytes(&owner)`.

### Valid operations

- `len(&view)` returns byte length.
- `view[i]` returns byte `i` as `uint8`.
- Negative indices are **not** supported (out-of-bounds panic).

**Tests:** `testdata/golden/abi/abi_string_bytesview.sg`, `internal/vm/vm_abi_layout_test.go`

## Array ABI (dynamic `Array<T>`)

### Value representation

- `Array<T>` is a **handle** to a heap object (pointer-sized in the ABI).
- The heap object is conceptually `len`, `cap`, and a data pointer in VM address space.

### Header layout (conceptual)

- `len: uint`
- `cap: uint`
- `data: *byte` (data pointer in VM address space)

### Element addressing

- Element `i` address: `data + i * stride`.
- `stride = roundUp(size_of(T), align_of(T))` using LayoutEngine rules.
- Alignment is `align_of(T)`.

### Mutability and bounds

- `__index_set` writes through this addressing.
- `__index(int)` panics if out of range; negative indices are normalized via `len + i`.

**Tests:** `testdata/golden/abi/abi_arrays_views.sg`, `internal/vm/vm_abi_layout_test.go`

## Array Slice View ABI (dynamic view object)

### Surface type and ownership

- Slicing returns a **view** object that still has surface type `Array<T>`.
- The view holds a **strong reference** to the base array.

### View layout (conceptual)

- `base: Array<T>`
- `start: uint`
- `len: uint`
- `cap: uint` (stored; derived at slice creation)

### Addressing formula

- `view.data_ptr = base.data_ptr + start * stride`.
- Element `i` address: `base.data_ptr + (start + i) * stride`.

### Constraints

- Views are **not resizable**. `push/pop/reserve` must panic with a stable VM error.

**Tests:** `testdata/golden/abi/abi_arrays_views.sg`, `testdata/golden/abi/abi_arrays_views_panics.sg`, `internal/vm/vm_abi_layout_test.go`

## Fixed arrays (`ArrayFixed<T, N>` / `T[N]`)

- Storage is **inline**.
- `align = align_of(T)`
- `stride = roundUp(size_of(T), align_of(T))`
- `size = stride * N`
- Indexing uses `base + i * stride`.
- Slicing returns a dynamic `Array<T>` view.

**Tests:** `testdata/golden/abi/abi_core_sizes.sg`, `internal/vm/vm_abi_layout_test.go`

## Range<T> ABI

- `Range<T>` is an **opaque handle** to runtime state.
- Runtime uses two internal states:
  - **Descriptor ranges** for literals (`start`, `end`, `inclusive`).
  - **Iterator ranges** for array iteration (`base`, `start`, `len`, `index`).
- The internal state layout is not part of the ABI; only the handle size/align are.
- The wrapper struct in `core/intrinsics.sg` (`{ __state: *byte }`) is for ABI sizing only.

**Tests:** `internal/vm/vm_abi_layout_test.go` (sizes + snapshot)

## Structs and field offsets

Given fields in declaration order:

1. Each field starts at the next offset rounded up to the field alignment.
2. Struct alignment is `max(field aligns)`.
3. Struct size is rounded up to struct alignment.

### Layout attributes

The v1 ABI layout respects only the following layout-affecting attributes:

- `@packed` (type)
- `@align(N)` (type, field)

#### `@packed` on a struct type

- Field offsets are sequential, with **no rounding** to field alignment.
- Struct alignment is `1`.
- Struct size is `sum(size(field_i))` with **no tail padding**.

#### `@align(N)` on a type

- `align(type) = max(naturalAlign, N)`
- `size(type) = roundUp(size(type), align(type))`

#### `@align(N)` on a field

- `fieldAlign = max(naturalAlign(fieldType), N)`
- `fieldOffset = roundUp(prevOffset, fieldAlign)`
- Struct alignment is `max(fieldAligns)`

### Drop order (runtime rule)

- Local values are dropped in **reverse declaration order** (relevant for RC/lifetime).

**Tests:** `internal/vm/vm_abi_layout_test.go`, `internal/vm/vm_layout_test.go`

## Tagged unions (v1)

For `union` layout queries:

- Discriminant (tag): `uint32` with `size=4`, `align=4`
- Payload offset: `roundUp(tagSize, payloadAlign)`
- Payload size: `max(size(payload_i))`
- Payload alignment: `max(align(payload_i))`
- Union alignment: `max(tagAlign, payloadAlign)`
- Union size: `roundUp(payloadOffset + maxPayloadSize, unionAlign)`

**Tests:** `internal/vm/vm_abi_layout_test.go`
