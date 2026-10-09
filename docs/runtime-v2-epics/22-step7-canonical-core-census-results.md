# D2 canonical core-mirror census

Verified 2026-10-09 with compiler `ad9ccb7648424f9bdf5cf36f9469cd5504736acd`.

## Correction

The D2 sweep treated each byte-identical `testdata/golden/core_stdlib/*.sg`
mirror as a user module. That contradicts the SC-C owner ruling: mirror source
drives tokens/AST/fmt, while `.diag` must diagnose the absolute canonical
`core/*.sg` path so the declaration keeps core identity.

The corrected sweep retains all 1187 record keys and mirror input hashes. For
the ten core-mirror keys only, the diagnostic command uses the matching absolute
`core/<name>.sg`; identity records the complete mapping. Every other command,
timeout, worker count and binary is unchanged. The compiler identity guard is
unchanged: `TestCoreRootIdentityCannotBeClaimedByACopy` still proves an actual
copy outside the stdlib root is uncertified.

## Verification

| Check | Result and limit |
| --- | --- |
| Source precondition | All ten mirror `.sg` files are byte-identical to their canonical core files, as established by SC-C. |
| Canonical roots | All ten absolute `core/*.sg` diagnostics exit 0 with the exact candidate binary. Relative/user-copy controls remain refused. |
| Census | 1187 records, 11 to 1 unfinished, 0 timeouts. Exactly ten mirror keys change rc1 to rc0; 2284 rows disappear and none appears. Census SHA-256 is `9a99485711b8fbe195a213e9be970337dcc84094a4b4846fdc0bfe86f61a236a`; comparison SHA-256 is `97d299e6b73c3d363d0fd2f0e6be237fdaaeb30685c67629417230e407f81d65`. |
| Identity | Identity SHA-256 is `981d03506af2768c96eb3ce624c730a295f260eb862aad5bda1fd1a02b875c68`; it records compiler `ad9ccb76`, empty diff, binary SHA-256 `9cb4212aab54afefcb80983f420a03894c1106f7f3fca6bb9c362c20859ae0f1` and all ten canonical source paths. |
| Remaining row | The sole unfinished record is `array_view_facts_a_resize_rule_withdraws.sg`, owned by RV2-DEBT-475. |

## Limits

This is a census/golden routing correction. It grants no identity to mirror
paths or arbitrary byte-identical copies and changes no compiler behavior.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-canonical-core-census/`:
the exact runner, identity, census, comparison and per-core diagnostic outputs.
