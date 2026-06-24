# Runtime Performance Notes

## 2026-06-24 - `stdlib/bytes` LF line scanning

Change: `stdlib/bytes` now exposes `find_byte`, `find_lf`, `find_crlf`, and
`ByteBuffer.peek_line_lf/peek_line_crlf` for protocol input buffers.

Standalone native benchmark:

- fixture: `benchmarks/native/byte_lines`
- script: `scripts/bench_native_byte_lines.sh`
- payload: 256 lines, width 32, 200 rounds
- comparison: `string.from_bytes + string_find_from` vs `ByteBuffer.peek_line_lf`

| run | string line us | byte line us | speedup |
| ---: | ---: | ---: | ---: |
| 1 | 29891498 | 3900029 | 7.66x |
| 2 | 29921010 | 3902029 | 7.67x |
| 3 | 29922786 | 3894605 | 7.68x |

Median speedup: `7.67x`.

Conclusion: pure Surge byte scanning is already enough for this slice. Do not
add `rt_byte_find` until token/dispatch benchmarks show a specific gap.

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

## 2026-06-24 - Worker-side net polling before sleep

Change: multi-worker runtime workers now try a short `poll_net_waiters(1ms)`
pass before sleeping on `ready_cv`. This avoids routing every idle-worker socket
wake through the dedicated I/O thread and a condition-variable handoff.

The same change also keeps the narrower inline-await optimization for freshly
created child tasks that are still at the current worker local queue tail.

Focused downstream benchmark:

- downstream repo: `surgekv`, branch `codex/append-string-response-bytes`
- topology: `SURGEKV_BENCH_WORKERS=1`, `SURGEKV_BENCH_SHARDS=1`
- command shape: release build, `SURGEKV_BENCH_OPS="ping get"`,
  `SURGEKV_BENCH_CLIENTS=1`, `SURGEKV_BENCH_REQUESTS=5000`

| SURGE_THREADS | op | before worker poll avg us | worker poll avg us | delta |
| ---: | --- | ---: | ---: | ---: |
| 2 | ping | 225 | 150 | -33.3% |
| 2 | get | 411 | 281 | -31.6% |

Default downstream topology (`workers=8`, `shards=8`, `SURGE_THREADS=8`) also
improves the single-client rows:

| op | previous avg us | worker poll avg us | delta |
| --- | ---: | ---: | ---: |
| ping | 232-239 | 157 | ~-33% |
| get | 473-485 | 337 | ~-30% |

Native net fixture, `SURGE_THREADS=8`, remains below the original main baseline
for direct request/reply, but the short worker poll is not a complete win for
all synthetic rows:

| mode | original main avg us | inline-only avg us | worker poll avg us |
| --- | ---: | ---: | ---: |
| echo seq | 207 | 95 | 101 |
| direct seq | 193 | 137 | 150 |
| manager seq | 218 | 201 | 211 |

Conclusion: the remaining `SURGE_THREADS>1` TCP gap is primarily the worker/IO
handoff model. Worker-side net polling is the first Surge-side change in this
series that materially improves the live `surgekv` TCP hot path; multi-client
GET still needs separate downstream/channel-topology work.
