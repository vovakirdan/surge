# Epic 8 Task 12: Performance Benchmark And CI Gate

## Status

Complete. Post-F2 re-baseline recorded as the epic's performance record
(replacing the pre-F2 8x1024 rows, which measured single-worker
placement-funnel execution and are not comparable). A deterministic
trace-counter performance gate (`TestRuntimeV2PerfControlLaneGate`) is wired
into `make runtime-v2-check` via the new `runtime-v2-perf-check` stage. The
Epic 8 Performance Contract is satisfied at HEAD `8c89f358`; `RV2-DEBT-016` is
closed with the re-baseline evidence.

## Task Identity And Scope

- Task: Epic 8 Task 12 per `08-tasks/README.md` and the epic's Performance
  Contract (`08-task-lifecycle-lane-and-net-fairness.md`). This task exists to
  prove the contract and wire it into CI.
- Baseline anchor: HEAD `8c89f358` (Task 10 review fixes; Tasks 1-11 complete).
  Fresh matching-commit build of both `surge` and the net fixture
  (`bench_native_net.sh` enforces the commit-match check; the pre-existing
  binary was stale at `8c4b16f9`).
- Reference host: WSL2, kernel `6.18.33.2-microsoft-standard-WSL2`, 32 hardware
  threads, `ulimit -n` 1048576. Host load (`uptime`) and UTC timestamps stamped
  per run by the harness scripts.
- In scope: the post-F2 net re-baseline matrix, a channels reference refresh,
  the sustained-stall / CPU-distribution acceptance re-verification, the
  per-commit CI gate, and the `RV2-DEBT-016` final-state decision.
- Out of scope (no code behavior change): no runtime C change (the gate reuses
  existing trace counters); no new counters; no select / net-handle / placement
  work; the cross-owner `ctrl_scope` residual and the external-await
  `ctrl_await_compat` artifact are NOT chased (owned elsewhere, see DEBT); no
  Task 13/14 work.
- Proving spike: no.

## Known Inputs Respected (from `08-evidence.md` Task 12 Inputs)

1. **Pre-F2 rows are not comparable.** Every pre-F2 8x1024 row (Epic 6/7
   closeouts, Epic 8 Task 1/5 baselines) was measured on the placement-funnel
   topology where ALL user tasks executed on shard 0's single worker. The 8v1
   ratio there was funnel + cross-shard-wake overhead, not control contention.
   Post-F2 rows have 8 genuinely contending workers. This re-baseline REPLACES
   those rows as the epic's performance record.
2. **Sustained throughput is client-bound.** The Python client saturates well
   before the server does (the 1024-thread `stallrepro.py` client ~8.5k req/s
   with server workers near 40% CPU; the `bench_native_net.sh` client at
   `client_parallel=128` ~5.5k req/s). Throughput rows are labeled client-bound;
   no server-scaling claim is made from them.
3. **Unimodal p50 is the fairness shape, not a regression.** Post-F2 1024-conn
   rows are unimodal (p50 ~= outstanding x service time) in BOTH shard configs;
   fair round-robin replaced the pre-F2 streak bimodality (p50 ~175us + heavy
   tail). Totals are the comparable metric.
4. **The 1-shard total shift was confounded.** The historical 15.5s -> 17.2s
   1-shard drift mixed a host reboot with Task 6/7 overhead. This re-baseline
   owns the clean number (see below).
5. **`ctrl_await_compat` is a harness artifact.** ~27% of the net bench's
   control acquisitions are the `@entrypoint main` externally awaiting
   `serve_many` for the whole run (`done_waiters=1`), which serializes each
   net-wrapper child completion on control as external-await compat. It is
   reported as its OWN column and EXCLUDED from the primary metric:
   **steady-state-control = `control_lock_acquired` − `ctrl_await_compat`**.

## Post-F2 Re-Baseline (the epic's performance record)

Net matrix, `bench_native_net.sh`, direct/seq, 8 req/conn, `SURGE_TRACE_EXEC=1`,
fresh build at `8c89f358`. Headline 1024-conn rows repeated x5 (host load
0.6-1.7 at start of each run; counters are bit-stable, wall-clock totals vary
with the client). Reports under `build/benchmarks/` (git-ignored).

### Latency / throughput (median of x5 on the 1024 rows)

| shards | conns | total us | p50 us | p95 us | note |
| ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 1 | 3529 | 90 | 261 | small-row noise |
| 1 | 8 | 16175 | 1079 | 1853 | |
| 1 | 32 | 55881 | 2875 | 5811 | |
| 1 | 1024 | ~1538061 | 20440 | ~37000 | x5 mean; **client-bound** |
| 8 | 1 | 4118 | 235 | 278 | low-count `SO_REUSEPORT` skew |
| 8 | 8 | 16206 | 1165 | 2314 | |
| 8 | 32 | 55754 | 2744 | 5951 | |
| 8 | 1024 | ~1479968 | 15065 | ~35750 | x5(4 clean) mean; **client-bound** |

> **Table footnote (1-shard rows):** the 1-shard configuration runs the `N=1`
> single-worker runner loop (`run_until_done`/`run_ready_one`, spike rule 5
> counted-separate compat). Its totals AND its `control_lock_acquired` (30.8/req
> on the 1024 row — see the counter table below, with `ctrl_await_compat`=0 and
> an OTHER residual ~203k that is the per-poll runner-loop control) are the
> legacy single-worker runner, NOT a worker steady-state point. The epic's
> control target and the 26.4/req Epic 7 baseline are the 8-shard row.

- **8-shard >= 1-shard (contract):** MET. 8-shard/1024 total ~1.48M us is ~4%
  FASTER than 1-shard/1024 ~1.54M us across all repeats (8-shard consistently
  1.47-1.49M vs 1-shard 1.51-1.58M). 8-shard p50 (15.0ms) is also lower than
  1-shard (20.4ms). Both p50s are unimodal (the fairness shape, input #3), NOT
  the pre-F2 bimodal 175us+tail — a p50 SHIFT, not a regression; totals carry
  the judgment.
- **Clean 1-shard number (input #4):** at `8c89f358` the 1-shard/1024 total is
  ~1.54M us (~1.51-1.58M across x5). The earlier reboot/Task-6-7 confound is
  resolved: this is the current clean value on this host.

### Control-lane counters (8x1024, deterministic; the contract's core)

| Site | Total (8x1024) | Per request | Meaning |
| --- | ---: | ---: | --- |
| `control_lock_acquired` | ~105316 | **12.86** | all control acquisitions |
| `ctrl_await_compat` | 28674 | 3.500 | external-await harness artifact (input #5) |
| **steady-state-control** | ~76642 | **9.36** | `control_lock_acquired − ctrl_await_compat` |
| `ctrl_scope` | 19464 | 2.376 | cross-owner `scope_on_child_done` fallback |
| `ctrl_handle` | 29696 | 3.625 | net-wrapper child last-ref free (`ctrl_handle_free` 28672 + `ctrl_handle_wake` 1024) |
| `ctrl_join_poll` | ~2030 | 0.248 | F2 placement-adoption fallback (O(connections)) |
| `ctrl_create` | 8-11 | ~0.001 | segment-growth residual only |
| `ctrl_completion` | 0 | 0 | (moved to `ctrl_await_compat` by Task 10's tag split) |

`ctrl_await_compat` = 28674 is bit-identical across all repeats; `ctrl_handle` =
29696 bit-identical; `ctrl_scope` 19464-19467; only the untagged
`RT_CTRL_SITE_OTHER` residual (net/accept/io-drain/shutdown, not Epic 8's
surface) jitters with scheduling.

**1-shard/1024 is NOT a steady-state comparison point.** Its
`control_lock_acquired` ~= 252400 (30.8/req) with `ctrl_await_compat`=0 and an
OTHER residual ~203k is the `N=1` single-worker runner loop
(`run_until_done`/`run_ready_one` take control per poll, spike rule 5,
counted-separate compat). It is the legacy runner, not worker steady state; the
epic's control target and the 26.4/req Epic 7 baseline are the 8-shard row.

### Control-per-request vs the contract

- Epic 7 closeout baseline: `control_lock_acquired` ~= **26.4/request** (8x1024).
- HEAD `8c89f358`: **12.86/request** total, **9.36/request** steady-state
  (excluding the external-await artifact). Both drop materially from 26.4 —
  the Performance Contract's control-lane target is met.

### Channels reference refresh (`bench_native_channels.sh`, ns/op)

The channel rendezvous path is untouched by Epic 8 Tasks 6-10; this refresh
confirms no regression (all within host noise of the Task 1 baseline).

| mode | ping_pong | reused_reply | new_reply | sync_new_reply | Task 1 sync ref |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 4712 | 3778 | 4218 | 4581 | 4255 |
| 2 | 17430 | 13694 | 15128 | 55283 | 45277 |
| default(32) | 16984 | 13720 | 15210 | 489663 | 456739 |

The `sync_new_reply` default(32) 456739 -> 489663 is within noise; the B2
rendezvous cost (`RV2-DEBT-017`) is unchanged. The sync/default probe completed
in ~13s this run (did not hit the `RV2-DEBT-006` outer-timeout risk).

### Sustained-stall / CPU-distribution acceptance (manual, `8c89f358`)

- `scripts/run_stallrepro.sh` (90s sustained 8x1024, direct): 746372 requests,
  0 errors, **0 tails >=5s, 0 tails >=10s**, one 1.25s blip (p50 82ms, p95
  301ms, p99 430ms, max 1.25s). Host load hit 22 during the run from the
  1024-thread Python client. The single ~1s blip is the client-load-coupled
  residual band the Task 11 investigation pinned (WSL2 host load stretching the
  fair-round-robin sojourn); it is NOT a server stall and the harness exits 0
  (the ">10s tail" fixed criterion passes).
- `scripts/cpu_validate.sh` (30s): 242241 requests, 0 errors, **0 tails >=1s**,
  per-shard-worker CPU balanced over the 15s window (jiffies 305-526 across the
  8 active shard threads, max/min ~1.7; one spare thread idle). No funnel — the
  F2 fix holds at HEAD (pre-F2 was ~150x imbalance, all work on shard 0).
- `RV2-DEBT-015` remains fixed at `8c89f358`.

## CI Gate

### Design: deterministic trace counters, not wall-clock

The per-commit gate asserts trace-counter thresholds from a fixed workload; it
does NOT assert wall-clock timing. Rationale:

- Control-lock acquisitions per request are a LOGICAL property of request
  processing (how many times the control lane is taken to serve N requests),
  independent of wall-clock time, host load, or worker count. The tagged
  per-site lifecycle counters are bit-stable across runs and client patterns.
- A wall-clock throughput/ratio gate is WSL2- and CI-load-fragile and, post-F2,
  client-bound (the client saturates before the server), so it cannot cleanly
  attribute a regression to the runtime. The 90s stallrepro even on the
  reference host produces occasional ~1s client-coupled blips under load — a
  poor per-commit signal.

### Per-commit gate: `TestRuntimeV2PerfControlLaneGate`

`internal/vm/runtime_v2_perf_gate_test.go` (`//go:build runtime_v2_pending`),
wired into `make runtime-v2-perf-check` -> `make runtime-v2-check`. It builds a
direct-mode net fixture (faithful to `benchmarks/native/net_request_reply`:
`serve_many` -> `@local spawn serve_conn` -> `net.read_some`/`net.write_all`,
plus the `@entrypoint main` external await) via `go test` (no `./surge`
dependency), drives a fixed workload (**8 shards x 128 conns x 8 req = 1024
requests**; fd-safe, ~4s), and parses the server's `TRACE_EXEC`/`TRACE_NET` exit
line. Assertions (measured HEAD value in brackets; ceilings carry headroom for
core-count/scheduling variance while failing on a regression toward 26.4/req):

1. **lifecycle-control/req** = `(ctrl_create + ctrl_join_poll + ctrl_completion
   + ctrl_scope + ctrl_handle)/req` <= **9.0** [measured ~6.0, bit-stable]. The
   precise Epic-8-surface regression detector: a change that reintroduces
   control-lane traffic on task create/join/completion/scope/handle relifts it.
2. **steady-state-control/req** = `(control_lock_acquired −
   ctrl_await_compat)/req` <= **20.0** [measured ~8.1]. The contract-literal
   backstop; a regression toward the 26.4/req baseline fails.
3. **placement_adoptions > 0** [measured ~253]. F2 join-consume placement
   adoption is firing, proving the `RV2-DEBT-015` placement funnel has not
   regressed.
4. **accept_owner_active_shards >= 2** [measured 8]. Accepts are distributed,
   not funneled onto one shard.

Stability (5 runs at HEAD): lifecycle 5.995-6.001/req, steady 8.00-8.25/req,
adoptions 249-255, active accept shards 8 — the thresholds have ~1.5x (lifecycle)
and ~2.4x (steady-state) headroom.

The gate is 128 connections, not 1024: it exercises multi-shard placement,
per-connection scope trees, net-handle frees, and F2 adoption with the same
per-request ratios as the 8x1024 row (measured 8x128: 13.0/req total, 9.49/req
steady, all 8 accept shards active), while staying fd-safe and fast enough for
every commit. The 8x1024 headline numbers live in the re-baseline above, not the
gate.

### Manual / nightly acceptance runbook (NOT per-commit)

The 90s sustained probe and the full 8x1024 matrix are too heavy for every CI
run and their wall-clock signal is host-fragile. They are the acceptance
runbook, run on demand / nightly. Each piece proves ONE contract point — run
exactly the piece you need:

```bash
# (A) RV2-DEBT-015 stall contract — the >10s-tail class:
STALL_CONNS=1024 STALL_SHARDS=8 STALL_MODE=direct \
  bash scripts/run_stallrepro.sh <tag>
# (B) F2 CPU-distribution contract — work spread across shard workers, no funnel:
STALL_CONNS=1024 STALL_SHARDS=8 bash scripts/cpu_validate.sh <tag>
# (C) scaling / latency judgment — 8-shard >= 1-shard on totals, unimodal p50:
SURGE_NET_BENCH_SHARDS="1 8" SURGE_NET_BENCH_CONNECTIONS="1 8 32 1024" \
  SURGE_NET_BENCH_MODES=direct SURGE_NET_BENCH_PATTERNS=seq \
  SURGE_NET_BENCH_REQUESTS=8 bash scripts/bench_native_net.sh
```

- **(A) run_stallrepro.sh -> the >=1s-stall / RV2-DEBT-015 contract.** Read it
  with TWO criteria, or a client-load blip will be misread as a regression:
  - **HARD (pass/fail):** ZERO tails >= 10s (the RV2-DEBT-015 starvation class).
    `stallrepro.py` exits non-zero only on a >=10s tail; this is the gate.
  - **ADVISORY (report, do not fail):** tails >= 1s, reported WITH the host/client
    load stamps the script already prints (`uptime` before/after). Under the
    1024-thread Python client, host load runs ~20+, and occasional ~1-1.5s blips
    are the client-coupled residual band the Task 11 investigation pinned
    (fair-round-robin sojourn stretched by WSL2 host scheduling) — expected, not
    a regression. A >=1s tail observed on a MACHINE-IDLE host (load ~1) is
    investigation-worthy; a loaded-host blip is not. (This re-baseline: 1 blip at
    1.25s / 746372 req at load ~22 — advisory only; HARD criterion PASS.)
- **(B) cpu_validate.sh -> the F2 CPU-distribution contract.** Per-shard-worker
  CPU jiffies over the sample window must be balanced (this re-baseline: 305-526
  across 8 threads, max/min ~1.7), proving no placement funnel (pre-F2 was ~150x,
  all on shard 0).
- **(C) bench_native_net.sh matrix -> the scaling / latency judgment.** 8-shard
  total <= 1-shard total (this re-baseline: 8-shard ~4% faster), unimodal p50 in
  both configs (fairness shape). These totals are client-bound; they judge
  relative shard scaling, not absolute server throughput.

Split rationale: per-commit = bounded, deterministic, counter-based (catches
control-lane and fairness regressions); manual/nightly = sustained wall-clock
acceptance, which is host-load-coupled and belongs off the per-commit path.

## RV2-DEBT-016 Final State: CLOSED

With the re-baseline recorded, `RV2-DEBT-016` is closed:

- **Control-lane target met:** steady-state-control 9.36/req (total 12.86/req)
  on the 8x1024 row, materially below the Epic 7 closeout ~26.4/req. Task
  create/join/normal-done-completion and same-owner scope bookkeeping run
  shard-locally on the steady path (proven by Tasks 6-9 static gates + the
  per-site counters).
- **Scaling met:** the 8-shard/1024 row is at least as fast as the 1-shard row
  (~4% faster on totals here; +12% on the Task 11 sustained run).

Two residuals remain, reassigned to their existing owners (not Epic 8 lifecycle
debt):

- The 28674 external-await `ctrl_await_compat` completions are a
  harness-structural artifact (every multi-worker Surge program parks a root
  external awaiter); the underlying `done_cv` ordering is owned by
  `RV2-DEBT-022`.
- The 19464 cross-owner `ctrl_scope` (`scope_on_child_done`) fallback is future
  net-handle/placement work (re-pin a scope to its owner task's F2-adopted
  shard); its test gap is `RV2-DEBT-021`.

The honest three-step attribution chain (Task 8 wrong -> Task 9 wrong -> Task 10
proven) is preserved verbatim in the `DEBT.md` closed entry.

## Gates

| Command | Result |
| --- | --- |
| `git diff --check` | clean |
| `make check` | PASS |
| `make runtime-v2-check` (incl. new `runtime-v2-perf-check`) | PASS (all stages green, 0 FAIL) |
| `make runtime-v2-perf-check` (standalone) | PASS (~4s): lifecycle 6.0/req, steady 7.85/req, adoptions ~253, 8 accept shards |
| `./check_file_sizes.sh -a` | PASS; `runtime_v2_perf_gate_test.go` 422 lines (<=500) |
| `gofmt -l` / `go vet -tags runtime_v2_pending` | clean |
| Sentrux root/runtime/runtime-native | 6174 / 5295 / 5382, all rules pass (no drop vs Task 10; no C changed) |

No C touched, so `make c-check`/`make cppcheck` are N/A (the gate reads existing
trace counters); `git diff --check` still run.

## Files Touched

- `internal/vm/runtime_v2_perf_gate_test.go` (new, 422 lines): the per-commit
  performance gate.
- `Makefile`: new `runtime-v2-perf-check` stage, added to `.PHONY` and called
  from `runtime-v2-check`.
- Docs: this file; `08-evidence.md` Task 12 section (the re-baseline record);
  `08-tasks/README.md` status row; `DEBT.md` (`RV2-DEBT-016` closed);
  `NOTES.md` handoff.

## Commit Boundary

One commit: the perf gate test + Makefile stage + the re-baseline evidence and
docs. No runtime C behavior change.
