# Step 7 D2 Runtime V2 topology cutover

Status: candidate implemented in `codex/step7-d2-greenup`; the final audit found
an unresolved owner-placement violation (487) and a reproduced synchronous
entrypoint numeric-RC failure (488). Final closure requires their resolution
and the all-green gate packet on one clean exact SHA.

## Decision

Owner clarification (2026-10-14): this cutover may change `core` and `stdlib`
when their current contracts rely on invalid runtime behavior. Backward
compatibility is not an acceptance condition. When the compiler can prove an
operation invalid, it must diagnose it before lowering; value-dependent owner
and lifetime errors still require precise runtime refusal. A library repair
must use those same rules rather than bypass them.

Tier 1 has one supported topology: `N shards x 1 carrier`. `SURGE_SHARDS`
selects both counts. Without an override, the runtime uses the online CPU count,
capped at 64.

The cutover removes the former `1 shard x N carriers` migration mode. It also
removes `SURGE_THREADS`, peer stealing and wake rules, per-worker local queues,
carrier-affine task constructors, and synchronous channel compensation workers.
The I/O service thread and bounded blocking pool remain separate roles and never
poll Tier 1 tasks.

Epic 22 keeps non-atomic numeric reference counts. Their required safety
argument is that every task owning or borrowing a local value stays on one
owner shard and that shard has one carrier. The final audit below found that
owner adoption still violates this requirement. Cross-shard boundaries retain
their clone, unshare, or refusal obligations.

## Runtime and compiler changes

- Every shard owns one local queue, one injection queue, one carrier context,
  and one durable shard wake token.
- `SURGE_SHARDS=1` starts a carrier thread. External `rt_task_await` uses the
  completion condition variable rather than an inline scheduler.
- Tier 1 scheduler selection reads only the shard's local and injection queues.
  No task can be stolen by a same-shard peer.
- Borrowing tasks use owner-shard placement. MIR and LLVM emit the ordinary
  cold constructor; the affine constructor ABI no longer exists.
- Channel send/recv in ordinary sync helpers is rejected with SEM3007; blocking
  bodies cannot inherit an enclosing async/root suspension context. Legal async
  controls run on both backends. Native blocking ABI calls from a task still
  fail closed with `channel send requires async lowering` or
  `channel recv requires async lowering`.
- Scope completion from another shard remains an owner-lane event. It cannot
  mutate scope state while the owner's carrier is held inside a poll.
- Waiter keys that can outlive their task carry a stable owner-shard route.
  `WAKER_TRANSPORT_SLOT` uses a nonzero one-based identity even for shard 0,
  and a blocking job retains its owner-stamped completion key instead of
  looking up a possibly retired task.
- Shutdown of a slot-parked crossing retires the slot registration and wakes
  the producer by task id. A foreign shutdown wake lands on the shard-local
  exit queue, which the shard's sole carrier drains before it leaves.
- Return-origin analysis now accepts checker-certified, source-free captures and
  payloads when lowering emits the recursive unshare walk. Views, captured
  loans, references, hidden container storage, and unsafe task payloads remain
  refused.

## Removed transition evidence

Tests that only exercised same-shard peer stealing, peer wakeup, carrier pins,
inline-main scheduling, or compensation workers were deleted. Lifecycle stands
that tested a real invariant were rewritten around owner-shard serialization.
For example:

- DEBT-248 now proves that the sole carrier retires one stale join registration
  before it can publish one fresh registration.
- DEBT-261 holds the scope owner carrier while a foreign child completes. Scope
  state remains unchanged until the owner applies the queued event.
- Scope teardown registers its waiter before the owner drains a queued foreign
  completion event.

## RV2-DEBT-452 counterfactual

The retained counterfactual runs a parent and local child that repeatedly clone
and compare the same heap integer `9223372036854775808` for 200,000 iterations.

On the original D2 topology at `SURGE_SHARDS=1 SURGE_THREADS=8`, 8 of 10 runs
failed with SIGSEGV 139 or `bigint release below zero`. On the cutover candidate,
the probe passed:

| Configuration | Result |
| --- | ---: |
| native, `SURGE_SHARDS=1` | 100/100 |
| native, `SURGE_SHARDS=2` | 100/100 |
| native, `SURGE_SHARDS=8` | 100/100 |
| TSan, shards 1/2/8 | PASS, no race report |
| VM | PASS |

The repository gate row is
`TestRuntimeV2LocalCountedScalarStaysOnOneCarrier`. Raw local artifacts are under
`/home/zov/.cache/surge-artifacts/step7-d2-greenup/20261012-cutover/`.

## Validation so far

Before the final lifetime/placement audit, the dirty candidate passed this
pre-commit roster. These runs do not certify later edits:

- `make check` and `make check-full` after the last source and golden changes;
- the 21-row `make runtime-v2-check` aggregate, including ownership, crossing,
  heap/Valgrind, owned-storage, lifecycle/TSan, panic, carrier, transport,
  sync-point, and H2 gates;
- the full ownership census: 1,162 attempted programs, 629 MIR successes,
  449 invalid programs, 84 other non-invalid outcomes, and zero normalized
  findings;
- `make behaviour-check-all` and `make behaviour-check-mt` on both backends;
- the focused transport shutdown, blocking-completion, scope-reclamation,
  HTTP-owner, sanitizer, TSan, and strict Valgrind repetitions;
- two consecutive `make golden-update` runs with an identical SHA-256 manifest
  for every golden file.

Sentrux was compared directly with base `85827bcf`. Root quality moves from
5311 to 5313, runtime from 4616 to 4604, and `runtime/native` from 4719 to 4708.
The rounded quality scores and the known cycle are unchanged; the one maximum
complexity violation improves from 167 to 166. The owner accepts this small
structural delta for D2.

The heavy-run guard now supports explicit local evidence. Clean runs identify an
exact SHA. Dirty development runs require a second opt-in and print a content
fingerprint; they do not count as final landing evidence.

## Remaining closure work

1. Fix synthetic-root execution (RV2-DEBT-488), then the owner-placement
   violation (487), and restore model-aligned lifetime regression coverage;
   repeat the affected development gates.
2. Commit the reviewed candidate after owner authorization.
3. Re-run the final roster on that clean exact SHA.
4. Run `make golden-check` twice consecutively on the same SHA; its preflight
   intentionally refuses a dirty golden tree.

## Final audit: the synchronous root is not a task

RV2-DEBT-488 is the first repair priority. The same counted-bigint loop used
above, moved directly into synchronous `@entrypoint fn main`, diagnoses clean.
On the retained native build, `SURGE_SHARDS=1 SURGE_BLOCKING_THREADS=1` produced
exit codes `1, 0, 0`; the first run reported `bigint release below zero` at the
root loop. VM ran the same source with exit 0. No network operation or owner
adoption is needed for this failure.

`rt_entry.c` calls `__surge_start` on the external main thread, and the retained
LLVM shows `__surge_start` calling the user main directly. A spawned child is
published to shard 0, whose separate carrier runs concurrently with main. The
V2 synthetic-root ruling explicitly forbids this exception. The root must use
the ordinary task/frame machinery, with startup, argv/stdin, global
initialization and exit-code handling kept correct; numeric RC stays
non-atomic. Rejecting the documented local main/child program is not the fix.

Artifacts are under `20261014-task-ref-audit/`: source
`surge-d2-root-counted-probe.sg`, `root-counted-run-{1,2,3}.{out,err}`,
`root-counted-vm.{out,err}`, and the retained `target/debug/.tmp/` LLVM and
native source/object build. The executable SHA-256 is
`5348fae252d43c71da2e88c453308a10f4f5023b5da864d57de4c13a1b3ebbed`.
The full lifecycle gate and `make check` passed immediately before this
probe; their coverage did not include a counted-copy race in synchronous main.

### Synthetic-root repair candidate (2026-10-14)

The dirty candidate now lowers startup and the entrypoint activation through
the ordinary coroutine machinery. The external adapter receives only a fixed
`int64` exit code. Global initialization, argv/stdin parsing, default arguments
and user-defined exit-code conversion execute on the root carrier. Private
compiler-owned Task/TaskResult types also support `no_std` without reserving
source names. Numeric RC remains non-atomic.

Focused evidence passed: the synchronous counted-scalar reproducer ran 20
times at each of 1, 2 and 8 shards; shared and mutable main-local borrows across
suspension passed VM, native at those shard counts, and strict Valgrind. The
no_std user-name regression passed both backends. Entrypoint argument storage
and invalid/missing argument paths retain the physical-zero Valgrind criterion.
The latter now covers signed, unsigned and floating-point parse errors. Real
leaks found in already-prepared arguments, default-expression temporaries and
error-message construction were fixed in production code.

`make check` passed after the root changes. Logs are
`/tmp/surge-root-make-check3.log`, `/tmp/surge-root-borrows.log`,
`/tmp/surge-root-private-abi-runtime.log`, and
`/tmp/surge-root-parse-error-matrix.log`. This is development evidence on the
dirty candidate, not the final exact-SHA gate roster; RV2-DEBT-488 remains open
until that proof is complete. RV2-DEBT-487 remains an implementation blocker.

Follow-up review found that removing the old scope-local-index parameter
heuristic exposed lost initial captures in synthetic async blocks. The added
`captured-block` row failed on VM with `seed used before initialization` before
the correction. The async transform now obtains the parameter set from the
original callable, whose ParamCount is authoritative, rather than its generated
zero-argument poll function. That row passes VM, native at 1/2/8 shards and
strict Valgrind (`/tmp/surge-root-captured-block-{before,after}.log`).

The first new `make check-full` run also rejected `channel_basic_ops.sg`, whose
generic method-resolution fixture used suspending send/recv in ordinary sync
helpers. Only those helpers were changed to async; the nonblocking try-method
control stays synchronous. Full validation is being repeated after these fixes.

The second full run passed the ordinary packages, including the representative
ownership gate, then found three driver fixtures that still used sync channel
suspension. `channel_own` in the moved-receiver fixture is now async, with its
source digest and exact spans updated. The two leaking-task fixtures now use
`try_send`: a successful send still lets a borrowing task escape its local
owner, so they continue to require exactly SEM3021. The expected error set was
not widened to accept the unrelated context error. All affected driver rows
pass (`/tmp/surge-root-context-fixtures.log`); the complete `make check-full`
roster has not passed after these changes.

### Open root edge: source calls back into the entrypoint

A fresh compiler build still accepts this source:

```sg
@entrypoint
fn main(depth: int = 0) -> int {
    if depth == 0 { return main(1); };
    checkpoint().await();
    return 0;
}
```

The startup invokes the coroutine specialization, but `main(1)` invokes the
retained ordinary callable body, whose await was not transformed. Native fails
with `task await requires async lowering`; VM reports `async join_all outside
task`. This is a concrete unresolved edge of RV2-DEBT-488, not a reason to weaken
the runtime guard. Whether to prohibit source calls/references to an entrypoint
or support these activations through coroutine lowering needs a contract check
against the model's no-surface-change ruling before choosing a language rule.
The owner subsequently approved the runtime-only rule for synchronous
entrypoints: source calls and function-value references are rejected with
SEM3226. The diagnostic and its reference/import tests are being implemented;
the original failing runs remain the before evidence. Focused tests now pass
for recursion, direct calls, function aliases/arguments and imported calls and
values. Diagnostic controls preserve ordinary functions (including an
unannotated `main`) and explicit async entrypoints. Log:
`/tmp/surge-root-reentry-diagnostic4.log`.

The complete `make runtime-v2-lifecycle-check` subsequently passed on the dirty
candidate (`/tmp/surge-root-lifecycle.log`), including root counted scalars,
borrowed locals, captured async blocks, no_std, deterministic task reclamation,
cancellation, cold-task TSan and strict Valgrind rows. This is development
evidence: broad validation and the final clean exact-SHA roster remain open.
The gate still includes the legacy owner-adoption test; its pass does not
discharge RV2-DEBT-487 or make the network path model-correct.

`make check` also passed after the final SEM3226 changes, including lint,
C checks and file-size checks (`/tmp/surge-root-make-check4.log`). The latest
logs are retained under
`~/.cache/surge-artifacts/step7-d2-greenup/20261014-synthetic-root/`.

Probe and logs are retained in `20261014-synthetic-root/` as
`surge-root-reentry-probe.sg`, `surge-root-reentry-native.log`, and
`surge-root-reentry-vm.log`. The fresh compiler SHA-256 is
`04edd20ef358c1f3cb9a73d9efc7dbb21976867c9ddb38e232a694db33f5dc80`.

## Final audit: owner placement is still mutable

`docs/RUNTIME_V2.md` requires owner assignment before publication and forbids
a `RUNNING` task from repinning itself. The code still has the older F2
protocol: both DONE branches of `rt_task_poll` call
`rt_task_poll_adopt_placement`, which replaces the running joiner's owner with
the completed connection task's owner. Accept readiness and the accept fast
path also replace ownership (`rt_async_waiter.c`, `rt_net_accept_group.c`).

The existing `TestRuntimeV2LifecycleJoinConsumePlacementAdoption` passes on the
candidate and explicitly requires the running joiner to change from shard 0
to shard 1. This is evidence of the mismatch, not V2 acceptance. Older Epic 8
evidence describes this as a fairness fix; it does not override the later V2
owner ruling. A local task may still carry non-atomic counted values and
borrows shared with tasks remaining on its original shard.

Consequently the topology alone does not justify removing the reference held
by `apply_poll_outcome`: a subsequent publication can reach another carrier
after owner adoption. The attempted pin removal was withdrawn before being
accepted. The old same-shard peer proof needs replacement. Task-handle
reclamation now has a deterministic external-drop/owner-carrier proof:

- one carrier is held inside the target's poll;
- an external last-handle drop pauses immediately after decrementing to zero;
- the carrier completes and frees the task, with the slot observed empty;
- the shipping drop resumes without dereferencing the reclaimed task;
- the split-decision control reads the freed status and ASan reports that
  precise access in `task_drop_ref_owes_free`.

`TestRuntimeV2LifecycleTaskFreeIsOneObservation` passed both arms and is back
in the mandatory lifecycle gate. Strict C and sync-point static checks passed.
These task-handle control references are distinct from Epic 22's non-atomic
numeric value reference counts.

Closure must remove silent owner adoption and make acceptors/handlers begin
on the connection's owner shard, preserving the crossing clone/unshare and
borrow rules. Merely turning the local numeric reference counts atomic or
rewriting the model to allow adoption is outside the approved decision.

### Repair dependencies under investigation

The supported network surface currently exposes a group through one
`TcpListener`. `http.serve` duplicates that handle into `@local` workers;
`rt_net_wait_accept` registers a task on every group member, and
`rt_net_accept` scans all members. Replacing owner adoption with an owner
assertion alone would strand or reject these workers. Redirecting each accepted
fd to the worker would violate the same V2 rule in its resource-migration form.

The model-aligned repair must cover the whole path:

1. Give each acceptor only its shard's socket and create the acceptor on that
   shard before publication. A shared group must remain a bootstrap/control
   object, never a local listener value copied across shards.
2. Create connection handlers on that same owner. Ordinary `await`, accept
   readiness, and connect cannot change a running task's owner.
3. Carry server configuration and cancellation across the explicit placement
   boundary through typed clone/unshare and far control endpoints. Local
   borrowed values and listener handles cannot be smuggled as scalar ids.
4. Establish the required placed-async capability before rewriting HTTP around
   it. A source probe using `spawn on shard(1:ShardId) { worker().await(); ... }`
   currently returns FUT7023; `lowerSpawnOnPollFunc` builds an `IsAsync: false`
   function. Simply spawning an unjoined local worker inside that body is
   SEM3107. RV2-DEBT-344 records the underlying missing state-machine support.
   The network fix must not bypass either barrier with a hidden blocking await.
5. Replace adoption tests with owner-immutability and cross-shard bootstrap
   evidence; retain real cross-owner scope completion and handle lifetime
   tests. Reassess the poll-outcome pin only once every owner writer is covered.

The public listener/bootstrap API and the extent of the placed-async lowering
work are not yet settled by this audit. No network implementation change is
claimed here; the recorded passing legacy placement test is the reproducer.

`TcpListener` and `TcpConn` already carry `@nosend @shard_pinned` in core.
Preserve and test those compile-time barriers through captures, aliases,
aggregates and returned payloads. The old net wrappers erase handles to
`int64` and reconstruct them inside async bodies, so replacing them with typed
borrow/owner parameters is part of the repair, not a reason to exempt stdlib
from the ownership checker. Negative diagnostics need legal same-shard
controls, so a conservative rejection cannot stand in for a soundness fix.

The first diagnostic probe pair confirms the existing barrier: explicitly
moving an owned `TcpListener` into a placed body produces SEM3167; a joined
local child borrowing `&TcpListener` diagnoses clean. The next implementation
must retain both results. Probe sources, logs, the new task-ref evidence, and
the full passing lifecycle run are retained under
`/home/zov/.cache/surge-artifacts/step7-d2-greenup/20261014-task-ref-audit/`.

The preferred compiler direction is to reuse `LowerAsyncStateMachine` for
placed bodies rather than introduce a network-only task runner. The shared
lowering already provides stable resident places, pending/resume variants,
scope draining and typed frame disposal. Crossing preparation must create the
matching initial frame from the transferred captures; nested placed bodies
must be lowered before their enclosing caller's liveness analysis so a retry
does not repack already consumed captures. Source-copy relinquishment and
result unsharing must remain explicit at their existing crossing boundaries.
For placement bodies, FUT7023 can be retired only after real native
suspend/resume and cancellation rows pass with lifetime/sanitizer evidence.
The VM currently rejects placement crossings (FUT7015); preserve and test that
documented boundary. Local VM coroutine controls prove the shared lowering,
not execution of a crossing on VM.

### Shared placed-body lowering: implementation constraints

The read-only follow-up narrowed the change to `spawn on Placement` and
`on Placement`. Both should use a synthetic async body with captures in its
leading ParamCount locals, an ordinary scope, `CapturesArriveOwned=true`, and
`ResultCrossesThreads=true`. The latter metadata must reach the generated poll.
The existing async normalization, liveness, resident storage and frame cleanup
remain shared; no local task is created just to ship its state elsewhere.

Extract a common initial-frame builder from `buildAsyncConstructorState`.
Ordinary async calls use it before `__task_create`; a crossing uses its frame
and poll id for transport. Transferred captures move into that frame: the
ordinary constructor's retain of borrowed RC parameters is wrong for holders
that `relinquishCapture` has already made private. Private Copy captures also
need ordinary body cleanup obligations, replacing the old hand-written return
drops. Preserve unshare on the returned payload after scope drain and local
drops, before terminal frame release.

Rewriting crossing initial state must precede the enclosing function's
liveness analysis. Once captures are packed in the payload, their metadata
cannot remain additional executable uses of the already-moved locals. Update
the ownership/relinquish validators and visitors together. Admission failure,
pending retry and cancellation must each leave one owner for every initialized
payload; preparing a payload before handle admission must not leak it on the
failure edge. Capture-less suspending bodies still need a PC-bearing frame.

Anchored `on far_handle` has a different existing one-operation protocol:
its runtime channel helper yields internally and replays the body from its
beginning. Its shape restriction makes that replay safe, and the anchor is a
caller-owned lease excluded from frame disposal. Wrapping it in the async
transform without a real MIR suspension site would be incorrect. Keep that
supported protocol and its explicit restrictions; do not silently widen it or
the existing prohibition on nested source crossings in this dependency fix.

Required new witnesses include multiple awaits with exactly-once effects,
unchanged owner across resumes, a placed body's borrowed local child,
counted/composite/moved captures, private returned values, and cancellation
before first poll, during await, during transport retry and before reply
consumption. Repeat native execution at 1/2/8 shards, keep the existing anchored
lease matrix, and pair refused captures/results with legal same-shard controls.
### Placed-body candidate and new rendezvous blocker (2026-10-14)

The shared lowering is now implemented in the dirty candidate. Placement
bodies become ordinary async activations with owned transferred parameters.
`buildAsyncInitialFrame` serves both normal constructors and crossing initial
states; lexical crossing dependencies are lowered before caller liveness.
Capture records become metadata after packing. The pack moves the already
private operands rather than retaining them, and it does not declare later
resume variants or local resources generally shard-movable. Return unsharing
is preserved on the successful edge after scope drain. Anchored replay bodies
keep their existing protocol and suspension diagnostic.

First execution evidence passes:

- `TestRuntimeV2PlacedBodySuspendsWithLocalBorrow`: spawn/immediate placement,
  multiple awaits, exactly-once effects, a borrowed local child, preserved
  counted source capture, 1/2/8 shards and strict Valgrind;
- the five frozen H2 runner/await/local-select/far-select programs now run with
  their exact expected exit values on native and have physical-zero Valgrind
  results (`/tmp/surge-placed-backend-contract-native.log`);
- the former FUT7023 expectations are replaced with execution controls;
  SEM3021/SEM3139/temporary-borrow refusals remain required and pass;
- cancellation immediately after publication passes, and the capacity-1
  entered-body cancellation control passes 30 native runs and strict Valgrind.

The capacity-0 entered-body case remains RED (RV2-DEBT-489). Both remote
selectors are waiting before cancellation: native wakes seq0 subscriptions
without transferring a value. A local selector pair reproduced the same
defect with the retained compiler from before these placed-body changes:
native spins, and VM reports `async deadlock`. The zero-capacity case has not
been replaced by the buffered control. Both it and a local VM/native pair
regression are now in the lifecycle gate.

Artifacts are retained under
`~/.cache/surge-artifacts/step7-d2-greenup/20261014-placed-async/`, including the
pre-feature local source/binary, bounded native traces and VM outcome.
The old compiler SHA-256 is recorded in the root-reentry section above.

Two additional compiler defects found while minimizing this probe were fixed:
the unused plain synchronous entrypoint body is no longer emitted (it retained
an unlowered local select), and an exact source-body symbol takes precedence
over name-based LLVM intrinsic dispatch (a user `send` was treated as Channel
send). Explicit async entrypoints keep their normal callable body. New root
select and user send/recv tests preserve these distinctions.

Rendezvous repair must retain the model's reservation-versus-commit boundary.
A persistent selector generation and arm identity must join the same receiver
FIFO as plain receives, reserve one arm, transfer into owned typed storage
outside locks, and publish one winner only at commit. Close/cancel before
commit cannot be reported as a successful send plus an orphan drop. A later
poll must consume the committed winner before rescanning; stale generations,
self matching and duplicate arms need explicit checks. Native uses the
channel owner lane and existing claim/retry/pin machinery. VM can use SelectID,
ChannelSendReservation and asyncSelectOwner capabilities; terminal cleanup must
drop a committed receive even if the cancelled task never polls again.

This is not D2 closure. DEBT-487 network owner adoption, DEBT-489 rendezvous,
remaining regression integration and the final exact-SHA gates remain open.

### Cancellation reply ownership audit (2026-10-14)

The strengthened placed-body cancellation witness includes a child borrowing a
resident string and looping through checkpoints. Native publication cancellation
passed 30 runs, but one Valgrind execution exited 0 with 1,214,648 reachable bytes
in 52 blocks. That failure remains open; later zero-heap runs do not erase it.
A link-only wrapper around `rt_executor_release_empty_storage`, inspecting raw
state only after worker joins, is retained outside production for capturing a
future guard refusal. It has not yet captured that original residue.

Repeated runs of the retained binary instead exposed 376 definitely/indirectly
lost bytes: a remote-select pending and its arm table. The request lifecycle
has a confirmed independent hole: real reply dispatch unlinked the request
before its caller consumed it, so a subsequent caller cancellation could not
find and release its reference. Older caller-abandon unit rows constructed the
terminal state directly and therefore missed the dispatch operation.

The candidate now keeps a landed request discoverable while its caller owns it.
Reply retirement unlinks an orphan under the same token lock that caller
teardown uses; teardown consumes an already-terminal bound request instead of
leaving a dangling registry entry. The deterministic real-dispatch witness
fails before the fix and passes after. Both reply-before-cancel and
cancel-before-reply pass physical-zero Valgrind for await, cancel, immediate,
anchored and select request families. Restoring unconditional reply unlink in
`RV2_REPLY_UNLINK_NEGATIVE_CONTROL` fails the precise cleanup-route assertion.
The original publication and buffered-entered source rows also pass again,
including their 1/2/8-shard repetitions and strict Valgrind. This is partial
RV2-DEBT-490 evidence; create/share request cleanup and the original
reachable-only failure still require investigation.

Logs and diagnostic shim sources are retained in
`20261014-placed-async/reply-abandon/`. Diagnostic binaries are explicitly
relinked probes, not evidence from an unchanged exact executable. No runtime
trace flag or production shutdown sweep was added to make the failure disappear.

Further audit isolated the shutdown counterpart: the dropped-message path used
plain `pending_release` and could free an orphan while leaving its pointer in
the registry. A diagnostic run captured no live tasks/scopes/waiters, but a
listed remote-select pending with zero references; Valgrind traced its free to
`rt_remote_task_release_msg_payload` during shutdown. The empty-storage guard
then correctly refused to destroy the registry. The same conditional unlink
now applies to dropped transport messages. A deterministic shutdown-drop row
passes strict Valgrind and restoring the old drop path fails its list/refcount
assertion.

Create/share were also absent from caller teardown. Their records now use the
shared caller cleanup, and a minted lease belongs to its pending until a
successful source-side take transfers it to the handle. Final pending release
returns an untaken lease. A held-transport test calls the real owner/reply
dispatchers and cancels before dispatch, while the reply is queued and after
reply delivery, for both create and share. It checks that the abandoned token
no longer resolves and only the original sibling remains active, then verifies
all registry storage is gone before shutdown. Lease tombstones retained by a
still-live original entry are distinguished from active leases. This matrix
passes strict-zero Valgrind; suppressing pending-owned lease release makes the
pre-shutdown assertion fail. The full remote behavior roster also passed after
the production changes. An older observer test was corrected to hold its own
reference while reading the target after teardown; previously it read freed
memory.

The retained pre-fix runtime then reproduced the registry residue as well:
`surge-original-residue-1.mem` traces the freed pending to the shutdown message
release, and the wrapper reports an otherwise empty runtime plus its dangling
registry entry. The capacity retained varies with how many checkpoint tasks
ran; this reproduction retained 67,768 bytes rather than the first run's
1,214,648 bytes. This establishes the shutdown-retirement defect on the original
runtime, rather than attributing it only to an intermediate fix.

A subsequent fixed-candidate stress reached the newly effective last release
inside `mark_done` while a runtime lock was held. Its select-arm destructor
correctly tripped the detached-drop guard. The same boundary is now covered by
`pending-reclaim-detached`: sixteen final releases under a shard lock and then
under control must perform no generated drops until unlock and exactly sixteen
after it. It fails before the repair and passes with strict Valgrind after.
Pending destruction now uses the existing lane-unlock drain pattern, with an
intrusive per-thread list reusing the already-unlinked pending's list link;
there is no fixed-capacity fallback or allocation. Recursive unlocks cannot
re-enter that list's drain. Terminal shutdown sweep references also retire the
orphan registration before release. This remains candidate evidence pending
the final stress series and full gates.

The completed pending-lifetime candidate passes the full remote behavior roster,
five deterministic lifetime modes under Valgrind and all three negative-control
builds. A fresh compiler and full native rebuild of the original source then
passed 12 consecutive strict-zero Valgrind executions with the same link-only
20 ms cancellation delay and post-shutdown inspection. Each exited 0, freed
all heap blocks and emitted no residue report. The final compiler SHA-256 is
`bd33e1ba9ba39f1b7cfb679f2a5061c15e034f23b4d7bdfd76ba05986d046cc5`;
the unwrapped binary is
`3dcc93ca195bc4d3cc56b56673b0bb9d6708b073853eda15462e07844fcab7f2`,
and the diagnostic relink is
`0b665a279b0430b7d49fea1c76649e7e666f00d79be15688c689b3613baa0c7f`.
All use the matching absolute greenup checkout for `SURGE_STDLIB` at build
and source-test time. Raw before/after logs and hashes are in the artifact
subdirectory above. DEBT-490 remains open for the unmodified source checks and
final D2 acceptance; DEBT-489's zero-capacity entered case remains failing.

Unmodified source confirmation also passes after the completed repair:
`PlacedBodyCancellationReclaimsFrame/publication` and `buffered-entered`, each
30 native runs across 1/2/8 shards and strict Valgrind. The state-handoff and
far-select initial-failure ownership static contracts pass, with the moved
final-release guard checked in its new reclamation file. The raw results are
`surge-placed-cancel-detached-unmodified.log` and `surge-pending-static.log`.

The first integration `make check` passed the compiler packages but failed
panicgate because the new registry-retirement assertion had no declared
coverage. `pending-listed-final-refused` now deliberately releases a still-linked
request in the native harness and requires the exact report. Its test is in
`TestRuntimeV2ReplyCancellationLifetime`, already selected by the lifecycle
gate. Panicgate records this concrete native proof as
`PG-REMOTE-REQUEST-RETIREMENT` (`other_lane`), rather than treating it as a
source-language panic or untested debt. The guard test and panicgate both pass.

### Remote checkpoint requested by the owner (2026-10-14)

The owner explicitly requested committing and pushing the current work to avoid
losing it. This checkpoint includes the whole accumulated D2 cutover on
`codex/step7-d2-greenup`; it is not landing acceptance. `make check` passes
(`surge-reply-make-check3.log`), including the full fast-lane compiler tests,
Go lint, C formatting/warnings and size checks. The final MIR lint cleanup
removes an unnecessary large range copy and an always-nil error result.
The Valgrind helper's argument-use annotation names its build-tagged callers.
A pre-commit review also removed a duplicated `SURGE_SHARDS` Python keyword
left by the topology replacement; every changed Python file compiles.

Current Sentrux root quality is 5312. No live session baseline was available;
the earlier direct comparison in this document recorded base 5311. This is a
recorded checkpoint metric, not a fresh before/after acceptance measurement.
`STATS.md` is regenerated for the checkpoint.

Remaining work is explicit: DEBT-489 local/remote selector rendezvous, DEBT-487
network owner immutability, remaining placed-body acceptance, and full D2
rosters plus two golden checks on the final candidate. Earlier all-green
results predate the latest root/placed-body changes. The new zero-capacity
selector regressions remain in the gate and are not waived. No merge into the
D2 or main refactoring branch is part of this checkpoint push.

The repository pre-commit hook additionally checks every staged C/header file.
Its first attempt found a header relying on the includer's typedefs, two
const-correctness hints in stands, a standard POSIX feature-test macro warning
and duplicate switch branches left by topology removal. The header now imports
its own declared types, the stands use const where they only inspect state,
the POSIX macro follows the existing scoped annotation pattern, and identical
owner-key routes share case labels while retaining the scope negative control.
The focused changed-C check passes for all five affected files. Commit hooks
remain enabled for the checkpoint.
