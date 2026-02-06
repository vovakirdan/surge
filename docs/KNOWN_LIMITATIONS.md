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

