# Runtime V2 Working Rules

These rules govern Runtime V2 planning, implementation, review, and evidence.
They are stricter than ordinary project notes because scheduler and ownership
mistakes are hard to see after they enter the runtime.

## Rule Levels

- **MUST:** blocks task completion, epic completion, or merge.
- **MUST NOT:** forbidden unless a later accepted rule explicitly changes it.
- **SHOULD:** expected default. A task may deviate only with a recorded reason.
- **MAY:** allowed, but not required.

## Global Rule 1: MUST Rules And Proving Spikes

A `MUST` rule blocks completion. It does not block a clearly marked experiment.

When an idea cannot be proven by discussion alone, use a proving spike. A
proving spike is allowed to violate a `MUST` temporarily only if it records all
of the following before implementation starts:

- the hypothesis being tested;
- the files, paths, or runtime surfaces it may touch;
- the behavior that is explicitly not final design;
- the proof test, benchmark, trace counter, invariant, or negative compile test;
- the success and failure criteria;
- the rollback or rewrite note if the hypothesis fails.

A proving spike must not be silently promoted into architecture. After the proof
run, the team must either accept the result as a design input, rewrite it into a
rule-compliant implementation, or delete it.

The purpose is to answer "will this work?" without letting temporary shortcuts
become hidden runtime semantics.

## Global Rule 2: Runtime Must Be Explainable

Every accepted Runtime V2 change must be explainable through ownership and
wakeup paths. If the path cannot answer these questions, the design is not ready:

- who owns the state;
- who may mutate it;
- who wakes the task or shard;
- who cleans it up on cancellation;
- which lifetime or generation protects stale work;
- where backpressure is applied;
- which trace, test, or invariant exposes a violation.

## Global Rule 3: Sentrux Is A Required Gate

Runtime V2 must improve the runtime's structure, not only make tests pass.
Sentrux is therefore mandatory for every Runtime V2 epic.

Before implementation starts:

- run a repository scan and record the `quality_signal`;
- run a scoped scan for the main affected directory, usually `runtime/`;
- call `session_start` for the scan that will be used for the final diff.

Before completion:

- rerun the same scoped scan;
- call `health` and record the root cause breakdown;
- call `check_rules`;
- call `session_end` and record whether quality changed.

A lower `quality_signal` blocks completion unless the task is a documented
proving spike or the epic records an accepted recovery task. If no
`.sentrux/rules.toml` exists for the scanned path, record that as a blocker or
create the rules file as part of Epic 1.

Sentrux can scan a subdirectory by absolute path. The active sentrux context is
the last scanned path, so root and scoped evidence must both name the path that
was scanned.

## Global Rule 4: File Size And Modularity

Runtime V2 code must stay small enough to review. The hard line for new or
heavily rewritten code files is 500 lines.

- New runtime or compiler code files MUST be `<=500` lines.
- New test files SHOULD be `<=500` lines. If a test file must be larger, split
  fixtures, helpers, or scenarios first.
- Existing files over 500 lines MAY be touched, but the task MUST NOT make them
  larger unless it is a proving spike.
- Any task touching an over-limit file MUST record whether it reduces the file,
  keeps the line count flat, or creates a follow-up split task.
- Do not create vague catch-all files such as `misc`, `helpers`, or `common`
  unless the epic defines their exact responsibility.

## Global Rule 5: Reuse Before New Machinery

Before adding a runtime primitive, queue, table, helper, or abstraction, check
the existing code path first. A new primitive is allowed only when the task
records:

- why the existing primitive is wrong or insufficient;
- which invariant the new primitive owns;
- how cancellation, shutdown, and error paths use it;
- which test or trace proves it behaves correctly.

Duplicated data structures are a design smell. The default is to reuse or split
an existing primitive by ownership, not to add a parallel mechanism.

## Global Rule 6: Required Code Checks

Every Runtime V2 code task must record the checks it ran. Native runtime changes
must include:

```bash
git diff --check
make c-check
make cppcheck
make check
```

Runtime and VM liveness checks must use exact focused probes from
`LIVENESS_PROBES.md` or the active epic's CI contract. Do not use the broad
focused VM command `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a
required green gate until the later test/backend matrix epic fixes or replaces
that accepted debt.

Scheduler, wakeup, cancellation, channel, timer, and shutdown changes also need
a liveness proof. Performance-sensitive changes also need benchmark and trace
evidence, not only passing tests.

If a command is skipped, record the exact reason and whether the task is blocked
or can continue as a proving spike.

## Global Rule 7: Comments And Names

Names must expose ownership and lifecycle. Prefer names that state the runtime
role, such as `owner`, `shard`, `generation`, `waiter`, `inbound`,
`remote_free`, `park_state`, or `credit`.

Comments are for invariants, memory ordering, ownership transfer, cancellation,
wakeup races, and non-obvious failure handling. Do not add comments that merely
repeat the code.

## Global Rule 8: Owner-Oriented C And Explicit Status

Runtime V2 C code must be owner-oriented, data-explicit C. Each module owns one
runtime concept, such as a shard, fd registry, waiter table, inbound queue,
timer structure, channel queue, or remote-free queue.

New V2 C APIs MUST:

- take the owner pointer as the first argument when they mutate owned state;
- expose a clear lifecycle: `init`, optional `start`, `stop` or `drain`, and
  `destroy`;
- return an explicit status code for allocation failure, syscall failure,
  capacity failure, cancellation, shutdown, and invalid state;
- make callers handle or propagate every non-OK status;
- clean up partial initialization through one documented failure path.

New V2 C APIs MUST NOT:

- use `panic_msg` for recoverable errors;
- return plain `bool` when the caller needs to distinguish failure causes;
- store borrowed pointers across await, thread, or shard boundaries;
- add mutable global state on the hot path;
- hide lifecycle work in helper side effects.

`panic_msg` is reserved for violated internal invariants. A proving spike may
temporarily bridge an old boundary only when its pre-recorded rollback requires
the bridge to be deleted before production integration. An accepted Runtime V2
migration MUST NOT leave a compatibility adapter, dual ABI, or panic-based
fallback in the final path.

Implementation-level synchronization rules belong in the epic or task that
introduces a concrete primitive. Global rules define the development contract,
not the lock strategy.

## Global Rule 9: Subagent Plan Gate

Subagents that implement, test, or review Runtime V2 work must start with a
plan-only pass. The subagent must state:

- the exact task and files it will touch or inspect;
- the tests, static checks, sentrux scans, and evidence it expects to run;
- the non-goals and out-of-scope surfaces;
- the commit boundary, if the task can close independently;
- the risks or open questions that could change the plan.

The main agent must approve or revise that plan before the subagent edits files
or runs destructive or long-running work. If the tool cannot start a real plan
mode, emulate it by instructing the subagent: no edits and no implementation
until the plan is approved.

Review subagents follow the same gate: they first propose the review surface and
checks, then start the review after approval.

Parallel implementers and reviewers MUST use separate temporary worktrees from
an explicitly recorded base commit. Their plans must declare non-overlapping
file/symbol ownership or the dependency that serializes overlapping work. They
must not edit a shared dirty checkout or integrate one another's branches.

The main agent reviews the intended commit/diff before integrating it. Every
integration wave and final epic diff require review by a non-author from a clean
worktree. Review findings are classified as blockers or durable debt; memory
safety, ownership/lifetime, liveness, accepted semantics, required diagnostics,
golden stability, and mandatory-gate failures are blockers.

## Global Rule 10: Keep Working Notes Current

Runtime V2 work must keep a live notes file. The notes file is
`docs/runtime-v2-epics/NOTES.md` unless an epic names a more specific notes
file.

Update notes:

- before starting a new task, with the current context and intended proof;
- after changing code or docs, with what changed and why;
- after each test, benchmark, trace run, or sentrux pass, with the result and
  exact command or tool path;
- when a path is proven wrong, with the reason it should not be retried;
- when an invariant, assumption, or open question changes.

Notes MUST make context switching cheap. A reader should be able to answer:

- what exists now;
- what was just changed;
- what has been tested;
- what has not been tested;
- which paths are known dead ends;
- what must be decided before the next task.

Notes are a working memory, not final architecture documentation. At the end of
each epic, consolidate the durable parts into the relevant epic document,
`README.md`, `RULES.md`, `docs/RUNTIME_V2.md`, or another linked document. Do
not leave important decisions only in notes.

## Global Rule 11: Friendly Compiler Diagnostics

Surge is a friendly language. When the compiler can prove an invalid program,
it MUST reject it at the earliest reliable stage instead of selecting a runtime
fallback. The diagnostic must use the facts the compiler already knows:

- primary span at the operation the user can change;
- concrete type and first relevant field/type path when available;
- a note explaining the ownership, lifetime, layout, effect, or crossing rule;
- actionable help;
- a machine-applicable fix only when the compiler proves that edit preserves
  intent.

Ambiguous advice remains a note/help, never an automatically applicable edit.
Generic checks that cannot be decided before monomorphization record a deferred
obligation and report a source diagnostic at concrete instantiation; a raw
internal/monomorphization error is not an acceptable user experience.
Optional diagnostic advice is different: when a generic fact is unknown, defer
only that advice text. It MUST NOT create a semantic constraint, reject an
instantiation, or alter the primary diagnostic unless the source operation
itself requires the property.
Advice must also be syntactically and semantically callable at the reported
site: a Copy fact must not become a fictitious `.__clone()` call, and an
"implement this method" hint is legal only when the compiler proves the target
type is extendable there. LSP edits carry the analyzed document version and an
old-text guard; stale diagnostics expose no Code Action and trigger fresh
diagnostics.

Diagnostic evidence MUST exercise the user-facing compiler path, not only
construct an internal `Diagnostic`. When ordinary goldens omit notes or fixes,
add direct structure tests and apply-and-rediagnose tests for every safe fix.

## Global Rule 12: Golden Stability

Compiler-output work MUST start from a passing `make golden-check` baseline
followed by zero output from
`git status --porcelain=v1 --untracked-files=all -- testdata/golden` and a
NUL-safe filesystem-versus-`git ls-files` census of every generated golden root,
including ignored files. Any tracked, untracked, ignored, or missing entry
blocks the baseline. This target runs `golden-update` before comparing with the
checked-in corpus; it is an updating detector, not a read-only preflight. Before
the first compiler-output wave, the target itself MUST gain the equivalent
fail-closed post-generation status and filesystem/index checks.

After each integration wave, run `make golden-check`. If output changes, the
command fails and leaves the generated diff as a proposal. Inspect and explain
every changed line and every new/deleted sidecar before accepting exactly that
reviewed set. Blanket reblessing is forbidden. An untracked generated file
remains a proposed update until reviewed and committed. Record a complete
path-plus-content-hash manifest, regenerate again, and require byte-for-byte
run-to-run identity plus zero status and filesystem/index differences. AST/MIR
trees may rebuild or renumber only when the semantic reason is recorded and the
resulting output is deterministic.

After the reviewed goldens are integrated, epic closeout requires two
serialized `make golden-check` runs on the same tree with no intervening source
edit, zero tracked/untracked/ignored/missing status after each run, and an
unchanged reviewed content manifest.
