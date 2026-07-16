# Known Limitations (v1)

This document tracks known limitations and sharp edges of the current Surge
implementation (compiler, standard library, and runtimes). It is not intended
to be an exhaustive specification.

See also:
- `docs/LANGUAGE.md`
- `docs/PARALLEL.md`
- `docs/RUNTIME.md`
- `docs/ABI_LAYOUT.md`

## Language / Syntax

- `mut` in function parameters is not supported: `fn foo(mut a: int)` is rejected. Use a mutable local inside the function (`let mut x = a;`) or take a mutable reference (`a: &mut T`).
- `parallel` and `signal` keywords are reserved but not supported yet (compile-time error).

## References / Borrows

References (`&T`, `&mut T`) are second-class values: they live in locals,
parameters, and returns, never inside data. With lexical lifetimes and no
lifetime parameters, a reference stored in data could outlive the value it
borrows, so the compiler rejects (kindness-first diagnostics with owned/
`.__clone()` alternatives):

- Reference types in aggregates — struct fields, tag payloads, tuple/array
  element types, and tuple/array/map literal elements (`SEM3138`). Store an
  owned value instead, or pass the reference as a function parameter.
  (`@intrinsic` core types such as `BytesView` are exempt; the runtime pins
  their storage.)
- Binding a borrow to an owned non-Copy destination — an owned function/method
  parameter (`b.eat(&needle)` where `eat` takes `x: string`) or an owned
  struct-literal field (`Box{ &l }`) (`SEM3137`). Both would make the callee
  (or the aggregate's drop) and the caller free the same value.
- Returning a borrow that roots in frame-local storage — `return &local`,
  laundered `let r = &l; return r`, or `return &owned_param` (`SEM3139`).
  Returning a `&T` parameter as `&T` stays legal.

Not yet caught (future escape analysis): a local borrow deep-laundered through
several call frames before being returned.

## Arrays

- Nested arrays and multi-dimensional arrays are currently unreliable. Examples: `T[][]`, `T[N][M]`. Symptoms can include unexpected aliasing or incorrect copies. Prefer flattening (`T[N*M]` or `T[]`) with manual indexing.
- In the VM backend today, slicing a dynamic array produces a view. Views are not resizable: `push`, `pop`, and `reserve` panic at runtime (see `docs/ABI_LAYOUT.md`).

## Concurrency / Runtime

- VM backend: no OS-thread parallelism (single-threaded runtime). See `docs/RUNTIME.md`.
- `parallel map/reduce` and `signal` are reserved and rejected. See `docs/PARALLEL.md`.

## Types / Stdlib

- `print` is single-argument today; multi-argument `print("a", "b")` is not supported.
- `Map<K, V>` keys are limited to `string` and integer types in v1.
- Raw pointers (`*T`) are restricted to `extern` and `@intrinsic` declarations; there is no `unsafe` user mode yet.

## Native (LLVM) backend

These are sharp edges specific to the native (LLVM) backend. The VM backend is not affected by any of them.

- `format(...)` / `fmt_arg(...)` with a `string` or string-slice argument currently double-frees: `format("x={}", fmt_arg("hi"))` aborts with "double free or corruption", and a head slice such as `fmt_arg(s[0..50])` reports "free(): invalid pointer". Integer arguments (`fmt_arg(42)`) are unaffected. The VM formats all argument types correctly.
- `for` loops leak per iteration: each step boxes an `Option<T>` on the heap and the iterator object is allocated once, but neither is reclaimed yet. This affects both array iteration and integer range-for. It is a steady per-iteration leak (memory grows with iteration count), not a correctness bug.
- Small `int`/`uint` values are stored inline in the runtime word and no longer allocate, so hot integer loops are allocation-balanced. Genuinely large integers (outside the inline range, roughly `|v| >= 2^62`) are still heap-boxed and, because `int`/`uint` are `Copy`, are not reclaimed when retained to scope exit — such values leak. Arithmetic that consumes a large intermediate still frees it, so only values kept past their last use leak.
- Integer and unsigned range-for (`for i in a..=b` or `a..b`, in a for-head or as a stored/passed `Range<int>` / `Range<uint>` value) works correctly on both backends. The residual gap: a fixed-width `Range<i32>` used as a stored value is not yet covered on the native backend (the common `int`/`uint` case is).

