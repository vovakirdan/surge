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
- A module declares only `const`. Module-level `let` and `let mut` are rejected (`SEM3177`), so there are no global arrays, global structs, or global mutable state. `const` accepts compile-time numbers and strings only; anything else moves into the function that uses it, or is passed explicitly. The reason is ownership: a module-level binding would be one slot every shard reads, with no boundary at which each shard could be given its own copy, whereas `const` is inlined per use site and shares nothing.

## References / Borrows

References (`&T`, `&mut T`) are second-class values: they live in locals,
parameters, and returns, never inside data. With lexical lifetimes and no
lifetime parameters, a reference stored in data could outlive the value it
borrows, so the compiler rejects (kindness-first diagnostics with owned/
`clone(x)` alternatives — the free function, which is what they print):

- Reference types in aggregates — struct fields, tag payloads, tuple/array
  element types, and tuple/array/map literal elements (`SEM3138`). Store an
  owned value instead, or pass the reference as a function parameter.
  (`@intrinsic` core types such as `BytesView` are exempt, but a `BytesView` still borrows its string:
  returning or keeping one past its string is `SEM3139`.)
- Binding a borrow to an owned non-Copy destination — an owned function/method
  parameter (`b.eat(&needle)` where `eat` takes `x: string`) or an owned
  struct-literal field (`Box{ &l }`) (`SEM3137`). Both would make the callee
  (or the aggregate's drop) and the caller free the same value.
- Returning a borrow that roots in frame-local storage — `return &local`,
  laundered `let r = &l; return r`, or `return &owned_param` (`SEM3139`).
  Returning a `&T` parameter as `&T` stays legal.
- References crossing task boundaries — `Channel<&T>` cannot be formed
  ("channel payload"), a borrow cannot be sent through a channel, and a task
  handle whose spawn borrowed frame-locals cannot be returned
  (`return spawn worker(&l)` — the caller could await it after the local is
  freed). Spawning with borrows and awaiting in the same function stays legal.
  The send rule is asked UNDERNEATH `own`, and that word is load-bearing:
  `own` says who releases a value, never that the value was copied out of where
  it lived, so `own &T` still names a place. `ch.send(own xs[0])` is refused
  exactly as `ch.send(xs[0])` is, at a plain send and at a local or far select
  arm (`SEM3105`) and inside an anchored `on ch` block (`SEM3015`, a sink with
  a rule of its own); `&mut T` is a borrow for this rule too. Asking only the
  payload's surface type left `own` a one-token bypass, and what crossed was an
  ADDRESS: a `@copy @shard_movable` pair of `int`s read in place as `ps[0]` and
  sent as `{ a: 11, b: 22 }` arrived on the far side answering `b == 11` with
  exit 0 and no diagnostic, at 2 shards and at 8, while a reader that touched
  the field the address landed on dereferenced it and crashed. Silent wrong
  answer and segmentation fault were the same corruption seen through different
  fields, and the silent half is the dangerous one.
  What `SEM3105` offers depends on the shape it refused: an element read is told
  to bind the element to a name and send the name, a payload that NAMES a borrow
  is told to read through it or to bind a copy out first (a borrow itself can
  never be given away — that is the rule), and a payload that borrows a value
  the author owns is told to send that value. `SEM3015` does NOT branch that
  way: it names an element read (`let v: T = xs[0];`) for every payload it
  refuses, so `ch.send(own p)` for a `p: &T` parameter is told to bind an
  element out of a container its program need not have. That is a rough edge in
  the wording and not in the rule — the payload is refused either way — and it
  is recorded as `RV2-DEBT-353`.

Not yet caught (future escape analysis): a local borrow deep-laundered through
several call frames before being returned, a local-borrowing task handle
escaping via an argument or container rather than `return`, aliasing through
`&mut`-parameter reborrows passed twice into one call, and anonymous-fn
captures (not loan-tracked yet).

## Arrays

- Nested arrays and multi-dimensional arrays are currently unreliable. Examples: `T[][]`, `T[N][M]`. Symptoms can include unexpected aliasing or incorrect copies. Prefer flattening (`T[N*M]` or `T[]`) with manual indexing.
- In the VM backend today, slicing a dynamic array produces a view. Views are not resizable: `push`, `pop`, and `reserve` panic at runtime (see `docs/ABI_LAYOUT.md`).
- A VIEW cannot cross a shard or thread boundary, and neither can a BASE some
  live view still reads. A view's slots are the base's slots, so letting one
  cross would put a writer on the far side inside a buffer this shard is still
  reading. The runtime refuses both by name in the crossing itself — `panic
  VM1003: array view cannot cross a shard boundary: its elements live in the
  base's buffer, which the origin shard keeps; cross an owned array instead`,
  and `array with a live view cannot cross a shard boundary: a view on this
  shard still reads its buffer`. The checker turns some of these away earlier
  with a diagnostic, but not all: a slice handed back by a callee, or carried
  out in a struct field, reaches the runtime refusal instead and the program
  exits 1. Cross an owned array, or let the view drop before the base crosses —
  once it has, the base crosses normally.
- A slice or a range over a FIXED array (`T[N]`) borrows the array itself rather
  than copying it, so it must not outlive it. `fn f() -> int[] { let a: int[4] =
  [...]; return a[[1..3]]; }` is accepted by the checker today and should not be:
  the VM refuses the read afterwards with a stale-reference panic, and the native
  backend reads freed memory instead. Keep such a slice inside the scope that owns
  the array, or copy out of it before returning. Refusing the shape at compile
  time is the fix; until then the two backends disagree about what happens, and
  the VM is the one telling the truth.
- A view, a slice or a walk over an array whose ELEMENTS can themselves hold a
  borrow is refused at build time, by decision rather than by accident; the
  build stops with `return-origin analysis unfinished`. When the element is a
  reference (`Array<&string>`), every one of `xs[[a..b]]`, `xs.slice(...)`,
  `for x in xs` and `xs.__range()` is refused. When the element is an array that
  may be a view into other storage (`Array<uint64[]>`, `int[][]`), a concrete
  read-only view `xs[[a..b]]` IS accepted — the analysis tracks the loans it
  carries, and returning one that holds a dying local's storage gets the precise
  `SEM3139` — while `for x in xs`, `xs.__range()`, the generic `xs.slice(...)`
  and storing a borrowing value into the array, through a view or directly, are
  refused. A view is not a copy — native slicing
  hands out a pointer into the base's buffer and registers the view on the
  base, and the VM's slice keeps the base alive — so a write through the view
  lands in the base, and the analysis has no model of two names reaching one
  buffer. Without that model it would accept:

  ```sg
  let mut v: Array<&string> = xs.slice(0..1);
  {
      let s: string = "x";
      v[0] = &s;      // writes into xs's buffer
  }
  return xs;          // xs[0] points at the dead s
  ```

  When the boundary was chosen (2026-09-16), a scan of the golden corpus,
  `core`, `stdlib`, the benchmarks, the showcases and the Surge sources inside
  Go tests found no program outside the analysis's own probes that uses these
  shapes. Rewrites the analysis already answers (measured at `5d83577e`, each
  leaving no unfinished row of its own):
  - store owned elements — `Array<string>` rather than `Array<&string>`; a view
    or an index read over it is accepted;
  - walk by index — `let n: int = xs.__len() to int; let mut i: int = 0;
    while i < n { let v = xs[i]; ... i = i + 1; }` — which is accepted over
    `Array<uint64[]>` as long as no element leaves the function;
  - write through the array, not through a view — `xs[0] = &s` is refused with
    the precise `SEM3139` ("borrow of `s` outlives its owner") when it is wrong,
    and accepted when the stored reference lives long enough;
  - for a nested array, replace a whole row (`grid[1] = row;`) or flatten it
    (`grid[r * width + c]`).

  The way to lift the boundary — a buffer-alias model in the analysis — is
  written down in `docs/runtime-v2-epics/22-step7-execution.md` ("D2 boundary")
  and tracked as `RV2-DEBT-364`.

## Concurrency / Runtime

- VM backend: no OS-thread parallelism (single-threaded runtime). See `docs/RUNTIME.md`.
- `parallel map/reduce` and `signal` are reserved and rejected. See `docs/PARALLEL.md`.

## Types / Stdlib

- `print` is single-argument today; multi-argument `print("a", "b")` is not supported.
- `Map<K, V>` keys are limited to `string` and integer types in v1.
- Raw pointers (`*T`) are restricted to `extern` and `@intrinsic` declarations; there is no `unsafe` user mode yet.
- Reading a MOVE-ONLY composite out of a container yields an ALIAS, not a value, and the container stays usable beside it: `let e = o.inner; e.x = 99;` is visible through `o`. Moving a field out of a live struct is a partial move, and partially-moved bindings are not tracked yet, so the program is neither rejected nor made independent. `@copy` composites are unaffected — those duplicate.
- A `compare` over a value-composite scrutinee that is Copy or read through a borrow LEAKS its duplicate — roughly two blocks per evaluation for `compare *h { ... }` on a `@copy` union. The value is correct and nothing is freed twice; memory grows with the number of evaluations.
- A value whose arbitrary-precision numbers, or whose dynamic array, sit BEHIND A HANDLE cannot cross a shard boundary: a map's table, a channel's ring, a task's result slot. The crossing makes a value's counted blocks private before it ships, and walks a dynamic array's buffer element by element, but neither walk is ever handed the storage a handle only names — so `Map<int, float>`, `Channel<float>`, `Map<int, int[]>` and `Channel<int[]>` are refused with a message naming the table or the ring **at every sink that asks the question: a capture of `on`, `spawn on` or `blocking`, a channel's element type at its creation, and the reply of an `on` or `spawn on` body. One sink asks neither question** — a `blocking` body's RESULT, `blocking { ... ret m }` — and what you get there depends on which half of the pair `m` is, with no diagnostic in either half. For a DYNAMIC ARRAY behind the handle (`m: Map<int, int[]>`, `m: Channel<int[]>`) nothing is asked at all: the program builds and runs, and that is the safety hole. For COUNTED BLOCKS behind the handle (`m: Map<int, float>`, `m: Channel<float>`) the LLVM emitter stops the build instead — `Error: LLVM emit failed: ... (Unshare): unshare of Map<int, float> ...: the value may share a counted block that the walk cannot make private (a map's table or a channel's ring has no walk)` — which is a build failure naming compiler internals, not a diagnostic pointing at your code. Both halves are recorded as `RV2-DEBT-356`. Take the value out of the container and cross it on its own, or in a field of the value that crosses; for a map or channel that must itself cross, use a fixed-width element type (`float64`) and keep dynamic arrays out of it. A `@copy` composite carrying an arbitrary-precision `float` DOES cross — as a capture, as a channel element and as a reply. A `float[]` crosses at fewer sinks: as an `own` capture of `on`, `spawn on` or `blocking`, as a `blocking` result, and as a remote channel's element — but NOT as the reply of an `on` or `spawn on` body, which refuses it with `FUT7020`, "the crossing result `Array<float>` cannot ride the reply: it is not plain-copy data". The reply takes plain-copy data only and an array is not Copy, so that rule turns away any owned value there, a struct carrying a `float` included. `int` and `uint` do not cross-share a count for the opposite reason to the one this entry used to give: they are not reference-counted scalars yet at all.
- A `__clone` declared on an alias alone (`type Handle = Leaf` with `extern<Handle> { fn __clone(self: &Handle) -> Handle }` and nothing on `Leaf`) is accepted at its declaration but not found at the use site: `clone(&value)` reports `SEM3116` naming `Leaf`. Canonical clone selection asks the type the alias resolves to, and a declaration written against the alias spelling does not answer for it. Declare `__clone` on the target type. Other magic methods declared on an alias alone are unaffected.

## Native (LLVM) backend

These are sharp edges specific to the native (LLVM) backend. The VM backend is not affected by any of them.

- `format(...)` / `fmt_arg(...)` with a `string` or string-slice argument currently double-frees: `format("x={}", fmt_arg("hi"))` aborts with "double free or corruption", and a head slice such as `fmt_arg(s[0..50])` reports "free(): invalid pointer". Integer arguments (`fmt_arg(42)`) are unaffected. The VM formats all argument types correctly.
- `for` loops leak per iteration: each step boxes an `Option<T>` on the heap and the iterator object is allocated once, but neither is reclaimed yet. This affects both array iteration and integer range-for. It is a steady per-iteration leak (memory grows with iteration count), not a correctness bug.
- Small `int`/`uint` values are stored inline in the runtime word and no longer allocate, so hot integer loops are allocation-balanced. Genuinely large integers (outside the inline range, roughly `|v| >= 2^62`) are still heap-boxed and, because `int`/`uint` are `Copy`, are not reclaimed when retained to scope exit — such values leak. Arithmetic that consumes a large intermediate still frees it, so only values kept past their last use leak.
- Integer and unsigned range-for (`for i in a..=b` or `a..b`, in a for-head or as a stored/passed `Range<int>` / `Range<uint>` value) works correctly on both backends. The residual gap: a fixed-width `Range<i32>` used as a stored value is not yet covered on the native backend (the common `int`/`uint` case is).

