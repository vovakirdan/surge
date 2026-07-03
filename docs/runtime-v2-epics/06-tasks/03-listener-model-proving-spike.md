# Task 3: Listener Model Proving Spike

**Status:** Draft
**Kind:** proving spike
**Depends on:** Task 1, Task 2

## Context

The public Surge net API exposes one `TcpListener` handle backed by one
`NetListener{int fd; bool closed;}` (`rt_net.c:45-48`) and one accept loop
written by the user's own program. `RUNTIME_V2.md` Phase 3 wants
`SO_REUSEPORT` so each shard can own an accept socket and receive connections
directly (`docs/RUNTIME_V2.md` §2, "FD Ownership"), with a documented
fallback of "single acceptor plus explicit handoff" if that is not the ideal
first hot path. Neither shape currently exists: `rt_net_listen`
(`rt_net.c:413`) creates exactly one fd with `SO_REUSEADDR`
(`rt_net.c:435`).

This creates the central open problem the epic document names explicitly in
its Accept Ownership Contract: *"The one-user-accept-loop API conflict must be
resolved before implementation: Task 3 must decide where internal accept
tasks live and how a handler task lands on the accepted connection's owner
shard without new Surge syntax."* Spawn is shard-local by default
(`RUNTIME_V2.md` §9), and this epic explicitly forbids any new Surge syntax
(epic Not Included: "no Surge syntax changes"; "no parser, semantic-analysis,
async-lowering, or public example changes for crossing syntax"). So whatever
this spike proposes must place handler tasks on the correct owner shard using
only internal runtime mechanism — not a language feature.

This is a `RULES.md` Global Rule 1 proving spike: the hypothesis cannot be
settled by discussion alone, because it depends on how the existing single
opaque `TcpListener` handle actually behaves once multiple `NetListener`
instances/fds exist behind it, and how the current shard-0-only scheduler
would need to place a runtime-internal accept task before Task 6/7 give it a
real per-shard scheduler to place it on.

## Goal

Decide the first implementable listener model for Epic 6 and answer, with
runnable evidence, exactly how an internal accept task is represented and how
a handler task ends up running on the accepted connection's owner shard.

## Why This Task Exists

Every later implementation task in this epic (6 through 11) depends on this
decision. Task 6 needs to know whether one listener produces one fd per shard
or one fd with explicit handoff, because that changes what "owner shard"
means at listen time. Task 9 (accept distribution) literally implements
whatever this spike proves works. Doing this as code first, tests after
(Tasks 4/5) would violate the epic's own Refactor Safety Contract ("write or
select the behavior proof before moving code") and Global Rule 1's proving
spike discipline exists precisely to allow provisional code here without that
becoming silently permanent.

## Scope

Per `RULES.md` Global Rule 1, this spike must record all of the following
**before** any implementation:

- **Hypothesis**: state the specific listener model being tested (see
  Approach below for the two candidates to evaluate) and the specific
  mechanism for placing a handler task on its connection's owner shard.
- **Files/surfaces allowed to change**: name them explicitly (expect
  `rt_net.c`, a small scratch/spike-only C file or Go test file, not the
  fd registry or scheduler yet).
- **Behavior that is explicitly not final design**: e.g. hard-coded shard
  count, no cancellation/shutdown handling, no trace counters.
- **Proof**: a runnable test, benchmark, or native probe that produces
  observable evidence (e.g. N `SO_REUSEPORT` sockets bound to one port,
  connections distributed across them, and a runtime-internal task per fd
  observed running the connection's continuation).
- **Success/failure criteria**: stated in advance, not decided after seeing
  results.
- **Rollback/rewrite note**: what happens to the spike code if the hypothesis
  fails (delete it; do not promote a failed spike into Task 9).

Evaluate at least these two candidates and record findings for both, even if
one is chosen decisively:

1. **Per-shard `SO_REUSEPORT` listener group.** `rt_net_listen` (or an
   internal variant) opens `N` sockets, each with `SO_REUSEPORT` +
   `SO_REUSEADDR`, one per shard, bound to the same port. The public
   `TcpListener` handle becomes a thin wrapper around a listener-group
   object (owner metadata is Task 8's job; this spike only needs to prove
   the group can be created, each fd independently polled/accepted, and the
   kernel does distribute connections across the group members). Decide: is
   accept driven by a runtime-internal task per shard (one "system" task
   that loops accept and spawns handler tasks locally), or by the existing
   shard's net poller directly completing accept readiness into a spawn
   call? Either way, name exactly which code creates that internal
   accept-side task/callback and on which shard's scheduler it is placed.
2. **Documented explicit handoff fallback.** One accept socket (current
   behavior), one shard designated as the accepting shard; each accepted
   connection is explicitly reassigned to a target shard (round-robin or
   least-loaded) via the same "attach state to new shard" idea `RUNTIME_V2.md`
   §3 describes for migration, but only ever exercised once, at accept time,
   never mid-connection. State plainly whether this counts as "migration" in
   the `RUNTIME_V2.md` sense (the epic's Not Included list bars "migration
   control plane for moving an existing connection to another shard" — this
   spike must decide whether one-time placement at accept is that migration
   primitive or a distinct, allowed mechanism, and justify the answer).

For whichever model is chosen (or if a hybrid is chosen), answer explicitly:

- Does the public `TcpListener`/`TcpConn` ABI change in field layout, only in
  internal representation, or not at all? (Epic Scope requires "preserve
  public Surge syntax, public standard-library signatures, and native net ABI
  while changing internal ownership.")
- What does "no new Surge syntax" mean concretely here — i.e., confirm the
  user's existing accept-loop source code needs zero changes to run
  correctly under the new model, only the runtime underneath changes.
- Is skew at low connection counts (1, 8, 32 clients) expected under the
  chosen model, matching `RUNTIME_V2.md`'s Benchmark Plan note that
  `SO_REUSEPORT` "can be skewed at low connection counts"?

## Out Of Scope

- Full fd-registry integration (Task 9/11).
- Trace counters and benchmark rows (Task 12) beyond whatever ad hoc
  measurement the spike needs to prove its point.
- Static/behavior test suites (Tasks 4/5) — this spike may produce throwaway
  test code, but the durable contract tests are written after this task
  closes and can assert only what this spike actually proved.
- Listener-group close semantics in full (Task 8 implements; this spike only
  needs enough close behavior to clean up its own test).
- CI gating.

## Approach / Steps

1. Write the proving-spike record (hypothesis, files, non-final behavior,
   proof, success/failure criteria, rollback note) in
   `docs/runtime-v2-epics/06-listener-model-proving-spike.md` **before**
   touching code.
2. Prototype candidate 1 (`SO_REUSEPORT` group) in a scratch/throwaway path:
   confirm multiple sockets can bind the same port with
   `SO_REUSEPORT`+`SO_REUSEADDR`, confirm the kernel actually distributes a
   burst of client connections across them (this is empirical — do not
   assume it from documentation alone), and confirm a runtime-internal task
   per fd can be represented with the current task/executor primitives
   (even with a fake single-shard placement for now).
3. Prototype candidate 2 (explicit handoff) far enough to compare: one
   accept loop, explicit reassignment call, confirm it can move a connection
   struct's registry entry to a different shard's fd registry (a stub is
   fine since the real fd registry isn't shard-indexed yet).
4. Compare against the success/failure criteria written in step 1. Choose
   one model (or a documented hybrid) and write the decision with evidence,
   not intuition.
5. Answer the ABI/no-new-syntax/skew questions above explicitly in the
   decision document.
6. Delete or clearly quarantine spike-only code per the rollback note; if
   any spike code is kept as a real implementation seed, say so explicitly
   and note it will be rewritten to the Refactor Safety Contract's standard
   in Task 9 (owner-first APIs, explicit status codes — Global Rule 8).
7. Update `06-evidence.md` and `NOTES.md`, including the rejected path's
   evidence so it is not retried later (Global Rule 1: "must either accept
   the result as a design input, rewrite it into a rule-compliant
   implementation, or delete it").

## Files

Read:

- `docs/RUNTIME_V2.md` (§2 FD Ownership, §3 No Hot-Path Stealing, §9
  Structured Concurrency, Migration Plan Phase 3)
- `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md`
- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` (Task 2
  output)
- `runtime/native/rt_net.c` (listener/connection creation, accept path)
- `runtime/native/rt_async_task.c` (spawn/task creation primitives)

Touch (spike, quarantined per Global Rule 1):

- A scratch C probe or a `_test.go` file exercising `SO_REUSEPORT` directly
  (may live under `internal/vm/` behind a build tag, or as a standalone `.c`
  probe under `runtime/native/` if that is faster to iterate — name the
  exact location chosen in the spike record).

Create:

- `docs/runtime-v2-epics/06-listener-model-proving-spike.md`
- Update `docs/runtime-v2-epics/06-evidence.md`, `NOTES.md`

## Skills & Working Practice

- If a subagent runs this spike, it must still start with a plan-only pass
  per Global Rule 9, but the plan itself is the Rule-1 spike record — write
  it once, satisfying both rules together, rather than a generic plan
  followed by a separate spike record.
- This is empirical kernel-behavior work (does `SO_REUSEPORT` actually
  distribute connections the way the docs claim, on this machine's kernel).
  Do not accept a citation from `RUNTIME_V2.md`'s Sources section as proof —
  run it and observe.
- Keep the spike small and disposable. If prototyping candidate 1 takes more
  than roughly a day of iteration, stop and write down what is blocking it as
  a dead end rather than letting the spike creep into a real implementation
  without contract tests.
- Do not let this task quietly start Phase 4 work (cross-shard messaging) to
  make handler-task placement "easier." The whole point of the spike is
  proving placement is possible without that — if it is not possible without
  Phase 4 primitives, that is a valid (if unwelcome) spike result, and must
  be reported as such rather than worked around by pulling in Phase 4 scope.

## Checks

- `git diff --check`
- The spike's own proof command(s), whatever they are (record exact commands
  in the spike document)
- No `make check`/`make runtime-v2-check` requirement for throwaway spike
  code, but if any spike code is retained past this task, it must pass
  `make c-check` and `make cppcheck` before being merged into Task 9's
  starting point

## Definition Of Done

- [ ] The proving-spike record exists with all six required fields (Global
      Rule 1) filled in before implementation.
- [ ] Both candidate listener models have recorded findings, even if one was
      abandoned quickly.
- [ ] `SO_REUSEPORT` connection distribution is empirically observed on this
      machine, not assumed.
- [ ] The internal-accept-task representation and the handler-task
      owner-shard placement mechanism are both named concretely, with no
      remaining "TBD" on either question.
- [ ] The ABI-stability and no-new-syntax questions are answered explicitly.
- [ ] Expected `SO_REUSEPORT` skew at low connection counts is stated as an
      explicit expectation for Task 12's benchmark evidence to reference.
- [ ] Spike code is either deleted or explicitly named as a kept seed with a
      rewrite note for Task 9.
- [ ] Task 4, Task 5, and Task 6 have a decided listener model to write tests
      and structure against — no task past this one should re-litigate the
      choice.

## Evidence To Record

- `docs/runtime-v2-epics/06-listener-model-proving-spike.md`: hypothesis,
  files, non-final behavior, proof, success/failure criteria, rollback note,
  and the final decision with supporting evidence for both candidates.
- `06-evidence.md`: mark `Proving spike: yes` in Task Identity And Scope per
  `EVIDENCE_TEMPLATE.md`; fill the spike-specific fields there too.
- `NOTES.md`: the decision, and explicitly the rejected path with why it was
  rejected, so it is not rediscovered and retried in Task 9 or later.
