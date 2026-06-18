# Native channel request/reply baseline

Generated: 2026-06-18T11:32:58Z

## Environment

- compiler base commit: `e6b44148bb0e` (merged PR #115)
- fixture: `benchmarks/native/channel_request_reply`
- command: `./scripts/bench_native_channels.sh`
- modes: `1 2 4 8 default`
- machine: local developer workstation

## Results

| mode | probe | iterations | total us | ns/op |
| --- | --- | ---: | ---: | ---: |
| 1 | empty_loop | 20000 | 6159 | 307 |
| 1 | response_bytes | 20000 | 454514 | 22725 |
| 1 | channel_reused_reply | 20000 | 79565 | 3978 |
| 1 | channel_new_reply | 20000 | 87077 | 4353 |
| 2 | empty_loop | 20000 | 7892 | 394 |
| 2 | response_bytes | 20000 | 448844 | 22442 |
| 2 | channel_reused_reply | 20000 | 943410 | 47170 |
| 2 | channel_new_reply | 20000 | 1059313 | 52965 |
| 4 | empty_loop | 20000 | 7591 | 379 |
| 4 | response_bytes | 20000 | 449635 | 22481 |
| 4 | channel_reused_reply | 20000 | 2102557 | 105127 |
| 4 | channel_new_reply | 20000 | 2163796 | 108189 |
| 8 | empty_loop | 20000 | 7757 | 387 |
| 8 | response_bytes | 20000 | 448600 | 22430 |
| 8 | channel_reused_reply | 20000 | 2178635 | 108931 |
| 8 | channel_new_reply | 20000 | 2234688 | 111734 |
| default | empty_loop | 20000 | 7635 | 381 |
| default | response_bytes | 20000 | 452752 | 22637 |
| default | channel_reused_reply | 20000 | 2247712 | 112385 |
| default | channel_new_reply | 20000 | 2319939 | 115996 |

## Signal

`channel_reused_reply` is the regression signal for the surgekv-style actor hop:
single-worker baseline is about 4.0us/op, while multi-worker modes are about
47-112us/op on this machine.
