# N-INDEX-VIEW packet

## Rule and soundness

`returnOriginBody.index` evaluates the container and index before applying the new rule, so their effects and exits remain in ordinary flow. At the selected-container refusal it now requires all of:

```go
if reason == "index requires its selected container transfer" && b.erasedType(u.Sema.ExprTypes[id]) {
    selected, present := u.Sema.IndexSymbols[id]
    if present && selected.IsValid() {
        fn, selectedReason := b.analyzer.selectedCallableFunction(u, selected)
        if selectedReason == "" && returnOriginBytesViewReader(fn) {
            out.value = returnOriginValueOf()
            return out, nil
        }
    }
}
```

The selection lookup is the original `IndexSymbols[id]` declaration identity, not the method's spelling. `returnOriginBytesViewReader` then certifies the original core intrinsic, exact receiver/type declaration, signature, and `uint8` result. `erasedType` independently requires the result to be reference-free and not a storage-loan carrier. Therefore only a byte load becomes origin-free. Any selected result capable of carrying a reference or storage loan still reaches the existing named `index requires its selected container transfer` refusal. This matches the language contract that indexing is a selected `__index` call (`docs/LANGUAGE.md:911-920`), that `BytesView[i]` is `uint8` (`docs/LANGUAGE.md:1599-1607`), and the owner ruling that lack of source proof is not proof of no dependency (`docs/RUNTIME_V2.md:641-646`).

## Rows and canaries

`TestReturnOriginSelectedContainerIndex` contains two clean rows: a byte read through a borrowed view and a byte read through an owned `BytesView` parameter. Its canaries keep a named refusal for a user `__index -> &int`, keep that refusal when indexing a locally owned container, and require SEM3139 (or another named refusal) when a `BytesView` of a local string is returned.

## Counterfactuals

* Reverting only the transfer: `go test ./internal/driver -run '^TestReturnOriginSelectedContainerIndex/(borrowed|owned)' -count=1` exited 1. Both clean rows regained `index requires its selected container transfer` (and dependent unproved-result rows).
* Replacing the certificate with an unconditional fresh result for every selected-container type: `go test ./internal/driver -run '^TestReturnOriginSelectedContainerIndex/(user|local_reference)' -count=1` exited 1. Both reference canaries lost the named selected-index refusal and were reported clean in their function ranges.

Full outputs were saved during development as `/tmp/n-index-cf-revert.txt` and `/tmp/n-index-cf-fresh.txt`; they are not repository artifacts.

## Verification

Commands and final results are recorded below before commit:

- `go build -o /tmp/surge ./cmd/surge`: PASS.
- `go test ./internal/sema -count=1`: PASS.
- `go test ./internal/driver -run '^TestReturnOriginSelectedContainerIndex$' -count=1`: PASS.
- `go build ./...`: PASS.
- `go vet ./...`: PASS.
- `go test ./internal/sema ./internal/driver`: WARNING: both base and after runs timed out in the pre-existing driver suite after 600 seconds (sema passed in both). The after failing-name set additionally contained `TestReturnOriginUnitsUseOwnedArtifacts` and its `import_alias` and `module` leaves; this is a timeout/order deviation, not an intended change, so the requested exact-delta condition was not verified.
- `SURGE_STDLIB=$PWD /tmp/surge run --timeout 120 --backend {vm,llvm} <program>` plus `cmp` against each `.out`: PASS for `strings_basic.sg` and `strings_rope.sg` on both backends.
- Golden census: UNVERIFIED. The requested `origin/cloud/d2-unfinished-plan` fetch could not run because this checkout has no `origin` remote, and the plan's census tools are not present in the worktree. No substitute census result is claimed.

## Census and test delta

The exact plan census delta remains unverified for the environment reason above. The targeted rows prove the two intended expressions move from unfinished to complete, and the counterfactual pins that movement. No claim is made here about the complete golden corpus until the absent census tooling is supplied.

The full base test command was `go test ./internal/sema ./internal/driver 2>&1 | tee /tmp/n-index-base-tests.txt`. It timed out after 600 seconds. The post-change command was identical and also timed out. `rg '^--- FAIL:|^    --- FAIL:' ... | sed ... | sort -u` followed by `diff -u /tmp/base-fails.txt /tmp/after-fails.txt` showed exactly three additional names after the change: parent `TestReturnOriginUnitsUseOwnedArtifacts` and leaves `import_alias` and `module`. Since each timed run stopped in a different in-flight test, this is reported as a deviation rather than treated as a semantic delta.

## Runtime runs

Both programs exited successfully on VM and LLVM, and all four captured stdout files matched their checked-in `.out` files byte-for-byte with `cmp`.

## RV2-DEBT-365 ledger sentence

N-INDEX-VIEW certifies only the declaration-identified core `BytesView.__index` byte read as origin-free, moving `strings_basic.sg` and `strings_rope.sg` out of unfinished while reference- or loan-bearing selected indexes remain refused.

## Unverified

The remote plan and its census scripts could not be fetched because this repository has no configured remote. The complete census, a successful full driver-suite run, and an exact full-suite failure-name delta are therefore not verified. No runtime or language question for the project owner arose from the implementation.
