# Packet N-ALIAS-ARG: a generic call argument that disagrees with its source signature only by an alias

Base `045c113` (`origin/validation/step7-d2-on-d1`, contains `045c1135`). Branch `cloud/n-alias-arg`.
This is step 9 of `cloud-work/d2-unfinished-plan/PLAN.md`. It answers "generic original call
argument disagrees with its substituted source signature" (`originalArgumentType`,
`internal/sema/return_origin_generic_signature.go`) only for the **alias form**. The union-member
form (N-TAGMEMBER-ARG) and the element-place `&mut` receiver form (N-MUT-PLACE-RECV) keep the row.

Documents read:
- `docs/RUNTIME_V2.md`
- `docs/RUNTIME_MODEL_EXPLAINED.ru.md`
- `docs/runtime-v2-epics/DEBT.md` (rows 365 and 368)
- `docs/RUNTIME.md`
- `docs/LANGUAGE.md` §2.5 and §6.9 on aliases

`docs/CONCURRENCY.md` (v1) was not used. The change touches no task, scope or runtime
behaviour: it is a type-match in the analysis.

## 1. The rule

### What disagreed (measured with a temporary debug print, not committed)

| Site | original formal | bound template argument | actual |
|---|---|---|---|
| `recursive_handles.sg:33`, `len(nodes)` | `&T` | `T := Nodes` (alias `Nodes = Node[]`) | resolved actual `Array<Node>` (G3 already resolves the actual's alias) |
| `stable64_*.sg:23` and `stdlib/hash/xxh64.sg:255,271`, `out.push(clone(...))` | `T` | `T := byte` (alias `byte = uint8`) | `uint8` |

In both cases the template argument is an alias and the actual is its target. The existing
matcher compares `args[slot]` and the actual by kind first (`l.Kind != r.Kind`: `alias` against
`struct` or `uint`), so it refuses.

### The code (`internal/sema/return_origin_generic_signature.go`)

```go
// returnOriginAliasChainReaches follows `from`'s alias targets and reports whether
// the chain reaches `to` itself. TypeIDs are interned per declaration (and type
// arguments), so this compares declaration identity, never a spelled name.
func returnOriginAliasChainReaches(in *types.Interner, from, to types.TypeID) bool {
	seen := make(map[types.TypeID]bool)
	for id := from; id != types.NoTypeID && !seen[id]; {
		seen[id] = true
		target, alias := in.AliasTarget(id)
		if !alias {
			return false
		}
		if target == to {
			return true
		}
		id = target
	}
	return false
}
```

In the matcher, only where a template parameter binds, and only in the new `aliasArgs` mode:

```go
		if bind {
			if slot := slices.Index(params, left); slot >= 0 {
				if aliasArgs && args[slot] != right && (returnOriginAliasChainReaches(in, args[slot], right) ||
					returnOriginAliasChainReaches(in, right, args[slot])) {
					return true
				}
				return match(args[slot], right, false, depth+1)
			}
		}
```

`matchReturnOriginSourceType` keeps its old behaviour: it calls the mode with `false`. Only
`originalArgumentType` asks with `true`, both in its first check and in its `match` closure for a
non-parameter original, such as `&T` in `len`. Every other caller of `matchReturnOriginSourceType`
(existing-descriptor checks, signature views) is unchanged.

### What counts as equal

- **Exactly:** the bound template argument and the actual are joined by **one alias chain**, in
  either direction: `T := byte` against `uint8`, `T := uint8` against `byte`, or `T := Nodes`
  against `Node[]`.
- **Identity, not names.** The chain is followed through `Interner.AliasTarget`, and the test is
  TypeID equality.
  - TypeIDs are interned per declaration: `RegisterAlias(name, span)`, with generic instances
    interned by declaration and arguments.
  - So two aliases of the same target (`type WinA = int[]; type WinB = int[];`) are **not**
    related. Neither chain reaches the other, and that call keeps its row (canary
    `sibling_aliases_keep_row`).
- **Only the alias.** Nothing else is relaxed.
  - A union member against its union keeps the row. The matcher's `KindUnion` case is untouched,
    and `Some(3)` is not an alias of `Option<int64>`.
  - An element place `xs[1]` as a `&mut` receiver keeps the row.

### Why an alias can never hide a reference or a loan

- **The language.** LANGUAGE.md §6.9: "An alias is transparent: `type Handle = Leaf` introduces a
  second name, not a second type". §2.5 says it "inherits semantics of" its target. An alias has
  no fields or storage of its own: its values are the target's values.
- **The analysis reads an alias as its target** at every point where it asks whether a type can
  carry a reference or a loan:
  - `returnOriginTypeShape`: `case types.KindAlias: … AliasTarget(id) … returnOriginTypeShape(interner, target, seen)`
  - the requirement walk (`returnOriginTypeView.requirement`): `if typ.Kind == types.KindAlias { target, found := in.AliasTarget(id) … return walk(view, target, active) }`

  So `IntRef = &int` has the shape of `&int`, and `Win = int[]` holds exactly the loans an `int[]`
  holds.
- **The rule decides typing only, not origins.** It decides only that the call is well-typed
  against its source signature. What the argument carries is still read from the **actual's
  value**:
  - `originalArgumentType` returns `actual`, or the formal reference type for a borrowed formal.
  - The value's origins come from the flow, not from the type.

  A window pushed through an alias therefore keeps its storage loan, and meets G6
  (`p_stash_push_window_alias`). A reference passed as `T := IntRef` is still followed to the local
  it borrows, and gets SEM3139 (`reference_alias_through_generic`).

## 2. P-STASH and P-VIEW2 (the fence)

The ledger, `docs/runtime-v2-epics/DEBT.md` line 229 (row RV2-DEBT-365), quoted exactly:

> "**Found by P1u-TC2 and not the task check's** (RV2-DEBT-206's rule guards only the `return` exit;
> measured 2026-09-22: P-STASH is unfinished on D2 and faults `panic VM3301: storage: stale
> reference` on D1 `c7e34178`, so it is live on D1 and on D2 held only by return-origin's
> unfinished verdict; P-VIEW2 is unfinished on D2): a window into a fixed array that leaves its
> frame with no task at all — pushed into a `&mut` container parameter, written through `*out =`,
> sent into a channel — and a slice of a window returned (`let v = a[[0..3]]; return v[[0..1]];`)."

> "The same reason text on calls that are not an `.await()` (`xs[1].push(9)`, `opts.push(Some(...))`,
> `len(nodes)`: 15 rows in 6 programs of the morning census of `b229b1f8`) is not this change's and
> stays, and with it P-STASH's and P-VIEW2's unfinished verdicts."

The probes are not in the repository. These canaries follow the ledger's description, each written
with and without an alias, because an alias spelling is what this packet could let through. Each
was measured with `surge diag --format short` on both binaries:

| Canary | Base | After |
|---|---|---|
| P-STASH push: `out.push(xs[[1..3]])` into `&mut int[][]` | G6 `storage loan would be discarded by a payload-free value` | G6 (same) |
| P-STASH push, alias: `out: &mut Win[]`, `Win = int[]` | **only** `generic original call argument disagrees…` | **G6** at the same span |
| P-STASH push through an alias local: `let w: Win = xs[[1..3]]; out.push(w)` | G6 | G6 |
| P-STASH through a user generic: `keep::<Win>(out, xs[[1..3]])` | **only** `…disagrees…` | **G6** at the same span |
| P-STASH `*out = xs[[1..3]]` with `out: &mut Win` | G6 | G6 |
| P-STASH channel: `ch.send(own w)`, `ch: Channel<Win>` and `Channel<int[]>` | G6 | G6 |
| P-VIEW2: `let v = a[[0..3]]; return v[[0..1]];` | **SEM3139** (checker) | SEM3139 |
| P-VIEW2 alias: `let v: Win = a[[0..3]]; return v[[0..1]];` | SEM3139 | SEM3139 |

- **None becomes clean.**
- **Two canaries were held on the base only by this packet's row**: the alias push and the alias
  through a user generic. They are now held by G6, the named loan-discard refusal and P-STASH's
  own fence. That is the fence moving from an incidental row to a named one. It is not an opening,
  but section 8 raises it for the coordinator.
- **P-VIEW2 is now refused by the eager checker (SEM3139) on the base as well.** The ledger's "P-VIEW2
  is unfinished on D2" was measured on 2026-09-22 and is out of date at `045c113`.

## 3. Rows (`internal/driver/return_origin_alias_args_test.go`)

These are ROOT programs against the real core, run in the `analyzeOriginRoot` harness.

**`TestReturnOriginAliasArgumentsFinish`**: no Pending row anywhere, core included, and no
diagnostic in the source. On the base, each is refused **only** by this row.
- `alias_container_len`: `recursive_handles`' `let first: NodeId = len(nodes);` with `Nodes = Node[]`.
- `byte_push_both_directions`: `out.push(v)` of a `uint8` into `&mut byte[]`, and of a `byte` into `&mut uint8[]`.
- `byte_frame_push`: `stable64_*`'s `push_le_u64` shape, a computed `uint8` pushed into `byte[]`.
- `user_generic_alias_argument`: `keep::<byte>(out, v)` with `v: uint8`.

**`TestReturnOriginAliasArgumentCanariesKeepTheirRows`**: the exact Pending rows inside the source.
- P-STASH: `p_stash_push_window`, `p_stash_push_window_alias`, `p_stash_generic_alias`,
  `p_stash_deref_write_alias` and `p_stash_channel_alias` each keep G6 and nothing else.
- Union-member form: `union_member_form_keeps_row` (`opts.push(Some(3:int64))`) keeps `…disagrees…`.
  `union_member_window_keeps_both_rows` keeps `…disagrees…` and G6.
- Index-receiver form: `index_receiver_form_keeps_row` (`xs[1].push(9)`) and
  `index_receiver_window_keeps_row` (`xs[0].push(a[[1..3]])`) keep `…disagrees…`.
- Identity: `sibling_aliases_keep_row` (`WinA` against `WinB`) and `sibling_byte_aliases_keep_row`
  (`T := byte` against `Octet = uint8`) keep `…disagrees…`.

**`TestReturnOriginAliasArgumentEscapesAreRefused`**: a full `DiagnoseWithOptions` run, which must
give SEM3139.
- `p_view2_slice_of_window` and `p_view2_slice_of_window_alias`.
- `reference_alias_through_generic`: `type IntRef = &int; return id::<IntRef>(&v);`. On the base it
  is unfinished (`…disagrees…`, `outgoing reference…`, `function result…`). After, the analysis
  follows the argument to `v` and reports **SEM3139** "borrow of 'v' outlives its owner".

## 4. Counterfactuals (run)

Command: `go test -count=1 ./internal/driver -run 'TestReturnOriginAliasArgument' -v`.

### 4.1 Revert (`return_origin_generic_signature.go` at `045c113`)

```
--- FAIL: TestReturnOriginAliasArgumentsFinish           (all 4: unexpected "…disagrees…" at each alias call)
--- FAIL: TestReturnOriginAliasArgumentCanariesKeepTheirRows
    --- FAIL: .../p_stash_push_window_alias   missing G6 at "out.push(xs[[1..3]])"; unexpected "…disagrees…"
    --- FAIL: .../p_stash_generic_alias       missing G6 at "keep::<Win>(out, xs[[1..3]])"; unexpected "…disagrees…"
    (the other 9 PASS)
--- FAIL: TestReturnOriginAliasArgumentEscapesAreRefused
    --- FAIL: .../reference_alias_through_generic   (no SEM3139: unfinished instead)
```

### 4.2 Accept any disagreement (the final `return` gives back `actual, ""`)

```
--- PASS: TestReturnOriginAliasArgumentsFinish (4)
--- FAIL: TestReturnOriginAliasArgumentCanariesKeepTheirRows
    --- FAIL: .../union_member_form_keeps_row           missing "…disagrees…" at "opts.push(Some(3:int64))": []
    --- FAIL: .../union_member_window_keeps_both_rows   missing "…disagrees…"
    --- FAIL: .../index_receiver_form_keeps_row         missing "…disagrees…" at "xs[1].push(9)": []
    --- FAIL: .../index_receiver_window_keeps_row       missing "…disagrees…"; unexpected cursor-loan and G6 rows
    --- FAIL: .../sibling_aliases_keep_row              missing "…disagrees…" at "keep::<WinA>(out, b)": []
    --- FAIL: .../sibling_byte_aliases_keep_row         missing "…disagrees…" at "keep::<byte>(out, o)": []
    (all 5 P-STASH canaries PASS: G6 still holds them)
--- PASS: TestReturnOriginAliasArgumentEscapesAreRefused (3)
```

### 4.3 Compare resolved targets instead of one chain (a structural, name-like match)

The bind step accepts when `returnOriginResolveAlias(args[slot]) == returnOriginResolveAlias(right)`.

```
--- PASS: TestReturnOriginAliasArgumentsFinish (4)
--- FAIL: TestReturnOriginAliasArgumentCanariesKeepTheirRows
    --- FAIL: .../sibling_aliases_keep_row        missing "…disagrees…" at "keep::<WinA>(out, b)": []
    --- FAIL: .../sibling_byte_aliases_keep_row   missing "…disagrees…" at "keep::<byte>(out, o)": []
    (the other 9 PASS)
--- PASS: TestReturnOriginAliasArgumentEscapesAreRefused (3)
```

The sibling canaries were added for 4.3.

**What these do not show.** Neither widening makes a **leak** canary clean. Every P-STASH shape is
also held by G6, which is P-STASH's own fence. The canaries that go red are over-refusals pinned to
their row: union member, index receiver and sibling aliases. So they show that the rule is exactly
"one alias chain". They are not evidence that a widening would reopen P-STASH on its own, because
G6 backs that. See section 8.

## 5. Census

The tools are `run_one.sh` and `classify.py` from `cloud-work/d2-unfinished-plan/tools/`, run
over all 1079 `testdata/golden` programs in both forms with 4 jobs:

```
export ROOT=$PWD OUTDIR=<dir>/<side> SURGE=<surge-side>
find testdata/golden -type f -name '*.sg' | LC_ALL=C sort > <dir>/<side>/files.txt
xargs -a <dir>/<side>/files.txt -P 4 -I{} bash tools/run_one.sh {} > <dir>/<side>/status.tsv
python3 tools/classify.py $ROOT <dir>/<side> <dir>/<side>/census.json <side>
```

| | ok | unfinished | diagnostics | other |
|---|---:|---:|---:|---:|
| user form, base and after | 544 | 84 | 448 | 3 |
| harness form, base and after | 542 | 83 | 451 | 3 |

**No program changes class, in either form.** That is a deviation from the brief's "exactly the
predicted programs move from unfinished to ok": on this base the four programs cannot move, because
they also carry rows that other packets answer (section 10). The effect is at row level:

| Program | Rows of this reason, base → after | Other rows |
|---|---|---|
| `sema/valid/recursive_handles.sg` | 3 → 0 (`len(nodes)` ×3) | unchanged: 3 `generic tag use disagrees with its original typed call` (N-TAGCONV) |
| `vm_hash/stable64_frames.sg` | 3 → 0 (`out.push(clone(value[i]))`, `xxh64.sg:255,271`) | unchanged |
| `vm_hash/stable64_primitives.sg` | 3 → 0 | unchanged |
| `vm_hash/xxh64_vectors.sg` | 2 → 0 (`xxh64.sg:255,271`) | unchanged |
| `vm_hash/hash64_basic.sg` (**not predicted**) | 2 → 0 (the same two `xxh64.sg` rows) | unchanged |
| `sema/valid/stdlib_hash_api.sg` (**not predicted**) | 2 → 0 (the same two `xxh64.sg` rows) | unchanged |

- **The two unpredicted programs** import `stdlib/hash` and share the alias rows in
  `stdlib/hash/xxh64.sg`. They lose only those rows and stay unfinished on their other rows.
- **No row was gained anywhere. The only other file difference** is
  `crossing/block04/invalid/movable_negative_nested_unmarked_user_field.sg`, where the user form
  differs in line order only (known output noise).
- **This reason across the corpus:** 25 rows in 9 programs on the base, which matches the plan's
  census. After, 10 rows in 3 programs, all of them non-alias forms:
  - 9 union-member pushes in `vm_compare/counted_payload_clone_and_borrow.sg:43-45` and
    `vm_compare/for_in_compare_reads_heap_free_union.sg:47-49,54-56`
  - 1 element receiver, `sema/valid/array_view_facts_a_resize_rule_withdraws.sg:27`
    (`xs[1].push(9)`)

  So all 15 alias rows are answered and none of the others is.

## 6. Runs of the vm_hash programs

The brief says the vm_hash programs have `.out` files. **They do not**:
`testdata/golden/vm_hash/` holds only `.sg`, `.ast`, `.diag`, `.fmt` and `.tokens` for its four
programs. They are self-checking. `main` returns 1 to 9 on the first failed check and `0` when all
checks pass, and prints nothing, so their golden result is exit status 0 with empty output.

They are also still **unfinished** on this base after this packet (section 5): `surge run` refuses
them. As in the plan's measurements, they were run through a scratch build whose analysis gate
returned "complete" when `SURGE_SCRATCH_GATE_BYPASS` was set. That build was made in the working
tree and reverted, and is not committed. Other refusals and diagnostics still stop the build.

| Program | after, VM | after, LLVM | base, VM | base, LLVM |
|---|---|---|---|---|
| `vm_hash/stable64_frames.sg` | 10/10 exit 0, empty | 10/10 | 10/10 | 10/10 |
| `vm_hash/stable64_primitives.sg` | 10/10 | 10/10 | 10/10 | 10/10 |
| `vm_hash/xxh64_vectors.sg` | 10/10 | 10/10 | 10/10 | 10/10 |

`sema/valid/recursive_handles.sg` has no `@entrypoint` ("no @entrypoint found"), so it cannot be
run. The change is analysis-only, and base and after behave identically.

## 7. Build and suites

`go build ./...` and `go vet ./...` pass (exit 0).

Command: `go test -count=1 -timeout 55m ./internal/sema ./internal/driver -json`, run in a worktree
at `045c113` and in the work tree. Failing names are the `"Action":"fail"` events that carry a `Test`.

| | internal/sema | internal/driver | failing test entries | passing test entries |
|---|---|---|---:|---:|
| base `045c113` | ok | FAIL (814 s) | 55 | 2668 |
| after | ok | FAIL (821 s) | 55 | 2689 |

- **Failing-name sets:** after minus base is empty, and base minus after is empty. The 55 are the
  known base failures.
- **Test-entry difference:** exactly this packet's 21 new entries (3 tests and 18 leaves), all
  passing. No entry disappeared.
- **Tripwires:**
  - `TestH2Tripwire*` 40/40 pass on both sides. That includes the non-`own` `t.await()` rows
    DEBT.md:229 pins to this reason text (W4-G1).
  - `TestAnalyzeTaskAwaits` passes 10/10.
  - `TestTaskCheck*` passes 247/247.

## 8. Owner question (coordinator)

**Q-AA1. Two P-STASH spellings move from this row to G6.**

- **Context.**
  - `fn stash(out: &mut Win[]) { … out.push(xs[[1..3]]); }` with `type Win = int[]`, and the same
    shape through a user generic `keep::<Win>(out, xs[[1..3]])`.
  - On the base both were refused **only** by "generic original call argument disagrees with its
    substituted source signature". After this packet they are refused **only** by G6, "storage
    loan would be discarded by a payload-free value", at the same span.
  - The ledger says P-STASH is held on D2 "only by return-origin's unfinished verdict", and names
    this reason text as part of what holds it. For these two spellings, G6 is now the only
    barrier, as it already was for the unaliased P-STASH shapes (push, `*out =`, channel), which
    carry only G6 on the base.
- **Options.**
  - **A.** Land. The alias spellings join the unaliased P-STASH shapes behind G6. Whoever removes
    G6 re-measures all of them.
  - **B.** Land, and add both alias spellings to RV2-DEBT-365's list of second-line barriers (a
    tripwire row).
  - **C.** Hold N-ALIAS-ARG until P-STASH has its own fence.
- **Recommendation: B.** The unaliased shapes already rest on G6 alone, and G6 is a named precise
  refusal. B keeps the alias spellings visible to whoever removes G6. This packet already pins
  them in `TestReturnOriginAliasArgumentCanariesKeepTheirRows`.

## 9. Ledger sentence for RV2-DEBT-365

> 2026-09-25, N-ALIAS-ARG: "generic original call argument disagrees with its substituted source
> signature" no longer fires when the bound template argument and the actual are joined by one
> alias chain (declaration identity through `AliasTarget`, e.g. `T := byte` against `uint8`,
> `T := Nodes` against `Node[]`). The union-member (`opts.push(Some(...))`) and element-receiver
> (`xs[1].push(9)`) forms keep the row. The P-STASH canaries spelled through an alias (`out: &mut
> Win[]`, `keep::<Win>(...)`) were held only by this row and are now held by G6, like the
> unaliased P-STASH shapes. P-VIEW2 is refused by SEM3139 at `045c113` already, with or without an
> alias. No canary became clean.

## 10. Unverified

- **The P-STASH and P-VIEW2 probes themselves.** The coordinator's probes are not in the
  repository. The canaries are written from the ledger's description.
- **The four programs do not finish with this packet alone.**
  - `recursive_handles` also needs N-TAGCONV (`generic tag use disagrees with its original typed
    call`).
  - The vm_hash programs also need N-CLONE-COPY, N-FIELD-BORROW and N-INDEX-VIEW.
  - The plan's "new ok at step 9" was cumulative over steps 1 to 8 of its prototype
    (`prototype_measurements.json`, `necessary_packets`). Section 5 shows the standalone effect.
- **Runs.** The vm_hash runs used a scratch gate bypass (section 6). They show the programs'
  behaviour, which this packet does not change, and not that the analysis admits them.
