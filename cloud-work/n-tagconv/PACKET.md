# Packet N-TAGCONV: return-origin for a generic tag use made by an implicit Some/Success wrap

Base `045c113` (`origin/validation/step7-d2-on-d1`, contains `045c1135`). Branch `cloud/n-tagconv`.
This is step 5 of `cloud-work/d2-unfinished-plan/PLAN.md`. It answers "generic tag use disagrees
with its original typed call" where the tag use is an implicit `Some`/`Success` wrap the checker
inserted.

Documents read:
- `docs/RUNTIME_V2.md`
- `docs/RUNTIME_MODEL_EXPLAINED.ru.md`
- `docs/runtime-v2-epics/DEBT.md` (rows 365 and 368)
- `docs/RUNTIME.md`
- `docs/LANGUAGE.md`, "Auto-wrapping": "In clear contexts, Surge automatically wraps return values
  … `return 42;  // Auto-wrapped to Success(42)`", and the `let x: int? = 1` form

`docs/CONCURRENCY.md` (v1) was not used. The change touches no task, scope or runtime behaviour:
it is an instance certificate in the analysis.

## 1. The rule

### Where the row came from

- `checkTagUse` (`internal/sema/return_origin_tags.go`) certifies a finalized `InstantiationTag`
  use by finding a **typed call expression** at the use's site. An implicit wrap has no call
  expression: the node at the site is the wrapped value (`1`, `x`, `first`), so it returned
  "generic tag use disagrees with its original typed call".
- The **value** of the wrap was already transferred soundly by
  `return_origin_implicit_conversions.go` (unchanged, quoted):

```go
	switch conversion.Kind {
	case ImplicitConversionSome, ImplicitConversionSuccess, ImplicitConversionTagUnion:
		// A wrapper holds the payload itself, with every loan it carries. An erased
		// wrapper would hand a loan to readers that drop it: refuse it, keep the roots.
		if b.erasedType(conversion.Target) && returnOriginTypeShape(u.Sema.TypeInterner, conversion.Source, nil) == returnOriginRefFree {
			b.discardLoans(out.value, u.Builder.Exprs.Get(id).Span)
		}
```

  `out.value`, the payload's roots and loans, passes through unchanged. What was missing was only
  the certificate of the **instance** the finalized use names.

### The code

In `checkTagUse`, the existing typed-call check runs first, now as `checkTagCallUse`. Only if it
refuses, and the checker recorded an implicit wrap at exactly this site, does the new certificate
decide:

```go
	reason := a.checkTagCallUse(caller, use)
	// N-TAGCONV: an implicit Some/Success wrap the checker recorded at this exact
	// site is a tag use with no call expression; certify its instance instead.
	if conversion, found := a.checkTagConversionUse(caller, use); found && reason != "" {
		return conversion
	}
	return reason
```

`checkTagConversionUse` (new file `internal/sema/return_origin_tag_conversions.go`):

```go
	for _, candidate := range u.Sema.ImplicitConversions {
		if candidate.Span == use.Site && (candidate.Kind == ImplicitConversionSome || candidate.Kind == ImplicitConversionSuccess) {
			conversion, matches = candidate, matches+1
		}
	}
	...                                   // 0 records: not ours; >1: "…ambiguous implicit conversion records"
	sym := u.Symbols.Table.Symbols.Get(conversion.Callee)
	if sym == nil || sym.Kind != symbols.SymbolTag {
		return "generic tag conversion lacks its selected tag constructor", true
	}
	canonical, reason := canonicalTagSymbol(u, conversion.Callee)
	...
	if canonical != use.CalleeTemplate {
		return "generic tag conversion disagrees with its selected source declaration", true
	}
	if use.Caller != (InstanceKey{}) || len(caller.candidate.TemplateParams) != 0 || len(use.CallerTemplateArgs) != 0 {
		return "generic tag conversion in a generic caller needs its exact type-dependent payload transfer", true
	}
	if !slices.Contains(u.authority.InstantiationClosure.LiveCallables, use.CallerTemplate) {
		return "generic tag use disagrees with its original bound arguments", true
	}
	// The instance is Tag<Source>, and the target union has that member with that payload.
	...                                   // exactly one member of Target named sym.Name, TagArgs == [Source]
	if members != 1 || !slices.Equal(use.TemplateArgs, []types.TypeID{conversion.Source}) {
		return "generic tag conversion disagrees with its constructor instance", true
	}
	return "", true
```

`canonicalTagSymbol` is the root-to-local mapping `tagPayload` already used, moved out unchanged
so both paths share it.

### Why it is sound

- **Identity.** The constructor is `ImplicitConversion.Callee`, the exact symbol the checker
  selected ("HIR must not repeat a scope lookup after module merging", its declaration comment). It
  is mapped to its declaration's root symbol through `Publication.RootToLocalSymbols`, and must
  **equal** the use's `CalleeTemplate`.
  - No name is compared. A tag of another union that is merely spelled `Some` is a different root
    symbol, so it is refused (canary `TestReturnOriginTagConversionIsKeyedByDeclaration`).
- **Only the instance is certified.**
  - The use's one type argument must be the wrapped value's type (`Source`).
  - The target union must hold exactly one member of that tag, with payload `[Source]`.
  - A payload whose type disagrees with the constructor's instance keeps a named row: "…disagrees
    with its constructor instance".
- **No origin is invented.** The value that reaches the result is the payload's own value (the
  switch quoted above). A wrapped reference keeps its roots:
  - `fn pass(x: &int) -> Option<&int> { return x; }` publishes a summary naming parameter 0.
  - A wrapped reference to a dying local that escapes gets SEM3139.
- **Generic callers keep a named refusal.** The wrap's instance there depends on `T`, and that
  needs the type-dependent payload transfer this packet does not provide.
- **Scope.** Only kinds `Some` and `Success` are covered. `ImplicitConversionTagUnion` and `To`
  are untouched. A site with an explicit typed call is decided by the existing check first.

## 2. Rows (`internal/driver/return_origin_tag_conversions_test.go`)

These are ROOT programs against the real core, run in the `analyzeOriginRoot` harness.

| Test | Program | Requires |
|---|---|---|
| `TestReturnOriginTagConversionsFinish/option_let_wrap` | `let x: int? = 1; let y: Option<int> = 2;` (`mono/option_implicit_wrap`) | no Pending row anywhere |
| `…/return_sugar_some_and_success` | `return 1;` into `Option<int>`, `return 2;` into `Erring<int, Error>` (`return_type_and_sugar`, `hir/option_erring`) | no Pending row |
| `…/assigned_wrap` | `next = first;` into `NodeId?` (`recursive_handles`) | no Pending row |
| `TestReturnOriginTagConversionKeepsPayloadRoots` | `fn pass(x: &int) -> Option<&int> { return x; }` | no Pending row; summary `pass` is normal, not unknown, sources `[0]` |
| `TestReturnOriginTagConversionGenericCallerStaysRefused` | `fn wrap<T>(x: T) -> Option<T> { return x; }` | exactly "generic tag conversion in a generic caller needs its exact type-dependent payload transfer" at `x` |
| `TestReturnOriginTagConversionIsKeyedByDeclaration` | `return 7;` into `Option<int>`, with the recorded `Callee` forged to a user tag `Found` (of `Lookup<T> = Found(T) \| nothing`) and that symbol renamed `Some` | exactly "generic tag conversion disagrees with its selected source declaration" at `7` |
| `TestReturnOriginTagConversionEscapeIsRefused` | `let r: &int = &v; let o: Option<&int> = r; return o;` | **SEM3139** (full diagnose) |
| `TestReturnOriginTagConversionOtherFormsKeepTheirRows/alias_argument` | `len(nodes)` with `Nodes = int[]` | exactly the N-ALIAS-ARG row |
| `…/union_member_argument` | `opts.push(Some(3:int64))` | exactly the N-TAGMEMBER-ARG row |

Canary notes:
- A user tag named `Some` cannot be written next to core: SEM3002 "duplicate declaration of
  'Some'". Another union cannot be the target of an implicit wrap: SEM3015 "expected Lookup<int>,
  got int". That is why the identity canary forges the record.
- In a `no_std` module with its own `tag Some<T>(T); type Option<T> = Some(T) | nothing;`, the
  checker records a Some-wrap whose `Callee` is that module's `Some`. The certificate accepts it,
  since identity is consistent (measured: base refused, after clean). That is correct: the tag the
  checker selected is the one certified.
- `return &v` wrapped in `Option` or `Erring` is refused by the eager checker's own SEM3139 before
  the analysis runs. The escape row therefore goes through a local, which the eager checker admits,
  so it tests the analysis.
- Probe, not a row: `return Some(1);` into `Option<Option<int>>` (an explicit tag that is itself
  wrapped) has identical rows on base and after, including "generic use has duplicate or
  contradictory finalized authority". It is not widened.

## 3. Counterfactuals (run)

Command: `go test -count=1 ./internal/driver -run 'TestReturnOriginTagConversion' -v`.

### 3.1 Revert (`return_origin_tags.go` at `045c113`, new file removed)

```
--- FAIL: TestReturnOriginTagConversionsFinish (option_let_wrap, return_sugar_some_and_success, assigned_wrap:
          unexpected "generic tag use disagrees with its original typed call" at each wrap)
--- FAIL: TestReturnOriginTagConversionKeepsPayloadRoots      (unexpected "…original typed call" at `x`)
--- FAIL: TestReturnOriginTagConversionGenericCallerStaysRefused   (the old row instead of the named one)
--- FAIL: TestReturnOriginTagConversionIsKeyedByDeclaration   (the old row instead of the named one)
--- FAIL: TestReturnOriginTagConversionEscapeIsRefused        (no SEM3139: unfinished)
--- PASS: TestReturnOriginTagConversionOtherFormsKeepTheirRows (2)
```

### 3.2 "Fresh" instead of the payload's origins (`out.value = returnOriginValueOf()` for Some/Success)

```
--- PASS: TestReturnOriginTagConversionsFinish (3)
--- FAIL: TestReturnOriginTagConversionKeepsPayloadRoots
       pass summary = {… ParamSlots:[] …}            (the root to `x` is lost)
--- PASS: TestReturnOriginTagConversionGenericCallerStaysRefused
--- PASS: TestReturnOriginTagConversionIsKeyedByDeclaration
--- FAIL: TestReturnOriginTagConversionEscapeIsRefused
       expected SEM3139, got:                         (the leak of `v` is accepted clean)
--- PASS: TestReturnOriginTagConversionOtherFormsKeepTheirRows (2)
```

### 3.3 Match by name (`sym.Name` compared to the template's local name instead of `canonical != use.CalleeTemplate`)

```
--- PASS: TestReturnOriginTagConversionsFinish (3)
--- PASS: TestReturnOriginTagConversionKeepsPayloadRoots
--- PASS: TestReturnOriginTagConversionGenericCallerStaysRefused
--- FAIL: TestReturnOriginTagConversionIsKeyedByDeclaration
       missing "generic tag conversion disagrees with its selected source declaration" at 96:97 "7": []
--- PASS: TestReturnOriginTagConversionEscapeIsRefused
--- PASS: TestReturnOriginTagConversionOtherFormsKeepTheirRows (2)
```

## 4. Census

The tools are `run_one.sh` and `classify.py` from `cloud-work/d2-unfinished-plan/tools/`, run
over all 1079 `testdata/golden` programs in both forms with 4 jobs. The command is the same as in
the N-ALIAS-ARG packet:
`export ROOT=$PWD OUTDIR=<dir>/<side> SURGE=<bin>; xargs -a files.txt -P 4 -I{} bash tools/run_one.sh {}; python3 tools/classify.py $ROOT <dir>/<side> <dir>/<side>/census.json <side>`.

The four configurations all use `045c113` for everything outside the packets:

| Configuration | user form ok / unfinished | harness form ok / unfinished |
|---|---|---|
| base | 544 / 84 | 542 / 83 |
| N-ALIAS-ARG only (`cloud/n-alias-arg`'s change) | 544 / 84 | 542 / 83 |
| **N-TAGCONV only** (this branch) | **547 / 81** | **545 / 80** |
| **N-TAGCONV + N-ALIAS-ARG** | **548 / 80** | **546 / 79** |

The diagnostics (448 / 451) and other (3 / 3) columns are the same in all four.

The base and N-ALIAS-ARG-only census outputs were reused from the N-ALIAS-ARG session, which used
the same base binary built from `045c113` and the same N-ALIAS-ARG code. The two new censuses were
run for this packet. The combined binary is `045c113` with N-ALIAS-ARG's
`return_origin_generic_signature.go` diff applied, plus this packet's two sema files.

**Class changes, identical in both forms:**

| Program | N-TAGCONV only | with N-ALIAS-ARG |
|---|---|---|
| `hir/option_erring.sg` | unfinished → **ok** | unfinished → **ok** |
| `mono/option_implicit_wrap.sg` | unfinished → **ok** | unfinished → **ok** |
| `sema/valid/return_type_and_sugar.sg` | unfinished → **ok** | unfinished → **ok** |
| `sema/valid/recursive_handles.sg` | stays unfinished | unfinished → **ok** |

**Row-level changes, base → N-TAGCONV only.** No row is gained anywhere.

| Program | Rows lost | Rows kept |
|---|---|---|
| `hir/option_erring.sg` | 1 `generic tag use disagrees with its original typed call` | none |
| `mono/option_implicit_wrap.sg` | 2 of the same | none |
| `sema/valid/return_type_and_sugar.sg` | 3 of the same | none |
| `sema/valid/recursive_handles.sg` | 3 of the same | 3 `generic original call argument disagrees with its substituted source signature` (`len(nodes)`, N-ALIAS-ARG's) |

**Row-level changes, base → both packets:** the table above, plus N-ALIAS-ARG's own changes and
nothing else:
- `recursive_handles` also loses its 3 alias rows.
- `stable64_frames` and `stable64_primitives` lose 3 alias rows each.
- `xxh64_vectors`, `hash64_basic` and `stdlib_hash_api` lose 2 each.

Those five stay unfinished on their other rows. The only other file difference in any
configuration is `crossing/block04/invalid/movable_negative_nested_unmarked_user_field.sg`, whose
user form differs in line order only (known output noise). The harness-form outputs change for
exactly the programs listed.

## 5. Runs

None of the four programs has an `.out` file:
- `hir/option_erring` has a `.hir` sidecar and `mono/option_implicit_wrap` a `.mono` sidecar.
- The other two have only `.ast`, `.diag`, `.fmt` and `.tokens`.
- None has an `@entrypoint`.

So a scratch program (not committed) exercises the same wraps:
- `let x: int? = 1`
- a returned `x` wrapped into `int?`, and `nothing`
- `return 2` wrapped into `Erring<int, Error>`, and an `Error` value
- `return x` of `&int` wrapped into `Option<&int>`, then dereferenced

| | base | after, VM | after, LLVM |
|---|---|---|---|
| diagnose | unfinished (`generic tag use disagrees with its original typed call` at every wrap) | clean | clean |
| output of 10 runs | not runnable | `1 5 -1 2 -1 9`, identical 10/10, exit 0 | identical to the VM, 10/10, exit 0 |

## 6. Build and suites

`go build ./...` and `go vet ./...` pass (exit 0).

Command: `go test -count=1 -timeout 55m ./internal/sema ./internal/driver -json`. The base run is
the N-ALIAS-ARG session's run in a worktree at `045c113`, the same commit. The after run is this
branch.

| | internal/sema | internal/driver | failing test entries | passing test entries |
|---|---|---|---:|---:|
| base `045c113` | ok | FAIL | 55 | 2668 |
| after | ok | FAIL | 55 | 2679 |

- **Failing-name sets:** after minus base is empty, and base minus after is empty.
- **Test-entry difference:** exactly this packet's 11 new entries (6 tests and 5 leaves), all
  passing. No entry disappeared.
- **Tripwires, after:** `TestH2Tripwire*` 40/40, `TestAnalyzeTaskAwaits` 10/10 and `TestTaskCheck*`
  247/247 all pass.

## 7. Ledger sentence for RV2-DEBT-365

> 2026-09-25, N-TAGCONV: the finalized use of an implicit `Some`/`Success` wrap is certified by
> the constructor the checker recorded (`ImplicitConversion.Callee`, mapped to its declaration's
> root symbol and required to equal the use's template) and by its one type argument being the
> wrapped value's type, in a nongeneric live caller; the wrap's value is the payload's own (roots
> and loans kept), so a wrapped reference to a dying local reaches SEM3139 instead of an
> unfinished row. Generic callers keep a named refusal. No barrier listed in this row is touched;
> no task, await or crossing path changes.

## 8. Unverified

- **Runs.** The four golden programs cannot be run: they have no entry point and no `.out`. The
  runs in section 5 are a scratch program with the same wraps.
- **Nested wraps.** `Some(1)` into `Option<Option<int>>` is refused on other rows on base and after
  alike. Its behaviour under this certificate is not reached, because those rows fire first.
- **`ImplicitConversionTagUnion`** (a wrap into a user union through its unique member) is outside
  this packet and still has no instance certificate.

## 9. Owner questions

None. The rule is an identity certificate with named refusals, and no language behaviour changes.
