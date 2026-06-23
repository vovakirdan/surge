# Runtime Performance Notes

## 2026-06-23 - Direct network readiness wait

Change: `stdlib/net` no longer awaits a spawned `Task<nothing>` for socket
readiness. `rt_net_wait_accept`, `rt_net_wait_readable`, and
`rt_net_wait_writable` now park the current async task directly on a net waker.

Benchmark:

- downstream repo: `surgekv`, branch `codex/append-string-response-bytes`
- before: `origin/main` at `610cd2d7`
- after: working tree on `codex/net-direct-wait`
- command shape: release build, `SURGEKV_BENCH_OPS="ping get get_pipe"`,
  `SURGEKV_BENCH_CLIENTS=1`, `SURGEKV_BENCH_REQUESTS=2000`

| SURGE_THREADS | op | main avg us | direct wait avg us | delta |
| ---: | --- | ---: | ---: | ---: |
| 1 | ping | 102 | 99 | -2.9% |
| 1 | get | 196 | 178 | -9.2% |
| 1 | get_pipe | 81 | 80 | -1.2% |
| 8 | ping | 223 | 217 | -2.7% |
| 8 | get | 286 | 281 | -1.7% |
| 8 | get_pipe | 92 | 94 | +2.2% |

Reports:

- `/tmp/surgekv-main-threads1.md`
- `/tmp/surgekv-direct-threads1.md`
- `/tmp/surgekv-main-threads8.md`
- `/tmp/surgekv-direct-threads8.md`

Conclusion: direct readiness wait removes measurable single-client GET overhead,
but it does not solve the broader `SURGE_THREADS=8` latency gap by itself.
