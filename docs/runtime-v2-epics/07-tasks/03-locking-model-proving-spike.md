# Epic 7 Task 3: Locking-Model Proving Spike

**Kind:** proving spike. **Depends on:** Tasks 1, 2.

**Goal:** fix the locking model before tests and implementation: answer
every *(spike)* mark in `07-executor-lock-dependency-map.md` and prove the
riskiest mechanism — no lost wakeups under split locks with
collect-then-wake — with a runnable model.

## Deliverable

`docs/runtime-v2-epics/07-locking-model-proving-spike.md` with:

- the Global Rule 1 spike record (hypothesis, allowed surfaces, non-final
  behavior, proof, success/failure criteria, rollback);
- decisions D1-D16 covering: shard lock + two condvars per shard; lock
  order and its debug assertion; task deref/free/owner-stability rules;
  control-lane accept re-placement; the park/wake protocol with owner-hinted
  entries and collect-then-wake; ready-queue and worker-loop lanes; atomic
  virtual clock with per-shard sleep stores and last-idle-worker advance;
  control-lane scope keys; blocking completion lane; gated `done_cv`;
  `mark_done` shard-phase/control-epilogue split; control-serialized
  multi-key paths (`select`/timeout); non-user polls under the shard lock;
  sync-channel compat and compensation lanes; the io thread's N=1 and N>1
  roles; spawn/cancel/await/runner/shutdown/trace lanes;
- rejected alternatives recorded so they are not retried (single cv per
  shard, owner-only free, shard-lock owner chasing, foreign-entry deref
  under store locks, per-shard clocks);
- the proof: a standalone C model of the protocol (4 shards, 32 tasks,
  20000 cycles each, 3 cross-shard wake threads), run under ThreadSanitizer
  and optimized builds, source inlined verbatim.

## Proof Results

5/5 runs pass (4 TSan `-O1 -g`, 2 plain `-O2`): zero TSan reports, no hangs,
`total_wakes=639968` equals the exact park count (every registered waiter
woken exactly once — nothing lost, nothing double-consumed),
spurious parks 0.04-0.08% of wakes.

## Out Of Scope

- No repository code changes; the prototype is not repository code.
- Test writing (Tasks 4-5) and migration (Tasks 6-11) consume these
  decisions.

## Checks

- Prototype: `clang -O1 -g -fsanitize=thread` x4 runs, `clang -O2 -DNDEBUG`
  x2 runs, all PASS.
- Docs: `git diff --check`.

## Success Criteria

- Every *(spike)* mark in the dependency map has a numbered decision.
- The proof record satisfies Global Rule 1.
- Evidence ledger and `NOTES.md` updated; index status flipped.
