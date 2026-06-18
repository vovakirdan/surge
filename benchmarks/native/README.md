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
```

Compare future runtime PRs against
`benchmarks/native/channel-request-reply-baseline.md`.
