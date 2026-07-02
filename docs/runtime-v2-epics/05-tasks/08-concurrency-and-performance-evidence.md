# Task 8: Concurrency And Performance Evidence

**Status:** Draft
**Kind:** evidence/benchmark

## Goal

Prove the new heap-accounting model under concurrent allocation pressure and
record performance impact before CI gate work.

## Scope

- Run focused concurrent heap-accounting tests with more than one worker.
- Add or run an allocation-heavy microprobe if the behavior tests do not produce
  enough accounting pressure.
- Compare before/after evidence against the Epic 5 kickoff baseline.
- Run native net or channel benchmarks only if accounting changes can affect
  their covered paths beyond cheap counter writes.
- Record whether Sentrux signals changed after the runtime-code tasks.

## Files

- Modify if needed: `scripts/bench_native_heap_accounting.sh`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Modify if needed: `docs/runtime-v2-epics/LIVENESS_PROBES.md`

## Checks

- `go test ./internal/vm -run '^TestRuntimeV2HeapAccounting' -count=3 -parallel=1 -p=1 -v --timeout 240s`
- `make runtime-v2-check`
- Optional if created: `SURGE=/path/to/current/surge ./scripts/bench_native_heap_accounting.sh`
- Optional if touched paths require it: `SURGE=/path/to/current/surge ./scripts/bench_native_net.sh`
- Optional if touched paths require it: `SURGE=/path/to/current/surge ./scripts/bench_native_channels.sh`
- `git diff --check`
- Root and scoped Sentrux scans plus rule checks

## Done

- Concurrent allocation evidence is recorded.
- Performance impact is explained with current-checkout commands and reports.
- Any benchmark or probe too unstable for CI has an owner and close condition.
