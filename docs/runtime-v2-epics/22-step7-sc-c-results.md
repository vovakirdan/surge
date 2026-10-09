# SC-C: synchronize the core golden mirrors

Verified 2026-10-09 on `eaf1864a8b2b7393623083fe703e256fbae06153`.

## Scope

`testdata/golden/core_stdlib` keeps ten source mirrors of `core`. The owner
ruling of 2026-09-28 requires byte equality: tokens, AST and formatting come
from the mirror, while `.diag` is produced by diagnosing the real file under
the stdlib root so it retains core identity.

Fresh SHA-256 inventory found nine mirrors already equal. Only
`intrinsics.sg` differed. It lacked the six `@return_source` annotations already
present on dynamic/fixed array mutation, Map reference/mutation, and the two
array index declarations in `core/intrinsics.sg`. SC-C copies those six
annotations and nothing else. All ten `.sg` mirrors are now byte-identical to
their canonical `core` files.

## Generated outputs

The current compiler regenerated `intrinsics.tokens`, `intrinsics.ast` and
`intrinsics.fmt`; diagnosing the absolute real `core/intrinsics.sg` produces
the unchanged empty `intrinsics.diag`. A second independent generation produced
the same four hashes. The large token diff is index renumbering after the six
new attribute token sequences, not a source expansion.

The EXITCODE golden updates named by the earlier SC-C plan were already landed
in `59906d4e` (`feat(entrypoint): use ExitCode and release the borrowed main
result`): 28 golden files, including the `core_stdlib` entrypoint, Option and
Result mirrors and their renumbered outputs. They are not regenerated or
duplicated here.

## Golden gate limit

The full sanctioned `make golden-update` ran over the complete corpus after
the mirror sync and reached the generator. It stopped on exactly the 22 known
valid-case/emit failures retained by G5; no SC-C-specific failure appeared, and
the mutator rolled its staged tree back. This is RV2-DEBT-421's existing gate
state, not a SC-C PASS. The frozen manifest still records 5487 entries while the
current corpus has grown, so SC-C does not close or weaken RV2-DEBT-421.

The current corpus must be refrozen only after those generator failures are
resolved or explicitly reclassified by their owning work. Until then the
authoritative SC-C checks are byte equality of all ten sources, deterministic
generation of the four `intrinsics` outputs, and diagnosis of the real core
file.
