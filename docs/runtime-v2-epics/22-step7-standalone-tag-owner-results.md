# D2 standalone imported-tag owner certificate

Verified 2026-10-09. Base: `0e65a28bc7b8d660bc17e1e3f84db858897d9050`.
Source candidate: `50f9b9d97e8d05088f934779cecf6c1d08fbd1ab`.

## What changed

A standalone module root has no root-to-local symbol remap. Its imported core
tag copy therefore could not reach the owning `core/option.sg` declaration:
the copy retained the declaration's physical source span and module identity,
but its `Decl` fields named the importing root. `Some(...)` then stopped at
`tag constructor lacks its exact owning source declaration`.

When the using root has no remap and an owner's canonical mapping has no entry,
return-origin now uses the imported tag's retained module plus physical source
file/span to enumerate candidate declarations in that owning unit. This is only
candidate discovery. The existing `tagPayload` proof still requires the exact
owning AST item, unique owner, name, signature, generic slots, typed payload,
explicit arguments and finalized instantiation before it transfers a value.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Standalone `hash.sg`, `stable64.sg` and `xxh64.sg` root tests PASS. Existing template-tag, instantiated-caller, generic-result-loan and 59 typed-backing rows PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | Restoring only the previous tag-owner search restores the exact base records: each hash root has 4 rows and each JSON root has 296. |
| Targeted corpus | Three hash roots move to rc0. Three JSON roots lose 185 rows each, from 296 to 111, but remain unfinished. Across the six affected roots: 567 lost rows and no gained row. |
| Census | Exact clean-candidate sweep: 1187 programs, 40 unfinished, 0 timeouts. Exactly six programs change: three hash roots move to rc0 and three JSON roots lose 185 rows each; 567 rows lost, none gained, no other status/code/reason set changes. |
| Structural | Sentrux keeps quality 0.53, coupling 0.08, one cycle and the old max-cc violation. Its broad complex-function count rises from 1082 to 1083; `return_origin_tags.go` is 237 lines and passes the size gate. |

## Limits

The fallback is disabled when the using root has a publication remap; mapped
programs retain the existing canonical-symbol route. It does not accept a tag
by name, signature or module spelling alone, and it does not relax generic tag
use checks. The JSON roots still retain 111 independent return-origin rows.

No language, MIR, backend or Runtime V2 behavior changes. These are standalone
library-root analysis results, not executable backend acceptance.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-standalone-tag-owner/`:
candidate/counterfactual compilers, six targeted records, focused test logs,
Sentrux output and the resumable exact census.
