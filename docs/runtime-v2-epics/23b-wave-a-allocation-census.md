# Epic 23b Wave A Allocation Census

Status: **FROZEN ORACLE INPUT**

Authority: `23b-inline-storage-and-typed-carriers.md`, Wave A and Sections 8-9.

This record separates allocations that typed carriers must remove from
structural allocations that still create an owner, container, continuation, or
transport record. It is the evidence behind
`candidate_structural_allocations_per_batch` in
`testdata/runtime-v2-carrier-bench.json`.

## Method

The census ran at clean product commit
`877e974cfeeb6445ba2d60e97d2a5156d71c43bf` on the reference host. A temporary,
uncommitted diagnostic build did only three things:

1. toggled a census window at the fixture's two existing
   `rt_array_debug_deferred_base_drops` marker calls;
2. logged every `rt_alloc`/`rt_realloc` caller, size, and alignment inside that
   window;
3. linked non-PIE so each caller could be classified with `addr2line`, the
   native C source, and the retained emitted `out.ll`.

No scheduler, allocator, fixture, or ownership behavior changed. Each of the 46
rows ran twice. Event counts matched between captures except
`channel-unbuffered-composite` (`421` then `420`): the differing event was in
the generated per-value/poll-result set that the typed representation deletes,
not the owner/container set. Its structural target is therefore the stable
scalar/composite owner topology, `130`, rather than either raw total.

The classification rule is deliberately semantic:

- remove every allocation whose object *is a language value or erased carrier*
  (composite/scalar result box, tag/poll-result box, payload clone box, array/map
  value box, or far payload box);
- retain an allocation whose object owns lifecycle independent of the value
  (task, scope, channel, async continuation/state, map owner/table, array growth
  buffer, pending request, lease, or transport record);
- retain `rt_realloc` only when it grows structural container/owner storage;
- do not infer a budget by subtracting scalar from composite. The paired rows
  are a cross-check after both callsite sets have been classified.

The emitted-IR pass is load-bearing for generated `fn.*` callers: their bodies
distinguish async state/continuation owners from the value/tag boxes that happen
to have similar sizes. Native callers resolve directly to sites such as
`__task_create`, `rt_scope_enter`, `deque_reserve`, `rt_map_new`, and container
growth. This avoids treating today's total allocation count as tomorrow's
allowance.

## Frozen Per-Batch Budgets

Every row performs 64 operations and two batches. “Raw” is the pre-cutover
diagnostic count inside one batch; `C/S` means composite/scalar siblings. The
target is exact for every candidate warmup and every candidate measured timing
batch.

| Row or paired rows | Raw pre-cutover allocations | Exact structural target | Classified remainder |
| --- | ---: | ---: | --- |
| `array-grow-{composite,scalar}` | C `327`, S `71` | `7` | Seven backing-buffer growth reallocations; all element/result boxes disappear. |
| `array-steady-{composite,scalar}` | C `320`, S `64` | `0` | Reserved storage already exists; every in-window allocation is a value/result box. |
| `array-teardown-{composite,scalar}` | C `192`, S `128` | `0` | Teardown releases prebuilt containers; no new owner is created. |
| `blocking-{composite,scalar}` | C `663`, S `407` | `278` | Blocking job/task/continuation owners remain; capture/result carrier boxes disappear. |
| `channel-buffered-{composite,scalar}` | C `272`, S `80` | `15` | Channel owner/buffer and bounded scheduler growth remain; element/result boxes disappear. |
| `channel-unbuffered-{composite,scalar}` | C `420..421`, S `197` | `130` | Wait/task/continuation owners remain; rendezvous payload and poll-result boxes disappear. |
| `far-channel-{composite,scalar}` | C `1122`, S `866` | `544` | Far channel leases, pending/transport records, tasks, and continuations remain. |
| `far-immediate-{composite,scalar}` | C `667`, S `347` | `281` | Immediate crossing request/task/continuation owners remain. |
| `far-jumbo-contention` | `67347` | `1042` | Request/task/credit-wait topology remains; the repeated 8192-byte carrier boxes disappear. |
| `far-large-capture` | `26267` | `1049` | Request/task/transport owners remain; the 4096-byte capture boxes disappear. |
| `far-large-result` | `17307` | `537` | Reply/task/transport owners remain; the 4096-byte result boxes disappear. |
| `far-select-{composite,scalar}` | C `937`, S `745` | `615` | Select/pending/lease/task records remain; staged payload boxes disappear. |
| `far-share-control` | `353` | `351` | Lease/control topology is the workload; two language-value boxes disappear. |
| `far-task-{composite,scalar}` | C `923`, S `603` | `537` | Remote task/pending/reply/continuation owners remain; payload/result boxes disappear. |
| `local-argument` | `64` | `0` | Argument composite boxes disappear into typed call storage. |
| `local-copy` | `128` | `0` | Source and destination boxes disappear into inline storage. |
| `local-fixed-array` | `256` | `0` | Fixed-array elements and extracted values are inline. |
| `local-return` | `64` | `0` | Destination-oriented return storage replaces result boxes. |
| `map-insert-{composite,scalar}` | C `324`, S `132` | `4` | Map owner/table growth remains; key/value/result boxes disappear. |
| `map-rehash-{composite,scalar}` | C `340`, S `140` | `4` | Rehash owner/table growth remains; per-entry value boxes disappear. |
| `map-remove-{composite,scalar}` | C `128`, S `64` | `0` | Existing table storage is reused; removed-value boxes disappear. |
| `map-replace-{composite,scalar}` | C `448`, S `128` | `0` | Existing slots are replaced in place; old/new/result boxes disappear. |
| `map-teardown-{composite,scalar}` | C `192`, S `128` | `0` | Teardown creates no owner; all observed allocations are value/result boxes. |
| `scalar` | `1` | `0` | The remaining ordinary scalar result box disappears. |
| `select-send-{composite,scalar}` | C `522`, S `73` | `7` | Channel/select owner growth remains; staged send and arm-result boxes disappear. |
| `task-clone-{composite,scalar}` | C `599`, S `407` | `278` | Task/entitlement/continuation owners remain; each result carrier box disappears. |
| `task-{composite,scalar}` | C `471`, S `343` | `278` | Task/continuation owners remain; completion/await result boxes disappear. |
| `zero` | `1` | `0` | Zero-sized language value has no storage allocation. |

This grouped table covers all 46 manifest rows; the manifest spells each row's
integer separately so omission cannot inherit a group default.

## Executable Oracle

The allocation proof is independent of resource counters and timing scores:

`candidate_structural_allocations_per_batch` is the sole executable
`allocation_count` contract. Row invariants and cross-row invariants are not
allowed to restate or weaken it; the scalar/composite pairings above are census
classification evidence only.

1. Base timing and candidate timing binaries run the deliberate
   `allocation-control` fixture first. Its one `reserve(1)` must report exactly
   one allocation on both sides; zero proves a dead observer and aborts.
2. Timing binaries contain no `rt_carrier_bench_*` definition or callsite and do
   not receive counter/nonce environment variables.
3. All `2` warmups plus all `7` measured pairs for all rows complete before the
   first resource capture. Every candidate timing batch must equal the exact
   target above.
4. Candidate-only resource binaries are separately compiled with
   `RT_CARRIER_BENCH_ENABLED`. Their elapsed time and latency samples remain raw
   evidence and never enter `TimingSample` or performance scores.
5. Attempt identity records capture kind, row, side, phase, warmup/pair index,
   batch, and global ordinal. Missing, duplicate, reordered, or cross-kind
   attempts fail closed.

The mutation controls pin the two failure directions: a stuck-zero allocation
observer fails the deliberate control, and adding one allocation uniformly to
both a scalar and composite row fails both exact budgets. A paired
“composite <= scalar” check alone would miss that mutation and is not accepted
as the allocation oracle.

## Boundary Still Requiring Owner Disposition

The manifest still names raw `EPIC_BASE`
`7df10725e001ddf915d536aa58f880bd7e04aafd`. That revision predates the
register-then-verify repair in `RV2-DEBT-143`; its blocking row can lose a wake
and time out. This census does not choose a replacement base, apply an overlay,
or waive the failure. The project owner must explicitly settle the benchmark
base before the complete Wave A capture is accepted.
