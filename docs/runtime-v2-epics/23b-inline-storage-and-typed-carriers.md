# Epic 23b — Inline Storage And Typed Carriers

Status: **ACCEPTED — READY FOR END-TO-END IMPLEMENTATION**

Design authority:
`23-storage-model-and-typed-carrier-abi.md`

This epic completes Epic 23 Phase 2. It replaces composite boxes and the
one-word erased payload ABI with one exact-sized typed-carrier model across the
compiler, VM, native runtime, containers, concurrency primitives, and far
transport. It is intentionally one epic rather than a hierarchy of task
documents: the lead coordinates bounded workstreams, reviews their commits,
and integrates the complete vertical.

## 1. Outcome

After this epic:

- value composites live inline according to `../ABI_LAYOUT.md`;
- ordinary VM/LLVM calls, returns, frames, places, and references use typed
  destination storage;
- arrays and maps contain exact element/key/value slots, not composite boxes or
  universal `uint64` entries;
- task, channel, select, blocking, far, cancellation, and shutdown paths use
  the same slot lifecycle and monomorphic `ValueOps<T>`;
- `Task<T>.clone()` produces an independent result entitlement exactly when
  `T` is clonable under the existing Copy-or-`__clone` contract;
- transport credits count physical carrier bytes and arbitrary results never
  ride the reserved control lane;
- the old erased payload ABI and every compatibility path are deleted;
- friendly, type-directed diagnostics reach the user-facing compiler paths;
- the integrated tree passes the correctness, liveness, sanitizer, performance,
  golden, absence, and independent-review gates below.

The implementation merges to `main` and is closed by main CI. **It does not carry
a version bump.** An earlier draft of this document had this epic merge together
with a live version-surface bump to `0.2.0`; that was a misreading of the owner's
direction and is corrected here.

`0.2.0` belongs to the END of the whole Runtime V2 refactor, not to any single
epic inside it. The owner's framing is "within this global refactor (Runtime V2,
Surge 0.1.X -> 0.2.X)" — the version names the refactor, and the refactor is not
this epic. Closing Epic 23b leaves unfinished epics to carry to completion, the
debt ledger to burn down, a stdlib refresh the owner has deferred until after
Runtime V2, further work still to be planned, and features whose ideas are not
yet written down anywhere. The version moves when that work is done and the
owner says so.

Nothing in this epic changes `internal/version` or any other version surface.

## 2. Authority And Starting State

Read before editing:

1. `RULES.md` — process, evidence, worktree, review, and quality gates;
2. `23-storage-model-and-typed-carrier-abi.md` — normative target;
3. `23-value-composites.md` — value semantics and Phase 1 evidence;
4. `24-partial-moves.md` — place/residual ownership semantics;
5. `../ABI_LAYOUT.md` — layout source of truth;
6. `../RUNTIME_V2.md` — surrounding scheduler/crossing architecture;
7. `DEBT.md` — causal history and close conditions.

The implementation must begin from a recorded clean base commit and a fresh
baseline. Pre-existing failures are evidence, not permission to add another
failure. A failure that appears only after a 23b integration commit is a 23b
regression until a clean-base comparison proves otherwise.

## 3. Scope

### In scope

- canonical `ValueLayout`, monomorphic `ValueOps<T>`, and `KeyOps<K>` lowering;
- exact aligned owner storage and explicit slot lifecycle state;
- partial-initialization rollback and cleanup outside runtime locks;
- VM inline aggregate cells/frames/places/tracing;
- LLVM native composite types, destinations, calls, returns, and places;
- fixed and dynamic arrays, maps, globals, locals, temps, async frames/captures;
- task result/resume, channel ring/rendezvous, select staging, blocking jobs;
- local and far task/channel paths, pending publication, replies, cancellation,
  stale generations, shutdown, and transport drain;
- byte credits, bounded control metadata, and exclusive jumbo reservation;
- anchored far-handle lease semantics;
- the six existing crossing deep-copy barriers for current `float` values and
  composites containing them, implemented through generic cross operations;
- compile-time clonability obligation for `Task<T>.clone()` including generic
  instantiation;
- user-facing diagnostic propagation required to make this epic's errors and
  notes observable;
- removal of all old carrier entrypoints, fields, glue, wrappers, and tests;
- new stable Runtime V2 carrier/diagnostic/absence gates.

### Out of scope

- compatibility with old source, runtime APIs, generated objects, caches, or
  independently versioned runtimes;
- a dual ABI, migration shim, tombstone API, deprecation period, or fallback
  composite box;
- new Surge syntax or public clone/crossing APIs;
- finishing WidthAny `int`/`uint` reclamation from Epic 22;
- stable plugin/dynamic-module descriptor identities or external wire formats;
- general recoverable-OOM language semantics;
- unrelated flaky/legacy test cleanup unless the failure is introduced,
  exposed as a deterministic blocker, or directly prevents a required 23b gate.

## 4. Workstream And Worktree Protocol

The epic lead owns the integration branch and is the only writer to its working
tree. Every parallel implementer works from an explicitly named base commit in
a separate temporary worktree and starts with the plan gate in `RULES.md`.

Each approved workstream records:

- owned files/symbols and a non-overlap declaration;
- the design sections and debt rows it implements;
- focused positive, negative-control, failure, cleanup, and benchmark evidence;
- commands run and any skipped gate;
- one reviewable commit (or a short dependency-ordered series);
- discoveries in `NOTES.md` and durable debt in `DEBT.md`.

The lead reviews the commit and intended diff before integration. Workers do
not edit the shared dirty checkout, merge each other, rebase the integration
branch, or resolve cross-workstream conflicts implicitly. After integration,
a non-author performs an independent review of the combined result in another
clean worktree. A review finding is classified as:

- **blocker:** violates this design, ownership/liveness/memory safety,
  observable semantics, required diagnostics, stable goldens, or a required
  gate;
- **debt:** useful improvement outside the accepted epic contract.

Only blockers expand current implementation work. Nonblocking findings receive
an owner and close condition in `DEBT.md`.

## 5. Execution Waves

These are integration waves, not separate task-document requirements. The lead
may parallelize only surfaces whose contracts and file ownership no longer
overlap.

### Wave A — baseline, census, and proving contracts

1. Record the clean base commit, dirty-file inventory, compiler/runtime
   versions, and all baseline command results.
2. Freeze a semantic census of every erased carrier symbol/signature and every
   composite allocation path. It explicitly starts with VM frame/call
   `[]Value`/`LocalSlot.V`, `Object.Arr []Value`, `MapEntries []mapEntry` with
   `Value` key/value fields, and async/global/temp owner slots in addition to
   boxed struct/tag and `any` payload paths. The census becomes a source-shape
   plus type-aware structural absence gate, not a broad ban on `uint64` or
   scalar VM `Value` use.
3. Capture baseline allocation counts, channel/crossing benchmarks, transport
   resident-byte/credit counters, and Runtime V2 liveness timings.
4. Approve the bounded hypotheses, fixtures, negative controls, and rollback
   for the proving verticals in Section 6. Run each vertical only after its
   stated foundation wave exists; a failed hypothesis is removed or rewritten
   before that dependent production surface proceeds.
5. Freeze a reviewed benchmark manifest and harness revision before carrier
   implementation. The manifest names every row, workload/timeout, counter,
   aggregation formula, threshold, host constraint, and the exact `EPIC_BASE`;
   the same harness revision drives base and candidate binaries.
6. Record a NUL-safe filesystem-versus-index inventory for every generated
   golden root, including ignored files. Before any compiler-output wave, add a
   fail-closed inventory check to `make golden-check` so an untracked, ignored,
   or missing generated sidecar makes the target fail.

The classified allocation evidence and every exact per-batch candidate budget
are frozen in `23b-wave-a-allocation-census.md`. The manifest is the executable
copy of that census: a nonzero allocation control first proves the observer is
live, then every candidate warmup and measured timing batch must equal its
structural owner/container budget before any resource-capture binary runs.
Both timing artifacts are built first; controls and the entire paired timing
matrix run next; only then may the candidate resource artifacts be built and
run, so candidate-only compilation cannot warm caches or alter thermal state
before scoring.

`make runtime-v2-carrier-bench` remains the strict final gate and aborts on an
exact allocation mismatch. The separate
`make runtime-v2-carrier-baseline-capture` command may collect numeric
pre-cutover allocation/resource mismatches as endpoint RED only. It still
aborts on dead controls, missing/null required metrics, identity/checksum
drift, timeout, malformed or incomplete attempt order, and any other protocol
corruption; it exits successfully only after writing a complete
protocol-passed endpoint-RED report.

### Wave B — layout/operations foundation

1. Reuse `internal/layout` for size, align, stride, offsets, packed/over-aligned
   values, unions, fixed arrays, and zero-sized values.
2. Replace unchecked signed layout/envelope math with target-width checked
   add/multiply/round-up operations. Static overflow is a precise compile-time
   diagnostic; dynamic planning fails before source ownership changes.
3. Generate concrete move/copy/clone/drop/trace/cross operations and `KeyOps`.
4. Add transactional initialization helpers and explicit slot state.
5. Generate read-only cross plans plus move/clone operations that cover the
   current refcounted `float` scalar recursively inside composites.
6. Add the single machine-readable ABI manifest, generated C and Go/LLVM
   declarations, layout/signature assertions, content-hash generator, and
   required link symbol
   `__surge_runtime_abi_typed_carrier_v2_<manifest-hash>`. A generated-view
   mismatch or missing symbol fails with the design's rebuild message; no
   alternate ABI is tried.
7. Prove generated callbacks are detached from runtime locks before invocation.

This wave is an integration dependency for all erased runtime owners. It must
land before parallel owner migrations branch from it.

**Status correction, owner ruling 2026-08-20.** Items 3 and 5 say GENERATE, and
nothing generates. `internal/backend/llvm/` mentions `rt_value_ops` exactly once
- as the shape of the record, in `typed_carrier_v2.generated.go` - and emits no
descriptor constructor at all; the only constructions in the tree are the
runtime's own and the harnesses under `internal/vm/testdata/`. Since
`rt_slot_operations_preflight` requires `move_init` AND `plan_cross` to be
non-null unconditionally (`rt_slot_control.c:42`), no owner can be migrated
until those two exist for real types. Wave B is therefore NOT closed, and any
owner migration that begins before it is building on an entry condition that
was never met.

#### What a slot costs, and what it must not cost

A descriptor is per HOMOGENEOUS OWNER, not per element: one `rt_value_ops` for a
whole channel, array or map region. Per-element lifecycle metadata is the
one-byte `rt_slot_header`, not a copy of the owner's control block. An owner
that gives every element a full `rt_slot_control` (144 bytes measured on LP64)
has chosen a representation, not obeyed this design: a 64-slot
`Channel<nothing>` would cost 9 KB of control for 0 bytes of payload, which is
refused here explicitly.

Zero-sized payloads keep a real lifecycle and no storage. `size == 0` means the
payload occupies nothing; a fabricated filler byte is forbidden. Where an owner
is a FIFO of zero-sized values, occupancy is already encoded by its own
head/tail/count plus outstanding claims, so per-element lifecycle records are
not required at all and the memory cost is O(1) in capacity. This matters
because `Mutex`, `Condition` and `Semaphore` are all built on
`Channel<nothing>` (`core/sync.sg:5,42,80`): the cheapest primitives in the
language must not be the ones that pay for typing.

#### One destination contract, not one per owner

Channel code must not learn about tasks or select arms. The transfer boundary is
a typed destination plus a generation plus a slot claim: an owner moves out of
its own typed slot into a destination that satisfies that contract, and what
stands behind the destination - an ordinary receive, a select operation, a
remote reply - is not the owner's business. Reintroducing
`channel_deliver_same_shard_locked(..., resume_bits, ...)` with `void*` in place
of `uint64_t` satisfies the absence gate's letter and none of its purpose.

### Wave C — ordinary storage (parallel backend workstreams)

LLVM workstream:

- replace composite `ptr` typing and box allocation with canonical native
  layouts, aligned destinations, field GEPs, and destination-oriented returns;
- migrate direct, indirect, recursive, generic, async-state, and extern-facing
  in-tree calls together;
- preserve partial moves, active union arms, borrows, packed/over-aligned and
  zero-sized values.

VM workstream:

- add aggregate-capable frame/owner storage and offset-resolved `Location`s;
- remove struct/tag language-value handles and aggregate payload `any` paths;
- implement copy/move/drop/trace through the common type operations;
- generation-check locations across frame reuse/relocation in development
  builds.

Both workstreams must pass the same semantic fixtures before either result is
treated as complete. The lead integrates them before carrier owners migrate.

### Wave D — containers and local carriers

Migrate exact storage through:

- fixed arrays and dynamic array element buffers;
- map key/value entries, rehash, replace, remove, failed insert, and teardown;
- task canonical result/resume and cloned result entitlements;
- buffered/unbuffered channel send/receive/waiter mailboxes;
- local `select` staging and losing-arm cleanup;
- blocking captures/results and every cancellation timing;
- async frames, captures, polling, wake, and normal/shutdown drains.

Task/channel/select/blocking may be separate worktrees only after the shared
owner/slot API is integrated and their production files do not overlap. Every
owner migration deletes its old fields and dispatch path in the same commit;
there is no adapter milestone.

### Wave E — far carriers, leases, and byte credits

1. Move exact payloads through pending publication, remote task/channel/select,
   stale/refused routes, reply, cancel, and shutdown.
2. Keep the caller's anchored handle and give the body a generation-checked
   non-owning pinned lease.
3. Charge header, padding, inline payload, sidecars, and transport-owned
   cross-clone storage before ownership publication.
4. Move arbitrary completion/reply payloads out of the reserved control lane.
5. Implement normal byte reservations and exclusive jumbo reservations.
6. Prove saturation parks producers without busy retry, bounded control
   messages make progress, and every byte credit returns after adopt/drop.

### Wave F — diagnostics, deletion, and closeout

1. Add one authoritative tri-state clonability classifier and a shared
   post-sema obligation validator used by build, ordinary diagnose, LSP, and
   monomorphization; prove the concrete and instantiated-generic negative
   matrix as `SEM3116`.
2. Make all clone advice type-directed; never suggest an impossible clone.
3. Add the explicit Help diagnostic channel. Ensure CLI build/diagnose output
   carries primary message, code/span, Notes, Help, and safe fix titles. Map
   Notes/Help to LSP `relatedInformation`, fix ids to `Diagnostic.data`, and
   expose Code Actions only for `AlwaysSafe` edits.
4. Make `fix once` apply only `AlwaysSafe` edits automatically before adding
   any non-AlwaysSafe carrier diagnostic suggestion.
5. Delete legacy carrier symbols, box glue, compatibility declarations,
   feature flags, and obsolete tests.
6. Run the full absence, correctness, liveness, sanitizer, benchmark, golden,
   Sentrux, independent-review, and documentation gates.

Wave F has no dependency edge back into P3 or P4: P3 closes on local runtime
evidence, P4 adds far-runtime evidence, and Wave F independently closes the
diagnostic contract through P5.

AlwaysSafe Code Action transport and the snapshot/old-text guards are mandatory
for every safe fix reachable through the 23b diagnostic matrix, including the
existing safe partial-move `own` edit. Broader action kinds outside these paths
may become debt. Dropping notes, fix metadata, or a reachable AlwaysSafe action
at a path that claims to expose them is not acceptable.

## 6. Mandatory Proving Verticals

Each proving vertical records hypothesis, touched surface, non-final behavior,
proof, pass/fail threshold, and rollback as required by `RULES.md`.

### P1 — ordinary destination ABI

Entry condition: Wave B layout/operations foundation is integrated. P1 is the
first thin vertical of Wave C, before either backend expands to the full corpus.

Prove one nested struct/tuple/union/fixed-array value through local construction,
copy, move, borrow, partial move, direct/indirect call, and return on VM and
LLVM. Include packed/misaligned field references, over-aligned, deterministic
padding/inactive union bytes, and zero-sized rows.

Success:

- backend parity;
- zero composite-box allocations;
- no integer/pointer carrier reinterpretation in emitted IR;
- strict-zero Valgrind/ASan result;
- the destination protocol handles early exit and partial initialization.

Negative layout controls cover nested fixed arrays near the uint32 length
limit, `stride * count`, field/tail padding, packed/over-aligned round-up,
zero-sized element multiplication, envelope+sidecar totals, and host-int versus
target-size conversion. Every overflow must fail before allocation with the
first overflowing type path in a note.

### P2 — typed channel owner

Entry condition: P1 and the shared Wave B owner/slot API are integrated. P2 is
the first local runtime-owner vertical in Wave D.

Prove a payload larger than one word that owns a string/refcounted scalar
through buffered, unbuffered, parked sender/receiver, full, close, cancel, and
shutdown paths.

Success:

- exact alignment and one channel-level descriptor;
- no per-element box or allocation for trivial/composite bits;
- generated clone/drop work occurs outside the channel lock;
- every outcome moves or drops once under Valgrind/ASan/TSan.

### P3 — local typed task result and clone entitlement

Entry condition: P1 and the shared Wave B owner/slot API are integrated. P3 may
run alongside P2 only when task/channel files and shared APIs no longer overlap.

Prove Copy and valid user-`__clone` results, concurrent cloned handles, the
last-consumer move, local awaits, completion/cancellation races, and task-global
cancel through each sibling handle. Deterministic sync-point rows cover an
out-of-lock clone reader versus sibling drop/last-await, cancel versus `READY`,
shutdown versus a claimed clone, and stale-generation late publication. Far
routing belongs to P4; concrete/generic diagnostic rejection belongs to P5. A
generated `ValueOps` failpoint returns after initializing one child to prove
rollback, and bounded child-process controls cover user panic plus deterministic
allocator `NULL` without attempting real OOM. Negative-control builds omit the
canonical pin, rollback, or reader retirement and must trip the state/heap/ASan
assertion at the intended sync point rather than merely time out.

Success:

- each successful await receives an independent `T`;
- no user clone runs under task/scheduler locks;
- the explicit entitlement/result counters and transitions from the design
  prevent a clone reader racing the last-consumer move or canonical teardown;
- the all-awaited contention row performs exactly `N-1` logical duplications
  plus one reserved final move; a mixed await/drop row never exceeds `E-1`
  duplications for its closed entitlement cohort and drops the canonical value
  if no move waiter could be reserved;
- cancel remains task-global across entitlements; no clone failure is translated
  into entitlement-local `Cancelled`;
- every returning success/refusal plus cancellation/shutdown path retires the
  claim and drops canonical/partial values exactly once;
- a returned internal status restores the destination to `EMPTY`, drops its
  initialized child once, changes only the claimed entitlement to `DROPPED`,
  releases the pin, and leaves a sibling plus canonical `READY` result intact;
- a child-process user-clone panic follows the existing terminal
  `panic -> exit(Error)` path and is never observed as `Cancelled`; the test
  makes no destructor/unwind claim after process termination. The allocator
  `NULL` control likewise proves no result publication, `Cancelled`, or retry
  spin, but makes no cleanup/balance claim.

### P4 — transport/select saturation

Entry condition: the relevant local P2/P3 carrier owners are integrated. P4 is
the first Wave E far/transport vertical, not a Wave A prerequisite.

Prove a typed value larger than former word/message assumptions through far
task/channel/select with forced data-credit exhaustion, losing arms, stale
generation, caller cancel, and shutdown. Cover affine `far Task<T>` await plus a
local P3 entitlement result subsequently published/routed; this does not add
`far Task<T>.clone()`. Include two competing jumbo producers, an arbitrary-`T`
task reply larger than the normal window, a deliberately under-budget
cross-plan negative control, and cancellation/shutdown while a request or reply
budget is reserved.

Success:

- `credit_stalls > 0`, no busy retry, exact credit restoration;
- arbitrary payload never enters the reserved control lane;
- only one competing jumbo reservation is admitted per target and the loser
  parks without starving control progress;
- control progress continues with the data lane saturated;
- transport resident bytes equal or stay below admitted reservations plus the
  fixed control budget;
- far-routed task results preserve every applicable P3 entitlement invariant
  while `far Task<T>` itself remains affine;
- losing/stale/cancelled payloads drop exactly once;
- request and reply reservations both return exactly on rejection,
  cancellation, stale generation, and shutdown; plan-limited allocation cannot
  exceed its reserved bytes.

### P5 — clonability diagnostics and advice

Entry condition: Wave F's shared post-sema validator and user-facing diagnostic
plumbing are integrated. P5 consumes P3's local semantics and is not a
prerequisite of P3 or P4.

Prove concrete and instantiated-generic non-clonable `Task<T>.clone()` failures,
the complete per-emitter advice capability matrix, CLI/diag/LSP parity, and
stale-document suppression for safe edits.

Success:

- concrete and instantiated generic failures report the same `SEM3116` with
  their required source/type-path notes and no automatic Task-clone fix;
- Copy, callable-method, implementable-here, unavailable/sealed/far, and
  deferred-generic advice never produce an impossible spelling;
- a non-clonable generic at an optional advice site retains only its original
  ownership/lifetime diagnostic, with no clone Help and no `SEM3116`;
- an LSP action applies on its original version/old text and is absent after a
  document-version or guarded-text change.

## 7. Ownership And Liveness Matrix

Every carrier owner must have positive and negative-control coverage for all
applicable rows:

| Event | Required proof |
| --- | --- |
| normal initialize/consume | destination becomes initialized; source becomes empty |
| Copy/clone | independent result; source remains valid |
| returned internal-status failure | initialized children unwind; destination returns empty; claimed entitlement/pin retires once |
| user `panic`/`exit` or allocator `NULL` | bounded child process does not return, publish a result, or report `Cancelled`; no exact-drop/heap-balance assertion after termination |
| full/refused publication | source obligation follows the public consuming rule; no abandoned sidecar |
| close | buffered/staged values delivered or dropped once |
| cancel before claim | original owner drops/retains exactly once |
| cancel after claim | claimed worker finishes cleanup; caller storage is not touched late |
| stale generation | event rejected before reused storage mutation; payload dropped once |
| replacement/rehash | old entry dropped after new entry commits |
| shutdown/drain | every owner reaches zero live slots and full credit restoration |
| callback reentry | no generated/user operation executes under owner lock |
| saturation | producer parks; bounded control wakes/progresses; no busy loop/lost wake |

Where timing matters, add a deterministic sync point and a negative-control
build that reproduces the old lost/drop/wake behavior. A timeout-only test
without proving the intended interleaving is not sufficient.

## 8. Diagnostic Contract

Surge is a friendly language. If sema/monomorphization can prove an invalid
operation, it reports it before runtime with the most useful facts it knows.

### `Task<T>.clone()`

- reuse `SEM3116`;
- primary span is `.clone()` or its call span;
- headline explains that cloning the handle requires an independent result but
  `T` is neither Copy nor validly `__clone`-able;
- notes show the task payload declaration, first non-clonable component path,
  and an incompatible `__clone` declaration when one exists;
- help suggests consuming/moving the single task handle or implementing the
  already legal `__clone(&T) -> T` contract only when the advice classifier
  proves that this concrete type can legally receive that method;
- attach **no automatic fix**: the compiler cannot safely invent a clone body
  or add `@copy` without changing global semantics;
- generic code records a tri-state/deferred `Clonable(T)` obligation and emits
  the same source diagnostic at concrete instantiation, with notes linking the
  generic clone site and instantiation.

Sema stores these obligations in its result. The shared instantiation-discovery
and post-sema validator runs for build, ordinary `surge diag`, and LSP without
requiring backend lowering. Uninstantiated generic definitions remain deferred.
The same authoritative classifier is used by this validator, mono rewrite,
direct clone calls, `Task<T>.clone()`, and every clone-advice emitter.

### Type-directed advice

Wave A records, and the final absence gate rechecks, every clone-advice emitter.
The initial mandatory census includes `move_tracking.go`,
`type_expr_calls.go`, `reference_containment.go`, `nosend_checks.go`, and
`borrow_lints.go`; newly discovered emitters join the manifest rather than
bypassing the shared query. Each operation uses this capability matrix:

| Capability | Allowed advice |
| --- | --- |
| implicit Copy | explain/use the operation's ordinary Copy route; never invent a method call |
| exact callable method | for an ordinary value the emitter may show the verified public `clone(&value)` route; `.clone()` is only the local Task operation |
| legally implementable target from this module | help may describe the required method signature, but cannot invent its body or attach a fix |
| unavailable, sealed, far/runtime-owned, or non-extendable | offer only applicable move/consume/borrow/restructure help |
| deferred generic | retain a non-semantic deferred-advice context and wait for concrete instantiation; do not add a clonability constraint |

The result is operation-aware: an otherwise clonable fact is not enough if the
suggested spelling is inaccessible or illegal at that source position.
`CanDefineHere` must use the same canonical receiver resolution and
resolver/sema legality checks that would register the new `extern<T>` method;
`!sealed` alone is not proof, especially for aliased/qualified imported types.

The initial census expands to these logical sites and expected routes:

| Emitter/site | Copy | Valid method | Non-clonable | Deferred |
| --- | --- | --- | --- | --- |
| `move_tracking.go`: moved local `Task<T>` | `task.clone()` | `task.clone()` | consume the affine handle once; definition help only when proven legal | omit clone clause |
| `move_tracking.go`: ordinary moved value | unreachable Copy-move invariant | borrow or `clone(&x)` before move | borrow/change ownership flow | omit clone clause |
| `type_expr_calls.go`: owned-parameter marker | unreachable existing guard | `own x` or `clone(&x)` | `own x` or change parameter to borrow | omit clone clause |
| `type_expr_calls.go`: borrow into owned | unreachable existing guard | move `x` or `clone(&x)` | move `x`/change destination lifetime | omit clone clause |
| `reference_containment.go`: returned borrow | return the Copy value | return owned `clone(&x)` | move/change return ownership | omit clone clause |
| `reference_containment.go`: task borrows frame local | pass the Copy value | pass `clone(&x)` or await locally | move into task or await locally | omit clone clause |
| `reference_containment.go`: reference in aggregate | store the Copy value | store `clone(&x)` | move owned value/redesign lifetime | omit clone clause |
| `nosend_checks.go`: channel borrow | send the Copy value | send `clone(&x)` | move/restructure | omit clone clause |
| `borrow_lints.go`: partial move | unreachable Copy-move invariant | borrow field or `clone(&place)` | borrow/restructure only | omit clone clause; retain independently safe `own` fix |

One renderer owns these phrases. A static census test rejects user-facing clone
advice outside it, and a table test covers every site by every capability.
Only an actual generic operation whose validity requires cloning—most notably
`Task<T>.clone()`—records a semantic `Clonable(T)` obligation. Deferring an
optional clone sentence on some other ownership/lifetime diagnostic must not
constrain `T`, reject an instantiation, or change the primary diagnostic.

For a use after consuming `try_send(own value)`, explain that the argument is
consumed even when the operation returns `false` and the runtime drops the
unaccepted value. A fix is attached only when the exact edit is `AlwaysSafe`.

### Compiler/runtime failures

A legal concrete type without layout/`ValueOps` is an internal compiler error,
not `FUT7020` or a runtime fallback. An internal ABI manifest/sentinel mismatch
reports one concise rebuild/reinstall message by default; expected/actual
manifest details appear only with the existing
`--trace=- --trace-level=debug` interface. Old source/API names receive ordinary
unresolved-name diagnostics; there are no migration fixes or deprecated aliases.

### Diagnostic tests

`.diag` goldens pin the headline. Direct diagnostic-structure tests pin codes,
spans, all Notes/Help/Fixes, applicability, and zero fixes for
`Task<T>.clone()`. Real CLI tests cover `surge build --ui=off` and `surge diag`;
LSP tests cover `relatedInformation`, `publishDiagnostics.version`, trusted
server-cached snapshot records referenced by `Diagnostic.data`, and
AlwaysSafe-only Code Actions. The action gate checks canonical URI,
analysis/diagnostic/fix ids, every analyzed open document's snapshot/version,
bounds, overlap, and `OldText` both before and after materialization, then emits
only versioned `documentChanges`. Replace/delete without `OldText` is rejected;
empty `OldText` means insertion only. First-phase edits to closed/disk-only
documents are suppressed. Stale/unknown actions return `[]` (or
`Content modified` from resolve), never an edit.

Required races cover fresh success, `didChange`, `didSave` without a version
change, another snapshot document changing, close/reopen with a reused version,
forged or URI-mismatched data, heuristic/manual applicability, `OldText`
mismatch, mutation between materialization and the final check, and a versioned
multi-document result. A negative CLI fix-engine test proves heuristic/manual
candidates make `fix once` apply nothing. Every `AlwaysSafe` edit has an
apply-and-rediagnose proof.

The clonability matrix covers concrete/generic Copy; valid local/imported
`__clone`; alias and nested-component failures; missing/private method; wrong
receiver, arity, variadic marker, or result; local extendable, imported public,
aliased/qualified imported, sealed, protected/conflicting, structural, and
non-canonicalizable targets; `Task<far Task<T>>`; and identical `SEM3116`
behavior from build/diag/LSP.
Tests also assert the absence of stale impossible clone advice and of `FUT7020`
for legal typed-carrier values. A skewed generated-manifest negative control
proves nonzero build exit, one concise default mismatch, no compatibility
execution, and expected/actual details only under
`--trace=- --trace-level=debug`.

## 9. Semantic Absence Gate

The gate targets erased carrier semantics, not the `uint64` token globally.
Language numeric values, ids, generations, routes, sizes, alignments, strides,
lengths, counters, deadlines, and numeric-runtime APIs may legitimately remain
64-bit.

Production code must contain no definitions, declarations, or uses of:

- `emitValueToI64` or `emitI64ToValue`;
- payload fields named `result_bits`, `resume_bits`, `value_bits`, or
  `send_bits`;
- carrier `payload_drop_fn_id`/`result_drop_fn_id` and old numeric drop
  dispatches;
- a channel payload buffer typed `uint64_t *`;
- a universal map entry made from `uint64_t key` plus `uint64_t value`;
- task/channel/map/far/blocking APIs whose payload contract is `value_bits`,
  `out_bits`, `uint64_t *values`, or a `uint64_t` result;
- a captured state described by `(void* state, uint64_t state_size,
  uint64_t state_align)` — two integers cannot construct, copy or destroy what
  the pointer addresses, so the capture is erased exactly as a word payload is;
- a suspension frame whose storage compiled code reserves, sizes and releases,
  and the address-plus-id pair a task must hold to give such a frame back;
- composite `llvmType -> ptr` solely because the type is a composite;
- `isBoxedComposite` allocation/free/clone paths;
- VM task/channel/select/blocking payload fields typed `any`;
- VM struct/tag handles or heap objects as the representation of language value
  composites;
- VM frame/call/global/temp/async owner slots whose statically typed composite
  payload remains in a universal `Value` field/slice or a renamed indirect
  cell/sidecar;
- `Object.Arr []Value` (or an equivalent universal element buffer) for a typed
  dynamic array, and `MapEntries []mapEntry`/equivalent entries whose typed
  composite key or value is stored as `Value`;
- carrier `ptrtoint`/`inttoptr` conversions;
- old API declarations, feature flags, wrappers, or compatibility tests.

Implement this gate in three layers:

1. a narrow source-shape denylist over production carrier symbols/signatures,
   with explicit reviewed allowlisting for legitimate numeric/ID mechanisms;
2. type-aware VM structural assertions proving every statically known composite
   local/global/temp, direct/indirect call argument/result, async capture/resume,
   dynamic-array element, and map key/value selects exact layout-owned storage,
   never a universal `Value` or hidden indirect cell;
3. emitted-IR and behavioral assertions proving composite construction,
   copy/move/drop, direct/indirect call/return, local/global/temp access,
   fixed-array access, dynamic-array grow/reallocate/slice/index/teardown, map
   insert/replace/rehash/remove/teardown, async capture/suspend/resume/drain,
   task, channel, select, blocking, and far paths do not allocate a composite
   box or use integer reinterpretation.

The census at this epic's base commit is the initial manifest. Any newly found
carrier path is added to both the migration and the final absence manifest.

## 10. Performance And Resource Gates

Record a fresh clean-base baseline before implementation and compare on the
same host/configuration.

Epic 23b adds `make runtime-v2-carrier-bench`. On the reference
`x86_64-linux-gnu` host it builds the base and candidate at their recorded
commits in release mode and drives both with the frozen Wave A harness. For each
row it performs two warmups per binary followed by seven paired, serialized
measurements, alternating base/candidate execution order between pairs. The
machine-readable report records the base/candidate/harness/manifest hashes and
per-run throughput, p50/p95 latency, allocation count, bytes copied/moved,
callback count, credit stalls, and peak transport bytes. CPU affinity,
shard/thread counts, payload sizes, compiler, and host identity are fixed in the
report.

The harness builds counter-free base/candidate timing artifacts, runs both
allocation controls and every timing warmup/measured pair, and only afterward
builds and runs the separately instrumented candidate resource artifacts.
Resource-build cache effects therefore cannot bias the paired timing scores,
and resource elapsed/latency samples never participate in scoring.

For each row and for base and candidate independently, compute two coefficients
of variation across the seven measured runs: one over throughput samples and
one over the seven per-run p95 latency values. CV is sample standard deviation
divided by arithmetic mean. If any of those four CV values exceeds 5%, discard
and record the complete paired session; rerun both sides only after documenting
the environmental correction. Removing outliers or selectively rerunning one
side/row is forbidden. A timeout is a test failure, not a noisy sample.

The throughput score is the median of the seven throughput samples. The latency
score is the median of the seven per-run p95 latency values.

Required properties:

- zero heap allocations for local composite construction/copy/drop, ordinary
  arg/return, place access, and fixed-array element access;
- zero per-value allocation for task completion/await and channel send/receive
  of trivial or inline-composite payloads;
- dynamic arrays/maps allocate for container growth, never an element box;
- a channel uses one owner/buffer allocation where alignment permits;
- transport resident bytes are bounded by admitted data/jumbo reservations
  plus the fixed control budget;
- saturated send records stalls, parks rather than spins, restores credits, and
  cancels/shuts down within its liveness budget;
- for every pre-existing word/scalar row, the candidate throughput score is at
  least 95% of the clean-base score and the candidate latency score is at most
  110% of the clean-base score;
- typed payload rows meet the absolute allocation/resource properties above;
  their copy cost must be proportional to the reported physical bytes;
- generated callback count and bytes copied/moved are reported for large
  payloads so a hidden clone storm cannot pass on throughput alone.

Run the repository's channel and relevant crossing/far benchmarks through that
stable harness. Every absolute allocation/resource invariant must hold in every
measured run; it is not median-aggregated. A threshold miss blocks closeout.
Only the project owner—not the epic lead, implementer, or reviewer—may approve a
threshold/manifest change or an intentional architectural regression. The dated
decision must be recorded before the affected change is integrated; a measured
miss cannot receive a retrospective waiver. Do not hide a timeout behind a
rerun. If a Runtime V2 test newly times out, bisect to the first integration
commit and treat it as a blocker until the clean-base comparison proves it
unrelated.

## 11. Golden Protocol

The AST is allowed to rebuild; unexplained or unstable goldens are not.

1. Before code changes, run `make golden-check`; then require zero output from
   `git status --porcelain=v1 --untracked-files=all -- testdata/golden` and from
   the NUL-safe filesystem-versus-`git ls-files` golden-root census (the latter
   also sees ignored files). Any tracked, untracked, ignored, or missing golden
   blocks the baseline. The target runs `golden-update` before comparing, so it
   is an updating detector, not a read-only preflight.
2. Before the first compiler-output wave, add the equivalent post-generation
   status and filesystem/index checks to `make golden-check` itself. A newly
   generated sidecar must make that target fail even when `git diff` cannot see
   it.
3. After each compiler-output integration wave, run `make golden-check`. If the
   compiler output changed, the command fails and leaves the generated diff as
   the proposed update. Every tracked/untracked generated entry remains a
   proposal until reviewed and committed; do not run a separate blanket
   regeneration first.
4. Inspect and explain every proposed changed line by semantic category. Accept
   only that reviewed set; never blanket-rebless the corpus.
5. Record a path-plus-content-hash manifest for every reviewed generated file,
   regenerate again, and require byte-for-byte identity plus zero status and
   filesystem/index differences.
6. AST/MIR ids or shape may change only when the semantic reason is recorded
   and the resulting goldens are deterministic.
7. Integrate the accepted goldens with their compiler wave. At closeout, run two
   serialized `make golden-check` executions on that same tree. Both must pass
   with no intervening source edit, zero tracked/untracked/ignored/missing
   status, and the unchanged reviewed content manifest.

## 12. Required Commands And Evidence

Focused tests are named as implementation paths become concrete, then promoted
into one stable `make runtime-v2-carrier-check` target. That target must run
twice consecutively without rerun or hidden retry before closeout.

At minimum record:

```bash
EPIC_BASE="7df10725e001ddf915d536aa58f880bd7e04aafd"  # the commit that added this document; also legacy_carriers.json base_commit
git merge-base --is-ancestor "$EPIC_BASE" HEAD
git diff --check "$EPIC_BASE"..HEAD
make c-check
make cppcheck
make runtime-v2-abi-manifest-check
make runtime-v2-crossing-check
make runtime-v2-carrier-check
make runtime-v2-carrier-check
make runtime-v2-carrier-sanitizer-check
make runtime-v2-carrier-bench
make runtime-v2-check
make runtime-v2-check
make golden-check
make golden-check
make runtime-v2-file-size-check EPIC_BASE="$EPIC_BASE"
make check
```

Also required:

- exact focused Go/package tests for every touched compiler/runtime owner;
- VM/LLVM parity fixtures for every carrier family;
- `make runtime-v2-carrier-sanitizer-check` on the reference
  `x86_64-linux-gnu` runner. It first requires Valgrind, ASan/UBSan, and TSan
  availability, then runs the focused carrier rows with skip-on-missing disabled;
  any skip or unavailable tool fails the target;
- deterministic liveness sync-point positives and negative controls;
- before/after channel/crossing/resource benchmarks from Section 10;
- Sentrux scans for root, `internal`, `runtime`, and `runtime/native`. The lead
  serializes one `session_start`/final scan/`health`/`check_rules`/`session_end`
  pair per enforced scope on the integrated tree; parallel workers do not keep
  overlapping active Sentrux sessions;
- `make runtime-v2-file-size-check EPIC_BASE=7df10725e001ddf915d536aa58f880bd7e04aafd`, which evaluates
  the committed diff even on a clean tree: every new production file and every
  file with added+deleted effective LOC >=50% of its base effective LOC must be
  <=500 effective lines; an existing >500-line file below that rewrite threshold
  may not grow and must record its split/debt disposition;
- independent ABI/layout, ownership/lifetime, transport-credit, diagnostics,
  and final integrated-diff reviews;
- documentation/status/debt reconciliation.
No version gate runs here. When the whole Runtime V2 refactor is finished and
the owner decides the version moves, the surface to pin is `internal/version`'s
fallback, new-project and tool metadata defaults, and the public install
examples — without rewriting unrelated fixture package versions. That check does
not exist yet (there is no `version-check` target in the Makefile), and building
it belongs to whoever closes the refactor, not to this epic.

An unavailable mandatory carrier/sanitizer/performance/golden/Sentrux/file-size
gate blocks closeout; it cannot be converted to ordinary tooling debt. Known
broad legacy matrix failures remain governed by `RULES.md`, but every focused
23b path must be green.

## 13. Debt Disposition

Epic 23b owns, but does not close until evidence passes:

- `RV2-DEBT-031` — replace message-count placeholders with physical byte
  credits and progress-safe reply/control accounting;
- `RV2-DEBT-056` — remove boxed struct elements from dynamic arrays;
- `RV2-DEBT-062` — exact nested Task/Channel handle drop glue through typed
  carrier/container teardown;
- `RV2-DEBT-080` — typed blocking body/result ownership and cancellation;
- `RV2-DEBT-082` — caller-owned anchored handle plus generation-checked pinned
  body lease;
- `RV2-DEBT-133` — non-clonable `Task<T>.clone()` unsoundness and independent
  result-entitlement semantics.

Adjacent debts:

- `RV2-DEBT-061` is a required regression sentinel on the replaced immediate-on
  handoff path. It becomes an epic blocker only if reproduced or retained by
  the new mechanism; deletion alone does not close it without Valgrind/TSan
  stress evidence.
- `RV2-DEBT-125` keeps its benchmark/matrix/seam history. Typed-carrier evidence
  may supersede its old-representation wording, but closes it only when every
  named acceptance property is measured.
- Epic 22's six current `float`/composite crossing barriers are absorbed here:
  retaining them would leave a forbidden carrier fallback. Their portion of
  `RV2-DEBT-038` closes only with all six deep-copy/exact-drop proofs. Epic
  22's `int`/`uint` reclamation (`RV2-DEBT-035`/`068`) remains separate;
  `ValueOps` must support that future value class without performing its COW/RC
  migration here.
- `RV2-DEBT-120` and unrelated backend/test-matrix debt remain nonblocking
  unless they directly prevent a required 23b proof.

New nonblocking findings are recorded with an owner and close condition. New
memory-safety, ownership, liveness, semantic, diagnostic-contract, golden, or
required-gate failures are blockers.

## 14. Independent Review Protocol

At the end of each integration wave, at least one non-author reviews the
integrated diff from a clean temporary worktree. The final pass uses independent
reviewers for these lenses:

1. ABI/layout and full carrier census/absence;
2. ownership, partial initialization, cancel/shutdown, lock callbacks;
3. transport byte credits, saturation, control progress, anchored leases;
4. diagnostics/fixes and generic clonability obligations;
5. golden determinism, test validity, negative controls, and benchmark claims.

Reviewers first submit a plan under `RULES.md`, cite concrete files/evidence,
and classify every finding blocker or debt. The lead records dispositions in
`NOTES.md`; unresolved blockers prevent the next wave or closeout.

## 15. Done Definition

Epic 23b closes only when:

- the normative design is implemented on every owner surface;
- no erased carrier or composite-box fallback survives the semantic absence
  gate;
- VM/LLVM and local/far paths pass the complete behavior/lifecycle matrix;
- `Task<T>.clone()` has independent result semantics and precise concrete plus
  generic negative diagnostics;
- all callbacks run outside runtime locks; every returning, cancellation, and
  shutdown path drops once, while terminal panic/OOM retains the language's
  explicit no-unwind contract;
- byte credits and control progress hold under deterministic saturation;
- allocation, sanitizer, liveness, and performance evidence meets Sections
  7/10/12;
- goldens are reviewed, deterministic, and pass twice;
- all mandatory Sentrux and independent reviews pass with no blocker;
- owned debt rows close with exact evidence and nonblocking findings have
  durable owners;
- target/current docs are reconciled without rewriting Phase 1 history;
- the main integration candidate passes main CI, at the version the tree already
  carries.

Closing this epic is not close to the end of the work. The epic chain resumes at
Epic 22's remaining WidthAny `int`/`uint` reclamation, and beyond it are the
other unfinished epics, the debt ledger, the stdlib refresh the owner deferred
until after Runtime V2, and further work still to be planned — including ideas
not yet written down anywhere. The version surface moves once, at the end of all
of that, and only when the owner says so.
