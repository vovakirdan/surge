---
name: surge-issue-fixer
description: "Fix Surge compiler, language, stdlib, VM, LLVM backend, semantic analysis, parser, runtime, tests, or golden-fixture GitHub issues end to end. Use when a user asks to take a Surge issue, fix a Surge bug/regression/API problem, update Surge .sg behavior, add unit and golden tests, run make check or make golden-update, prepare conventional commits with closes #N, open a PR, address review, or merge after approval."
---

# Surge Issue Fixer

## Operating Model

Treat a Surge issue as a compiler-change workflow, not a patch-only task. Read the issue, reproduce or narrow the failure, inspect the current repo, change the smallest coherent layer set, prove it with tests and goldens, then publish through a reviewed PR.

Use this skill with `surge-language` for `.sg` code, `compiler-development` for frontend/MIR/type-system changes, `llvm` for IR lowering, `use-modern-go` for Go edits, `coding` for general implementation quality, and `sanitizers` or `static-analysis` when touching native/runtime memory paths.

## Workflow

1. Resolve context.
   - Fetch the GitHub issue and PR/review state from live sources.
   - Identify the affected layer: stdlib `.sg`, parser/AST, sema/types, MIR/lowering, VM, LLVM backend, runtime C/C++, diagnostics, formatter, or docs.
   - Search with `rg` first; read nearby tests before editing.

2. Isolate work.
   - Work in a separate branch or worktree named like `fix/issue-N-short-topic`.
   - Do not work directly on `main`.
   - If local `.git` is read-only, say so, keep the workspace changes scoped, and use the GitHub connector/API only when it is the available publish path.
   - Preserve unrelated user changes; do not reset, checkout, or clean them away.

3. Reproduce and design the fix.
   - Prefer a minimal failing `.sg` example or unit test before implementation.
   - If the issue exposes an API smell, fix the public API and all compiler backends together. For opaque stdlib types, prefer public builders/helpers over user access to opaque fields.
   - For behavior used by both VM and LLVM, update both paths or explain why one is intentionally out of scope.

4. Implement with repo style.
   - Follow the existing package boundaries and naming.
   - Use Go version from `go.mod`; run `gofmt`.
   - Keep files under project file-size gates; split helpers before `make check` complains.
   - Do not add broad abstractions unless the repo already points that way.
   - For LLVM tests, assert lowered IR properties and absence of leaked external intrinsic calls where relevant.
   - For VM tests, assert observable program exit/stderr and edge cases, not implementation details only.

5. Test at the right levels.
   - Add focused unit tests near the touched package, for example `internal/sema`, `internal/vm`, or `internal/backend/llvm`.
   - Add golden fixtures when syntax, formatting, diagnostics, public stdlib examples, or language surface behavior changes. Use the repo's canonical golden location, commonly `testdata/golden` or `tests/golden`.
   - Use `SURGE_STDLIB=$(pwd)` when running compiler tests that import stdlib.
   - Prefer caches under `/tmp`, for example `GOCACHE=/tmp/surge-go-cache` and `GOLANGCI_LINT_CACHE=/tmp/surge-golangci-lint-cache`.
   - If touching C/C++ runtime or memory-sensitive paths, run available ASan/UBSan/sanitizer targets; if the repo has no sanitizer target, state that clearly.

6. Verification gate.
   - Run targeted tests first.
   - Run `make check`.
   - Fix every failure, including lint, formatting, generated files, file length, and flaky assumptions.
   - After `make check` passes, run `make golden-update` when golden coverage is relevant or requested. Review the generated diff; do not accept unrelated churn.
   - Re-run any tests invalidated by golden changes.

7. Commit and PR.
   - Commit only after checks pass.
   - Use Conventional Commits and include `closes #N`, for example `fix: lower duration intrinsics (closes #82)`.
   - PR body must include summary, tests run, golden update status, and known limitations.
   - Wait for CI and review. Address every actionable review comment, including nits that improve regression coverage.
   - After review fixes, re-run targeted tests plus `make check`; re-run `make golden-update` only if sources/goldens changed.

8. Merge only when asked.
   - Confirm review and checks are green.
   - Merge with the repository's normal method, usually squash for issue branches unless maintainers indicate otherwise.
   - After merge, switch/update to current `main`. If local git metadata cannot be updated, report the exact filesystem/network blocker and the remote merged SHA.

## NEVER

- NEVER claim the issue is fixed without naming the commands that passed.
- NEVER update goldens to hide a compiler bug; goldens are regression evidence, not a way to erase failures.
- NEVER skip one backend when the language feature has VM and LLVM behavior.
- NEVER expose or depend on private opaque stdlib fields in user-facing `.sg` code when a builder/helper belongs in the API.
- NEVER commit before `make check` has passed.
- NEVER leave review threads unresolved after fixing them.
- NEVER merge a PR without explicit user approval.
- NEVER overwrite unrelated dirty work in the shared workspace.

## Surge-Specific Heuristics

- Duration/time-style APIs should prefer whole nanosecond `int64` semantics unless the issue explicitly requires fractional units.
- Module-qualified type APIs such as `time.Duration.new()` may need sema support for type operands, not just backend lowering.
- Intrinsics must be traced through declaration, sema resolution, MIR shape, VM dispatch, LLVM lowering, and runtime symbols.
- Diagnostics fixes need negative tests or golden diagnostic fixtures.
- Formatter/parser changes need token, AST, and formatted-source goldens where the repo supports them.
