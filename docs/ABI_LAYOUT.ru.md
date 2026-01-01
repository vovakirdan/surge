# ABI Layout (v1) — x86_64-linux-gnu
[English](ABI_LAYOUT.md) | [Russian](ABI_LAYOUT.ru.md)
> Примечание: этот файл пока не переведен; содержимое совпадает с английской версией.

This document defines the **v1 ABI layout contract** for Surge on the supported
runtime target:

- Target triple: `x86_64-linux-gnu`
- Pointer size/alignment: `8` / `8` bytes

The VM may store values differently internally, but **layout queries** must
follow these rules:

- `size_of<T>()` returns the ABI size.
- `align_of<T>()` returns the ABI alignment.

The single source of truth is `internal/layout` (`LayoutEngine`).

---

## 1. Conventions

- **Handle**: a pointer-sized value referring to a heap object.
- Handle **numeric values are not ABI-stable** and must not appear in goldens.
- `own` and type aliases **do not affect layout** (canonicalized before layout).

---

## 2. Scalars

### 2.1. Core scalars

- `bool`: size `1`, align `1`
- `unit`: size `0`, align `1`
- `nothing`: size `0`, align `1`

### 2.2. Fixed-width numerics

- `int8/uint8`: `1/1`
- `int16/uint16`: `2/2`
- `int32/uint32`: `4/4`
- `int64/uint64`: `8/8`
- `float16`: `2/2`
- `float32`: `4/4`
- `float64`: `8/8`

### 2.3. Dynamic numerics (v1 contract)

`int`, `uint`, `float` are **handle-sized** for ABI layout queries:

- `int`: `8/8`
- `uint`: `8/8`
- `float`: `8/8`

---

## 3. Pointers and References

- Raw pointers (`*T`): `8/8`
- References (`&T`, `&mut T`): `8/8`
- Function pointers: `8/8`

---

## 4. Enums

- If an enum has an explicit base type, it uses the base layout.
- Otherwise, v1 defaults to `uint32` (`4/4`).

---

## 5. Handle-Backed Values

The following surface types are handle-sized in the v1 ABI:

- `string`
- `Array<T>` / `T[]` (dynamic)
- `Range<T>`

Their ABI size/align is `8/8` on this target.

Other standard-library handle types are defined as opaque structs in
`core/intrinsics.sg` and follow normal struct layout rules (e.g. `Task<T>`,
`Channel<T>`, `Mutex`, `RwLock`, `Condition`, `Semaphore`).

---

## 6. BytesView ABI

`BytesView` is defined in `core/intrinsics.sg` and has a **stable field order**:

1. `owner: string`
2. `ptr: *byte`
3. `len: uint`

Layout (x86_64): size `24`, align `8`.

Semantics:

- `owner` keeps bytes alive.
- `ptr` points to contiguous UTF-8 bytes.
- `len == rt_string_len_bytes(&owner)`.

Tests:

- `testdata/golden/abi/abi_string_bytesview.sg`
- `internal/vm/vm_abi_layout_test.go`

---

## 7. Arrays

### 7.1. Dynamic arrays (`Array<T>` / `T[]`)

- ABI size/align is `8/8` (handle).
- Internal layout is **VM-specific** and not part of the ABI.

VM notes (not ABI-stable):

- Arrays are heap objects with element storage.
- Slicing returns a **view object** that holds a strong reference to the base.
- Views are **not resizable**; `push/pop/reserve` panic at runtime.

### 7.2. Fixed arrays (`ArrayFixed<T, N>` / `T[N]`)

Fixed arrays are stored inline:

- `align = align_of(T)`
- `stride = roundUp(size_of(T), align_of(T))`
- `size = stride * N`

Indexing uses `base + i * stride`.

Tests:

- `testdata/golden/abi/abi_core_sizes.sg`
- `internal/vm/vm_abi_layout_test.go`

---

## 8. Tuples

Tuples use the same layout rules as structs (ordered fields):

1. Each element starts at the next offset aligned to its alignment.
2. Tuple alignment is the max element alignment.
3. Total size is rounded up to tuple alignment.

---

## 9. Tagged Unions (tag/union)

Tagged unions (`tag` + union types) use a fixed v1 layout:

- Tag is `uint32` (`size=4`, `align=4`).
- Payload is the **max** sized/aligned member payload.
- `payload_offset = roundUp(tag_size, payload_align)`.
- `overall_align = max(tag_align, payload_align)`.
- `size = roundUp(payload_offset + payload_size, overall_align)`.

If a tag has multiple payload values, the payload is laid out like a tuple.

Tests:

- `internal/vm/vm_abi_layout_test.go` (tag size/align, payload offset)

---

## 10. Structs and Field Offsets

Fields are laid out in declaration order:

1. Each field starts at the next offset aligned to its field alignment.
2. Struct alignment is the max field alignment.
3. Struct size is rounded up to struct alignment.

### 10.1. Layout Attributes

Only these attributes affect layout in v1:

- `@packed` (type)
- `@align(N)` (type or field)

#### `@packed` on a struct type

- No field padding; fields are packed sequentially.
- Struct alignment is `1`.
- No tail padding.

#### `@align(N)` on a type or field

- Field alignment is `max(field_align, N)`.
- Struct alignment is `max(all field aligns, type_align_override)`.
- Struct size is rounded up to the final alignment.

`@packed` and `@align` are **mutually exclusive** (compile-time error).

---

## 11. String ABI

### 11.1. Handle and pointer access

- `string` is a handle (size `8`, align `8`).
- `rt_string_ptr(&s)` returns a pointer to contiguous UTF-8 bytes.
- The VM may materialize (flatten) rope strings on demand.

### 11.2. Length

- `rt_string_len(&s)` returns **Unicode code point count**.
- `rt_string_len_bytes(&s)` returns UTF-8 byte length.

### 11.3. Normalization

String constructors normalize input to NFC:

- `rt_string_from_bytes`
- `rt_string_from_utf16`

Other string operations preserve existing normalization.

Tests:

- `testdata/golden/abi/abi_string_bytesview.sg`
- `internal/vm/vm_abi_layout_test.go`

---

## 12. Range ABI

`Range<T>` is an **opaque handle**. The runtime uses internal states for
range literals and array iteration, but these internal layouts are **not ABI
stable**. Only `size_of` / `align_of` are.

---

## 13. Notes and References

- Layout engine: `internal/layout`
- VM layout tests: `internal/vm/vm_abi_layout_test.go`
- Goldens: `testdata/golden/abi/`
