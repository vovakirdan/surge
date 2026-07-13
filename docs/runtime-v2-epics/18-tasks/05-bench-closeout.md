# Epic 18 Task 5: Bench + Debt + Closeout

## Bench (2026-07-13, committed tree, LLVM)

`scripts/bench_crossing.py --iterations 2000`, new `move-capture`
probe (one immediate-on round trip carrying a MOVED owned
@shard_movable capture) vs the plain-copy immediate-on baseline:

| probe | shards 1 | shards 2 | shards 8 |
| --- | --- | --- | --- |
| immediate-on (us/rt) | 7.1 | 55.9 | 80.4 |
| move-capture (us/rt) | 8.7 | 59.3 | 84.4 |

The capture move costs ~6% over plain-copy at 2-8 shards (state-struct
allocation + field moves) — no cliff; steady-state crossings unchanged
within noise vs the Epic 17 closeout numbers.

## Debt disposition

- NEW RV2-DEBT-034: the dormant drop-obligation plumbing, activated by
  language-wide drop emission (see the row for the full activation
  list: compiled drop functions, glue-edge rows, owned results).
- RV2-DEBT-031/032/033 untouched.

## Closeout gates

make check, remote-task behavior suite + deadlock rows (x2), crossing
e2e gate (genesis/on-ch/share/select/migration at SHARDS=1/2/8),
compiler package suites — green on the committed tree; second pass at
the closeout commit. Sentrux four scopes recorded in the epic doc.
