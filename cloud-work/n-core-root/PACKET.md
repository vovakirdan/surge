# Packet N-CORE-ROOT: diagnose core as core (the `core_stdlib` golden copies)

Base `045c113` (`origin/validation/step7-d2-on-d1`, contains `045c1135`). Branch `cloud/n-core-root`.

This implements owner question 1 of `cloud-work/d2-unfinished-plan/reasons/core-copies.md` as
ruled on 2026-09-25: the copies are diagnosed, **with core's identity**. It also carries packet
N-CORE-ROOT-TAGS (the 8 tag-constructor sites).

Documents read:
- `docs/RUNTIME_V2.md`
- `docs/RUNTIME_MODEL_EXPLAINED.ru.md`
- `docs/runtime-v2-epics/DEBT.md` (rows 365 and 368)
- `docs/RUNTIME.md`
- `docs/LANGUAGE.md`

`docs/CONCURRENCY.md` (v1) was not used. No task, scope or runtime behaviour changes, and **no
core line is changed**.

## 1. The mode

### What gets diagnosed, with which identity, and where it is compared

| Golden file | Output | Diagnosed file | Identity | Compared with |
|---|---|---|---|---|
| `core_stdlib/<f>.sg` | `.tokens`, `.ast`, `.fmt` | the copy `testdata/golden/core_stdlib/<f>.sg` (unchanged) | n/a (no analysis) | `core_stdlib/<f>.{tokens,ast,fmt}` |
| `core_stdlib/<f>.sg` | **`.diag`** | **the real `${ROOT_DIR}/core/<f>.sg`, by absolute path, run from `${ROOT_DIR}`** | **core** (module `core`, source keys `core/<f>.sg`) | `core_stdlib/<f>.diag` |

- The copy step (`golden_update.sh:254-256`) is kept, so the copies still exist and still carry
  their token, AST and format outputs.
- Before its `.diag` is produced from the real file, each copy must be **byte-identical** to the
  file it stands for (`cmp -s`). Otherwise the run records `core_stdlib copy differs from core`.
- Diagnosing core `<f>.sg` as the entry file analyses the whole `core` module as the root program,
  with that file's items as the entry.

The harness change (`scripts/golden_update.sh`, `generate_outputs`):

```bash
	local diag_src="${src}" diag_dir="${STAGED_ROOT}"
	if [[ "${rel}" == core_stdlib/* ]]; then
		diag_src="${ROOT_DIR}/core/${base}"
		diag_dir="${ROOT_DIR}"
		if [[ "${rel}" != "core_stdlib/${base}" ]] || ! cmp -s "${src}" "${diag_src}"; then
			record_error "core_stdlib copy differs from core: ${rel}"
		fi
	fi

	if (cd "${diag_dir}" && "${SURGE_BIN}" diag --format short --directives="${directives_mode}" "${diag_src}") > "${diag_path}" 2>/dev/null; then
```

- Every other golden file is diagnosed exactly as before: `diag_dir` is `STAGED_ROOT`, which is the
  script's working directory already (`cd "${STAGED_ROOT}"`, line 125).
- The expectations sidecars (`EXPECT_DIAG_ZERO`, formatter and emit failures) are keyed by `rel`
  and are unchanged.

**Why "run from `ROOT_DIR`" matters (measured).** Source keys are relative to the base directory,
and the core-intrinsic certificate requires `u.SourceKey == "core/intrinsics.sg"`
(`internal/sema/return_origin_core_identity.go:15`).
- Diagnosed with a different base directory, core's units get keys such as
  `home/user/surge/core/intrinsics.sg`, and no certificate answers. The analysis then refuses
  core's bodies by the hundreds of rows (the first run of the new driver rows).
- This is the certificate doing its job, not something to relax.

### Why a user module cannot claim core's identity

A module is **core** only when its path is `core` or `core/…`. Such a module is admitted only for
a file physically inside the detected stdlib root:
`internal/driver/module_validation.go:36-53` (`validateCoreModule`, called at
`internal/driver/diagnose.go:525-527`):

```go
	if meta.Path != "core" && !strings.HasPrefix(meta.Path, "core/") {
		return true
	}
	if stdlibRoot != "" && pathWithin(stdlibRoot, file.Path) {
		return true
	}
	... ReportError(ProjInvalidModulePath, "module %q is reserved for the standard library")
	return false            // diagnose: "core namespace reserved"
```

The certificates additionally need the source key `core/intrinsics.sg` and
`ModulePath == "core/intrinsics"`, with a builtin, intrinsic, body-less candidate
(`return_origin_core_identity.go:9-22`). A name alone selects nothing, and this packet adds no
certificate and no name or content check. So:

- **The golden copy** is `core_stdlib/<f>.sg` with `pragma module, no_std;`. Its module path is
  `core_stdlib`, so it is an ordinary user module. Diagnosed by itself, it stays uncertified and
  unfinished. `TestAnalyzeFoldedBuiltinDeclaration/core_stdlib_mirror` (existing) still passes.
- **A user copy of core's text named `core` outside the stdlib root** is refused as "core
  namespace reserved" (`TestCoreRootIdentityCannotBeClaimedByACopy/copy_named_core`). This holds
  even though, diagnosed from its parent directory, its source keys would be exactly core's
  (`core/intrinsics.sg`, …). The stdlib-root check is what stands between the copy and the
  certificates: counterfactual 4.2 removes it, and the copy is then diagnosed **clean**.
- **A user copy of core's text under any other name** is a user `no_std` module whose intrinsics
  no certificate answers. It is unfinished, and none of its rows is reported against a `core/…`
  source key (`…/copy_as_user_module`).
- **The mode adds no driver flag and no new trust.** It uses the one path the driver already
  admits as core: the real file under the stdlib root.
  - The stdlib root is whatever `SURGE_STDLIB`, or upward detection, names
    (`detectStdlibRootFrom`). Pointing `SURGE_STDLIB` at another directory replaces core for the
    whole compilation. It does not let one module sit beside the real core as core. That is
    today's behaviour and is unchanged.

### Alternatives considered

| Alternative | Why not |
|---|---|
| A driver flag (`--as-core`) that diagnoses a named file as core | New trust surface. A flag must still restrict itself to files inside the stdlib root, which is exactly what the absolute path already is. |
| Certify the copy by content or by its `core_stdlib` path | Option C of `core-copies.md`, rejected: a name or content alone would select core's contracts. (`internal/symbols/resolve_declarations.go:236-240` already has a pre-existing name/path allowance for `core_stdlib/intrinsics.sg` layout bodies; not touched, noted in section 8.) |
| Replace the copies with the real files (drop `core_stdlib/`) | Loses the tokens/ast/fmt goldens the directory exists for, and changes the corpus layout the golden checker indexes. |
| Diagnose only `core/base.sg` once for all ten copies | Measured: the entry file decides which generic uses are finalized. `string.sg` as the entry finalizes `levenshtein`'s array concatenations, and `base.sg` does not. Per-file diagnosis is the stronger check. |
| Exclude the copies from `diag` (options A and E) | Rejected by the owner ruling. |

## 2. The 8 tag-constructor sites (N-CORE-ROOT-TAGS)

### Measured cause

All 8 sites are explicit tag constructors whose tag is declared in a **sibling file of the root
module**:
- `core/array.sg:163,182,201,220,239` (`return Some(i to uint);`)
- `core/entrypoint.sg:84` (`return Success(text);`)
- `core/result.sg:34` (`err => Some(err);`)
- `core/string.sg:341` (`return Success(s.__clone());`)

`Some` lives in `core/option.sg` and `Success` in `core/result.sg`.

When core is the root program, **no unit has a publication root-to-local table**. Every unit has
its own symbol table, and the AST builder is shared. A temporary debug print (not committed) on
`core/array.sg:163` showed:

```
DBG tag core/array.sg at 1:4474-4489 selected=799 canonical=799 rootLocalsLen=0
   unit core/array.sg tableLen=0 locals=[799]
      local 799 Some kind=tag declAST=7 unitFile=2 declSrc=6 fileSpan=1 item=235 inItemSyms=false
   unit core/option.sg tableLen=0 locals=[]          (and every other unit: locals=[])
DBG unit core/array.sg sameTable=false ... (each unit's Symbols is its own)
```

- `tagPayload`'s owner search, for an empty table, only looks for the declaration in the **using**
  unit (`if unit == u { locals = []symbols.SymbolID{canonical} }`). The declaration is in AST
  file 7, `option.sg`, so no owner is found.
- The result is "tag constructor lacks its exact owning source declaration". Then the call target
  `Some` is read as a value, which gives "captured binding requires origin finalization".
- It is **not core-specific**. A two-file user module (`tag Found` in `a.sg`, `return Found(x);` in
  `b.sg`), with or without `no_std`, gets the same rows on the base (measured).

### The transfer (`internal/sema/return_origin_tags.go`)

```go
		locals := unit.Publication.RootToLocalSymbols[canonical]
		if len(unit.Publication.RootToLocalSymbols) == 0 {
			locals = nil
			if unit == u {
				locals = []symbols.SymbolID{canonical}
			} else if len(u.Publication.RootToLocalSymbols) == 0 {
				locals = siblingDeclarationSymbols(u, unit, sym)
			}
		}
...
func siblingDeclarationSymbols(u, unit *returnOriginUnitIndex, sym *symbols.Symbol) []symbols.SymbolID {
	if sym == nil || unit.Builder != u.Builder || unit.FileID != sym.Decl.ASTFile || !sym.Decl.Item.IsValid() {
		return nil
	}
	return unit.Symbols.ItemSymbols[sym.Decl.Item]
}
```

**Why it is sound.**
- **Identity, not name.**
  - The selected symbol's own declaration record (`Decl.ASTFile`, `Decl.Item`) names exactly one
    item in the shared AST.
  - Only the unit whose file **is** that AST file, over the same builder, is consulted, and only
    for its symbols of that item.
  - No name is read to find the owner.
- **Every existing check still runs afterwards, unchanged**: kind tag, the declaring file and
  source file, `ItemSymbols` membership, and the tag item's name span. Then
  `original.Name == sym.Name` and the canonical signature identity, the exact payload slots, the
  typed payload result and the generic bindings.
- **Scope.** It applies only when neither the using unit nor the owner unit has a publication table.
  That is a root module's sibling files: a published dependency keeps its existing path.
- **The value transfer is the existing constructor transfer.** The payload's roots and loans pass
  into the tag value (`tagCall` → `constructorChildren`).

Result, measured: with core as the root (`surge diag $PWD/core/<f>.sg`, run from the repository
root), **38 rows → 0** for 9 of the 10 files. For `string.sg`: **41 → 3** (section 3).

No core line was rewritten.

## 3. What remains: `core/string.sg` as the entry file (deviation)

As the entry file, `string.sg` also finalizes the generic uses in `levenshtein`: `prev = prev + one;`
(`core/string.sg:311`), `curr = curr + first;` (`:319`) and `curr = curr + one;` (`:331`), over
`uint[]`.
- **The row:** each keeps "generic use disagrees with its original typed operation". The finalized
  use sits on a binary expression, and `genericUseContext` accepts only a call or an index there
  (`internal/sema/return_origin_generics.go:92-96`).
- **The owner:** this is the reason text of plan step 17, N-CONCAT-USE (`generic-use-typed-operation`),
  not one of the 8 tag sites. The same 3 rows are on the base.
- **Not fixed here.** This packet neither answers it nor rewrites core. See owner question Q-CR1.
- **Pinned:** `TestCoreRootDiagnosesCoreAsCore/string.sg` requires exactly these 3 rows, so a
  later packet that answers them turns the row red and must update it.

## 4. Rows, canaries and counterfactuals

`internal/driver/return_origin_core_root_test.go`:

| Test | What it proves |
|---|---|
| `TestCoreRootDiagnosesCoreAsCore/<f>.sg` (10 leaves) | the harness path: the real core file, absolute path, base dir = repository root, `SURGE_STDLIB` = repository. 9 files are clean; `string.sg` gives exactly the 3 N-CONCAT-USE rows |
| `TestCoreRootRelativePathStaysReserved` | `core/base.sg` given relatively is refused "core namespace reserved" (unchanged behaviour) |
| `TestCoreRootIdentityCannotBeClaimedByACopy/copy_named_core` | core's 10 files copied to `<tmp>/core/`, diagnosed from `<tmp>`: refused "core namespace reserved" |
| `…/copy_as_user_module` | the same copy under `<tmp>/mycore/`: unfinished, and no row carries a `core/…` source key |
| `TestCoreRootSiblingTagShapes` | the 8 sites' shapes in a user two-file module: `return Found(i to uint);` in a loop, `return Done(text);` into `Outcome<string>` (a union with a bare `Error` member, as `Erring`), and `err => Found(err);` in a compare. The tags are declared in the sibling file, and the result is clean |
| `TestAnalyzeFoldedBuiltinDeclaration/core_stdlib_mirror` (existing) | the golden copy, diagnosed as itself, stays unfinished |

Command for both counterfactuals: `go test -count=1 ./internal/driver -run 'TestCoreRoot|TestAnalyzeFoldedBuiltinDeclaration' -v`.

### 4.1 Revert (`return_origin_tags.go` at `045c113`)

```
--- FAIL: TestCoreRootDiagnosesCoreAsCore      (all 10 leaves: "core as core is not clean"; string.sg: the 38 extra rows,
                                                "tag constructor lacks its exact owning source declaration",
                                                "captured binding requires origin finalization", …)
--- PASS: TestCoreRootRelativePathStaysReserved
--- PASS: TestCoreRootIdentityCannotBeClaimedByACopy (2)
--- FAIL: TestCoreRootSiblingTagShapes         (unfinished: function result / outgoing reference / tag rows)
--- PASS: TestAnalyzeFoldedBuiltinDeclaration (2)
```

### 4.2 Let a user module claim core identity (`validateCoreModule` admits any file)

```
--- PASS: TestCoreRootDiagnosesCoreAsCore (10)
--- FAIL: TestCoreRootRelativePathStaysReserved
       relative core path: want core namespace reserved, got <nil>
--- FAIL: TestCoreRootIdentityCannotBeClaimedByACopy
    --- FAIL: .../copy_named_core
       a copy of core named core outside the stdlib root: want core namespace reserved, got <nil>
    --- PASS: .../copy_as_user_module
--- PASS: TestCoreRootSiblingTagShapes
--- PASS: TestAnalyzeFoldedBuiltinDeclaration (2)
```

With the stdlib-root check removed, the user copy named `core` is **diagnosed clean**: it
received core's certificates. That is exactly what the check prevents.

## 5. Census

The tools are `run_one.sh` and `classify.py` from `cloud-work/d2-unfinished-plan/tools/`, over
all 1079 `testdata/golden` programs in both forms with 4 jobs.
- The **harness form** of the after side mirrors the new `golden_update.sh` rule. In a scratch copy
  of `run_one.sh`, for `testdata/golden/core_stdlib/<f>.sg` it diagnoses `$ROOT/core/<f>.sg`, from
  `$ROOT`, when `HARNESS_CORE_AS_CORE=1`.
- The **user form** is the brief's command, `surge diag <file>`, which diagnoses the copy itself.
- The base census is the N-ALIAS-ARG session's run of the same `045c113` binary with the same
  harness form.

```
export ROOT=$PWD OUTDIR=<dir>/after SURGE=<surge-after> HARNESS_CORE_AS_CORE=1
xargs -a files.txt -P 4 -I{} bash run_one_core.sh {} > <dir>/after/status.tsv
python3 tools/classify.py $ROOT <dir>/after <dir>/after/census.json after
```

| | ok | unfinished | diagnostics | other |
|---|---:|---:|---:|---:|
| user form, base | 544 | 84 | 448 | 3 |
| user form, after | 544 | 84 | 448 | 3 |
| harness form, base | 542 | 83 | 451 | 3 |
| **harness form, after** | **551** | **74** | 451 | 3 |

**Class changes, harness form: exactly 9, all `core_stdlib/`, unfinished → ok:**
`array`, `base`, `entrypoint`, `format`, `intrinsics`, `map`, `option`, `result` and `sync`.
- `core_stdlib/string.sg` stays unfinished (harness form, 301 → 3 rows): the 3 N-CONCAT-USE rows of
  section 3, now reported against `core/string.sg` instead of the copy's source key.
- **Deviation from "the 10 go to ok": `string.sg` (section 3, Q-CR1).**

**Class changes, user form: none, by design.** The user form diagnoses the copy as an ordinary
`no_std` module named `core_stdlib`, which must not get core's identity: that is the canary.
- Row-level, each copy loses the 38 rows the sibling-tag fix answers (8 × "tag constructor lacks
  its exact owning source declaration", 8 × "captured binding requires origin finalization", and 22
  derived rows).
- `array.sg` goes 310 → 272, `string.sg` 301 → 263, and the other eight 305 → 267. The remaining
  rows are the identity refusals of a user module's body-less intrinsics, as before.

**Every other golden program: identical output in both forms.** The only difference is
`crossing/block04/invalid/movable_negative_nested_unmarked_user_field.sg`, whose user form differs
in line order only (known output noise). No other golden file changes class or rows, including
the multi-file modules the sibling-tag rule could reach, so every golden program still builds
against core as before. No core line was edited, so no `.out` can change through core, and none
was run.

## 6. Build and suites

`go build ./...` and `go vet ./...` pass (exit 0). `go test ./internal/goldencheck` passes; its
script-fixture tests run the edited `golden_update.sh`.

Command: `go test -count=1 -timeout 30m ./internal/sema ./internal/driver -json`.
- **Base:** the N-ALIAS-ARG session's run of `045c113` in a worktree. The driver package finished
  in 814 s, well inside 30 minutes, so the different timeout does not matter.
- **After:** this branch, with `-timeout 30m`.
- **A first after run died when the disk filled up** (26 MB free; old scratch data). Its JSON
  stops mid-test in `TestAnalyzeTypedDeferredCloneOwnerPublication/intact`, which passes alone in
  4 s. After freeing space, the run was repeated in full, and the table is that run.

| | internal/sema | internal/driver | failing test entries | passing test entries |
|---|---|---|---:|---:|
| base `045c113` | ok | FAIL (814 s) | 55 | 2668 |
| after | ok | FAIL (790 s) | 55 | 2684 |

- **Failing-name sets:** after minus base is empty, and base minus after is empty.
- **Test-entry difference:** exactly this packet's 16 new entries (4 tests and 12 leaves), all
  passing. No entry disappeared.
- **Tripwires, after:** `TestH2Tripwire*` 40/40, `TestAnalyzeTaskAwaits` 10/10 and
  `TestTaskCheck*` 247/247 pass. That includes `TestH2TripwireImportAddsCoreRowsG5b`, which pins the
  core rows an explicit `core/intrinsics` import brings in.
- **`TestBuiltinOperationIsIngestedOnce`** (`core/intrinsics.sg` and `core_stdlib/string.sg`) fails
  on the base and after alike. It diagnoses with no `BaseDir`, so core's source keys are absolute
  (`home/user/surge/core/…`) and no core certificate answers. That is the same cause the first
  version of this packet's own rows hit (section 1). It is not changed here.

## 7. Ledger sentence for RV2-DEBT-365

> 2026-09-25, N-CORE-ROOT: `testdata/golden/core_stdlib/<f>.diag` is now the diagnosis of the real
> `core/<f>.sg` (absolute path, from the repository root, so core keeps its `core/…` source keys
> and is admitted as core only because it lies inside the stdlib root, `validateCoreModule`); the
> copies keep their token/ast/fmt goldens and must stay byte-identical to core. A tag constructor
> whose tag is declared in a sibling file of an unpublished root module now finds its owner by
> declaration identity (`Decl.ASTFile` + `Decl.Item` over the shared AST), which answers the 8
> core sites (`array.sg:163,182,201,220,239`, `entrypoint.sg:84`, `result.sg:34`, `string.sg:341`).
> No certificate, name or content check is added. A copy of core outside the stdlib root still
> gets no identity. `core/string.sg` as the entry file keeps 3 N-CONCAT-USE rows
> (`levenshtein`'s `uint[]` concatenations). No barrier this row lists is touched: the tripwire
> tests are unchanged (section 6).

## 8. Unverified or noted

- **`golden_update.sh` end to end.** The script was not run end to end. It cannot pass on this
  base: 85 valid golden programs already fail return-origin. Its new branch was exercised by the
  same command in the census harness form (section 5), and `go test ./internal/goldencheck`,
  whose script-fixture tests run the script, passes.
- **Relative core paths.** A relative path to a core file is refused even inside the stdlib root,
  because `pathWithin` compares a relative path with an absolute root (`filepath.Rel` fails). This
  is pre-existing and untouched. The harness therefore uses the absolute path.
- **A pre-existing name/path allowance.** `internal/symbols/resolve_declarations.go:236-240`
  (`allowIntrinsicTypeBody`) allows full-layout intrinsic type bodies for any file ending in
  `core_stdlib/intrinsics.sg`, or a module named `core_stdlib`. It concerns layout declarations, not
  return-origin identity, and it is untouched. I flag it because it is the one place where a copy
  is recognised by name.
- **Core edits.** No core file is edited, so no golden `.out` can change through core. The census
  confirms no class change outside `core_stdlib/`.

## 9. Owner question

**Q-CR1. `core/string.sg`'s three array concatenations: fix the analysis (N-CONCAT-USE) or rewrite
three core lines?**

- **Plain words.** When core is checked as a program with `string.sg` as its entry, `levenshtein`
  builds its rows by concatenation: `prev = prev + one;` adds one element by concatenating a
  one-element array. The analysis cannot yet certify a generic `__add` reached through a binary
  operator, so these 3 lines stay unfinished. The other 9 core files are clean.
- **Example.** `core/string.sg:309-312`:
  ```
  let value: uint = j to uint;
  let one: uint[] = [value];
  prev = prev + one;
  ```
- **The invariant.** `internal/sema/return_origin_generics.go:92-96`: a finalized generic use must
  sit on "an ExprCall or ExprIndex" at its site, otherwise it is `returnOriginUseOtherOperation`
  ("generic use disagrees with its original typed operation"). A binary operator's selected
  `__add` is neither.
- **Options.**
  - **A. Land N-CONCAT-USE** (plan step 17). It certifies a generic operator use by the selected
    operation's identity, about 35 lines in the prototype. It fixes the analysis, keeps core
    untouched, and also serves user code. Cost: a separate packet, and my plan pairs it with
    N-CORE-TEMP (owner question 3(a)).
  - **B. Rewrite the three lines as `prev.push(value);`, `curr.push(i to uint);` and
    `curr.push(best);`**. The result is the same: append one element to the array held in the
    same variable. Cost:
    - a core edit, which the owner asked to keep minimal;
    - it changes allocation behaviour (in place instead of a new array);
    - it needs a run of every golden program that reaches `levenshtein` on the VM and LLVM;
    - it removes a real use of generic array `+` from core's own check.
  - **C. Leave it.** `core_stdlib/string.diag` then records the unfinished verdict until A lands.
- **Recommendation: A.** The ruling prefers fixing the analysis, and the rows are exactly
  N-CONCAT-USE's reason.
- **What not to do until answered:**
  - do not rewrite `levenshtein`;
  - do not make `genericUseContext` accept a binary expression without the selected operation's
    identity check, which is exactly what the prototype's N-INDEX-CALL hole showed can accept a
    forged selection.
- **The question.** Should `core/string.sg`'s concatenations be answered by landing N-CONCAT-USE
  (A), by rewriting the three lines with `push` (B), or left unfinished for now (C)?
