# generic use disagrees with its original typed operation

| Measure | Value |
|---|---:|
| Programs carrying it | 3 |
| Programs where it is the only root reason | 0 |
| Rows | 9 |
| Of those programs, `core_stdlib` copies | 1 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **Text defined at `internal/sema/return_origin_synthesized_uses.go:14`, reported by `checkGenericUses`** (`internal/sema/return_origin_generics.go:175-179`). A finalized generic use sits at a site whose original typed operation is not the call the use names.
- In the non-core programs the site is an array `+` inside a core string body. The missing fact is that the binary `+` is the certified core array concatenation instance.

## Corpus examples

1. **`vm_strings/strings_std.sg`**, through `core/string.sg:311`: `prev = prev + one;` with `prev, one: uint[]`, plus `:319` and `:331`.
2. **`vm_strings/strings_rope_std.sg`**, at the same three sites.

The third carrier is the `core_stdlib/string.sg` copy.

## Sound transfer and size

- **N-CONCAT-USE, S, prototype 35 lines, measured.** A use at a binary expression whose `MagicBinarySymbols` entry selects the certified core array concatenation, with agreeing element type arguments, is that operation's instance.
- **What it frees.** Together with N-CORE-TEMP, `strings_std.sg` and `strings_rope_std.sg`. Both then run to their golden `.out` on both backends.

## Unsoundness risk

Low. The certificate is by identity.

## DEBT-365 and DEBT-368

None named for this reason. The programs also need N-CORE-TEMP, owner question 3(a).

## Owner decision

None for this reason. For the copy, see `core-copies.md`.
