# Epic 15 Task 4: Noise-Band Thresholds And Root Advisory

**Status:** complete (2026-07-12).

Applied from the 3r measurements: `runtime/native` and `runtime`
`min_redundancy` re-placed at 0.245 (>= 3 commit-step bands below the
0.2514/0.2513 operating points; the old 0.25 sat inside one commit's
noise and had flapped within the kickoff window). Root scope demoted to
advisory catastrophic-only floors (equality 0.44, redundancy 0.79) with
the 46%-unresolved rationale and a written re-promotion condition (>=90%
resolution) in `SENTRUX_POLICY.md`. Every changed threshold carries a
dated in-file rationale.

Debt: RV2-DEBT-028 closed as re-baselined (quality operating point 5405,
pre-family 5484 retired as a target — the family measures DRIER than the
rest of native; the residual is inherent coupling); RV2-DEBT-029 closed
as advisory with the re-promotion condition.

All four scopes pass `sentrux check` on the committed tree.
