# Epic 23b Wave C: Ordinary Storage Proving Spike

Wave C commit C1 output. This document is the `RULES.md` Global Rule 1 record
for the ordinary-storage representation change, written before the first P1
implementation commit, together with the frozen corpus that proves it.

Authority: `23b-inline-storage-and-typed-carriers.md` Section 6 (P1),
`23-storage-model-and-typed-carrier-abi.md` Sections 4-9 and 16,
`23b-wave-a-allocation-census.md` (the frozen allocation oracle).

Base commit for every anchor and every measurement in this document:
`535df877`. Spike branch: `codex/epic23b-wavec-c1-fixtures-20260806`.

The Wave C plan is four agentmemory records read together —
`mem_mshfijag` (the plan), `mem_mshfoed5` (the two lead decisions),
`mem_mshggrma` (revision v2, which raised the alignment invariant P0-1, the
call-layout purity rule P0-2, the bench-counter deliverable P1-5 and this
document P1-6), and `mem_mshglrj2` (addendum v3, the pre-split token rule and
the byval one-copy wording) — as amended by the independent plan review
`mem_mshgco4o`. The Wave C execution handoff is `mem_msfb4gxl`.

## Proving Spike Record (RULES.md Global Rule 1)

- **Hypothesis:** one nested struct/tuple/union/fixed-array value can be carried
  through construction, copy, move, borrow, partial move, overwrite, direct
  call, indirect call, return, early exit and partial initialization on BOTH
  backends using exact-layout inline storage — a byte arena plus a
  discriminated `LocalSlot` in the VM, and an `[N x i8]` storage type with an
  `align` from `PhysicalFacts` plus an sret/byval destination protocol chosen by
  `CallLayout` in LLVM — with backend parity, zero composite-box allocations, no
  integer or pointer carrier reinterpretation in the emitted IR, a strict-zero
  Valgrind and ASan result, and every `align` operand sourced from
  `PhysicalFacts` rather than inferred.
- **Files, paths and runtime surfaces this spike may touch:** the P1 slice of
  the VM storage core (`internal/vm` frame/place/slot storage and the ordinary
  composite operations that read it) and of LLVM emission (type and signature
  construction, function definition, direct and indirect call, return, place and
  aggregate access); `internal/mir/call_layout.go` from commit C0; and the
  corpus and harness this commit adds —
  `testdata/runtime-v2-ordinary-storage/**` and the four
  `internal/vm/runtime_v2_ordinary_storage_*_test.go` files. No container, map,
  task, channel, select, blocking, async-frame or far/transport surface is in
  the spike: those are Wave D and Wave E, and the fixed-array and dynamic-array
  ELEMENT buffers change only in each backend's own cutover commit, per the lead
  decision recorded in `mem_mshfoed5`.
- **Explicitly non-final behavior:** the old boxed representation and the new
  inline representation may coexist ON THE SPIKE BRANCH ONLY, for as long as the
  vertical needs both to compare against. That coexistence is never integrated.
  It is also why the spike branch is not `runtime-v2-carrier-check` gated — the
  gate keys on exact evidence text and a branch that keeps both paths would
  report every retained token as unexpected. The cutover commits ARE gated, and
  they are the commits that delete the old path.
- **Proof:**
  1. **Parity.** `TestRuntimeV2OrdinaryStorageParity` runs all fifteen corpus
     rows on the VM and as a native binary and compares stdout, stderr AND exit
     code. All three channels matter: the corpus reports failure through an exit
     code, so two backends returning 1 for different reasons are not agreement.
  2. **The frozen allocation oracle.** The four `local-*` rows of
     `testdata/runtime-v2-carrier-bench.json` —
     `local-copy`, `local-argument`, `local-return`, `local-fixed-array` — each
     carry `candidate_structural_allocations_per_batch` 0, and `local-copy`
     additionally carries `bytes_copied == 8192` and `callback_count == 128`.
     The counter invariants are a Wave C deliverable, not an accident: the
     generated copy path must emit exactly one 64-byte
     `rt_carrier_bench_record_copy` and one `rt_carrier_bench_record_callback`
     per counted operation, which is what bounds doc 23 Section 5's permission
     to inline copies. These four rows gate the LLVM lane
     (`"backend": "llvm"`); they are that lane's exit criterion and are not
     deferrable.
  3. **The alignment invariant (P0-1), as layer-3 IR assertions.** No
     unattributed `alloca`, `load` or `store` through storage-typed memory
     anywhere in the emitted IR: an `alloca`'s align is the layout align, a
     packed field access is align 1 or a `memcpy`, and every `byval`/`sret`
     attribute carries an explicit align. This is new emission discipline —
     `emit_func.go:126` writes no align today, and an unattributed
     `alloca [N x i8]` canonicalizes to align 1 while an unattributed
     `store i64` through it infers align 4. The assertions run against the
     SHIPPING artifact; timing and resource artifacts may differ only in
     instrumentation, never in representation. The `storage_packed_field` and
     `storage_over_aligned` corpus rows are the programs these assertions are
     checked against.
  4. **The VM lane's own exit evidence:** the layer-2 type-aware structural
     absence gate, plus VM/LLVM parity over the full corpus. The `local-*` bench
     rows do not gate the VM lane.
  5. **Valgrind and ASan, strict zero,** over the corpus binaries.
  6. **The overflow negative-control battery**, ten sources under
     `testdata/runtime-v2-ordinary-storage/overflow/`, each failing before any
     storage is laid out or allocated with the first overflowing type path in a
     note. Detailed below.
- **Success criteria:** every Section 6 P1 property holds — backend parity, zero
  composite-box allocations, no integer or pointer carrier reinterpretation in
  the emitted IR, a strict-zero Valgrind and ASan result, and a destination
  protocol that handles early exit and partial initialization — and every proof
  row above is green on both backends.
- **Failure criteria:** any parity divergence on any corpus row; any nonzero box
  allocation against the frozen per-batch budget; any unattributed align on an
  `alloca`, `load`, `store`, `byval` or `sret` through storage-typed memory; any
  Valgrind or sanitizer finding; or any overflow that reaches allocation instead
  of a compile-time diagnostic.
- **Disposition on success:** each backend's cutover commit DELETES that
  backend's old representation path. That commit is the spike's accept step —
  the point at which the coexistence described above stops existing — and it is
  the only commit in the lane that contains the representation flip, because
  every preparatory refactor (call-emitter unification, the
  `emit_access.go`/`vm/place.go`/`vm/heap.go` pre-splits, the storage types,
  `CallLayout`) lands earlier as its own green commit.
- **Rollback note:** delete the spike branch. No production surface changes
  until a cutover commit, and this commit changes no production code at all: it
  adds a document, a corpus under `testdata/`, and four test files.

## The Frozen Corpus

`testdata/runtime-v2-ordinary-storage/`. Both backend lanes drive it; neither
owns it. Every source is representation-free by construction — it names no box,
no pointer, no count and no allocation total, and every row asserts a property
of the LANGUAGE. A fixture that could tell today's representation from
tomorrow's would be pinning the thing that is about to change, so the corpus is
green before the cutover, green after it, and neither lane edits a fixture to
flip.

Each source self-checks and reports through its exit code and one line of
stdout, so a divergence is a diff of two short strings rather than an
inspection of two runtimes. `TestRuntimeV2OrdinaryStorageCorpusIsComplete` and
`TestRuntimeV2OrdinaryStorageOverflowBatteryIsComplete` keep the tables and the
tree the same set, because a fixture nothing runs reads like coverage.

### The family

```
@copy type Leaf = { x: int, y: int };
tag Wrapped(Leaf); tag Bare();
type Choice = Wrapped(Leaf) | Bare;
type Nested = { head: …, pair: (int, int), choice: Choice, cells: …[3] };
```

The family is MOVE-ONLY, and that is forced rather than chosen: the language
makes tuples and fixed arrays non-Copy, so a `@copy` type may not contain
either. A struct that holds a tuple, a two-arm union with a composite payload
and a fixed array therefore cannot be copied in one assignment, and the copy
row covers the Copy sub-composites it is built from plus a field-wise
duplicate of the whole thing. The rows that need a released resource
(`storage_move`, `storage_partial_move`, `storage_overwrite`,
`storage_direct_call`, `storage_indirect_call`, `storage_return`,
`storage_early_exit`, `storage_partial_init`) carry a `string` member so a
missing or doubled release is real work a memory checker can see.

### Operation rows — `ops/`

| Row | What it drives |
| --- | --- |
| `storage_construct` | local construction; read back through a struct field, a tuple element, a union payload, a fixed-array element, and the empty arm |
| `storage_copy` | copy independence in BOTH directions for a struct, a `@copy` two-arm union, a struct member and an array element lift, plus a field-wise duplicate of the whole family |
| `storage_move` | whole-value move; a three-hop relay through call boundaries; a move loop keeping one live value |
| `storage_borrow` | shared and mutable borrows of every member kind; the read-write-read sequences that separate a borrow from a silent copy |
| `storage_partial_move` | `own` of a struct field, reinitialization, the residual dropped by the callee, and a partial move left open across a scope exit |
| `storage_overwrite` | whole-value and per-member overwrite; union arm replacement both ways; self-assignment; an in-place tuple swap; take-replace-take-restore on a move-only field |
| `storage_direct_call` | by value, by shared borrow, by mutable borrow, several composites in one argument list, and a call whose arguments are call results |
| `storage_indirect_call` | the same signatures through function values, including a value rebound to a second implementation of the same type, and a composite returned indirectly |
| `storage_return` | one return site; two return sites into one destination; an early return from a loop and the loop's own exit; a forwarded return; a Copy composite returned out of a borrow |
| `storage_early_exit` | four functions whose early exits differ from their fall-through exits in the NUMBER of live values, plus an exit after a partial move and an exit under a live borrow |
| `storage_partial_init` | a destination rewritten member by member and observed — by return, by borrow and across loop iterations — while only part of the rewrite has happened |

### Layout rows — `layout/`

| Row | What it drives |
| --- | --- |
| `storage_packed_field` | `@packed` fields at offsets their own alignment does not divide; packed values nested in an unpacked struct, as fixed-array elements with an odd stride, and across call boundaries |
| `storage_over_aligned` | `@align(64)` and `@align(32)` types as locals, as fields between narrower neighbours, as array elements, and as argument and return destinations |
| `storage_padding_union` | types shaped to have padding holes on purpose, and a two-arm union whose arms differ in size, switched back and forth eight times |
| `storage_zero_sized` | a zero-sized value constructed, copied, moved, borrowed, returned, relayed, and held as a member beside sized ones |

`storage_padding_union` cannot assert anything about the bytes in a hole —
they are unreadable from the language, and that half of the claim belongs to
the IR assertions and the memory checker. What it pins is the observable
consequence: a copy, move or re-tag that walks bytes it does not own produces
wrong live members, and a union that consults its inactive arm answers wrongly
after a switch.

### Known-red rows — `known_red/`

Two zero-sized shapes produce the right answer on both backends and then abort
the native process while the value is released. They are not in the green
corpus, and they are not skipped either: their rows assert the DIVERGENCE — the
VM must be clean, the native binary must abort through the allocator after
printing its success line. That keeps the defect as executable evidence with a
name, and makes the fix self-announcing: when the native run stops aborting the
row fails and says to move the source into `layout/`.

| Row | Defect |
| --- | --- |
| `storage_zero_sized_member_order` | a zero-sized member FOLLOWED by a sized one double-frees on release; reordering so the zero-sized member is last makes it go away |
| `storage_zero_sized_array` | a fixed array of zero-sized elements double-frees on release |

### Overflow battery — `overflow/`

Ten sources. Seven must be refused at compile time; two must compile cleanly
because their defect is a SPURIOUS overflow rather than a missed one; one is
owed by a later workstream and says so.

| Row | Expectation at `535df877` |
| --- | --- |
| `nested_fixed_array_length` | `SEM3180`, path `Grid -> alias target [Row; 2] -> array element Row` |
| `array_stride_times_count` | `SEM3180`, path `Line -> alias target [Half; 4] -> array element Half` |
| `struct_field_padding` | `SEM3180`, path `FieldPadding -> field edge (Wide)` |
| `struct_tail_padding` | `SEM3180`; no path note — the overflowing site is the root type's own tail |
| `packed_total_size` | `SEM3180`, path `PackedTotal -> field edge (Wide)` |
| `over_aligned_round_up` | `SEM3180`; no path note — the overflowing site is the root type's own round-up |
| `alignment_target_width` | `SEM3181`; a validated `@align(2^63)` must be refused with the target limit, never silently converted to a host-width integer |
| `zero_sized_multiplication` | compiles clean — a zero stride times any count is zero, and a diagnostic here would mean the element's stride had been rounded up to its alignment |
| `field_offset_above_int32` | compiles clean — a field offset of 2^32 must survive intact rather than wrap through a signed 32-bit conversion |
| `envelope_sidecar_total` | OWED BY THE LANE, skipped with that reason |

The battery runs through `surge diag`, which is where the refusal has to
happen: no row reaches a backend, so no overflowing layout can have reached an
allocator.

`envelope_sidecar_total` is skipped rather than asserted because the arithmetic
does not exist. `internal/layout` sizes types only; the crossing plan's
envelope-plus-payload-plus-sidecar total belongs to the transport work that
follows ordinary storage. The source records the shape so the row has a home the
moment the arithmetic lands.

The two rows without a path note are a real limit on "the first overflowing type
path in a note", and the table states it rather than hiding it: when the failing
operation is the root type's own padding or round-up there is no member below
the root to point at, so `LayoutError.path` is empty and the message carries the
type name alone. Both are accurate; neither is a path.

## Measured Pre-Cutover Baseline

All measurements at `535df877` on the reference host, clang 18.1.3,
`SURGE_THREADS=1`. This is the RED endpoint the cutover has to move, not a
statement of health.

### Parity — green, 15 of 15

`TestRuntimeV2OrdinaryStorageParity` passes every row on stdout, stderr and exit
code. Two sources had to be written around backend gaps to get there, both
recorded under Defects below.

### Valgrind — RED on 8 of 15 rows

`valgrind --leak-check=full`, definitely-lost bytes and invalid accesses:

| Row | Definitely lost | Invalid accesses |
| --- | ---: | ---: |
| `storage_construct` | 16 bytes / 1 block | 0 |
| `storage_copy` | 32 bytes / 2 blocks | 0 |
| `storage_move` | 0 | 0 |
| `storage_borrow` | 24 bytes / 1 block | 0 |
| `storage_partial_move` | 0 | 0 |
| `storage_overwrite` | 104 bytes / 5 blocks | 0 |
| `storage_direct_call` | 45 bytes / 2 blocks | 0 |
| `storage_indirect_call` | 0 | 0 |
| `storage_return` | 0 | 0 |
| `storage_early_exit` | 0 | 0 |
| `storage_partial_init` | 368 bytes / 16 blocks | 0 |
| `storage_packed_field` | 0 | 0 |
| `storage_over_aligned` | 0 | 0 |
| `storage_padding_union` | 1,008 bytes / 18 blocks | 0 |
| `storage_zero_sized` | 0 | **2** |

The leaks are abandoned composite boxes — the allocations Wave C exists to
delete — so the strict-zero success criterion is the cutover's outcome, not its
entry condition. The two invalid accesses in `storage_zero_sized` are a
different kind of finding and are recorded below.

## Defects Found While Freezing The Corpus

Five pre-existing defects surfaced while writing sources that only use ordinary
composites. None was introduced by this commit; this commit changes no
production code. They are listed here because a corpus that had been quietly
routed around them would have looked like coverage.

1. **A struct with a zero-sized member overruns its box (`storage_zero_sized`).**
   Valgrind reports `Invalid write of size 8` at `0 bytes after a block of size
   32` from the constructing function, and a matching `Invalid read of size 8`
   from that type's generated drop glue. The program prints the right answer and
   exits 0 — the write lands in allocator slack — so nothing but a memory
   checker sees it.
   This is a heap buffer overflow on the current representation and is the most
   severe of the five.

2. **A zero-sized member followed by a sized one double-frees on the native
   backend.** Pinned by `known_red/storage_zero_sized_member_order`. Reordering
   the members so the zero-sized one is last makes the abort go away, which
   points at the release walk addressing members after a zero-sized one as if it
   had storage.

3. **A fixed array of zero-sized elements double-frees on the native backend.**
   Pinned by `known_red/storage_zero_sized_array`. Minimal reproducer:

   ```sg
   type Empty = { };
   @entrypoint
   fn main() -> int {
       let cells: Empty[2] = [Empty { }, Empty { }];
       return 0;
   }
   ```

4. **`bool == bool` and `bool != bool` do not emit on the LLVM backend.** The VM
   runs them; `surge build` fails with
   `LLVM emit failed: … unknown external function "__eq"` (or `"__ne"`).
   Minimal reproducer:

   ```sg
   @entrypoint
   fn main() -> int {
       let a: bool = true;
       let b: bool = false;
       if a == b { return 1; }
       return 0;
   }
   ```

   The three layout rows originally used boolean equality and were rewritten to
   use `!` and direct truth tests, which both backends handle.

5. **An inferred `let mut e = a[i]` on a Copy composite element binds a
   reference, and the two backends then disagree about what a write through it
   does.** With an explicit `let mut e: Leaf = a[i]`, or through a by-value
   return, the copy happens correctly. Without the annotation the VM traps at
   runtime (`panic VM2102: store through non-mutable reference`) while the
   native binary silently writes through into the array element. Both exit 1, by
   coincidence, through completely different behaviour. Minimal reproducer:

   ```sg
   @copy type Leaf = { x: int, y: int };
   @entrypoint
   fn main() -> int {
       let arr: Leaf[3] = [Leaf{x=3,y=4}, Leaf{x=5,y=6}, Leaf{x=7,y=8}];
       let mut lifted = arr[1];
       lifted.x = 99;
       if arr[1].x != 5 { return 1; }
       return 0;
   }
   ```

   `storage_copy` lifts array elements through a by-value return and says why in
   its header, so the corpus states what must be true rather than pinning the
   defect.

Two further borrow-checker conservatisms shaped how the sources are ORDERED and
are recorded so the next author does not rediscover them: a read of an indexed
place (`n.cells[i]`) holds a shared borrow of the container for the rest of the
function, so every later `&mut n` and every later move of `n` is refused, while
a field read does not do this; and an exclusive borrow bound to a local blocks
all other access to the value until the end of the function. Both are noted
inline in the sources that work around them.

## Harness

Four new test files, package `vm_test`, no production code:

| File | Contents |
| --- | --- |
| `runtime_v2_ordinary_storage_corpus_test.go` | the shared fixture table, the tree/table completeness check, and the VM driver |
| `runtime_v2_ordinary_storage_parity_test.go` | VM versus native binary over the same table |
| `runtime_v2_ordinary_storage_known_red_test.go` | the two quarantined zero-sized rows, asserting the divergence |
| `runtime_v2_ordinary_storage_overflow_test.go` | the overflow battery table and driver |

The tables live in the corpus file and are consumed by the others, so a row is
added once. Neither backend lane owns any of these files; each lane's own
allocation-census, IR-assertion and sanitizer rows land in its own new files,
per the plan's per-lane file rule.
