# ABI Layout (v1) — `x86_64-linux-gnu`

This document defines the **v1 ABI layout contract** for Surge on the single supported target:

- Target triple: `x86_64-linux-gnu`
- Pointer size/alignment: `8` / `8` bytes

The VM may store values differently at runtime, but:

- `size_of<T>()` and `align_of<T>()` must return **ABI-correct byte values** for this target.
- Future backends (e.g. LLVM) must use the same rules.

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

## `own T`

`own T` has the same ABI layout as `T`.

## Structs

Given fields in declaration order:

1. Each field starts at the next offset rounded up to the field alignment.
2. Struct alignment is `max(field aligns)`.
3. Struct size is rounded up to struct alignment.

## Fixed arrays (`ArrayFixed<T, N>` / `T[N]`)

- `align = align(T)`
- `stride = roundUp(size(T), align(T))`
- `size = stride * N`

## Tagged unions (v1)

For `union` layout queries:

- Discriminant (tag): `uint32` with `size=4`, `align=4`
- Payload offset: `roundUp(tagSize, payloadAlign)`
- Payload size: `max(size(payload_i))`
- Payload alignment: `max(align(payload_i))`
- Union alignment: `max(tagAlign, payloadAlign)`
- Union size: `roundUp(payloadOffset + maxPayloadSize, unionAlign)`

