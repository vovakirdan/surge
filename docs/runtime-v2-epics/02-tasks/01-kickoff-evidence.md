# Epic 2 Task 01: Kickoff Evidence

**Goal:** start Epic 2 from a clean, explicit baseline.

**Approach:** record current git state, accepted VM debt, Sentrux state, and the
first runtime-code gate before changing runtime code. This task does not edit
runtime implementation files.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `git`, `make check`, Sentrux root/runtime scans,
`docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`

---

## Files

- Modify: `docs/runtime-v2-epics/NOTES.md`
- Modify: `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`
- Create or modify: `docs/runtime-v2-epics/02-evidence.md`

## Steps

1. Record `git rev-parse HEAD` and `git status --short`.
2. Record that `go test ./internal/vm -run 'MT|Async|Net|LLVM'` is accepted
   backend-test debt, not an Epic 2 start blocker.
3. Run Sentrux scan, `health`, and `check_rules` for:
   - `/home/zov/projects/surge/surge`
   - `/home/zov/projects/surge/surge/runtime`
4. Record missing `.sentrux/rules.toml` as a blocker to rule compliance unless
   this task creates the rule file.
5. Run `git diff --check`.
6. Run `make check`.
7. Update `NOTES.md` with the exact start state and next task owner.

## Done

- Epic 2 has a current evidence file or section.
- Missing Sentrux rules are either fixed or explicitly deferred.
- Accepted VM debt remains named.
- Worktree is clean after commit.
