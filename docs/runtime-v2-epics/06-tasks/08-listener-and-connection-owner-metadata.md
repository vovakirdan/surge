# Task 8: Listener And Connection Owner Metadata

**Status:** Complete
**Kind:** runtime code
**Depends on:** Task 6

## Context

Today, `runtime/native/rt_net.c:45-53`:

```c
typedef struct NetListener {
    int fd;
    bool closed;
} NetListener;

typedef struct NetConn {
    int fd;
    bool closed;
} NetConn;
```

Two fields each, no owner-shard tag, no listener-group concept, no
generation. `RV2-DEBT-010` (`DEBT.md`) already records that copied
`TcpConn`/`TcpListener` handles carry only the raw fd view and are not
generation-aware — this task's changes to these structs are the natural
place to close that debt if Task 3's chosen listener model requires a
generation anyway, but closing it is optional per the epic's Accepted
Baseline Debt section: *"`RV2-DEBT-010` is in scope only if a task changes
listener or connection handle representation. If the epic leaves copied
handle generation open, the closeout must say why and keep the debt owner
explicit."* Since this task does change the representation, make a deliberate
decision here rather than an accidental one, and record it either way.

The epic's Accept Ownership Contract requires:

- "A listener object knows whether it is a single-fd listener, a per-shard
  listener group, or an explicit fallback handoff listener."
- "A per-shard listener group is closed as one logical listener handle.
  Closing it closes every fd in the group and wakes or cancels waiters on
  every owning shard. Linux may drop connections sitting in a closed
  `SO_REUSEPORT` socket's accept queue; Epic 6 must record this as expected
  OS behavior rather than promising those queued connections survive close."
- "Each accepted connection has one owner shard at creation time."

Whichever model Task 3's spike proved out determines the exact shape here:
a `SO_REUSEPORT` group needs an array of `(fd, shard_id)` pairs behind one
logical `NetListener` handle; an explicit handoff fallback needs one fd plus
a distribution/placement record.

## Goal

Attach shard-owner and lifecycle metadata to listener and connection runtime
objects, including listener-group close semantics, while keeping the public
`TcpListener`/`TcpConn` ABI stable.

## Why This Task Exists

Task 9 (accept distribution) cannot record "which shard owns this
connection" anywhere without this task first defining where that fact lives.
Task 11 (net lifecycle migration) needs the same metadata to route close/
cancel/shutdown to the right shard's registry. This task is the ABI-facing
half of "owner metadata"; Task 7 was the scheduler-facing half (task
placement) — they must agree on how a task finds its connection's owner
shard (see Task 7's Skills note on reconciling this).

## Scope

- Extend `NetListener` to carry: a discriminator for single-fd / per-shard
  group / explicit-fallback (per Task 3's decision), and either one fd or an
  array of `(fd, shard_id)` pairs as appropriate.
- Extend `NetConn` to carry an owner `shard_id` (or an index into the
  runtime's shard array) set at creation time and never changed afterward in
  this epic (no migration).
- Decide and implement the generation question for `RV2-DEBT-010`: either
  add a generation field to `NetConn`/`NetListener` and make copied-handle
  operations validate it before issuing a direct fd operation (closing the
  debt), or explicitly decline to and record why in `06-evidence.md` and
  `DEBT.md` (keeping the debt open with its existing owner, or reassigning
  it to this epic's closeout if it remains unresolved).
- Implement listener-group close: closing the public handle must close
  every member fd and drive cancellation/wake for waiters on every owning
  shard that group touches — not just the shard the close call happens to
  run on.
- Preserve the public Surge-visible `TcpListener`/`TcpConn` API surface and
  the native net ABI boundary exactly (epic Scope: "preserve public Surge
  syntax, public standard-library signatures, and native net ABI while
  changing internal ownership") — only the internal C struct layout changes.
- Use owner-first C APIs and explicit status codes for any new lifecycle
  function this task adds (`RULES.md` Global Rule 8): e.g. a
  `rt_net_listener_group_close(NetListener* listener)` that returns a status,
  not `panic_msg`, for a partial-close failure.

## Out Of Scope

- Actually distributing accepted connections across shards (Task 9) — this
  task only defines where the "which shard owns this" fact is stored and how
  a listener group closes; Task 9 populates it for real.
- Per-shard poller/wake mechanism (Task 10).
- Migrating an existing connection's owner shard — never in scope for this
  epic.

## Approach / Steps

1. Confirm Task 3's chosen listener model and Task 6's real shard array are
   both available.
2. Extend `NetListener`/`NetConn` with the owner/discriminator/generation
   fields decided above; keep the change additive where possible to
   minimize churn in `rt_net.c` call sites (904 lines already, near the
   `.loc-legacy-allowlist` ceiling — do not let this task push it further
   over without recording why in `06-evidence.md` per `RULES.md` Global
   Rule 4).
3. Implement listener-group close, iterating every member fd and every
   shard the group touches; write the OS-behavior note about dropped queued
   connections directly into the Accept Ownership Contract evidence, not
   just in a comment.
4. Decide and implement (or explicitly decline) the `RV2-DEBT-010` closure;
   update `DEBT.md` accordingly either way.
5. Update Task 4's pending contract tests for owner-metadata-visible
   properties and Task 5's static gates if the new struct fields are
   part of what those gates check.
6. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch:

- `runtime/native/rt_net.c` (`NetListener`, `NetConn` struct definitions and
  every call site that constructs or closes them)

Read:

- `docs/runtime-v2-epics/06-listener-model-proving-spike.md` (Task 3)
- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` (Task 2, for
  the `NetListener`/`NetConn` → public handle flow)
- `docs/runtime-v2-epics/DEBT.md` (`RV2-DEBT-010`)
- `docs/RUNTIME_V2.md` §2 (FD Ownership)

## Skills & Working Practice

- Full Global Rule 9 plan gate: state the exact struct field additions, the
  `RV2-DEBT-010` decision, and the listener-group close algorithm before
  writing code.
- Coordinate with Task 7 on how a task looks up its connection's owner shard
  — this is the one place the two tasks' scopes must agree, even if they
  proceed on separate write sets.
- Every touched line in `rt_net.c` counts against Global Rule 4 and
  `RV2-DEBT-004`; record the before/after line count explicitly, and prefer
  extending existing helper functions over adding new ones if it keeps the
  file flatter, per the Refactor Safety Contract's "reduce or keep flat every
  touched over-limit file" requirement.
- New lifecycle functions must follow Global Rule 8's owner-first,
  explicit-status shape; do not add a bare `bool` return where the caller
  needs to distinguish failure causes (e.g. partial-close failure vs.
  invalid listener state).

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- `go test ./internal/vm -run 'TestRuntimeV2Accept'` (owner-metadata-visible
  pending cases, if any now pass)
- `git diff --check`
- Sentrux root and scoped scans
- Line count for `runtime/native/rt_net.c` recorded before/after

## Definition Of Done

- [x] `NetListener`/`NetConn` carry owner-shard metadata and the
      single-fd/group/fallback discriminator matching Task 3's decision.
- [x] Listener-group close has an owner-first lifecycle helper that iterates
      every represented member fd. Public `rt_net_listen` keeps one live member
      until Task 9/10/11 add group wait/accept and owner-local wake routing;
      the OS queued-connection-drop behavior is recorded as expected, not
      promised otherwise.
- [x] `RV2-DEBT-010` is either closed with evidence or explicitly kept open
      with its owner and reason recorded in `DEBT.md`.
- [x] Public `TcpListener`/`TcpConn` Surge-visible API and native net ABI are
      unchanged.
- [x] New lifecycle functions follow the owner-first, explicit-status
      contract (Global Rule 8).
- [x] `rt_net.c` line-count impact is recorded; the file is not made larger
      without an explicit justification.

## Evidence To Record

- `06-evidence.md`: Files Touched with line-count deltas, Contracts Touched
  (listener-group close, owner metadata, `RV2-DEBT-010` decision),
  Commands/Checks.
- `DEBT.md`: updated `RV2-DEBT-010` status either way.
- `NOTES.md`: the owner-shard lookup contract agreed with Task 7, and any
  rejected representation (e.g. if a generation field was considered and
  dropped).
