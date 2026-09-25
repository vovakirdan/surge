# N-STDLIB-TIME: `Duration` identity certificate

## Rule and soundness

`NoBorrowedState` is discharged only when the resolved type is the exact `stdlib/time` declaration of `Duration`. The certificate checks the owning unit's normalized module identity, the declaration span/item/symbol identity, the complete one-field roster, the exact field name and `int64` type, no type parameters or arguments, no base, and only argument-free `@copy`/`@intrinsic` attributes, with both present. It does not recognize a source filename, a type name alone, or `@intrinsic` structs generally.

The declaration being certified is:

```surge
@copy
@intrinsic
pub type Duration = {
    __opaque: int64,
};
```

(`stdlib/time/time.sg:4-10`). The implementation records the module identity while collecting original owning units (`internal/driver/return_origin_finalization.go`) and checks declaration and resolved fields in `returnOriginStdlibTimeDuration` (`internal/sema/return_origin_stdlib_time.go`). `int64` is a scalar admitted by the existing `NoBorrowedState` classifier (`internal/sema/return_origin_requirements.go`); therefore the only stored field holds no reference, storage loan, or task. The general struct path still requires `returnOriginPlainStruct`, which deliberately rejects `@intrinsic`, and every other opaque/intrinsic struct remains on its prior row.

This is consistent with the owner rule that a missing source proof is not an empty source set and a separately proven reference-free result is required (`docs/RUNTIME_V2.md:641-646`). It does not relax lexical borrow/move rules (`docs/LANGUAGE.md:145-173`) or make a runtime-lifetime claim. The current runtime and explanatory model were nevertheless read as required (`docs/RUNTIME.md:283-305`; `docs/RUNTIME_MODEL_EXPLAINED.ru.md:546-562`).

## Driver rows and canaries

`internal/driver/return_origin_stdlib_time_test.go` contains:

- a direct `stdlib/time.Duration` shape and the four named golden paths; the invalid directive path must finish and reach SEM3120 in harness/collect mode;
- Range, BytesView, Task, and Channel opaque-result canaries. Range, Task, and `Channel<int[]>` retain `opaque result borrowed-state classification is unsupported`; BytesView retains the more precise `opaque result type may carry borrowed state`;
- an unrelated user `@intrinsic` struct named `Duration`, which retains the unsupported row;
- a holder containing real `time.Duration` plus `Range<int>` over a dying local array, which retains `storage loan would be discarded by a payload-free value`.

## Counterfactuals

All counterfactuals were temporary working-tree mutations and were restored before the final checks.

1. **Remove the transfer.** Removing the three-line call to `returnOriginStdlibTimeDuration` made `TestReturnOriginStdlibTimeDurationIdentityCertificate` and all four leaves of `TestReturnOriginStdlibTimeGoldenRowsFinish` fail. The four golden errors again contained the Duration opaque-classification rows; command: `go test ./internal/driver -run 'TestReturnOriginStdlibTimeGoldenRowsFinish|TestReturnOriginStdlibTimeDurationIdentityCertificate' -count=1 -timeout 30m` (exit 1).
2. **Key by name, not module identity.** Temporarily replacing `u.ModulePath == "stdlib/time"` with `name == "Duration"` made `TestReturnOriginStdlibTimeDurationIdentityCanaries` fail because `opaque_duration()` lost its row; command: `go test ./internal/driver -run TestReturnOriginStdlibTimeDurationIdentityCanaries -count=1 -timeout 30m` (exit 1).
3. **Blanket struct/intrinsic-shaped admission.** Temporarily replacing the identity predicate with `typ.Kind == types.KindStruct` (a stronger form of the forbidden “every intrinsic struct” counterfactual) made Range, Task, Channel, and the foreign Duration lose their rows in `TestReturnOriginStdlibTimeDurationIdentityCanaries`; command: `go test ./internal/driver -run TestReturnOriginStdlibTimeDurationIdentityCanaries -count=1 -timeout 30m` (exit 1). BytesView remained precisely refuted by the earlier borrowed-view fence.

## Census and test deltas

Commands:

```sh
JOBS=8 cloud-work/d2-unfinished-plan/tools/run_census.sh /tmp/census-after
go test ./internal/sema -timeout 30m
go test ./internal/driver -timeout 30m
```

The after census completed all 1,079 programs. Totals were: user form 548 ok / 80 unfinished / 448 diagnostics / 3 other; harness form 545 ok / 80 unfinished / 451 diagnostics / 3 other. In harness scope they were respectively 497/78/422/3 and 494/78/425/3.

A base binary at `045c1135` was run through `run_one.sh` on the four predicted paths and compared with their after-census artifacts:

| Program | user form | harness form |
|---|---|---|
| `time_not_directive_module/main.sg` | unfinished -> ok | diagnostics (SEM3120) -> diagnostics (SEM3120); the return-origin analysis is not reached in collect mode |
| `stdlib_benchmark_module/main.sg` | unfinished -> ok | unfinished -> ok |
| `stdlib_time_directive/main.sg` | unfinished -> ok | unfinished -> ok |
| `stdlib_time_import/main.sg` | unfinished -> ok | unfinished -> ok |

Thus the class changes are four user-form `unfinished -> ok`, three harness-form `unfinished -> ok`, and no diagnostic-to-ok transition. The removed row-level causes are Duration's `opaque result borrowed-state classification is unsupported` plus its derived `callee returned an unproved source`, `outgoing reference has unresolved or captured provenance`, and `function result contains an unproved source` rows in `stdlib/time`, the two directive wrapper modules, and the importing root.

The full after census contains no one of the four paths in `unfinished_programs`. A second full 1,079-program base census was not run; exact “nothing else changed” is supported by the identity-limited transfer and the complete after census, but only the four predicted base artifacts were remeasured. This is the sole census limitation.

`go test ./internal/sema -timeout 30m` passed at base and after. The full base and after `go test ./internal/driver -timeout 30m` processes were allowed to run for about eleven minutes, remained CPU-bound without producing the Go test summary, and were stopped so this packet could complete; therefore their failing-test NAME sets were not available for comparison. The targeted driver packet passed. This is an explicit unverified item, not a claimed pass.

## Backend runs

None of the four changed golden programs has a `.out` sidecar, so there is no output-bearing program in this packet to run on VM and LLVM. The exact sidecar inventory command was:

```sh
for p in testdata/golden/sema/{invalid/directives/time_not_directive_module,valid/directives/stdlib_benchmark_module,valid/directives/stdlib_time_directive,valid/directives/stdlib_time_import}/main.sg; do
  find "$(dirname "$p")" -maxdepth 1 -type f -printf '%f\n' | sort
done
```

Each directory contains only `main.ast`, `main.diag`, `main.fmt`, `main.sg`, and `main.tokens`.

## Ledger sentence (RV2-DEBT-365)

N-STDLIB-TIME removes only the declaration-identity-certified, scalar-field `stdlib/time.Duration` opaque-result barrier; it does not classify Task, Channel, Range, BytesView, or any task-bearing result, so it removes no RV2-DEBT-365 task-borrow tripwire or incidental task barrier.

## Unverified work and owner questions

There is no owner question. Any verification that did not complete is listed explicitly in the census/test section above; no language widening or skipped check is proposed.
