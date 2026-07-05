# Epic 8 Task 11: Net Fairness Starvation Investigation (RV2-DEBT-015)

## Status

Complete. `RV2-DEBT-015` = FIXED: mechanism pinned by this task (placement
funnel + single-consumer inject rotation), fix (F2 placement adoption at
join consume) folded into Task 7 and landed in `d998df20`, acceptance
re-verified in a granted quiet window (see the re-verification section).
Reproducer/validation harness promoted to `scripts/`.

## Task Identity And Scope

- Task: Epic 8 Task 11 per `08-tasks/README.md` and the epic boundary
  decision "Net fairness is investigated, not guessed"
  (`08-task-lifecycle-lane-and-net-fairness.md`).
- Debt: `RV2-DEBT-015` — 8-shard/1024-conn/100-req starvation with
  ~10.6-13.6s request tails healing on load drop; server socket `Recv-Q=1`
  while 7 pollers idle and ~29 ready tasks in inject (Epic 7 live evidence,
  `stallrepro.py`, not preserved in the repo).
- Resolution classes per the Performance Contract: **fixed** (probe
  completes without >10s tails on the reference host) or **constrained**
  (mechanism pinned to platform/subsystem with trace evidence, stable
  mitigation, updated debt owner).
- Baseline anchor: worktree branch `task11-net-fairness` at `27eeabd7`
  (last pre-lifecycle-migration state — the state DEBT-015 was observed
  against). Pinned for the whole investigation by main-session decision;
  no rebase while lifecycle Tasks 6-10 land.
- Constraint: runtime-only; no Phase 4 surfaces; lifecycle C files are
  owned by Tasks 6-10 — if the mechanism is lifecycle control traffic,
  stop and report (class (b)).

## Reference Host

- WSL2, kernel `6.18.33.2-microsoft-standard-WSL2`, 32 hardware threads,
  `ulimit -n` 1048576. Host load recorded per run (`uptime`); the main
  tree runs concurrent lifecycle builds/gates, so exploratory runs are
  labeled and cited evidence rows use requested quiet windows.

## Load Harness

- `scripts/bench_native_net.sh` probe shape (Task 1 continuity):
  `SURGE_NET_BENCH_SHARDS=8 SURGE_NET_BENCH_THREADS=8`
  `SURGE_NET_BENCH_CONNECTIONS=1024 SURGE_NET_BENCH_REQUESTS=100`
  `SURGE_NET_BENCH_MODES=direct SURGE_NET_BENCH_PATTERNS=seq`
  `SURGE_NET_BENCH_RUN_TIMEOUT=120s`. Client parallelism default 128
  threads; 10s socket timeouts define the >10s tail observation.
- Reconstructed `stallrepro.py` (scratchpad only, per plan approval —
  never committed): 1024 persistent connections, sustained time-bounded
  request loop (no think time by default), 30s socket timeouts, live
  STALL lines (>=1s) with timestamp/conn/local-port, stall flag file.
  Orchestrator `run_stallrepro.sh` starts the fixture server
  (`benchmarks/native/net_request_reply`, direct mode, 8 shards,
  `SURGE_TRACE_EXEC=1 SURGE_SCHED_TRACE=1`), a 250ms `ss -tni` watcher
  (Recv-Q/Send-Q counts), and on the first stall sends `SIGUSR1` (live
  `TRACE_EXEC` dump per `rt_async_trace.c`) plus full `ss`/proc
  snapshots mid-stall.

## Phase 1: Reproduction Log

Every run below is EXPLORATORY (concurrent lifecycle work in the main
tree) unless marked as quiet-window evidence.

### R0 — build

- `make build` in the worktree; binary embeds `27eeabd798a5`. Fixture
  built once via `surge build --release benchmarks/native/net_request_reply`.

### R1 — exact Task 1 probe x5 (bench script)

Host load at starts: 1.98, 2.60, 3.09, 3.33, 3.44 (1-min avg; rising due
to own runs + background).

| run | total us | avg us/op | p50 us | p95 us | >10s tails |
| ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 20005728 | 24151.94 | 177.30 | 335.84 | none |
| 2 | 19857493 | 23852.42 | 174.01 | 329.39 | none |
| 3 | 19798384 | 23708.51 | 174.84 | 329.62 | none |
| 4 | 19747874 | 23701.64 | 173.82 | 330.52 | none |
| 5 | 19978113 | 24026.14 | 176.18 | 335.01 | none |

5/5 clean, 102400/102400 requests each. Matches the Task 1 single-run
observation; the stall does not reproduce at the exact Task 1 shape on
the current baseline.

Trace side-note (not a stall): `fd_ready_batches_0` is consistently ~100
while shards 1-7 sit at ~400-570 — shard 0's poll pass runs materially
less often than the other shards' (io-thread interaction); noted for
Phase 2.

### R2 — reconstructed stallrepro sustained load (r2a-burst-90s)

- Shape: 1024 persistent conns, burst connect (0.03s), 90s sustained, one
  outstanding request per conn, 1024 client threads, 30s socket timeouts.
  Host load 2.87 at start, 3.91 at end. 8 shards / 8 threads, direct mode.
- Result: 544114 requests, 0 errors, **45762 requests (8.4%) in a discrete
  ~1.0-1.5s tail band** — p50 143.7us, p95 1007567us, p99 1070596us,
  p99.9 1237085us, max 1558195us. Zero >2s, zero >10s. Stalls arrive
  continuously (~500/s), synchronized across conns (whole cohorts stall
  and heal together).
- Kernel side (250ms `ss -tni` watcher): steady state `estab=1024
  recvq_pos=1023` — nearly every server-side socket holds unread request
  bytes; individual sockets show `lastrcv` >1s with `Recv-Q=1` mid-stall.
  This is the DEBT-015 `Recv-Q=1` signature, at scale.

### R3 — per-thread CPU split (r3-cpu-8shard / r3-cpu-1shard, 30s runs)

- 8 shards: over a 15s mid-load window, ONE worker thread burned 1577
  jiffies (~105% of a core); the other seven workers burned 9-12 jiffies
  each (<1%, poll ticks only). io/main threads 131/39.
- 1 shard, same load: 246975 requests in 30s (8232 req/s), **zero**
  stalls >=1s, max 334ms, smooth queueing distribution (p50 111ms).
- 8 shards, same load: 203093 requests (6770 req/s, 18% slower than
  1-shard) with 11698 stalls in the ~1s band.

Phase 1 verdict: the original >10s class does not reproduce at this
baseline, but a 100%-reproducible fairness defect specific to multi-shard
runs was established (the ~1s band), with the same kernel-side signature
as DEBT-015.

## Phase 2: Instrumentation (SIGUSR1 mid-stall dumps + counters)

Mid-stall `SIGUSR1` dumps (two, ~1s apart) during r2a:

- `TRACE_EXEC_SNAPSHOT`: `inject_len=1023 local_total=0 running=0`,
  `waiters=2049` all `waiters_join`, `waiters_net=0`,
  `tasks_ready=1024 tasks_waiting=2049` (3073 user tasks).
- Every `TRACE_TASK_WAITING`/`TRACE_TASK_READY` line — all 3073 tasks —
  reports **`owner=0`**. `TRACE_STORE shard=0 len=2049`, shards 1-7 `len=0`.
- `TRACE_NET_SHARDS`: accepts distributed (`accept_0..7` = 121-136), fd
  ready batches distributed (shards 1-7 ~11-13k each at exit).
- `SCHED_TRACE` at exit: `local=98312 inject=96260 steal=0
  conn_owner_placed=1030 conn_owner_local=2 conn_owner_mismatch=0`.
- `TRACE_EXEC` at exit: `cross_shard_wakes=84961` of `wake_called=194640`;
  `ctrl_scope=6001641` for 544k requests (11/req, the RV2-DEBT-016
  amplifier, Task 9's domain); `parked_with_work=0`.

### Mechanism

1. **Placement funnel.** `stdlib/net/net.sg` wraps every net operation in
   an async child task (`accept_owned`, `read_some_owned`,
   `write_all_owned` — `net.accept()` returns `Task<...>`). The accept
   transition (`rt_net.c:516` fast path via
   `rt_net_place_current_task_on_owner`; `rt_async_waiter.c:381` parked
   path) re-places the **current task**, which is the ephemeral stdlib
   wrapper child. The child completes immediately; the placement dies with
   it. The durable tasks (`serve_many`, hence every spawned `serve_conn`)
   keep owner shard 0 by inheritance (`rt_task_inherit_placement` /
   `rt_task_assign_spawn_owner`). Result: fd ownership is distributed
   across shards, task execution is not — the entire request workload
   executes on shard 0's single worker. `owner_replacements`/
   `conn_owner_placed` (~1030) are misleading: `rt_trace_owner_replaced()`
   counts calls, and every call lands on a wrapper task about to die
   (`conn_owner_local=2` for the whole run is the tell).
2. **Inject rotation tail.** Given the funnel, every parked read's
   completion (found by shard 1-7 poll passes) is a cross-shard wake that
   must land in shard 0's inject FIFO (a non-owner thread cannot push to
   another shard's local queue). Single consumer, drain ~1070/s in r2a;
   backlog ≈ number of concurrently parked conns (1023). Sojourn =
   1023/1070 ≈ 0.96s — matching the observed 1.0-1.2s band. The band is a
   fair FIFO rotation (no front-push sites into inject), so it scales
   with concurrent parked conns × per-request service cost; it is not
   per-conn starvation at this load shape.
3. **The original >10s class** requires the funnel topology plus a stall
   of the single consumer (host preemption under load; possibly the
   Epic 8 Task 1 lost-wake, though that fix was channel-key-specific and
   the direct-mode fixture uses no channels — attribution unproven).
   DEBT-015's live snapshot ("7 pollers idle, ~29 ready tasks in inject,
   Recv-Q=1, heals on load drop") is this topology at lighter load.

This also re-attributes part of `RV2-DEBT-016`: the 8-shard/1024 row runs
1.3x the 1-shard row primarily because the 8-shard row is a shard-0
execution with added cross-shard wake/inject overhead — not only because
of control-lane traffic.

## Phase 3: Conclusion

Pending main-session decision on fix ownership (checkpoint sent
2026-07-04): mechanism class (a) — net/scheduler placement design — with
candidate fixes:

- F1 (net lane): generalize the accept transition to read/write wait
  registration (re-place the parking task onto the fd owner shard).
  Partial: read children migrate, join wakes still funnel to shard 0.
- F2 (durable): placement propagation at join consume — a parent
  consuming a DONE child with `TASK_PLACEMENT_CONNECTION` adopts the
  child's placement. Makes `serve_many` adopt the accepting shard, so
  `serve_conn` spawns inherit it and the whole connection pipeline runs
  shard-local. Lives in `rt_async_task.c` (Epic 8 Task 7 write set) —
  ownership to be decided by the main session.
- F3 (palliative): inject aging/alternation in `worker_next_ready`
  (`rt_async_state.c`, contested) — bounds the band without fixing
  scaling.

Main-session decisions (2026-07-04): F2 folds into Task 7 (lifecycle
write set; spec below is the handoff). F1 is contingency only. F3
rejected. Task 11 stays open until F2 lands; re-verification happens on
the post-Task-7 tree (approved exception to the baseline pin).

### Attribution Experiment (pre-828462a3 vs 27eeabd7)

Between the Epic 7 closeout (`072bbde0`) and this task's baseline
(`27eeabd7`), the only runtime C change is `828462a3` (Task 1 lost-wake
fix + compat-lane extraction). Fresh clone built at `072bbde0`
(exploratory runs, host load 1.2-2.7):

| probe | pre-fix (072bbde0) | baseline (27eeabd7) |
| --- | --- | --- |
| bench 8x1024x100 x3/x5 | 3/3 clean, p95 322-326us, no >10s tails | 5/5 clean, p95 329-336us |
| sustained 1024-conn 45/90s | 16151/305119 stalls (5.3%), p95 1.000s, max 1.07s | 45762/544114 (8.4%), p95 1.008s, max 1.56s |

Verdict: the ~1s band is structural at both commits (the funnel + inject
rotation predates Epic 8). The >10s class reproduces at NEITHER commit
under today's host conditions, so it cannot be attributed to the Task 1
lost-wake fix. Supported reading: the DEBT-015 >10s tails were the same
rotation mechanism stretched by host load (WSL2 contention slowing the
single consumer), i.e. a load-coupled expression of the topology, healing
when load drops. The mechanism is now deterministically reproducible in
miniature (the ~1s band) without host load.

### RV2-DEBT-016 Reinterpretation (flag for the whole epic)

Every 8-shard/1024 number in this epic (including the Task 1 baseline
2.011s row, the ~26 control-acquisitions/request, and Task 5's per-site
census) was measured on a topology where ALL user tasks execute on shard
0's single worker. The 8v1 shard ratio is primarily the placement funnel
plus cross-shard wake/inject overhead, not control-lane contention alone
(a single worker cannot contend with itself; control cost is pure
overhead, not serialization, in that row). After F2, Tasks 7-10 and
Task 12 must re-baseline: per-request control cost will be paid by 8
workers contending, so `control_lock_acquired`/request may matter MORE,
not less, and comparisons against pre-F2 rows are apples-to-oranges.

## F2 Design Spec (handoff to Task 7)

**Semantics.** Connection placement propagates upward at join consume: when
a task consumes the result of a DONE child that carries
`TASK_PLACEMENT_CONNECTION`, the consuming task adopts the child's
placement (owner shard + class) before releasing the child. Rationale: the
stdlib wraps every net operation in an ephemeral child task, so the accept
transition's placement dies with the wrapper; propagation at consume is
the only point where the runtime sees both the placement carrier and the
durable task that will spawn the connection's pipeline.

**Insertion points (at `27eeabd7` line numbers; move with the consume
point if Task 7 relocates join polling).** Both DONE-consume branches of
`rt_task_poll` (`rt_async_task.c:119-126` and `:136-147`), immediately
before `task_release(ex, target)` (the release may free the child; read
placement fields first):

```c
if (current != NULL && target->placement_class == TASK_PLACEMENT_CONNECTION &&
    target->owner_shard_valid != 0 &&
    (current->placement_class != TASK_PLACEMENT_CONNECTION ||
     current->owner_shard_id != target->owner_shard_id)) {
    rt_task_replace_owner(ex, current, target->owner_shard_id,
                          TASK_PLACEMENT_CONNECTION);
}
```

Do NOT add adoption to `rt_task_await` (external/main-thread compat: no
worker placement semantics) or to the select slow lane (named non-goal;
note as follow-up if HTTP-on-select ever needs it).

**Why it is safe.**
- `rt_task_poll` holds the control lane at both branches;
  `rt_task_replace_owner` is the existing accept-transition primitive
  (control lane, migrates the moved task's join waiters source-then-
  destination). Lane order control -> one shard lock is preserved.
- `current` is RUNNING on this thread. Owner writes to a RUNNING task
  already happen only from its own polling thread
  (`rt_net_place_current_task_on_owner` during `rt_net_accept`); the
  parked-task writes (`rt_async_waiter.c:381`,
  `rt_net_accept_group.c:101`) target WAITING tasks. Adoption keeps that
  invariant: RUNNING-task owner writes stay thread-local.
- Worker `running_count` bookkeeping is tracked on the polling worker's
  shard (`rt_worker_turn.c:175,184`) or on a captured shard
  (`poll_ready_child_inline`, `rt_run_ready_one_nowait_locked`), so an
  owner change mid-poll cannot unbalance counts.
- After adoption, the current task finishes its poll on the old shard's
  worker; the next park/yield/ready-push routes to the new owner via
  `rt_task_owner_shard`. This is exactly the accept-transition behavior
  today.
- `SURGE_SHARDS=1`: guard makes it a no-op (owner always 0).

**Interaction with Task 6 (owner-lane create).** Spawn-inherit reads the
parent's `owner_shard_id` on the creating thread while the parent is
RUNNING; with the invariant above that read is thread-stable without the
control lane. Adoption BETWEEN two spawns is the intended effect: after
`serve_many` consumes accept child N (adopting shard X), the very next
`@local spawn serve_conn` must inherit X. Task 6's create path must
therefore read the parent's placement at create time (not a cached value
from task entry) — flag this in Task 6/7 review if create caches parent
placement.

**Expected system effect (measured prediction).** `serve_many` adopts the
accepting shard per accept; each `serve_conn` inherits it; read/write
wrapper children inherit it; net waits then register on the shard that
owns both the fd and the task, so poll-pass completions become same-shard
wakes (worker-local queue, not inject). Steady state: inject ~empty,
cross_shard_wakes collapse to ~accept traffic, all 8 workers busy.

**Adoption frequency bound.** Adoption fires only when placement differs:
once per accept for `serve_many` (O(connections)) and once per
`serve_conn` bootstrap; zero in per-request steady state (child and parent
already share shard+class). If Task 7 peels join-consume off the control
lane, the adoption branch may keep a control fallback without violating
the per-request steady-state contract — it is O(conns), not O(requests).

**HARD CONSTRAINT (control hold vs the children[]-append visibility
chain).** The safety argument above relies on `rt_task_poll` holding the
control lane at both adoption points TODAY — and that same control hold is
what makes Task 6's children[]-append protocol safe across a parent's
self-adoption: the child append under the old owner's lock happens-before
the adoption on the parent's own thread, and cancellation/scope-failfast
observes the child list through the control chain. Task 7 will migrate
join-consume off the control lane, so its commit MUST satisfy exactly one
of the following — there is no third option:

1. The adoption branch keeps an explicit control fallback: the migrated
   consume fast path takes the control lane only when the adoption guard
   fires. This is expressly permitted by the frequency bound above
   (adoption is O(connections), never per-request steady state) and does
   not violate the Epic 8 steady-state contract; count it under a named
   site (`placement_adoptions` or an existing `ctrl_*` site), not as
   anonymous control traffic.
2. Task 7 re-derives the children[]-append visibility chain without
   control: an explicit, recorded happens-before argument proving that
   (a) a parent's owner change at consume cannot race a concurrent
   `cancel_task`/scope-failfast walk of that parent's `children[]`, and
   (b) a child appended under the OLD owner shard's lock is visible to
   any thread that observes the parent's NEW owner. The argument must be
   written into the Task 7 evidence and reviewed against the Task 3
   spike decisions before the peel commit lands.

A Task 7 commit that migrates join-consume without doing (1) or recording
(2) is out of contract for this handoff.

**Proof obligations for the Task 7 commit.**
- Trace: add `placement_adoptions` to `TRACE_EXEC` (or reuse
  `owner_replacements` delta) so bench rows report it.
- SIGUSR1 mid-load dump: `TRACE_TASK_*` owner histogram spread across
  shards (today: 3073/3073 owner=0); `TRACE_EXEC_SNAPSHOT inject_len`
  ~0 steady (today: 1023); `TRACE_STORE` waiter entries distributed
  (today: all 2049 in shard 0).
- Per-thread CPU under sustained load: max/min worker jiffies ratio
  drops from ~150x to single digits.
- Acceptance probes: (1) reconstructed stallrepro 8x1024 sustained 90s —
  zero >=1s stalls AND 8-shard throughput >= the 1-shard row (today:
  8.4% stalls and 18% slower); (2) bench 8x1024x100 x5 clean;
  (3) `runtime-v2-check` + lock-check + 9-mode behavior suite at shards
  1 and 3 green.

## F1 Contingency Spec (only if F2 re-verification shows a residual band)

Generalize the accept transition to read/write wait registration: when a
task registers a net read/write waiter for an fd whose registry owner is
shard X and the task's owner differs, re-place the parking task onto X
(same `rt_task_replace_owner` call, `TASK_PLACEMENT_CONNECTION`), at the
registration point in the net wait path (`rt_async_waiter.c` /
`rt_net_lifecycle.c` — Task 11's lane). The task is WAITING-bound (about
to park), which is the proven accept-transition class.

Effect: net-woken wrapper children run on the fd's shard, halving inject
pressure and making read completion locality independent of the parent's
placement. It does not fix the parent (join wakes still cross to the
parent's shard), so it is a band-reducer, not a scaling fix — strictly a
fallback if F2 leaves residue.

## Post-Task-7 Re-Verification (QUIET-WINDOW EVIDENCE, tree `d998df20`)

F2 landed in `d998df20` as `rt_task_poll_adopt_placement`
(`rt_async_task.c:262`, called at both DONE-consume branches :210/:231),
taking hard-constraint arm (1): control fallback gated on
`!rt_lane_holds_control()`, counted under `RT_CTRL_SITE_JOIN_POLL`, with a
`placement_adoptions` trace counter. Worktree fast-forwarded 27eeabd7 ->
d998df20 (no local commits); binary embeds `d998df20d513`.

Environment note: the host REBOOTED between the pre-F2 runs and this
window (uptime ~30 min at run time, kernel unchanged). The scratchpad
harness was recreated byte-identical from the recorded content.
Cross-reboot comparisons are therefore about shapes and stall counts, not
microsecond-exact latencies. All runs below are quiet-window evidence
(grant from the main session; only this task's own client load present —
1-min load peaked at 24 during A3 from the 1024-thread client itself).

### Acceptance results

| probe | pre-F2 (27eeabd7, old boot) | post-F2 (d998df20, quiet window) | verdict |
| --- | --- | --- | --- |
| A3 sustained 90s 8x1024: stalls >=1s | 45762 (8.4%) | **0** (max 969ms, p999 546ms) | PASS |
| A3 sustained throughput | 6045 req/s | 8550 req/s (+41%) | PASS |
| A1/A2 sustained 30s: 8-shard vs 1-shard req/s | 6770 vs 8232 (0.82x) | 8796 vs 7822 (**1.12x**) | PASS (8 >= 1) |
| A1 per-worker CPU (15s jiffies) | 1577 / 9-12 (ratio ~150x) | 331-651 all busy (ratio ~2.0) | PASS |
| A4 bench 8x1024x100 x5 | 5/5 clean (old boot) | 5/5 clean, totals 16.91-17.31s | PASS (no >10s tails) |
| A5 bench 1-shard comparator x2 | 15.5s (old boot) | 17.07/17.30s | 8-shard avg 17.08s <= 1-shard avg 17.19s |
| mid-load owner histogram | 3073/3073 owner=0 | 456/353/320/365/377/401/407/386 across shards 0-7 | PASS |
| mid-load inject_len / stores | 1023 / all 2049 waiters in shard 0 | **0** / stores 321-458 per shard, waiters_net=1016 distributed | PASS |
| cross_shard_wakes (exit) | 84961 | 2017 | collapsed to adoption edges |
| placement_adoptions / ctrl_join_poll (exit) | n/a | 2017 / 2017 (~2 per connection: accept bootstrap + teardown join walk) | O(connections) bound holds; join-poll control traffic is adoption-only |

Bench latency shape note: post-F2 the 1024-conn bench rows are unimodal
(p50 ~20.4ms ~= 128 outstanding x ~160us service) in BOTH 1-shard and
8-shard configs — fair round-robin service replacing the pre-F2 streak
bimodality (p50 175us / heavy tail). Totals are the comparable metric.

Residual observations (not Task 11 acceptance items):
- The 1-shard bench total moved 15.5s (old boot) -> 17.2s (new boot,
  d998df20); host reboot and Task 6/7 overhead are confounded. Task 12's
  re-baseline owns separating that.
- Sustained scaling is now client-bound: the Python 1024-thread client
  saturates ~8.5k req/s while server workers sit at ~40% CPU each.
  Task 12 needs a stronger load generator for true scaling rows.
- control_lock_acquired is 18.3/request on the sustained run — the
  remaining consumer is scope traffic (Task 9), unchanged by this task.

### RV2-DEBT-015 final state: FIXED (proposed)

Per the Performance Contract "fixed" arm: the 8-shard/1024-conn/100-req
probe completes without >10s tails (5/5 bench runs; 90s sustained run
with zero >=1s stalls at full 1024-way concurrency), with the mechanism
documented (placement funnel + single-consumer inject rotation; the
historical >10s tails were the same mechanism stretched by host load)
and the fix landed as F2 in `d998df20`. Proposed DEBT.md close entry:
close RV2-DEBT-015 into Closed Debt citing this document, `d998df20`,
and the acceptance table above; add a residual note to RV2-DEBT-016
that pre-F2 8x1024 rows are not comparable to post-F2 rows.

## Landing Plan (main-session approved)

- Harness promotion (approved 2026-07-04, landed): `stallrepro.py`, the
  orchestrator (`run_stallrepro.sh`), and `cpu_validate.sh` are promoted
  from the scratchpad into `scripts/` as part of this task's landing
  commit. The client is verbatim; the two shell scripts were adapted for
  repo residence (repo-relative root, `STALL_OUT` output dir defaulting
  under `build/benchmarks/stallrepro`, missing-binary hint) with
  behavior otherwise identical. Unlike the bench script's outer-timeout
  wrappers, the harness owns its per-probe timeouts (per-socket
  timeouts, time-bounded duration, per-run watcher lifecycle), which
  addresses the RV2-DEBT-006-style timeout-ownership concern for the
  net side; noted in the DEBT-006 entry.
- Docs (this file, the `08-evidence.md` Task 11 section, `NOTES.md`, the
  `DEBT.md` RV2-DEBT-015 update) stay uncommitted in the Task 11
  worktree until the main session schedules the landing, because Tasks
  6-10 are rewriting the same documents on the branch.
- Re-verification happens on the post-Task-7 tree (fresh worktree or
  rebase, main-session choice at that time): rerun the acceptance probes
  above, produce before/after trace rows, then finalize the RV2-DEBT-015
  state (fixed if acceptance passes; otherwise constrained with F1 as
  the recorded next step).
