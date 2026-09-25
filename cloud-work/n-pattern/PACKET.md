# Packet N-PATTERN: return-origin transfer for compare patterns

Base `1b122bfa` (`origin/validation/step7-d2-on-d1`), branch `cloud/n-pattern`. Everything below was measured unless it says "(read from code)".

## What changed

- **`internal/sema/return_origin_compare_patterns.go`.**
  - It now reads constant patterns: literals of every kind the checker types, negated literals, and enum variants.
  - It reads tag patterns whose payload patterns can miss, and tuple patterns.
  - A new `partial` flag keeps a refutable pattern's alternative on the unmatched continuation.
  - An untyped constant pattern keeps the named row "compare pattern needs a precise matching transfer" instead of aborting the analysis.
- **`internal/sema/return_origin_selector_syntax_test.go`.** Row `enum_pattern` of `TestReturnOriginEnumVariantTargets` is updated; see below.
- **`internal/driver/return_origin_compare_patterns_test.go` (new).** It adds 22 rows in three tests.
- **Unchanged.** `return_origin_compare.go`, including its unmatched-continuation row, is not touched. The checker is not touched either.

## The rule and why it is sound

### Coverage

The transfer decides coverage alternative by alternative. `compareChoices` lists the subject type's union members, the same `types.UnionInfo` the checker reads. After an arm without a guard, only the alternatives its pattern fully matches are removed (`return_origin_compare.go`, unchanged):

```go
if !arm.Guard.IsValid() || b.literalBool(arm.Guard, true) {
	choices = missed
}
```

`split` now keeps a partial pattern's alternative on both sides:

```go
case !p.known || choice.opaque || p.choice.opaque:
	matched, missed = append(matched, choice), append(missed, choice)
case p.choice == choice && p.partial:
	matched, missed = append(matched, choice), append(missed, choice)
case p.choice == choice:
	matched = append(matched, choice)
```

A pattern is `partial` in four cases:

- It is a constant compared at run time (`runtimeTestPattern`: a literal other than `nothing`, `true` or `false`, a negated literal, or an enum variant from `Sema.EnumVariantUses`).
- It is a tag pattern with any payload pattern that is not irrefutable (`out.partial = out.partial || !child.all`).
- It is a tuple pattern with any refutable element (`out.partial = !out.all`).
- Its choice is opaque.

A partial pattern therefore never removes an alternative. Whatever it can miss reaches the later arms, and if no arm is left it reaches the unmatched continuation, which keeps "compare has an unproved unmatched continuation" exactly as before.

### Why coverage does not come from the checker's exhaustiveness facts

The brief asked for this, and I did not do it. The checker's facts would introduce the risk the brief names.

`checkCompareExhausiveness` consumes members through `matchedUnionMembers` (`internal/sema/type_expr_compare.go:616-685`, read from code):

```go
case ast.ExprCall:
	if call, ok := tc.builder.Exprs.Call(pattern); ok && call != nil {
		if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
			if idxs := tc.matchUnionTagMembers(ident.Name, members); len(idxs) > 0 {
				return idxs
```

That code:

- **Ignores payload patterns.** `Some(1)` and `Success(nothing)` each consume their whole member.
- **Ignores the arm's guard.** `consumeCompareMembers(remaining, arm)` never reads `arm.Guard`.

So "exhaustive" in the checker's sense includes exactly the partial patterns that must not cover. Using those facts would hide the later arms the rows below pin.

I measured the effect at run time with probe `q4b`, run with the analysis gate bypassed: `compare v { Some(1) => 10; nothing => 77; }` passes the checker, and `v = Some(2)` returns a silent `0` on the VM and on LLVM. That is neither arm's value, and not a trap. The transfer therefore keeps its own structural coverage, which never lets a refutable pattern consume its alternative. This is the conservative reading; see the owner question below.

### Bindings

`bindCompareOrigins` is unchanged:

```go
value := subject.clone()
switch returnOriginView(b.function).shape(sym.Type) {
case returnOriginRefFree:
	if !b.analyzer.loanCarrier(sym.Type) {
		value = returnOriginValueOf()
	}
case returnOriginShapeUnknown:
	b.pending(sym.Span, "pattern binding needs concrete reference contents")
```

A binding at any depth, including inside a nested tag or a tuple pattern, keeps every root of the subject unless its type can hold neither a reference nor a storage loan. That is a superset of the payload's own roots, so a payload holding a reference never becomes fresh.

### Constants

The operand of a constant pattern is evaluated as a typed expression before the match (the existing `pattern.runtime` walk), and it binds nothing. A constant the checker left untyped keeps the pattern row rather than being evaluated. Today that is a string literal pattern: `compare s { "a" => 1; _ => 0; }` aborted the analysis on the base with "expression 2 is not typed", and now it is unfinished with the named row.

### Guards

The guard code is not touched, and a guarded arm still does not narrow `choices`.

### Language

The language is not widened. The checker already accepted every pattern form here; only the return-origin analysis refused them.

## The `enum_pattern` flip, and why it is intended

`TestReturnOriginEnumVariantTargets/enum_pattern` pinned the analysis of `compare c { Color::Green => a; _ => b; }` as exactly one row, "compare pattern needs a precise matching transfer", at `Color::Green`. Its own comment said "A member is not a pattern the compare transfer knows".

An enum variant is now a constant compared at run time. It can miss, so the `_` arm stays reachable. The row now requires a complete analysis with no row and no diagnostic, and `probe`'s summary with parameter sources `[1, 2]` (both `a` and `b`). If the variant were read as covering, `_` would be dropped and the sources would shrink to `[1]`.

## Rows (`internal/driver/return_origin_compare_patterns_test.go`)

Every source is a root program against the real core, with frozen sha256 digests and spans derived from unique substrings.

- **`TestAnalyzeComparePatterns`: 13 leaves, each clean with an exact summary.**
  - The shapes of the five programs: `classify` (literal, guard, `_`), `nested_tag` (`Success(Some(s))`, `Success(nothing)`, `err`), `enum_label`, `literal_kinds` (`-1`, a big literal, a `uint` and a `float`), `tuple_patterns`, `nested_compare` (a compare in an arm with guards) and `bool_arms`.
  - Rows that keep origins:
    - `literal_keeps_both` and `enum_keeps_both`: sources `[1, 2]`
    - `nested_payload_keeps_origin`: `Success(Some(p)) => p` with `p` bound to a parameter's reference, source `[0]`
    - `tuple_binding_keeps_origin`: source `[0]`
  - Coverage rows, where a partial arm is followed by `nothing` and `_`:
    - `later_arm_after_partial_tag` (`Some(1)`): sources `[1, 2]`
    - `later_arm_after_partial_nested` (`Some(nothing)`): sources `[1, 2]`
- **`TestAnalyzeComparePatternContinuations`: 4 leaves, each keeping its named row.**
  - A guard with no arm left: "compare has an unproved unmatched continuation"
  - `Some(1)` plus `nothing`: the continuation row
  - `Some(Some(n))` plus `nothing`: the continuation row
  - An untyped string pattern: the pattern row
- **`TestComparePatternEscapeIsReported`: 5 canaries, each SEM3139 for its dying local.**
  - `literal_arm_local`, which is f04's shape
  - `nested_payload_local`: `Success(Some(p)) => p` with `p` bound to `&x`
  - `later_arm_local` and `later_nested_arm_local`: `&local` in `_` after a partial arm
  - `tuple_binding_local`: `(o, n) => o` with `o` holding `&x`

Canary f04 itself, run from the plan's fence directory, is SEM3139 after the change and was unfinished on the base. Canary f03 is refused by typing (SEM3138) on both.

## Counterfactuals

All three were run against the 22 leaves, and the code was restored afterwards.

| Change | Result |
|---|---|
| Transfer reverted: the base file restored | 16 of 22 red: all finishing rows except `bool_arms`, which the base already handled, and all 4 continuation rows. The 5 escape canaries stay green, because the base also reports those SEM3139s beside its pattern rows. |
| Pattern binding made fresh (`env.assign(id, sym.Scope, returnOriginValueOf())`) | 3 red: `tuple_binding_keeps_its_origin`, `nested_payload_binding_of_a_local` and `tuple_pattern_binding_of_a_local`. Each leak became clean. |
| Partial pattern treated as covering (the `p.partial` case adds to `matched` only) | 6 red: both `later_arm_*` finishing rows (source `b` lost), both `partial_*_keeps_the_continuation_row` rows (row gone) and both `later_*_local` canaries (escape hidden) |

## Census

I diagnosed every `.sg` under `testdata/golden` (1079 files), in both command forms, with the base and the after binaries, using `cloud-work/d2-unfinished-plan/tools/run_one.sh` and `classify.py`.

| | user form: ok / unfinished / diagnostics / other | harness form: ok / unfinished / diagnostics / other |
|---|---|---|
| base | 520 / 108 / 448 / 3 | 518 / 107 / 451 / 3 |
| after | 525 / 103 / 448 / 3 | 523 / 102 / 451 / 3 |

- **Exactly the five predicted programs move from unfinished to ok** in both forms: `hir/compare.sg`, `mir/erring_option_nested_tag.sg`, `vm_compare/compare_enum_variants.sg`, `vm_compare/compare_patterns.sg`, and `spec_audit/s03_compare.sg`. The last is outside harness scope, so the harness-scope effect is 4.
- **Deviations, both expected:**
  - **`sema/valid/compare_tag_ref.sg`** stays unfinished but loses its root "compare pattern needs a precise matching transfer". Its other roots are unchanged: the three call-family reasons and the projected-payload reason. My plan predicted this; it also needs N-CLONE-COPY and N-FIELD-BORROW.
  - **`sema/invalid/struct_literal_return_empty_for_nonempty.sg`** differs, in the user form only, by the order of two SEM3015 lines. The set of lines is equal. That order varies on the base binary alone, as measured during N-TUPLE, and the checker prints these lines before return-origin runs.
- **Owner-question-4 boundary.**
  - **`mir/compare_guard_await_release.sg`** is still unfinished on "compare has an unproved unmatched continuation", and nothing else.
  - **`vm_compare/compare_patterns.sg`** carried that row at line 95 on the base, and after the change it does not. Its compare there ends with the arm `(x, y)`, which the old reader could not read. It is a tuple of bindings, so it matches every tuple, and no guard is involved. The continuation handling is unchanged; the arm is now read correctly.

Commands are as in `cloud-work/n-tuple/PACKET.md`, with the binary swapped.

## Tests

| Suite | Base `1b122bfa` | After |
|---|---|---|
| `go build ./...`, `go vet ./...` | | clean |
| `go test ./internal/sema -count=1` | ok | ok, including the updated `enum_pattern` |
| `go test ./internal/driver -count=1 -timeout 55m` | FAIL, 56 failing entries | FAIL, the same 56 entries |
| `SURGE_BEHAVIOUR_BACKENDS=vm,llvm go test ./internal/vm -run TestVMCompareGolden` | 10 failing entries | 6 failing entries |

- **Driver.** The failing-name sets are equal. The only difference is the new passing tests. The 56 are the pre-existing failures recorded in the N-TUPLE packet.
- **VM compare corpus.** `compare_enum_variants` and `compare_patterns` now pass on the VM and LLVM. The other three failures are unchanged: `compare_arm_element_read`, `counted_payload_clone_and_borrow` and `for_in_compare_reads_heap_free_union`.

## Runtime runs

Each run used `surge run --backend vm|llvm` with the after binary.

| Program | VM | LLVM |
|---|---|---|
| `vm_compare/compare_enum_variants` (`.out`, no sidecar) | 20/20 byte-equal, exit 0 | 20/20 byte-equal, exit 0 |
| `vm_compare/compare_patterns` (`.out`, no sidecar) | 20/20 byte-equal, exit 0 | 20/20 byte-equal, exit 0 |
| `mir/erring_option_nested_tag` (no `.out`) | prints `x`, exit 0 | prints `x`, exit 0 |
| `spec_audit/s03_compare` (no `.out`) | every section prints PASS, exit 0 | same |

`hir/compare.sg` has no entry point.

## Owner question: owner question 4, extended to refutable payload patterns

**Plain words.** A compare can pass the checker's exhaustiveness check and still leave values unmatched. That happens when a variant's only arm is guarded, which is owner question 4 as posed, and, measured here, when its only arm has a payload pattern that can miss. When such a value arrives, both backends return the result type's default silently. Return-origin refuses these programs with "compare has an unproved unmatched continuation", and this packet leaves that row exactly as it was.

**Example:**

```
fn pick(v: int?) -> int {
    return compare v {
        Some(1) => 10;
        nothing => 77;
    };
}
```

`pick(Some(2))` returns 0 on the VM and on LLVM (probe `q4b`, gate bypassed). The checker reports no error.

**Invariants:**

- **`docs/LANGUAGE.md:977`.** "Exhaustiveness for tagged unions is enforced: arms must cover all variants or include `finally`."
- **`docs/LANGUAGE.md:979`.** "A failed guard falls through to the next arm with the compared value expected intact".
- **`internal/sema/type_expr_compare.go:616-685`.** A tag pattern consumes its member whatever its payload patterns and guard are.

| Option | Cost | Effect on this packet's rows |
|---|---|---|
| A. Document the silent default, `default::<R>()` as the MIR lowers it, and let return-origin model it for Defaultable, borrow-free results | S | the `partial_*` and guard continuation rows would become clean rows |
| B. Make the checker count only irrefutable, unguarded arms as covering. `Some(1)` or a guarded arm then needs `finally` or a later arm. | S in the checker, plus corpus programs that rely on today's rule (count not measured) | those programs move to diagnostics; the rows stay refused |
| C. Lower the unmatched path to a trap | S in lowering and in the analysis, on both backends | the rows become clean, with a no-return continuation |

**The question.** When the arms for a variant are guarded or have refutable payload patterns, and a value of that variant matches none of them, should the compare yield the result type's default (A), be refused by the checker (B), or trap (C)?

## Ledger text for RV2-DEBT-365, residual paragraph

> **N-PATTERN (cloud/n-pattern, over `1b122bfa`).** Return-origin now reads constant patterns (literals, negated literals, enum variants), tag patterns with refutable payload patterns, and tuple patterns, instead of refusing them with "compare pattern needs a precise matching transfer" (`internal/sema/return_origin_compare_patterns.go`). A pattern that can miss is `partial`: it never removes its alternative, so later arms and the unmatched continuation stay reachable. Bindings keep every root of the subject unless they can hold nothing. Coverage is not taken from the checker's exhaustiveness, which counts `Some(1)` and guarded arms as covering. The unmatched continuation keeps its row, and `mir/compare_guard_await_release.sg` stays on it (owner question 4). No residual R-a to R-j is touched. Five golden programs move from unfinished to ok, one of them in `spec_audit`, and `compare_tag_ref.sg` loses its pattern root. Rows: `TestAnalyzeComparePatterns`, `TestAnalyzeComparePatternContinuations` and `TestComparePatternEscapeIsReported`. The canaries go red when a binding is made fresh or a partial pattern is made covering.

## Found on the way (not fixed)

1. **The checker leaves bindings untyped in a second arm on a tag that an earlier arm already matched.** For example, `compare v { Some(1) => 1; Some(k) => k; nothing => 0; }`. `k` has no `BindingTypes` entry and symbol type 0 (measured by a debug print). Return-origin then aborts the diagnosis with "pattern binding N has no exact original scope and type", on the base and after alike. I suspect `consumeCompareMembers` has already removed `Some` when the second arm's payload is typed, but that is read from code and not established. This is why the coverage rows use `nothing` and `_` rather than a second `Some(...)` arm.
2. **The checker leaves string literal patterns untyped.** On the base this aborted the analysis. Now it keeps the named pattern row.
3. **The VM panics passing `Some(nothing)` as an `Option<Option<int>>` argument.** It fails with `panic VM1003: expected composite, got nothing` at the call site. LLVM runs the same program, probe `q4c`, and prints the right values. The panic is independent of compare coverage; it happens with a full nested compare too.

## Not verified

- **Enum types as the compare subject.** The five programs compare an `int` against enum-variant constants. An `enum`-typed subject reads through the same opaque alternative, and I wrote no row for it.
- **The number of corpus programs that rely on today's checker coverage** under option B.
- **The coordinator's tripwire probes** beyond the full driver suite, whose 10 `TestH2Tripwire*` tests are unchanged.
