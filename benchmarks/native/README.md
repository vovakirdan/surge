# Native Benchmarks

Manual runtime probes live here. They are intentionally not part of `make check`
because latency numbers are machine-dependent.

Run the channel request/reply probe:

```bash
make build
./scripts/bench_native_channels.sh
```

Run the TCP request/reply probe:

```bash
make build
./scripts/bench_native_net.sh
```

Run the byte range copy probe:

```bash
make build
./scripts/bench_native_bytes.sh
```

Useful overrides:

```bash
SURGE_CHANNEL_BENCH_MODES="1 2 4 8 default" ./scripts/bench_native_channels.sh
SURGE_CHANNEL_BENCH_REPORT=/tmp/channel.md ./scripts/bench_native_channels.sh
SURGE_CHANNEL_TRACE_PROBES="channel_reused_reply" ./scripts/bench_native_channels.sh
SURGE_CHANNEL_WAKE_INJECT=1 SURGE_CHANNEL_BENCH_REPORT=/tmp/channel-inject.md ./scripts/bench_native_channels.sh
SURGE_NET_BENCH_THREADS="1 2 4 8" SURGE_NET_BENCH_REPORT=/tmp/net.md ./scripts/bench_native_net.sh
SURGE_NET_BENCH_STDLIB=/path/to/surge ./scripts/bench_native_net.sh
SURGE_BYTES_BENCH_REPEATS=9 SURGE_BYTES_BENCH_REPORT=/tmp/bytes.md ./scripts/bench_native_bytes.sh
```

Compare future runtime PRs against
`benchmarks/native/channel-request-reply-baseline.md`.

Troubleshoot runtime latency with:

```bash
SURGE_TRACE_EXEC=1 ./target/release/your_program 2>trace.log
kill -USR1 <pid>
rg 'TRACE_EXEC|TRACE_NET|TRACE_EXEC_SNAPSHOT|SCHED_TRACE' trace.log
```

Read the counters this way:

- `channel_blocking_wait`, `channel_task_blocking_send`, and
  `channel_task_blocking_recv`: sync channel fallback from task context.
- `channel_handoff_yield`: direct async channel handoffs.
- `compensation_started` and `compensation_high_water`: worker-pinning fallback.
- `SCHED_TRACE local`, `inject`, `steal`, and `events`: scheduler source mix.
  High `steal` or high `worker_sleep`/`worker_wake` on sequential request/reply
  paths usually means worker-pool wake churn, not net poll rebuild cost.
- `io_poll_timeouts`, `io_poll_wake_fd`, and `io_poll_net_ready`: net poll
  progress and timeout-driven tails.
- `io_waiter_scan_entries`, `io_poll_rebuilds`, and
  `io_poll_dedup_checks`: net waiter-list scan and poll-set rebuild cost.

The fixture also prints scheduler-shape rows:

- `channel_ping_pong`: direct async send/recv handoff between two tasks.
- `channel_reused_reply`: request/reply over one reused reply channel.
- `channel_new_reply`: request/reply with a new reply channel per request.
- `channel_sync_new_reply`: sync wrapper fallback called from an async task.

`bench_native_channels.sh` runs runtime trace one probe at a time so counters
show the selected scheduler shape instead of an aggregate from the whole fixture.

The net request/reply probe prints three layers: `echo` for minimal socket
read/write, `direct` for socket task response writes, and `manager` for the
same response behind a channel request/reply hop. It enables both
`SURGE_TRACE_EXEC=1` and `SURGE_SCHED_TRACE=1` for each server run.

The byte range probe compares a per-byte `push` loop against
`stdlib/bytes.append_bytes_range` over the same reused output buffer. It is a
local proof for the byte-array bulk-copy primitive; it is not a full protocol
parser benchmark.

Default channel placement keeps generic wakes local-first, while no-signal
handoff wakes use inject placement. Use `SURGE_CHANNEL_WAKE_INJECT=1` only for
A/B experiments that force all channel wakes through inject.
