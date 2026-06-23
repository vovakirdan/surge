# Native Benchmarks

Manual runtime probes live here. They are intentionally not part of `make check`
because latency numbers are machine-dependent.

Run the channel request/reply probe:

```bash
make build
./scripts/bench_native_channels.sh
```

Useful overrides:

```bash
SURGE_CHANNEL_BENCH_MODES="1 2 4 8 default" ./scripts/bench_native_channels.sh
SURGE_CHANNEL_BENCH_REPORT=/tmp/channel.md ./scripts/bench_native_channels.sh
SURGE_CHANNEL_TRACE_PROBES="channel_reused_reply" ./scripts/bench_native_channels.sh
SURGE_CHANNEL_WAKE_INJECT=1 SURGE_CHANNEL_BENCH_REPORT=/tmp/channel-inject.md ./scripts/bench_native_channels.sh
```

Compare future runtime PRs against
`benchmarks/native/channel-request-reply-baseline.md`.

Troubleshoot runtime latency with:

```bash
SURGE_TRACE_EXEC=1 ./target/release/your_program 2>trace.log
kill -USR1 <pid>
rg 'TRACE_EXEC|TRACE_NET|TRACE_EXEC_SNAPSHOT' trace.log
```

Read the counters this way:

- `channel_blocking_wait`, `channel_task_blocking_send`, and
  `channel_task_blocking_recv`: sync channel fallback from task context.
- `channel_handoff_yield`: direct async channel handoffs.
- `compensation_started` and `compensation_high_water`: worker-pinning fallback.
- `io_poll_timeouts`, `io_poll_wake_fd`, and `io_poll_net_ready`: net poll
  progress and timeout-driven tails.

The fixture also prints scheduler-shape rows:

- `channel_ping_pong`: direct async send/recv handoff between two tasks.
- `channel_reused_reply`: request/reply over one reused reply channel.
- `channel_new_reply`: request/reply with a new reply channel per request.
- `channel_sync_new_reply`: sync wrapper fallback called from an async task.

`bench_native_channels.sh` runs runtime trace one probe at a time so counters
show the selected scheduler shape instead of an aggregate from the whole fixture.

Default channel placement keeps generic wakes local-first, while no-signal
handoff wakes use inject placement. Use `SURGE_CHANNEL_WAKE_INJECT=1` only for
A/B experiments that force all channel wakes through inject.
