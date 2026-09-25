# compare pattern needs a precise matching transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 6 |
| Programs where it is the only root reason | 4 |
| Rows | 30 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_compare.go:45`.** An arm's pattern is not "known": `readComparePattern` falls through to its unsupported path (`internal/sema/return_origin_compare_patterns.go`). That happens for literal patterns (`0`, `-1`, `"x"`), enum-variant patterns (`Color::Red`), tag patterns with such a child (`Success(nothing)` inside another tag), and tuple patterns (read from code). The missing fact is how such a pattern splits the remaining alternatives.

## Corpus examples

1. **`hir/compare.sg:3`**: `0 => 0;`. This is 1 row, its only root.
2. **`vm_compare/compare_enum_variants.sg:11`**: `Color::Red => "red";`, plus line 12. It is the only root.

Others:

- `vm_compare/compare_patterns.sg:5-8`: `-1 => "neg"; 0 => "zero"; ...`
- `mir/erring_option_nested_tag.sg:12`: `Success(Some(s)) => {`
- `spec_audit/s03_compare.sg:37`, outside harness scope
- `sema/valid/compare_tag_ref.sg`

## Sound transfer and size

- **N-PATTERN, S, prototype 33 lines, measured.**
  - A constant pattern is a run-time test: a literal other than `nothing`, `true` or `false`, a negated literal, or an enum variant. It binds nothing and may match or miss, so both continuations stay.
  - A tag pattern with a partial child is partial.
  - A tuple pattern covers only when every element does.
- **What it frees.** `hir/compare.sg`, `mir/erring_option_nested_tag.sg`, `spec_audit/s03_compare.sg`, `vm_compare/compare_enum_variants.sg` and `vm_compare/compare_patterns.sg`. With N-CLONE-COPY and N-FIELD-BORROW it also frees `compare_tag_ref.sg`. The two `vm_compare` programs run to their golden `.out` on both backends.

## Unsoundness risk

- **Marking a partial pattern as covering would drop the unmatched continuation.** That could hide an escape in a later arm. Canary `f04` (a literal arm that returns a reference to a local) goes from unfinished to a precise SEM3139 under the prototype (measured).
- **One test row flips.** Row `enum_pattern` of `TestReturnOriginEnumVariantTargets` pins today's row on an enum-variant pattern ("A member is not a pattern the compare transfer knows", `internal/sema/return_origin_selector_syntax_test.go:278-291`). It is the only sema test the prototype changes, and the packet must turn it into a cleared row.

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed.
