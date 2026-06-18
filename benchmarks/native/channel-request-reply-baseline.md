# Native channel request/reply baseline

Generated: 2026-06-18T14:32:48Z

## Environment

- compiler base commit: `1a5cfcec` (runtime before scheduler-matrix PR)
- fixture: `benchmarks/native/channel_request_reply`
- command: `SURGE_CHANNEL_BENCH_REPORT=build/benchmarks/native-channel-matrix.md ./scripts/bench_native_channels.sh`
- modes: `1 2 4 8 default`
- machine: local developer workstation

## Results

| mode | probe | iterations | total us | ns/op |
| --- | --- | ---: | ---: | ---: |
| 1 | empty_loop | 20000 | 10532 | 526 |
| 1 | response_bytes | 20000 | 564342 | 28217 |
| 1 | channel_ping_pong | 20000 | 88873 | 4443 |
| 1 | channel_reused_reply | 20000 | 87430 | 4371 |
| 1 | channel_new_reply | 20000 | 97097 | 4854 |
| 1 | channel_sync_new_reply | 5000 | 47515 | 9503 |
| 2 | empty_loop | 20000 | 10423 | 521 |
| 2 | response_bytes | 20000 | 568452 | 28422 |
| 2 | channel_ping_pong | 20000 | 1193172 | 59658 |
| 2 | channel_reused_reply | 20000 | 1132838 | 56641 |
| 2 | channel_new_reply | 20000 | 1145522 | 57276 |
| 2 | channel_sync_new_reply | 5000 | 409164 | 81832 |
| 4 | empty_loop | 20000 | 10395 | 519 |
| 4 | response_bytes | 20000 | 562272 | 28113 |
| 4 | channel_ping_pong | 20000 | 2671347 | 133567 |
| 4 | channel_reused_reply | 20000 | 2019452 | 100972 |
| 4 | channel_new_reply | 20000 | 2089624 | 104481 |
| 4 | channel_sync_new_reply | 5000 | 689648 | 137929 |
| 8 | empty_loop | 20000 | 10621 | 531 |
| 8 | response_bytes | 20000 | 562092 | 28104 |
| 8 | channel_ping_pong | 20000 | 2815943 | 140797 |
| 8 | channel_reused_reply | 20000 | 2253411 | 112670 |
| 8 | channel_new_reply | 20000 | 2309191 | 115459 |
| 8 | channel_sync_new_reply | 5000 | 1170321 | 234064 |
| default | empty_loop | 20000 | 10348 | 517 |
| default | response_bytes | 20000 | 558670 | 27933 |
| default | channel_ping_pong | 20000 | 2781608 | 139080 |
| default | channel_reused_reply | 20000 | 2319406 | 115970 |
| default | channel_new_reply | 20000 | 2338384 | 116919 |
| default | channel_sync_new_reply | 5000 | 3812288 | 762457 |

## Signal

`channel_reused_reply` is the regression signal for the surgekv-style actor hop:
single-worker baseline is about 4.4us/op, while multi-worker modes are about
56-116us/op on this machine.

`channel_ping_pong` isolates direct async channel handoff between two tasks.
`channel_sync_new_reply` keeps the sync-wrapper fallback visible; this is the
worker-pinning shape that future compiler/runtime boundary work should drive
out of hot async paths.
