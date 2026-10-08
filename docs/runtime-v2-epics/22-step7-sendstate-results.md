# SENDSTATE: measured ownership fix

Verified 2026-10-08. Source landing: `434016ea844fa8b70c89620d2734abe7793c1e9f`.
This documentation commit records the result; publication is verified separately.

Base: `8dc750284bc3c6eaa2b77055e920a1f49573b9f0`.
Production measurement: `94aacd712e56b3e2c60fdae0ab91c0ce776f69f0`.
Census correction: `f296f84cd957dc33d588b4892cd7b3a3518b75c5`.
The final source patch changes 21 files (487 insertions, 31 deletions), SHA256
`caf9dcea570035fbf072709b8ff77c9277bec2e6c935fc34b26bf5474814d80b`.
All 20 production file hashes remain identical to the measured snapshot.

## What changed

A consuming channel send records source transfer independently of readiness.
After transfer, the frame resumes without reading, packing or freeing the
source again. Counted-copy offers retain their owner. Native code also guards
the interval before acknowledgment. VM and native use the same transfer rule.

On the unfixed base, moved-string cancellation and four owned lifecycle shapes
report VM1002; the native cancellation witness double-frees. Four
counterfactuals, each repeated three times, restore the respective failure.
Restoring the patch restores the positive controls. R9/R10 preserve raw runs,
source/binary hashes, and allocation XMLs.

## Verified evidence

| Check | Result and limit |
|---|---|
| MIR / LLVM | MIR 664 PASS both; LLVM 336 to 337 PASS, 17 unchanged baseline FAIL. Declared rename and added control verified. |
| Counterfactuals | Four mutations, three red repetitions each, restored positive controls pass. |
| Independent review | Non-author review APPROVE for production and census correction; the census passed 139 tests. |
| C / vet / size | Required changed-source checks and targeted native/allocation/protocol rows pass; source hashes unchanged since those runs. |
| UAF comparison | 1044 diagnostic comparisons unchanged. This is not execution of the 210 already accepted programs. |
| Golden | S-GOLD-DIFF has no new effects. Both sides retain 24 generator errors and 65 stale paths; strict frozen S-GOLD remains RV2-DEBT-421 FAIL. |
| Fast | 8262 to 8263 PASS, 113 unchanged FAIL with equal full reasons. |
| Behaviour VM/native | Both 290 PASS / 24 FAIL; full reasons equal. |
| Full VM/LLVM | Complete plain and pending inventories, hashes and RUN/terminal counts. No old status changes or removals; +35 PASS plain and +80 PASS pending per backend. |
| MT, workers 2/8, repeat 2 | Both 104 PASS / 6 FAIL occurrences; full reasons equal. |
| Aggregate | All 21 verdicts collected: 7 PASS / 14 FAIL each. Ownership timeout was followed up; completed corpus reports the same full baseline reasons. |
| Skips | Every row has a completed counterpart or named exclusion. MT has its separate repeated lane. Three old exclusions remain: Epic 24 partial moves, future envelope/sidecar arithmetic, RV2-DEBT-255. Eight LLVM skips have unchanged VM FAIL, never claimed PASS. |
| Two-row ASan R2 | Actual execution, 14 PASS per side, no SKIP or empty selection. |
| Full sanitizer completion | All 36 jobs actually PASS, 18 per side; 667 base and 741 candidate PASS test results. No SKIP, failed or missing selected row. |
| Census | All 1187 records unchanged; 52 unfinished programs on both sides. |
| Sentrux | Owner accepted internal 6355 to 6354 and runtime 4619 to 4616. Raw FAIL scores retained; no scoring change. |

Sanitizer completion executes the exact carrier preflight and every original
recipe, retaining `--expect`, plus two lifecycle controls. All 36 jobs are
serial; the owner approved unlimited address space for the 22 sanitizer jobs,
while the other 14 retain 8 GB. Go2, timeouts, clean pinned snapshots and exact
stdlib paths remain required. Launch had no external SURGE_BIN or inherited
ASan/UBSan/TSan/LSan options. Preflight proves both clean execution and detection
of planted errors. Empty selection and skipped required rows fail the judge.

Three earlier READ differences concern rebuilt images, emitted IR/input hashes
and binary stat data in rows that could not execute ASan under 8 GB. Their raw
metadata is preserved. All three named tests now actually PASS on both sides:
`TestRuntimeV2BigfloatFromF64ExactUnderAddressAndUndefinedSanitizers`,
`TestRuntimeV2NumericEmittedLifecycleUnderAddressAndUndefinedSanitizers`, and
`TestRuntimeV2LifecycleTaskFreeIsOneObservation`. The image and stat differences
record rebuilt binaries. Every LLVM module emits the runtime declaration list
(`emitRuntimeDecls`, `internal/backend/llvm/emit.go`); adding
`rt_channel_send_tracked` changes the module digest even for the unchanged
numeric source. These hashes were not treated as equal. The missing execution
was obtained, with no other sanitizer reports from the positive task lifecycle
controls.

## Scope and remaining debt

Only the reproduced parked-MOVED-send subcase of RV2-DEBT-244 is closed.
Received-float binding VM3301 and the separate sync-channel composite report
remain open; the whole debt stays Open. RV2-DEBT-121's 17-byte identity-cast leak
also remains Open. Allocation baseline agreement is not strict-zero Memcheck.

The optional review limitation remains explicit: the C ack-window stand does
not itself exercise yield/scheduler resumption. It is not claimed as forced
end-to-end evidence for that interval.

Relative packet acceptance does not mean every repository test is green.
Existing project FAILs and exact reasons remain recorded. This landing does
not close Epic 22, D2, or all Runtime V2 debt, and does not integrate G5/SC-C.
Ryzen is permanently retired; the local verification profile was explicitly
authorized. Whole-D2 timing and closeout remain separate work.

## Publication

T+C source commit: `434016ea844fa8b70c89620d2734abe7793c1e9f`.
The committed source tree exactly equals the frozen census candidate tree.
Documentation changes only this report and the RV2-DEBT-244 note.
Sanitizer verdict is PASS. No surviving process in the measurement session and
no nonempty harness survivor record remained after completion.
Only authorized remote target: `validation/step7-d2-on-d1`; publication is
recorded in the artifact manifest after verifying the exact remote head.

Raw evidence and the final manifest are retained under
`~/.cache/surge-artifacts/step7-return-source/g5-capture-study/`.
