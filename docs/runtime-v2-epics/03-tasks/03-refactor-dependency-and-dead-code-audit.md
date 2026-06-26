# Epic 3 Task 03: Refactor Dependency And Dead-Code Audit

**Goal:** build a dependency-aware refactor plan for the runtime files touched
by Epic 3.

**Approach:** identify cohesive module boundaries, over-limit files, reference
graphs, and dead-code suspects before moving code.

**Skills:** `code-refactoring`, `static-analysis`,
`writing-clearly-and-concisely`

**Tech Details:** `runtime/native/`, `rg`, `make c-check`, Sentrux

---

## Files

- Create: `docs/runtime-v2-epics/03-refactor-audit.md`
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. List runtime files over or near 500 lines.
2. Map refactor clusters by responsibility, not by file size alone.
3. Identify dead-code suspects with exact symbols and reference evidence.
4. Define which suspects can be deleted in Epic 3 and which only stay noted.
5. Pick the first safe large-file tranche.
6. Start by verifying the `rt_select_poll_tasks` suspicion, but do not remove
   it without generated-IR search, ABI review, focused tests, and Sentrux
   evidence.

## Done

- Refactor tasks have dependency evidence.
- No code is deleted from search results alone.
