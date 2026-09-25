# Owner question 1: the `core_stdlib` copies

Everything here was measured unless it says "(read from code)".

## What happens

`testdata/golden/core_stdlib/` holds a copy of the ten files of `core/`. The harness refreshes it before every run (`scripts/golden_update.sh:255-256`) and then diagnoses each file as an ordinary entry program. The copy starts with `pragma module, no_std;` and lives in a directory named `core_stdlib`, so the driver does not treat it as core (read from code: `validateCoreModule` only reserves `core` and `core/...`, `internal/driver/module_validation.go:36`). The analysis therefore sees a user module that declares its own intrinsics. Every identity certificate refuses those intrinsics, because a certificate asks for the real declaration, not for one with the same name.

| Fact | Value |
|---|---|
| Unfinished programs | 10, one per copied file |
| Rows per program | 301 to 310 |
| Root reasons per program | 14, or 16 for `string.sg` |
| Row count after copying `core/` afresh (`base.sg`) | 305, the same 14 roots |
| `surge diag core/base.sg`, relative path | `core namespace reserved` (`internal/driver/diagnose.go:527`) |
| `surge diag $PWD/core/base.sg`, absolute path | accepted as core; 38 rows; 2 root reasons at 8 call sites |

The 8 call sites left when the real core is the root program are all explicit tag constructors inside core bodies: `core/array.sg:163`, `182`, `201`, `220` and `239` (`return Some(i to uint);`), `core/entrypoint.sg:84` (`return Success(text);`), `core/result.sg:34` (`err => Some(err);`) and `core/string.sg:341` (`return Success(s.__clone());`). Each carries "captured binding requires origin finalization" and "tag constructor lacks its exact owning source declaration". When core is analysed as the dependency of a user program, these same bodies are clean. The D2 gate relies on exactly that (DEBT.md:229, first sentence).

## The invariant in the way

`internal/sema/return_origin_core_identity.go:5-8`:

> A retained body-less core intrinsic, by its original declaration and publication: builtin, intrinsic, synchronous, from core/intrinsics at this template and receiver arity, with no defaults or variadics, published under its own body key. Its return-source promise is its reader's to check. A name alone selects nothing.

The check itself is at `return_origin_core_identity.go:15`, and it requires `c.SourceKey == "builtin"`, `c.ModulePath == "core/intrinsics"` and `u.SourceKey == "core/intrinsics.sg"`. A copy meets none of the three.

## Options

| Option | What changes | Frees | Cost | Risk |
|---|---|---:|---|---|
| A. Stop running `diag` on `core_stdlib/` in `golden_update.sh`, keeping its token, AST and format outputs | harness script, and the D2 criterion's wording | 10 | S: one exclusion and a comment | Core's own bodies get no root-program diagnosis. They are still analysed as a dependency in every other golden program. |
| B. Diagnose the copies as core: point the harness at the real `core/` by absolute path, then answer the 8 tag-constructor sites for core as root | harness, plus a small packet N-CORE-ROOT-TAGS | 10 | S harness, S to M analysis (estimate, read from code) | Low. The certificate keeps its identity rule. |
| C. Certify the copy by its content, since it is byte-identical to `core/` | analysis | 10 | M | Breaks "a name alone selects nothing". Any `no_std` user module could claim core's contracts. Rejected. |
| D. Prove the copy as user code: answer all 14 to 16 reasons for a user module that declares body-less intrinsics | analysis | 10 | L | A body-less user intrinsic has no runtime contract to read, so most rows can only be refused. This would be the refusal, not a proof. |
| E. Change the D2 criterion to say "every program except the `core_stdlib` copies" | docs only | 10 | none | The same as A without touching the script. |

## The question

Should the ten `core_stdlib` copies be diagnosed as core (B), taken out of the `diag` step (A or E), or proved as user modules (D)?

My recommendation is B. It keeps a root-program check of core's bodies, and the analysis change is limited to the 8 measured tag-constructor sites.
