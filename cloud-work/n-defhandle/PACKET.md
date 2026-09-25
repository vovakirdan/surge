# Packet N-DEFHANDLE: return-origin for the default value of a core runtime handle

Base `045c113` (`origin/validation/step7-d2-on-d1`, contains `045c1135`). Branch `cloud/n-defhandle`.
This is step 3 of `cloud-work/d2-unfinished-plan/PLAN.md`. It answers two reasons:
- "opaque result borrowed-state classification is unsupported"
- "generic opaque use requires its type-dependent effect transfer"

It answers them only where the value is the default of a core runtime handle.

Runtime claims are checked against these documents, and never against `docs/CONCURRENCY.md` (v1):
- `docs/RUNTIME_V2.md`
- `docs/RUNTIME_MODEL_EXPLAINED.ru.md`
- `docs/runtime-v2-epics/DEBT.md` (rows 365 and 368)
- `docs/RUNTIME.md`

None of the four mentions the default of a runtime handle. The rule rests on the owner ruling of
2026-09-25, as recorded in the VMDH packet (`cloud/vm-default-handle`, `cloud-work/vm-default-handle/PACKET.md`),
on the LLVM code, and on runs (section 5).

## 1. The rule

One change, in the **Defaultable** branch of the requirement walk
(`internal/sema/return_origin_requirements.go`, `returnOriginTypeView.requirement`):

```go
		if kind == returnOriginDefaultable {
			if elem, array := returnOriginDefaultArrayElement(in, id); array {
				return walk(view, elem, active)
			}
			switch typ.Kind {
			case types.KindStruct:
				// The default of a core runtime handle (Task, Channel, Range) is the
				// null handle: it names no runtime object and so holds no borrow,
				// whatever its payload type (owner ruling 2026-09-25; LLVM emits null in
				// emitDefaultValue, the VM follows with packet VMDH). The family is the
				// one core marked by declaration identity (MarkRuntimeHandleType).
				// Defaultable only: a handle obtained any other way still meets
				// NoBorrowedState below, which is R-i's fence (RV2-DEBT-365).
				if in.IsRuntimeHandleType(id) {
					return returnOriginRequirements{}
				}
				info, found := in.StructInfo(id)
				if !found || info == nil || !returnOriginPlainStruct(view.owner, info) {
					return unknown
				}
```

It adds 10 lines, of which 3 are code. Nothing else in the compiler changes.

### Why this answers only "the default of a runtime handle holds no borrow"

**What Defaultable means.** `returnOriginDefaultable` is the requirement on exactly two kinds of
value, both read from code:
- the result of the certified `default<T>()` intrinsic (`core/intrinsics.sg:777`). It is recorded
  by `requireDefaultable` in `return_origin_backing_intrinsics.go:233`, and a finalized use is
  checked by `checkBackingIntrinsicUse`, `:247-266`.
- a declaration without an initialiser, `let x: T;`, which HIR lowers to `default::<T>()`
  (`defaultInitValue`, `return_origin_synthesized_uses.go:20-27`).

A summary's Defaultable atoms are rebased onto the concrete type argument (`rebase`, which keeps
the atom's kind). No other value is ever asked Defaultable.

**Handles are keyed by identity, not by name.** `IsRuntimeHandleType` holds only for the family
recorded by `MarkRuntimeHandleType` (`internal/types/interner.go:248-273`):
- The key is `(name, Decl span)` of the struct declaration.
- `sema/type_decl_core.go:85-100` (`isRuntimeHandleTypeDecl`) marks a declaration only when it is in a core runtime module
  (`isCoreRuntimeModulePath`), is named `Task`, `Channel` or `Range`, and its symbol carries
  `SymbolFlagBuiltin`. `driver/diagnose_modules.go:357-380` (`markRuntimeHandleTypes`) applies the same filter to core
  exports.
- A user struct named `Task` has another `Decl`, so it is not in the family.

**Other handle values are unaffected.** A handle obtained any other way is never asked
Defaultable:
- a call's result, `pop()`, `await`, a payload, a capture.
- Those values meet **NoBorrowedState**, or their own transfer.
- The NoBorrowedState branch, the `switch` below the Defaultable block, is unchanged. It still
  walks a handle's type children (`returnOriginTypeChildren`), which is how a `Task<Task<int>>`
  payload and a borrowing task payload stay refused.

**The value really holds nothing.**
- LLVM: `internal/backend/llvm/emit_intrinsics_default.go:67-73` returns `"null", "ptr"` for every
  runtime handle, with the comment "Runtime-owned handles use a null pointer as their
  uninitialized sentinel".
- VM: the VMDH packet (`8f2ed40` on `cloud/vm-default-handle`) makes the VM build the same null
  (`internal/vm/intrinsic_default.go`, `runtime_handle_null.go`).
- A null names no runtime object, so it holds no frame's storage and no loan.

**`Mutex` follows.** `core/sync.sg:3-6` is `@copy pub type Mutex = { gate: Channel<nothing> };`.
It is a plain struct (`returnOriginPlainStruct` allows `@copy`), so the walk reaches its one
field, a `Channel`, and gets TRUE. `RwLock` is `@intrinsic` but not a marked handle, so it stays
unproven (canary `intrinsic_rwlock_default`).

### Why R-i is untouched

- **R-i's fence** (RV2-DEBT-365) is "NoBorrowedState on a joined Task payload; removed by a
  Task-payload certificate or by Wave D4b marking `Task` counted".
- **This change lives entirely inside `if kind == returnOriginDefaultable`.** A NoBorrowedState
  question over `Task<T>` takes the unchanged branch.
- **Measured:**
  - R-i's tripwire row `TestAnalyzeTaskAwaits/borrowing_task_payload_stays_refused` passes.
  - The counterfactual in section 3.3 moves the same line outside the Defaultable guard. That row
    then goes red, together with `task_payload_stays_refused`. So the guard is exactly what keeps
    R-i's fence.
- **Removed fences:** none. The rule removes no refusal of a handle value except its default.

## 2. Rows (`internal/driver/return_origin_handle_defaults_test.go`)

Every row is a ROOT program against the real core, in the existing `analyzeOriginRoot` harness.

**`TestReturnOriginHandleDefaultsFinish`** requires the whole analysis to have **no Pending row at
all**, in any unit, core included, and no diagnostic in the source:

| Row | Shape |
|---|---|
| `popped_task_drain` | t14 / `task_container_suspend_safe`: `while q.__len() != 0 { let t = q.pop().safe(); … t.await() … }` |
| `joined_clone_drain` | `task_clone_borrow_joined_then_returned::drained_then_clone_returned` (a borrowing task, cloned, drained through `pop().safe()`) |
| `declared_defaults` | `let ch: Channel<int>; let r: Range<int>; let t: Task<int>; let m: Mutex;` |
| `returned_defaults` | `let t: Task<int>; return t;` · `return cs.pop().safe()` from an empty `Channel<int>[]` · `Option<Range<int>>` `nothing.safe()` |

`for_in_reads_then_pop_drains` and `task_container_drain_with_nested_loop_break` are the same
`pop().safe()` over a `Task<int>[]` inside loops. The census covers them (section 4).

**`TestReturnOriginHandleDefaultCanariesKeepTheirRows`**: the exact Pending rows inside the source.

| Canary | Rows after (exact) | Rows at base |
|---|---|---|
| `borrowing_range_out_of_container`: `rs.push(xs.__range()); return rs.pop().safe();` over a local `xs` | G6 `storage loan would be discarded by a payload-free value` at `rs.push(xs.__range())` | the same G6 row **plus** the Defaultable and unsupported rows of `safe()` |
| `borrowing_range_through_option`: `Some(xs.__range())` then `.safe()` | G6 at `Some(xs.__range())` | the same G6 **plus** the Defaultable rows |
| `task_out_of_borrowed_container`: `fn(q: &mut Task<int64>[]) -> Task<int64> { return q.pop().safe(); }` | G6 at `q.pop().safe()` | `unsupported`, `callee returned…`, `outgoing reference…`, `function result…` and the two core rows (**row change, see section 8**) |
| `task_payload_await_reaches_r_i`: `t.await()` over `Task<Task<int>>` | `unsupported` + `callee returned an unproved source` at `t.await()` | identical |
| `intrinsic_rwlock_default`: `let l: RwLock; return l;` | `default result is not proven Defaultable` + `generic use lacks its original typed operation` | identical |
| `intrinsic_file_default`: `let f: File; return f;` | the same two rows | identical |

**`TestReturnOriginHandleDefaultTaskCanariesStayRefused`**: the checker refuses these before the
analysis runs. This is a full `DiagnoseWithOptions` run with the real stdlib.

| Canary | Code |
|---|---|
| `borrowing_task_returned`: `let l: string = "abcdef"; return worker(&l);` | **SEM3139** "cannot return this task: it borrows 'l'…" |
| `borrowing_task_through_option`: `Some(spawn worker(&l))` then `o.safe()` | SEM3021 "a task still borrows 'l' at this return" (the task check) |

Other canary forms were probed with `surge diag` and give the same code on both binaries. A task
from a borrowing call pushed into a local `Task<int64>[]` and returned from a drain loop, or pushed
through a `&mut` parameter, gets SEM3021 and SEM3107.

## 3. Counterfactuals (run)

Command for each: `go test -count=1 ./internal/driver -run 'TestReturnOriginHandleDefault' -v`.
Leaf lines are shown with the harness's own messages, abbreviated.

### 3.1 Revert (`return_origin_requirements.go` restored to `045c113`)

```
--- FAIL: TestReturnOriginHandleDefaultsFinish
    --- FAIL: .../popped_task_drain      unexpected "opaque result borrowed-state classification is unsupported" at core/intrinsics.sg 38263-38270,
                                         "generic opaque use requires its type-dependent effect transfer" at core/option.sg 260-274, ...
    --- FAIL: .../joined_clone_drain     (the same core rows, and the callee and outgoing-reference rows at x = tasks.pop().safe())
    --- FAIL: .../declared_defaults      "default result is not proven Defaultable" and "generic use lacks its original typed operation" at each of the 4 lets
    --- FAIL: .../returned_defaults      the Defaultable rows, the core rows, and 7 derived rows
--- FAIL: TestReturnOriginHandleDefaultCanariesKeepTheirRows
    --- FAIL: .../borrowing_range_out_of_container   extra base rows (unsupported, callee, outgoing, function result)
    --- FAIL: .../borrowing_range_through_option     extra base rows
    --- FAIL: .../task_out_of_borrowed_container     missing G6 at "q.pop().safe()"; base rows instead
    --- PASS: .../task_payload_await_reaches_r_i
    --- PASS: .../intrinsic_rwlock_default
    --- PASS: .../intrinsic_file_default
--- PASS: TestReturnOriginHandleDefaultTaskCanariesStayRefused (both leaves)
```

All four finish rows go red. The three canaries pinned to their exact after-rows go red because
the base carries the extra Defaultable rows. The R-i, `@intrinsic` and task-check canaries are
unchanged by the revert, as they must be.

### 3.2 Widen to every `@intrinsic` struct

A temporary helper, deleted after the run, read the struct declaration's attributes. The same
`return` then fired when any attribute was `intrinsic`, instead of `IsRuntimeHandleType`:

```
--- PASS: TestReturnOriginHandleDefaultsFinish (4 leaves)
--- FAIL: TestReturnOriginHandleDefaultCanariesKeepTheirRows
    --- FAIL: .../intrinsic_rwlock_default   missing "default result is not proven Defaultable" at "let l: RwLock;": []
    --- FAIL: .../intrinsic_file_default     missing "default result is not proven Defaultable" at "let f: File;": []
    (the other 4 canaries PASS)
--- PASS: TestReturnOriginHandleDefaultTaskCanariesStayRefused
```

`RwLock` (`core/intrinsics.sg:803`) and `File` (`:39`) are `@intrinsic` but not marked handles.
No ruling covers their defaults, and the widened rule proves them anyway. These canaries were
added for this counterfactual.

### 3.3 Apply the same rule to NoBorrowedState as well (outside the Defaultable guard)

Command: `-run 'TestReturnOriginHandleDefault|TestAnalyzeTaskAwaits$'`.

```
--- FAIL: TestReturnOriginHandleDefaultCanariesKeepTheirRows
    --- FAIL: .../task_out_of_borrowed_container   missing G6 at "q.pop().safe()": [] (accepted outright)
    --- FAIL: .../task_payload_await_reaches_r_i   missing "unsupported" and "callee returned" at "t.await()": []
--- FAIL: TestAnalyzeTaskAwaits
    --- FAIL: TestAnalyzeTaskAwaits/task_payload_stays_refused
    --- FAIL: TestAnalyzeTaskAwaits/borrowing_task_payload_stays_refused     <- R-i's tripwire
```

That is the unsound variant. R-i's tripwire catches it, and so does this packet's own canary.

## 4. Golden census (plan tools, base vs after)

The tools are `run_one.sh` and `classify.py` from `cloud-work/d2-unfinished-plan/tools/`, over all
1079 `testdata/golden` programs, in both forms, with 4 jobs:

```
export ROOT=$PWD OUTDIR=<dir>/<side> SURGE=<surge-side>
find testdata/golden -type f -name '*.sg' | LC_ALL=C sort > <dir>/files.txt   # also copied to <side>/files.txt
xargs -a <dir>/files.txt -P 4 -I{} bash tools/run_one.sh {} > <side>/status.tsv
python3 tools/classify.py $ROOT <dir>/<side> <dir>/<side>/census.json <side>
```

| | ok | unfinished | diagnostics | other |
|---|---:|---:|---:|---:|
| user form, base | 544 | 84 | 448 | 3 |
| user form, after | **549** | **79** | 448 | 3 |
| harness form, base | 542 | 83 | 451 | 3 |
| harness form, after | **547** | **78** | 451 | 3 |

Class changes, identical in both forms: **exactly the five predicted programs**, each `unfinished → ok`:
- `sema/valid/concurrency/task_clone_borrow_joined_then_returned.sg`
- `sema/valid/concurrency/task_container_suspend_safe.sg`
- `sema/valid/ownership/for_in_reads_then_pop_drains.sg`
- `sema/valid/task_container_drain_with_nested_loop_break.sg`
- `vm_async_suite/t14_loop_join.sg`

Raw output comparison for the other 1074 files: identical in both forms, with two exceptions.

- **Deviation, named: `sema/valid/concurrency/task_created_in_current_scope.sg`** stays unfinished,
  but loses 3 of its 4 rows.
  - The rows lost are `default result is not proven Defaultable` and `generic use lacks its
    original typed operation` at `let mut slot: Task<int>;` (0:543-567), and the derived
    `outgoing reference has unresolved or captured provenance` at 0:610-617.
  - Its remaining row is the capture row at the `async` block (0:579-768), "an `async` or
    `blocking` block that captures a value which can hold a reference, a storage loan or a task
    needs its capture origin". That is R-b(body)'s fence, and it is unchanged. The program is one
    of owner question 2's four.
  - The rows lost are exactly the handle-default rows, so this is the rule doing its job on a
    program the plan did not list: in the prototype this program also had the capture row, and
    it was never predicted to finish.
- **`crossing/block04/invalid/movable_negative_nested_unmarked_user_field.sg`** differs in
  line order only (user form). The same known multi-line ordering noise was seen in earlier packets.

## 5. t14 runs and handle defaults at run time

`testdata/golden/vm_async_suite/t14_loop_join.sg` is the only one of the five with an `.out` file
(`sum=6`).
- After the change it runs to that output **20 of 20 times** on `--backend vm` and **20 of 20
  times** on `--backend llvm`, with exit 0 each time.
- On the base it does not run at all: `run` stops with "return-origin analysis unfinished".

**Does it reach a handle default at run time? No.**
- **The code.** `ts.pop()` is called only under `while ts.__len() != 0:uint`, so `safe()` always
  takes the `Some` arm and never calls `default::<Task<int>>()` (`core/option.sg:12`).
- **The run.** This base does not have VMDH, so a reached default still panics on the VM. The probe
  `let mut q: Task<int>[] = []; let t = q.pop().safe();`:
  - diagnoses clean after this change
  - panics on the VM with `panic VM1999: storage: type#1508 has 1 members but 0 layout offsets at core/option.sg:12:18`
  - prints `reached` and exits 0 on LLVM

  The 20 clean VM runs of t14 therefore show that the default is not reached on the VM.

**Until VMDH lands, a program that does reach a handle default** now builds, where the gate used to
refuse it, and then panics VM1999 on the VM. That fails closed, as a panic and not a memory error.
The brief says VMDH lands first. After VMDH, the VM builds the null handle as LLVM does.

## 6. Suites (base vs after)

`go build ./...` and `go vet ./...` pass (exit 0).

Command: `go test -count=1 -timeout 55m ./internal/sema ./internal/driver -json`, run in a worktree
at `045c113` and in the work tree. Failing names are the `"Action":"fail"` events that carry a `Test`.

| | internal/sema | internal/driver | failing test entries | passing test entries |
|---|---|---|---:|---:|
| base `045c113` | ok (4.1 s) | FAIL (850 s) | 55 | 2668 |
| after | ok (3.9 s) | FAIL (831 s) | 55 | 2683 |

- **Failing-name sets:** after minus base is empty, and base minus after is empty. The 55 are the
  known base failures.
- **Test-entry difference:** exactly this packet's 15 new entries (3 tests and 12 leaves), all
  passing. No entry disappeared.
- **Task and tripwire tests, after:**
  - `TestAnalyzeTaskAwaits`: 10/10 pass, including `borrowing_task_payload_stays_refused`, R-i's
    tripwire
  - `TestAnalyzeTaskBlocks`: 12/12 pass
  - `TestH2Tripwire*`: 40/40 pass
  - `TestTaskCheck*`: 247/247 pass

## 7. Ledger sentence for RV2-DEBT-365

> 2026-09-25, N-DEFHANDLE: the Defaultable requirement now holds for the default of a core runtime
> handle (the family `MarkRuntimeHandleType` records by declaration identity: `Task`, `Channel`,
> `Range`, and through its field `Mutex`), because by the owner ruling of 2026-09-25 that default
> is the null handle, which holds nothing. The change is inside the Defaultable branch only:
> NoBorrowedState, R-i's fence on a joined Task payload, is untouched, and
> `borrowing_task_payload_stays_refused` stays green. Moving the rule outside the Defaultable
> guard turns that row and `task_payload_stays_refused` red. The packet frees 5 golden programs
> (t14 among them). No barrier listed in this row is removed. Until VMDH lands, a reached handle
> default panics VM1999 on the VM, which fails closed.

## 8. Owner question (coordinator)

**Q-DH1. A canary refused only by the Defaultable row is now refused by G6.**

- **Context.** `fn pop_task_param(q: &mut Task<int64>[]) -> Task<int64> { return q.pop().safe(); }`
  pops a handle out of a borrowed container and returns it.
  - **Base:** it was refused by the Defaultable and unsupported rows of `safe()`'s default path
    (`unsupported` at the call, plus the two core rows and three derived rows).
  - **After:** the default path is proven, and the program is refused by one row at the same span,
    G6 "storage loan would be discarded by a payload-free value".
  - The brief asked each canary to "keep its row or get SEM3139". This one gets a different row.
- **Why it happens.** The popped element carries `q`'s storage loan. `Task<int64>` has a
  payload-free shape, so its loan is discarded, and G6 refuses that. G6 is P-STASH's fence
  (`PLAN.md` §3, N-RANGE-FORMAL).
  - Before this packet, the Defaultable row was a second barrier on this program.
  - Now G6 is the only one.
  - Returning a task popped from the caller's own container is not a leak of this frame's
    storage, so G6 here is most likely an over-refusal. But the program's refusal now rests on
    P-STASH's fence alone.
- **Options.**
  - **A.** Accept: G6 is a named precise refusal, and P-STASH must re-measure this shape when it
    removes G6.
  - **B.** Add this shape to P-STASH's re-measure list explicitly, and to RV2-DEBT-365's second-line
    barriers.
  - **C.** Hold N-DEFHANDLE until P-STASH decides.
- **Recommendation: A with B.** The value's default path is sound by the ruling. The element path
  is still refused by a named row. B makes sure whoever removes G6 sees this shape.
- **Evidence.** Canary `task_out_of_borrowed_container` (section 2), counterfactual 3.1, and
  counterfactual 3.3, in which the unsound NoBorrowedState variant accepts this program outright.

## 9. Unverified

- **VM null default.** The VM's null default is not on this base. `t14` does not reach a default,
  so the 20 VM runs do not depend on VMDH. No reached-default program was run on the VM, as the
  brief asks.
- **Backend runs.** Of the five freed programs only `t14` has an `.out`. The other four are
  `sema/valid` programs with no `.out`, so they were diagnosed, not run.
- **The two finish rows not in the test file.** `for_in_reads_then_pop_drains` and
  `task_container_drain_with_nested_loop_break` are covered by the census, not by a dedicated row.
- **Read from code only.** The statement that Defaultable is asked of no value except a default is
  read from code (section 1). It is not a run.
