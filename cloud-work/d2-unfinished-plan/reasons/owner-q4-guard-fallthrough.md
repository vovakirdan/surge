# Owner question 4: a compare whose guarded arm fails with no arm left

Everything here was measured unless it says "(read from code)".

## Plain words

`mir/compare_guard_await_release.sg:12`:

```
return compare a.await() {
    Success(x) if compare b.await() { Success(v) => v; Cancelled() => false; } => x;
    Cancelled() => 0;
};
```

The checker accepts this compare as exhaustive, because it has one arm for `Success` and one for `Cancelled`. When `a` succeeds and the guard is false, no arm matches. Return-origin sees that path and refuses with "compare has an unproved unmatched continuation" (`internal/sema/return_origin_compare.go:105-108`). It joins an Unknown value into the result there.

## What the runtime does

- **Both backends produce a silent zero.** Scratch probe `c03` is `compare v { Some(x) if x > 5 => x; nothing => 77; }`, and `pick(Some(1))` returns 0 on the VM and on LLVM. It returns neither 77 nor a trap. A second probe, `c02`, has a `Word(s)` arm after a failed `Num(x) if ...` guard, and it also returns 0 on both backends, so control does not fall into the other variant's arm.
- **The MIR shows why.** In the committed MIR golden `mir/compare_guard_await_release.mir`, the no-match edge of the `switch_tag` goes to a block that calls `default()` for the result (`L24 = call default()`).

## The invariants in the way

- **`docs/LANGUAGE.md:977`.** "Exhaustiveness for tagged unions is enforced: arms must cover all variants or include `finally`."
- **`docs/LANGUAGE.md:979`.** "A failed guard falls through to the next arm with the compared value expected intact". The document says nothing about a failed guard when no later arm matches.

## Options

| Option | Frees | Cost | Risk |
|---|---:|---|---|
| A. Document the lowering: the unmatched continuation yields `default::<R>()` of the result type. Return-origin then models it as that default, and requires `R` to be proven Defaultable with no borrowed state. | 1 | S | A result type that is a reference or a handle cannot take this path and keeps a named refusal. The language quietly gains a default-valued path. |
| B. Count a guarded arm as not covering its variant, so the checker demands `finally` or an unguarded arm. The program moves to `invalid/` or gains `finally`. | 1 | S in the checker, plus the corpus programs that rely on it | Narrows what compiles today. Other golden programs may rely on guarded coverage; I did not measure how many. |
| C. Lower the unmatched continuation to a trap. Return-origin treats it as no-return. | 1 | S in lowering, S in analysis, both backends | Changes run-time behaviour from a silent zero to a panic. |

## The question

When the only arm for a variant is guarded and the guard fails, what is the value of the `compare`: the result type's default (A), a compile-time refusal (B), or a trap (C)?
