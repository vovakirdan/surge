# Runtime V2 Storage Model And Typed-Carrier ABI

Status: **ACCEPTED DESIGN — implementation is owned by Epic 23b**

Decision date: 2026-08-04

This document is the normative Runtime V2 contract for value storage,
places/references, ordinary calls, and every runtime carrier of a statically
known Surge value. It refines `../RUNTIME_V2.md` and supersedes the physical
Phase 2 placeholders in `23-value-composites.md`. The language semantics and
Phase 1 history in Epic 23 remain authoritative.

The document describes the target, not the current implementation. Until Epic
23b closes, `../LANGUAGE.md` and `../CONCURRENCY.md` continue to describe the
implemented user-visible behavior.

## 1. Problem

Surge's type system says that structs, tuples, tagged unions, and fixed arrays
are values. The VM and LLVM backend currently box those values, while the
native async/runtime ABI erases many unrelated values into one machine word.
That mismatch leaks into ordinary calls, task results, channel buffers, maps,
`select`, blocking jobs, and far transport.

A wider or non-scalar value therefore reaches one of three bad outcomes:

1. it is boxed only to fit the one-word path;
2. it is reinterpreted as `uint64`/`i64`, losing layout and lifecycle facts;
3. a new path grows a private adapter and the runtime keeps two ownership
   protocols indefinitely.

Phase 1 of Epic 23 repaired value semantics behind the existing boxes. Phase 2
must remove the false representation itself. Replacing only ordinary calls or
only channels is not a coherent intermediate architecture: any surviving
one-word carrier becomes the universal fallback again.

## 2. Accepted Decisions

1. **One typed-carrier model, end to end.** Every carrier stores an exact-sized,
   correctly aligned `T` and has access to the monomorphic operations for `T`.
2. **Value composites are inline.** Structs, tuples, unions, and fixed arrays
   are never heap-boxed merely to make them fit a word-sized ABI.
3. **Destination-owned initialization.** A producer initializes storage owned
   by the receiver/carrier. Composite returns use a caller-provided result
   destination; carriers own their slots.
4. **Descriptors are per homogeneous owner region, not repeated per value.** A
   task result owner, channel, dynamic array, map key/value region, or transport
   envelope records the relevant descriptor once. A heterogeneous owner such as
   `select` or an async frame records a compiled descriptor table once and each
   state/arm selects its statically known entry. Slots contain payload and
   lifecycle state, not copied callback metadata.
5. **Static code stays statically typed.** Ordinary monomorphic code calls or
   inlines concrete operations directly. Type erasure exists only at truly
   generic runtime boundaries.
6. **There is no compatibility layer.** Old runtime ABI/API entrypoints,
   one-word payload fields, adapters, wrappers, and obsolete tests are deleted.
   In-tree compiler, runtime, stdlib, and tests migrate atomically.
7. **No new source syntax.** This design changes representation and validates
   the already approved `Task<T>.clone()` rule. Any additional public language
   API or syntax decision still requires owner review.
8. **The merge to `main` is Surge 0.2.0.** The implementation branch does not
   claim the version early. Main integration updates `internal/version`'s
   fallback, new-project/tool metadata defaults, and public install examples to
   `0.2.0`, with a version-surface gate. Publishing tag `v0.2.0` remains the
   project owner's explicit main-only release-workflow action after merge; tag
   publication is not a hidden prerequisite for the implementation epic.

## 3. Non-Goals

- source, generated-object, or runtime compatibility with pre-0.2 programs;
- adapters that let old and new payload ABIs coexist;
- a stable external C ABI or dynamically unloadable type descriptors;
- wire-format compatibility between independently built runtimes;
- language-level recoverable out-of-memory semantics;
- finishing the WidthAny `int`/`uint` reclamation owned by Epic 22;
- new crossing, clone, or ownership syntax.

Only an operation that returns an explicit internal status is locally
rollback-capable: it drops initialized destination children and restores the
destination to `EMPTY` before the status follows its existing internal-error
path. `panic`/`exit` do not return, and raw allocator `NULL` is not a
`ValueOps` status. This is an implementation-safety rule, not a new unwind or
recoverable-OOM language feature.

## 4. Canonical Layout

`../ABI_LAYOUT.md` and `internal/layout` remain the single layout authority.
Epic 23b must not create a second layout calculator.

For each concrete `T`, lowering can obtain:

```text
ValueLayout<T> = {
    size,       // ABI bytes, including tail padding
    align,      // required base alignment
    stride,     // roundUp(size, align), for repeated elements
    flags       // Copy, Clonable, Droppable, Traceable, ShardMovable, ...
}
```

The layout rules include packed/over-aligned structs, active-union payload
offsets, fixed-array stride, and zero-sized values. A zero-sized value still
has a lifecycle state; code must not manufacture or dereference an invalid
address merely because no bytes are copied.

All layout and carrier arithmetic is checked in the target address width.
`roundUp`, field offset plus size, `stride * count`, envelope plus padding,
sidecar totals, and credit sums must report overflow before allocation or
lowering; signed host `int` wrap is forbidden. A statically known type that
exceeds the target address space receives a compile-time diagnostic at the
type/array length with a note showing the first overflowing layout path. A
dynamic carrier plan returns an explicit overflow/capacity status while the
source remains owned and the destination empty. Nested fixed arrays at the
maximum accepted length, packed/over-aligned aggregates, zero-sized elements,
and envelope-total overflow are required negative controls.

Initialized values have deterministic padding. Constructors/initializers zero
struct tail/inter-field padding and inactive union payload bytes before
publication; field-wise operations may then leave those bytes unchanged. A
whole-byte copy is permitted only from a fully initialized value whose padding
obeys this invariant. Hash/equality remain field/active-arm based and never
inspect padding. Transport cannot publish uninitialized padding.

Handle-backed language values remain handle-sized. In particular, a `Task<T>`
or `Channel<T>` handle may remain one word while the result or element it owns
uses exact typed storage. Dynamic arrays, strings, maps, ranges, runtime
resources, and refcounted numeric handles are not turned into inline object
graphs by this design.

## 5. Monomorphic Value Operations

Every erased owner uses compiler-generated, monomorphic operations for its
concrete payload. The exact C spelling may evolve, but the semantic interface
is fixed:

```text
ValueOps<T> {
    layout: ValueLayout<T>
    move_init(dst_empty, src_initialized)
    copy_init(dst_empty, src_initialized)       // only when T is Copy
    clone_init(dst_empty, src_initialized)      // when T is Clonable
    drop_in_place(value_initialized)
    trace(value_initialized, visitor)           // VM/GC roots when required
    plan_cross(src_initialized, mode)            // exact new transport storage
    cross_move_init(dst_empty, src_initialized) // only when shard-movable
    cross_clone_init(dst_empty, src_initialized)// when crossing duplicates T
}

KeyOps<K> {
    value: ValueOps<K>
    hash(key_initialized)
    equal(a_initialized, b_initialized)
}
```

`Clonable(T)` is the existing Surge rule: `T` is Copy, or it has a valid
`__clone(self: &T) -> T`. `clone_init` implements that rule; it is not a raw
byte copy of a move-only value. `copy_init` and ordinary compiler-generated
copies may be inlined when all fields permit it. `drop_in_place` visits only
initialized fields and only the active union arm. `plan_cross` is a read-only
preflight: it reports only the envelope/sidecar/storage that the crossing will
newly own, so byte credit can be reserved before the source moves. It does not
recursively charge pre-existing transitive heap. `cross_move_init` and
`cross_clone_init` apply the shard-movement and deep-copy rules field by field.
They are compiler-generated crossing operations and never invoke a user's
arbitrary `__clone`. A non-Copy value crosses by ownership move; if source code
explicitly calls `__clone`, that ordinary clone completes and is charged to the
source owner before the resulting value enters the crossing by move.

Recoverable internal refusals that actually return to their caller (checked
layout/capacity failure, stale generation, or a rejected publication) leave the
destination empty and the source obligation unchanged, rolling back any
compiler-generated children already initialized. `move_init` transfers the
obligation and leaves the source logically empty only after that commit point.

This is not language-level unwinding. A user `panic` terminates the process via
the existing `panic -> exit(Error)` path, and allocation exhaustion remains
unrecoverable; neither path promises destructor execution or partial-value
cleanup after the terminal event. `rt_alloc` retains its nullable C ABI: `NULL`
is not a `ValueOps` status, language result, or `TaskResult`. Epic 23b must not
add an exception mechanism, pretend that `rt_panic` returns, reclassify
allocator `NULL` as carrier cancellation, or advertise fallible allocation.
Generated clone/drop/cross callbacks must never run while a scheduler, channel,
task, map, or transport owner lock is held. A locked section may claim slots
and build a local work batch; user-defined or allocating operations run after
unlock.

Static ordinary calls do not carry a descriptor argument just because their
types are generic in source. Monomorphization selects concrete operations.
Each homogeneous runtime storage region whose payload type is erased records one
descriptor for all of its slots. Maps record one key descriptor and one value
descriptor. A heterogeneous operation records one immutable descriptor table
for its compiled arms/state fields and uses a small state/arm index; it does not
copy function pointers or numeric drop ids into every slot. There is no
per-value function-id dispatch and no fallback that boxes an unknown layout at
runtime.

Descriptors are immutable compiler-emitted data with whole-program/process
lifetime. Every erased owner keeps a valid descriptor for at least as long as
any of its slots can be initialized. Epic 23b does not introduce unloading,
descriptor negotiation, or a descriptor identity in the external wire format.

## 6. Storage And Slot Lifecycle

Every owner provides storage satisfying `size` and `align`. Repeated storage
uses `stride`; placement may not assume that `size == stride` or `align <= 8`.
Each slot follows one state machine:

```text
EMPTY -> INITIALIZED -> CLAIMED -> MOVED
                       \-------> DROPPED
```

`CLAIMED` is a short exclusive transition state used to detach work from an
owner lock. It is never visible as a second initialized value. A failed
initialization returns to `EMPTY`. A cancellation, close, stale generation,
replacement, or shutdown path must either transfer the single obligation or
drop it exactly once.

Required owner storage includes:

| Owner | Required representation |
| --- | --- |
| frame/global/temp | exact aligned slot plus compile-time or frame lifecycle state |
| fixed array | inline `N * stride`, no element boxes |
| dynamic array | one descriptor plus contiguous exact element slots |
| map | one `KeyOps<K>`, one `ValueOps<V>`, aligned key/value slots |
| task/blocking result | owner-resident canonical result slot |
| channel | one descriptor plus typed ring/rendezvous slots |
| `select` | one compiled descriptor-table entry per heterogeneous arm plus typed staged slots owned by the operation |
| async state/captures | one compiled frame/state descriptor table plus typed fields and a state-indexed resume slot |
| far pending/transport | aligned envelope tail or explicit typed sidecar |

Container growth and rehash are transactional. Until a destination element is
fully initialized, the source remains responsible. Replacement drops the old
entry once, after the new key/value state is committed. A failed reserve,
clone, publication, or insertion cannot leave two owners or abandon a partly
initialized aggregate.

## 7. Places And References

A place identifies storage owned elsewhere; reading a place does not create an
owner. A reference contains enough information to reach the owner storage and
validate its lifetime:

```text
PlaceRef = owner identity + byte offset/path + lifetime generation
```

LLVM may realize a live local place as an aligned pointer. The VM may retain an
abstract `Location`, but it must resolve into frame/container storage rather
than a heap box for the composite. VM relocation or frame reuse increments a
generation so stale locations fail deterministically in development builds.

A place also carries its proven alignment. Packed fields may be naturally
unaligned: compiler-generated loads/stores use the actual alignment (or
`memcpy`), and ordinary reference callees conservatively support alignment 1
unless a stronger alignment is proven at the call. Dedicated FFI lowering uses
an aligned temporary when the target C ABI requires it. No backend may attach a
false natural-alignment promise to a packed-field pointer.

The compiler preserves the existing ownership rules:

- copy reads initialize an independent destination only for Copy values;
- consuming reads move and empty the selected place;
- partial moves leave a residual obligation for the still-initialized fields;
- reinitialization restores the selected place;
- borrows do not outlive their owner or cross suspend/shard/transport
  boundaries;
- shard-pinned resources move only through the explicit migration/lease rules.

No implementation may store an ordinary `&T` or `&mut T` inside a task,
channel, far pending, blocking job, or other owner that can outlive/suspend the
borrow.

## 8. Ordinary Call ABI

The generated Surge call ABI is type-directed, not universally word-erased:

- native scalars and handles use their concrete native representation;
- every non-zero-sized composite result uses a hidden, aligned caller-owned
  destination (`sret` in LLVM terms); the callee initializes it exactly once;
- every by-value composite argument is first initialized by move/copy into a
  dedicated aligned argument slot and passed by address with the canonical
  `byval`/alignment contract; the callee owns that logical argument obligation;
- zero-sized composites carry lifecycle/ordering semantics but no invented
  payload byte or dereferenceable dummy pointer;
- indirect calls, function values, recursion, and separately compiled in-tree
  calls use the same canonical lowered signature;
- references point to caller/owner storage and never to an incidental box;
- partial initialization on a returning early-exit/status path unwinds only
  initialized fields; process-terminal panic/OOM does not unwind.

LLVM may scalar-replace or register-promote these destinations after lowering
when it proves equivalence. That optimization must not change signature
identity or the semantic ownership protocol. The VM uses the same logical
argument/result destinations even if its physical frame representation differs.
An explicit foreign C call follows its declared target C ABI through dedicated
FFI lowering; unsupported layouts receive a compile-time diagnostic rather than
falling back to the erased Surge carrier.

There is no `T -> i64 -> T` bridge, pointer-to-integer payload encoding, or
heap-box fallback for composite arguments/results.

### Internal ABI manifest and fail-fast sentinel

One machine-readable typed-carrier ABI manifest is the source for the native C
declarations and the compiler's Go/LLVM runtime declarations. Checked-in
generated views must reproduce byte-for-byte from that manifest. The C side
uses `_Static_assert`/`offsetof` checks for descriptor, slot, envelope, and
public carrier-boundary layouts; compiler tests compare the generated function
signatures, parameter attributes, size, and alignment.

The generator hashes the canonical manifest content. Every native module
requires a link symbol with the generated shape
`__surge_runtime_abi_typed_carrier_v2_<manifest-hash>`; the matching runtime
exports exactly that symbol. A forgotten declaration/layout update either makes
the generated-view diff gate fail, violates a compile-time signature/layout
assertion, or changes the required symbol. The build driver recognizes a
missing sentinel and reports one internal compiler/runtime mismatch message
telling the user to rebuild/reinstall one Surge revision. This is fail-fast
consistency checking only: there is no version negotiation, alternate symbol,
adapter, or execution path for old objects. VM/native parity tests cover the
logical descriptor schema even where the VM does not link the C runtime.

## 9. VM Aggregate Storage

VM frames and owner objects provide aggregate-capable storage. A local slot can
name an inline cell/arena region described by `ValueLayout`, and composite
field/tag/fixed-index locations resolve to offsets inside that region. Copy,
move, drop, and tracing use the same generated structure walk as native
lowering.

The following are not valid final representations of a Surge value composite:

- `VKHandleStruct`/`VKHandleTag` language values;
- `OKStruct`/`OKTag` heap objects allocated for ordinary literals;
- an `any` payload used to erase typed task/channel/select/blocking values;
- a heap allocation created solely because a frame slot stores one `Value`;
- a statically typed composite routed through universal VM owner storage such
  as frame/call/global/temp `Value` slots, `Object.Arr []Value`, or
  `MapEntries []mapEntry` whose key/value fields are `Value`;
- a sidecar/cell hidden behind such a universal `Value` that preserves the
  indirection while merely renaming the old struct/tag handle.

VM implementation-specific metadata may remain heap allocated. The acceptance
property is that constructing, copying, passing, returning, indexing, and
dropping a local composite does not allocate a composite box. Type-aware
structural tests must prove that a statically known composite in a local/global/
temp, ordinary or indirect call argument/result, async capture/resume field,
dynamic-array element, or map key/value resolves to exact layout-owned storage
rather than a universal `Value`. Dynamic-array grow/reallocate/slice/index and
map insert/replace/rehash/remove/teardown preserve the same inline storage and
transactional lifecycle.

## 10. Tasks And `Task<T>.clone()`

A task owns one canonical, exact-sized result slot and its `ValueOps<T>`. Task
completion initializes that slot before publishing `DONE`. Await consumes one
task handle/result entitlement and initializes the awaiter's destination.

The task and each handle have separate state:

```text
task result: EMPTY/PENDING -> READY(T) -> MOVE_CLAIMED -> MOVED
                           \-> CANCELLED
entitlement: LIVE -> CLAIMED_WAIT -> CLAIMED_CLONE -> DELIVERED
                                \-> CLAIMED_MOVE_WAIT
                                      -> CLAIMED_MOVE -> DELIVERED
             LIVE/CLAIMED_* -> DELIVERED_CANCELLED | DROPPED
```

The owner records `live_entitlements`, the claimed-waiter set,
`claimed_clone_readers`, an optional `move_waiter`, the result state, and a
generation. `clone(&Task)` may create and count a new `LIVE` entitlement only
while the borrowed source handle is still `LIVE`. `await(own Task)` atomically
changes exactly that handle to `CLAIMED_WAIT`; user code can no longer await,
clone, or drop it.

The owner classifies claimed waiters under its lock. While any `LIVE` handle
could still create/consume an entitlement, a waiter uses `CLAIMED_CLONE` once
the result is `READY` and increments `claimed_clone_readers`. When
`live_entitlements` reaches zero, no new entitlement can appear. The owner
reserves exactly one not-yet-cloning claimed waiter, when one exists, as
`CLAIMED_MOVE_WAIT`; all other claimed waiters clone once `READY`. The
reservation may be made by the last `LIVE -> CLAIMED_WAIT` transition before
completion or selected from the waiter set when completion publishes `READY`.
Before `READY` it reserves only the entitlement role. After `READY` it pins the
canonical result and parks until `claimed_clone_readers == 0`, then atomically
enters `CLAIMED_MOVE` with `READY -> MOVE_CLAIMED`. If the final live handle is
dropped after every remaining waiter has already begun cloning, no move waiter
exists and the canonical value is dropped after those readers retire.

Clone/move work then runs outside the lock while the claim and generation pin
the canonical slot. Completion reacquires the owner lock, validates the same
claim/generation, decrements any reader count, and publishes one terminal
entitlement state exactly once. `CANCELLED` retires a claimed await as
`DELIVERED_CANCELLED`; a returned internal failure or shutdown cleanup may
retire the applicable claim as `DROPPED`. Dropping a `LIVE` handle changes only
that entitlement to `DROPPED`. The canonical result may be dropped only when
there are no live/claimed entitlements, clone readers, or move waiter. Shutdown
first prevents new entitlements, then waits for/detaches claimed work before
dropping the canonical slot. Late work with a stale generation cannot publish
or touch reused storage. No operation infers ownership from the opaque handle
refcount alone.

`Task<T>.clone()` is valid exactly when `Clonable(T)` is true. Every successful
clone creates another result entitlement, not another alias to the same result.
For `N` successfully awaited entitlements, callers observe `N` independent
values. Once no new entitlement can appear, the reserved final waiter moves the
canonical result after earlier clone readers retire. If every entitlement in a
closed cohort of `E` is awaited successfully, this performs exactly `E-1`
logical duplications and one move. If some entitlements are dropped, an earlier
await may already have cloned for a then-live sibling; total duplications may
therefore equal the number of successful awaits but never exceed `E-1` for the
cohort. Scheduling the reservation/wait is not observable.

Selection of an entitlement and pinning of the canonical source happen under
the task owner lock. `__clone` itself runs outside runtime locks, on the owner
shard. There is no new `TaskResult` failure variant. A normally returning
compiler-generated recoverable status follows the empty-destination rollback
rule from Section 5, transitions only that entitlement to `DROPPED`, releases
its canonical pin, and routes the status to the existing terminal internal-error
path. It never becomes `Cancelled`. A user `__clone` panic follows Surge's
ordinary terminal `panic -> exit(Error)` behavior, and allocator `NULL` remains
unrecoverable: neither terminal path resumes the task, publishes a result,
becomes `Cancelled`, nor carries an exact-drop guarantee after process
termination.

All entitlements refer to one task. `cancel(&Task<T>)` through any live local
handle is task-global and idempotent, not entitlement-local: before committed
success it requests cancellation observed by every awaited entitlement; after
success is committed it does not revoke already available independent results.
Dropping or consuming one handle releases only its entitlement. Shutdown drops
any canonical result that no remaining entitlement can consume.

A non-clonable `Task<T>` is affine. Calling `.clone()` is a compile-time error;
there is no runtime fallback and no shallow handle alias. `far Task<T>` gains
no new clone operation and remains affine under the crossing contract. A task
entitlement that is later published/routed retains the same independent-result
semantics.

## 11. Channels, `select`, And Blocking

A channel owns one payload descriptor. Buffered channels use exact aligned
ring slots; rendezvous channels use an equally typed handoff slot. A send value
is evaluated once. Publication transfers the obligation atomically; refusal,
close, cancellation, and stale waiters reclaim the value according to the
source API's consuming semantics.

`select` stages each payload in typed operation-owned storage. Exactly one
winning arm transfers its obligation. Losing, cancelled, or stale arms are
returned to their defined owner or dropped exactly once. No `[N x i64]`,
`send_bits`, or pointer/integer reinterpretation is permitted. Select arms may
have different payload types; the operation owns one immutable descriptor table
indexed by arm, not a false single-`T` descriptor and not copied per-slot drop
metadata.

A blocking job owns typed capture/state/result slots. Cancel-before-run,
cancel-during-run, normal completion, completion-after-cancel, and shutdown all
use the common slot lifecycle. A late completion cannot publish into abandoned
awaiter storage. Cleanup is detached under the owner lock and executed after
unlock.

An async state machine has one compiler-generated frame/state descriptor table.
Each suspension state identifies the concrete resume type and its slot; the
producer must match that expected descriptor/state generation before
initializing it. Polling claims and empties the resume slot exactly once. A
wrong type/state is an internal ABI invariant failure, never an `any` payload or
runtime box fallback.

## 12. Far Values, Transport, And Anchored Resources

Crossing first creates a read-only `CrossPlan` from an exclusively movable or
immutable/pinned source. The plan fixes the descriptor, exact physical byte
charge, alignment, and any newly allocated sidecar shapes; that same plan is
passed to initialization. The sender reserves a destination-owned
pending/envelope for the complete charge, then moves or cross-clones into its
exact aligned payload. Publication is the ownership commit point. A plan/actual
size mismatch is an internal invariant failure before publication, never an
uncredited top-up after the source moved. If reservation or publication fails,
the sender still owns the source and unwinds any partial destination. Stale
generations, rejected routes, cancellation, and shutdown use the same
descriptor to drop pending payloads.

Cross initialization allocates only through a plan-limited allocator whose
remaining byte allowance is part of the reservation. Exhausting that allowance
fails before publication and rolls back; it cannot silently allocate outside
credits. Once adopted, storage retained by the destination leaves the transport
budget and becomes destination-owner memory, as specified below.

An anchored far handle remains owned by the caller. The remote body receives a
separate generation-checked pinned lease/capability. The lease cannot outlive
the call generation, cannot be mistaken for a second owning handle, and is
invalidated before the caller's handle can be released or migrated. This is
the accepted close direction for `RV2-DEBT-082`.

### Byte credits

Transport data credits count physical bytes owned by transport:

- fixed envelope/header bytes;
- alignment padding;
- exact inline payload tail;
- transport sidecars;
- storage newly allocated to perform a cross-clone while transport owns it.

Credits do not recursively graph-walk pre-existing handles and do not
double-count heap already charged to another owner. If retained target heap
needs its own bound, that is a separate owner-memory budget. Cross-clone bytes
cease to be transport credit when ownership is handed to the target owner.

The sender reserves the complete known transport charge before relinquishing
its source. A normal message waits for sufficient target data credit. A
payload larger than the normal credit window uses an exclusive jumbo
reservation: at most one such reservation is admitted for the target, it is
charged by its exact physical size, and normal data waits until it is adopted
or dropped. This avoids a hidden size cutoff without allowing an unbounded
number of oversized messages.

The reserved control lane contains bounded protocol metadata only: credit
return, cancellation, generation/route updates, and wake/progress records.
Task completion, reply, or any other operation carrying arbitrary `T` uses the
data lane or a previously reserved reply budget. Backpressure can never block
the bounded message that releases backpressure.

Credits return only after transport transfers or drops all storage they
covered. Saturation must park the producer without busy retry, preserve control
progress, restore the exact credits after cancellation/shutdown, and keep
resident transport bytes within data reservations plus the fixed control
budget.

## 13. Cancellation, Shutdown, And Locks

Every non-process-terminal carrier outcome that returns control—refusal, close,
cancellation, stale generation, shutdown, or returned internal status—is a
first-class cleanup path, not an afterthought. Shutdown drains or transfers
every task result, mailbox, channel slot, select staging slot, blocking job, far
pending, inbound envelope, and partial destination. Generation checks reject
late events before they touch new storage reusing an old address/id. Terminal
`panic`/`exit` and allocator `NULL` retain Section 5's no-unwind contract.

The lock rule is uniform:

1. validate generation and claim the slot under the owning lock;
2. publish the new lifecycle state and detach a local operation/cleanup batch;
3. release the lock;
4. run clone/drop/cross work;
5. reacquire only to commit a result that still matches the generation.

No generated value callback, allocator, destructor, or user `__clone` runs
under runtime owner locks.

## 14. Friendly Compiler Diagnostics

Representation errors that the compiler can prove are compile-time errors.
They must not become a runtime word-box fallback or a vague monomorphization
failure. Every applicable diagnostic includes:

- a primary span at the operation the user can change;
- the concrete type and the first relevant field/type path;
- a note explaining the ownership, lifetime, layout, or crossing rule;
- an actionable help message;
- a machine-applicable fix only when the compiler proves that edit preserves
  intent. Ambiguous advice remains a note/help, never an unsafe edit.

Required rows include:

| Situation | Required diagnostic behavior |
| --- | --- |
| `Task<T>.clone()` and non-clonable `T` | point at `.clone`, name the first non-clonable component, explain affine result ownership, and always offer consuming the single handle; mention defining `__clone` only when canonical receiver resolution proves that this exact type is legally extendable from the current module; no automatic edit |
| borrow captured across suspend/crossing | show borrow origin and crossing/suspend boundary; explain the owner lifetime |
| non-shard-movable/pinned value crossing | show the first blocking component and the explicit far/migration alternative already supported |
| use after a consuming send/move | show the consuming operation and note that the source obligation transferred |
| layout/descriptor unavailable after monomorphization | report the source instantiation and concrete type; never select an erased runtime fallback |
| stale compiler/runtime artifact | fail visibly in the build driver; no ABI negotiation or compatibility execution path |

The compiler diagnostic model distinguishes three channels: explanatory
`Notes`, actionable non-edit `Help`, and structured `Fixes` with applicability.
The CLI renders all three. LSP maps Notes/Help to `relatedInformation`, sets
`publishDiagnostics.version`, and carries an opaque analysis/snapshot id,
canonical URI, document version, diagnostic id, and fix ids in
`Diagnostic.data`. The server, not client data, retains the trusted snapshot,
old-text guards, and applicability.

An `AlwaysSafe` Code Action is offered only when every analyzed open document
still matches that snapshot, the request document has the same version and
snapshot id, and each materialized edit is in bounds, non-overlapping, and
matches its guarded old text. Replace/delete without `OldText` is suppressed;
empty `OldText` is valid only for insertion. The server performs this check
before and after materialization and returns versioned `TextDocumentEdit`s. A
stale/unknown action returns no edit (or `Content modified` from resolve) and
requests fresh diagnostics. `fix once` likewise revalidates old text and applies
only `AlwaysSafe`; when none remains valid it makes no edit.

Generic clonability uses one shared post-sema obligation path. Sema records a
`Clonable(T)` obligation at the generic operation; the normal instantiation
discovery resolves concrete substitutions without running backend lowering, and
one validator is invoked by build, ordinary `surge diag`, and LSP. Unused
generic definitions remain deferred. All three paths emit the same `SEM3116`
with the generic site and concrete instantiation notes; none exposes a raw
monomorphization error.

One authoritative clonability capability is reused by direct sema checks, the
post-sema obligation validator, monomorphization, and `Task<T>.clone()`. It
records state (`Copy`, `ValidMethod`, `NonClonable`, or `Deferred`), the exact
accessible method symbol when present, the first failure path/reason, and
whether the same canonical receiver-resolution path proves that a legal local
`extern<T>` method can be declared. `ValidMethod` means exactly one accessible,
non-variadic `__clone(self: &T) -> T` with canonical alias/import keys.
Definition advice is false for sealed, far/runtime-owned, structural,
protected/conflicting, or non-canonicalizable targets; an imported unsealed
type is suggested only when the real resolver would accept and later find the
method.

A shared operation-aware renderer consumes that capability. It never turns
`Copy` into a fictitious method call. For ordinary values the callable public
route is `clone(&value)`, while `.clone()` is reserved for local `Task<T>`;
`far Task<T>` remains affine. Unavailable cases receive only applicable
move/consume/borrow/restructure help, and deferred generics wait for concrete
instantiation. That optional advice deferral is not a semantic obligation and
cannot constrain or reject `T`; only an operation whose validity actually
requires cloning, such as generic `Task<T>.clone()`, records `Clonable(T)`.
The negative control instantiates a non-clonable generic at an advice-only site
and requires the original ownership/lifetime diagnostic alone: no clone Help
and no `SEM3116`.

Diagnostics are part of Epic 23b acceptance. They must survive the actual CLI
build/diagnose and LSP paths; constructing Notes, Help, or Fixes internally
while dropping them at the user boundary is not complete. ABI mismatch details
use the existing trace interface: the default is one concise rebuild message;
`--trace=- --trace-level=debug` adds expected/actual manifest details.

## 15. Cost And Extensibility

The model deliberately spends bytes proportional to the value instead of
paying a heap allocation and pointer chase to force every value through one
word.

Expected gains:

- zero composite-box allocation for local construction/copy/call/return;
- contiguous fixed/dynamic array and map storage;
- fewer pointer indirections and better LLVM scalar replacement;
- exact carrier memory accounting and deterministic cleanup;
- one extension point (`ValueOps<T>`) for future value kinds instead of new
  task/channel/select/far ABIs;
- type-directed diagnostics before code reaches runtime.

Expected costs:

- task/channel/select owners reserve or allocate storage proportional to `T`;
- moving a large inline value copies/moves its physical fields unless optimized
  into a destination;
- erased runtime boundaries make one indirect operation call per lifecycle
  event;
- descriptors and aligned allocation add owner-level metadata/complexity;
- transport credit acquisition becomes byte-sensitive instead of message-only.

Static paths should inline operations and construct directly in their final
destination. Runtime owners should allocate once per owner/buffer, not once per
value. Benchmarks must report allocation count, bytes copied/moved, resident
carrier bytes, throughput, and latency; a faster micro-path cannot excuse
incorrect ownership or an unbounded slow path.

## 16. Compatibility And Deletion Rule

Epic 23b performs an atomic in-tree cutover. It does not preserve old public C
runtime signatures, generated objects, cached artifacts, stdlib shims, or old
tests whose only purpose is the erased ABI. Stale artifacts must fail clearly;
they are rebuilt, not translated.

Final production code must not contain a hidden adapter, dual representation,
feature flag, or “temporary” universal box. Historical documents may describe
the old representation as evidence; live target documents must link here.

The main integration changes the live product-version surfaces to `0.2.0` and
passes `make version-check VERSION=0.2.0`. It does not invoke the remote release
workflow automatically. After main CI, the project owner may dispatch the
existing main-only workflow with input `0.2.0` to create changelog/release assets
and tag `v0.2.0`.

## 17. Design Acceptance

Epic 23b is complete only when all of the following are true:

1. every owner surface in Section 6 uses the exact typed model;
2. VM and LLVM agree on value/copy/move/drop/borrow semantics;
3. ordinary composite operations and carrier operations allocate no composite
   boxes;
4. the `Task<T>.clone()` entitlement contract is proven for Copy, user-cloned,
   move-only, local, concurrent, cancelled, and far-routed results;
5. success, refusal, close, cancellation, stale generation, replacement, and
   shutdown paths initialize/move/drop exactly once;
6. callbacks never run under runtime owner locks;
7. transport byte credits bound physical carrier storage and the control lane
   remains progress-safe under saturation;
8. the friendly diagnostics in Section 14 are observable through supported
   compiler entrypoints;
9. semantic absence gates prove that no old carrier fallback remains;
10. all correctness, liveness, sanitizer, benchmark, golden, and independent
    review gates in `23b-inline-storage-and-typed-carriers.md` pass.
