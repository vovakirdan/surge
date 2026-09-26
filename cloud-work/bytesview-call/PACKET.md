# BytesView call-result borrow packet

## Rule

Owner ruling Q-BV2 = A is implemented at the single question that decides whether an implicit `&` argument loan ends when a call returns:

```go
// BytesView is marked as a borrowed view by declaration identity.  Asking
// the type-shape question here also finds a view nested in an aggregate and
// means that the next non-reference borrowing type needs only that mark.
// Array windows deliberately have no such mark: their runtime header retains
// the base allocation, so returning one does not extend an argument loan.
if returnOriginTypeShape(tc.types, result, nil) == returnOriginCarriesRef {
    return
}
```

This is deliberately a type-property query, not a `BytesView` name check and not a core-callee check. `returnOriginTypeShape` already recursively classifies references, unions, tuples, structs and other aggregate components, and consults `Interner.IsBorrowedView`; the core declaration identity marks `BytesView` today. A future non-reference borrowing type therefore joins the rule by acquiring the same semantic mark. An `int[]` window has no borrowed-view mark and remains reference-free for this question because its runtime representation retains its allocation.

The former `viewedStringBorrowOutlivesCall` core-producer exception was removed. Both free-function arguments and method receivers now ask the same result-type question in `dropImplicitBorrowForRefParam`. When the actual is itself an existing reference and the result carries a non-reference borrow, the checker records a child shared loan of the referent. Assignment through `&mut` expands back to the referent, ignores only the parent exclusive permission that authorizes the write, and still rejects that child shared loan.

## Rows

Command:

```text
SURGE_STDLIB=$PWD go test ./internal/driver -run 'TestBytesViewReturnedByCall' -count=1 -timeout 10m
```

Result: PASS.

Refused with the established diagnostics:

| Row | Result |
|---|---|
| user `view(&s)` then move `s` | `SEM3020` |
| user `view(&s)` then reassign `s` | `SEM3019` |
| user `view(s)` through `s: &mut string`, then `*s = ...` | `SEM3019` |

Accepted controls:

- view is scoped to an earlier block;
- an `int` passed through `&int` is copied/read without creating a view;
- the view is derived from the post-move owner;
- calls returning plain `string` and plain `uint` do not retain their `&string` loan;
- returned array-window twins remain accepted for move, reassignment, and assignment through `&mut`.

## Counterfactuals

A full revert was exercised by copying the new row file into a detached `aaf96f7` worktree and running:

```text
SURGE_STDLIB=$PWD go test ./internal/driver -run 'TestBytesViewReturnedByCall' -count=1 -timeout 10m
```

Result: FAIL, as required. All three refusal subtests (`move`, `reassignment`, and `assignment_through_mut_ref`) reported that the checker accepted the unsafe program; the process exited 1. The controls are in a separate test and remain green.

The `&mut` implementation half was then removed while leaving the general call-result rule in place: the child reborrow block in `dropImplicitBorrowForRefParam` and the referent conflict check in `handleAssignment` were removed. The same command exited 1 with **only** `assignment_through_mut_ref` red; `move` and `reassignment` remained green. The failing row said the checker accepted `*s = "new"`. Restoring the two pieces returns all three rows to green.

## Census

The requested `cloud-work/d2-unfinished-plan/tools` directory is absent from base `aaf96f7` and from this checkout (`find /workspace -path '*/d2-unfinished-plan/tools/*'` returned no paths). Therefore the prescribed base/after corpus census over `testdata/golden`, `stdlib`, `core`, `showcases`, and `benchmarks` could not be reproduced with those tools. This is explicitly unverified rather than replaced by a different census. There are 1,164 `.sg` files under those roots.

Manual source census confirms `stdlib/json/parser.sg` stores `BytesView` in `ViewParser`; no stdlib/core source was changed. The full driver suite builds against the real core/stdlib and is the available acceptance check for those modules.

## Build/test delta

Base: `aaf96f7`. Commands were run with `SURGE_STDLIB=$PWD` from each worktree:

```text
SURGE_STDLIB=$PWD go build ./...
SURGE_STDLIB=$PWD go vet ./...
SURGE_STDLIB=$PWD go test ./internal/sema ./internal/driver -timeout 30m
```

`go build ./...` and `go vet ./...` were clean both before and after.

The base full test run had 33 failing test-name entries, all caused by the line's existing unfinished return-origin authority. The first after run had those same 33 plus the new `assignment_through_mut_ref` row, which exposed the missing write-through half and led to its implementation. The next full run had the same base 33 plus `TestSharedReferenceRebind`; its two event-shape assertions exposed that conflict probing must expand to the referent without changing the successful write event's historical place/note. That was fixed, and the exact regression test plus the complete sema suite were rerun clean. The full driver suite was not rerun a third time after this event-only correction; the new driver family and all pre-existing BytesView driver rows were rerun clean. Thus no new failing name remains in the suites that cover the final edit, while the final full combined failing-name set is inferred rather than re-measured.

## Ledger row text

| RV2-DEBT-388 follow-up (Q-BV2=A) | A call result whose type carries a borrow now keeps every implicit `&` argument loan live for the result's lexical lifetime. The decision is made from recursive result type shape, including the declaration-identity `IsBorrowedView` mark, rather than callee identity or the spelling of a reference. Thus a user function returning `BytesView` blocks moving or reassigning its source, including assignment through `&mut string`; established `SEM3020`/`SEM3019` diagnostics are reused. Array-window call results remain unchanged because their retained runtime base is not marked as a borrowed view. Driver rows cover three refusals, lifetime/copy/re-derive/plain-result controls, and three accepted window twins; revert and mutation-half counterfactuals are recorded in this packet. |

## Unverified

- The prescribed corpus census is unverified because its named tool directory is not present in the supplied base or workspace.
- No VM/Valgrind rerun was requested after the checker refusal; the packet relies on the coordinator's measured runtime evidence quoted in the task.
- The final full combined driver+sema failing-name set was not re-measured after the event-only correction described above; exact affected regression tests, full sema, and focused driver families were re-measured.

## Owner questions

None.
