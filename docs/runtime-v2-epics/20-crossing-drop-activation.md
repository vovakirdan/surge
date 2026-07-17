# Epic 20: Crossing Drop Activation (vertical 2 of the reclamation arc)

**Status:** IN EXECUTION — design review passed 2026-07-17: direction
and the FULL+expanded scope approved (Epic 19 closeout, RV2-DEBT-040,
local select send-arm ownership RV2-DEBT-048, crossing drop
activation RV2-DEBT-034 incl. remote send-arm symmetry, crossing
census). Fork 6 RESOLVED to local-plus-remote symmetry inside this
epic (local semantics Task 3; remote commit-bit work inside Task 7).
Forks 1-5 accepted at their recorded leanings (codex-endorsed).
Task index: `20-tasks/README.md`. Vertical 2 of the arc approved
2026-07-14 (`19-candidates.md`: local emission → crossing activation
→ owner-routed frees).

Draft v2 incorporated the codex second-opinion review (2026-07-17):
the activation work is split per crossing surface, remote select's
missing stateful lowering is scoped honestly, a fifth fork
(first-poll ownership) is recorded, and the review's two confirmed
incidental bugs are ledgered (RV2-DEBT-047; the fixed-width range
residual noted under Task 2).

Draft v3 (same day) corrects v2's fork-6 evidence and adds Task 3:
runtime-payload probes found a LIVE local soundness hole — select
send arms are invisible to move tracking, so a winning arm
double-frees deterministically (RV2-DEBT-048, upgraded from a
marker inconsistency) — and a valgrind-mode silent-exit hazard for
census harnesses (RV2-DEBT-049). String literals are static and
never heap-allocate; every reclamation row must use runtime-built
payloads.

## Why This Epic Exists

Epic 19 made local reclamation real: leaves, scope-exit synthesis,
statement temporaries, per-arm drops, and recursive composite glue all
ship (`19-tasks/04-recursive-glue.md`, Status: A + B + C SHIPPED).
The crossing side is still a stub. The crossing surfaces that ship an
owned state — spawn-on, placement `on`, and anchored `on ch` — already
carry a drop obligation end to end (pending rows, refusal paths, the
single final-release drop site), but the dispatch target is drop-fn
id 0, which is never registered: an owned payload abandoned
mid-crossing (cancel before first poll, refusal, stale generation)
leaks by design.

The language gate recorded on RV2-DEBT-034 ("no compiled drop function
exists because the LANGUAGE emits none") is now LIFTED: the recursive
glue Epic 19 built is exactly the function family the dispatch needs.

Two loose ends ride along:

- **RV2-DEBT-040:** `for` loops leak per iteration on LLVM (each
  `iter_next` boxes an `Option<T>`; the iterator cursor is never
  freed). It is NOT a logical blocker for the crossing census (gate
  programs can avoid `for`), so it runs as a parallel lane, not a
  serial prerequisite — but landing it lets census gate the EXISTING
  loop-based crossing programs unmodified.
- **Epic 19 Task 5** (bench + debt updates + closeout) never ran; the
  arc's vertical-1 record is incomplete until it does.

## Starting State (evidence, verified 2026-07-17)

- Dispatch stub: `__surge_drop_call(i64 id, ptr state)` is emitted
  with an empty switch and a default panic arm ("missing drop
  function") — `internal/backend/llvm/emit_async.go:76-96`. Drop-fn
  id 0 is passed everywhere and dispatched nowhere.
- Runtime plumbing (dormant, row-proven at id 0): `state_drop_fn_id`
  flows through remote spawn (`rt_remote_spawn.h:45,51`,
  `rt_remote_spawn_pending.c`), remote task await/cancel
  (`rt_remote_task_pending.c`, `rt_remote_task_internal.h`), and
  immediate `on` incl. anchored (`rt_immediate_on.c`,
  `rt_immediate_on_anchored.c`).
- **Remote select ships NO compiled state today**: LLVM lowers the
  selector with `state_drop_fn_id = 0` and `state = null`
  (`internal/backend/llvm/emit_crossing_select.go:111-118` — the
  `rt_far_channel_select` call passes literal `i64 0, ... ptr null`).
  Its abandon obligations are runtime-owned (arm table, pins,
  pending), not compiled payloads. Real-glue rows for select are
  impossible without new stateful select lowering — see fork 6.
- **Ordinary far-channel create/share carries no crossing state-drop
  obligation.** The crossing surfaces for this epic are exactly:
  spawn-on, placement `on`, anchored `on ch`, and remote select.
- **rt_task carries NO drop-fn id** (`rt_async_internal.h:206`: `id`,
  `generation`, `poll_fn_id`, `state`, results — nothing else), and
  `__task_state` detaches `task->state` into compiled code
  (`rt_async_task.c:127`). Cancellation-before-first-poll is therefore
  NOT a runtime direct-drop path today: after publication, reclamation
  is the compiled body's job. The epic must not describe pending
  final-release glue as covering it — see fork 5.
- **Confirmed incidental bug (RV2-DEBT-047):** the shutdown drain
  `rt_remote_spawn_fail_all_pending` (`rt_remote_spawn.c`) panics on
  queued `IMMEDIATE_ON_*` / `FAR_CHANNEL_*` / `CREDIT_CONTROL`
  messages — its kind allowlist predates Epics 13-17 — even though
  `rt_remote_task_release_msg_payload`
  (`rt_remote_task_completion.c`) already releases those payload
  kinds. Directly threatens abandon-path coverage during shutdown;
  fixed inside this epic (Task 5 precondition).
- Compiled glue exists: recursive composite drop functions for
  structs, tuples, unions, fixed/dynamic arrays, nested composition
  (commit `7d7f5230`, `19-tasks/04-recursive-glue.md`). The known
  class risk is recorded there: a runtime-backed type whose field
  type lies about ownership (BytesView was the only one found).
- RV2-DEBT-040 shape: `emitIterNext`
  (`internal/backend/llvm/emit_iter.go:115`) allocates an `Option<T>`
  box per iteration; the iterator struct (array or range cursor) is
  allocated once; drop emission reclaims neither. Recorded in
  `docs/KNOWN_LIMITATIONS.md` (for-loop leak row). Adjacent live
  residual (needs verification in Task 2): `emitRangeIterInit`
  returns the RAW range pointer for non-pointer element types
  (`emit_iter.go:78-82`) while `emitIterNext` expects an iterator
  struct — the documented RV2-DEBT-037 residual for fixed-width
  range elements.
- Epic 19 Task 5 pending (reassigned to Task 1 here);
  `RUNTIME_V2.md` Phase 4 status paragraph is stale at the Epic 13
  closeout (it still lists remote channels, remote select, and
  migration as future work; Epics 14/17/18 shipped them).
- Related open debts that stay OUT of scope: RV2-DEBT-035/036 (heap
  bignum tails; fixnum removed the pervasive leak), 038 (bigfloat
  frees), 030/031/033 (metric/profile-gated), 032 (design review
  only), 024/025/026 (postponed surface).

## Fixed Points

- No new surface syntax. `far`, `on dst { }`, `spawn on dst { }`,
  move-only + shard-movable capture checks are unchanged.
- Transport invariants unchanged: generation-token discipline, the
  single final-release drop site, refusal-before-pending drops, and
  the bounded two-lane queue contract stay as Epic 13-18 left them.
- Drop == recursive free; no user code runs at a drop point; no
  drop-on-panic (panic is process exit).
- Double-drop must remain structurally impossible: the obligation
  moves with the state exactly once (ship → pending → consume|abandon).
  Compiler RETRY paths must keep passing `(id=0, state=null)` — a
  retry that re-ships already-moved state reintroduces double-drop.
- The heap census (alloc/free balance) is the arc's observable and
  becomes an enforced e2e gate for crossing programs in this epic.
- **Acceptance bar per abandon edge:** BOTH (a) a deterministic
  edge-forcing row with a dispatch-hit/count assertion proving the
  intended drop function ran, AND (b) a compiled heap-census row.
  Census alone cannot distinguish "right glue ran" from "leak
  cancelled by an unrelated free".
- Owner-routed frees (Phase 5) are vertical 3, NOT this epic: glue
  frees with today's global `malloc`/`free`, so freeing on a
  non-allocating shard is correct now and becomes a routing concern
  only in vertical 3. The seam must be recorded, not built. For the
  CURRENTLY shipped glue the cross-thread audit is done: the
  heap-accounting cells are atomic, the process-global array-view
  registry is mutex-protected, and element drops run after that lock
  is released (`rt_array_reclaim.c:22`). Every NEW runtime-backed
  owning type repeats that audit (the BytesView lesson).

## Forks (RESOLVED by the 2026-07-17 design review at the leanings below)

1. **Drop-fn registration model.** Per-crossing-state compiled drop
   functions with deterministic nonzero ids, NOT a general per-type
   table (crossing states are distinct boxed structs; the existing
   glue family is a sufficient target; a general table is vertical-3
   material). The id allocation key must be DETERMINISTIC and sorted
   — the leading candidate is the synthetic poll FuncID — never map
   iteration order. The review must also fix the id NAMESPACE: what
   "module-local stable" means under whole-program compilation and
   imported crossing bodies.
2. **Phase-5 seam recording.** Record TWO site families, not one:
   (a) obligation-transfer sites (caller → pending → body), and
   (b) actual free sites (`rt_free` calls incl. DEFERRED array-orphan
   reclamation, which can run later on the last-view thread). Routing
   only abandon sites would miss the real Phase-5 boundary.
3. **RV2-DEBT-040 shape.** Free-at-consume, BUT the design must
   specify the consumed-envelope operation: after extracting
   `Some(x)` the envelope free must transfer ownership of `x` and
   shallow-free/suppress the box — a naive recursive drop of
   `Option<T>` would free the payload just moved out. Unboxed option
   representation stays recorded as a future optimization (census is
   its regression guard).
4. **Census strictness.** Strict zero definitely-lost for EACH
   selected gated program; no named allowlist. Out-of-scope leak
   classes (bigfloat, large bignum) are excluded by choosing gate
   programs that avoid them, not by an exception list.
5. **First-poll ownership (NEW — was implicit).** Restate Epic 18's
   deliberate model: cancellation-before-first-poll enters the
   compiled body, whose local drops reclaim the detached state —
   the runtime never direct-drops `task->state` (rt_task has no drop
   id; `__task_state` transfers ownership into compiled code). The
   review must confirm this model survives activation and demand rows
   proving no USER-visible effects run before the cancellation
   boundary, plus an explicit generated-IR/census proof that the
   outer state BOX itself is freed on: normal completion,
   cancel-before-first-yield, cancel-after-yield (codex flagged the
   box free as non-obvious in the current lowering).
6. **Select send-arm ownership, local AND remote (REVISED after the
   2026-07-17 probes).** Empirical state is worse than draft v2
   claimed: LOCAL select send arms are a live soundness hole
   (RV2-DEBT-048) — the payload never joins move tracking, so a
   winning arm double-frees deterministically (receiver's drop +
   sender's scope-exit drop) and use-after-move compiles. (The first
   probe round missed this: string literals are static and never
   heap-allocate; all rows must use runtime-built payloads. The
   valgrind-mode silent-exit hazard for census rows is
   RV2-DEBT-049.) A losing arm reclaims correctly. Fixing the LOCAL
   semantics is therefore MANDATORY regardless of this fork — and
   the natural fix is per-branch move semantics via Epic 19's
   existing per-arm drop synthesis: moved in the winner branch, still
   owned inside every other arm's body (Go-shaped retry loops keep
   working), maybe-moved after the join (compile error with a
   move-site hint). Require the `own` marker for spelling
   consistency with direct send.
   The remaining fork is only the REMOTE half. A far send-arm
   payload rides the arm table as raw i64 bits
   (`emit_crossing_select.go` `send_bits`); Model C linearizes the
   winner on the owner shard, and the reply / cancel-ack messages
   already carry a generation-checked outcome — so remote symmetry
   needs ONE authoritative "send committed" bit through existing
   messages plus the rule that the payload obligation stays with the
   CALLER until owner commit (pending release never drops payloads;
   a refused/shutdown caller resumes and its compiled branch drops).
   No new distributed protocol. Choose: (a) local fix only this
   epic; far send arms with owned non-copy payloads reject with a
   kind diagnostic pointing at `on ch { ch.send(own x) }` (the
   DEBT-033 pattern — diagnostic hits are the demand signal); or
   (b) local fix + remote symmetry in the same epic (~one extra task
   after the local semantics land: the commit bit, cancel-vs-commit
   race rows, refusal/shutdown obligation rows). Precedent framing:
   send arms in select are everyday Go code (GC erases the question)
   and solved in Rust by dropping the losing branch's future; no
   shared-nothing peer has a cross-shard select construct, so (b) is
   Surge-original completeness the cost model wants (same construct,
   local and remote, priced not forbidden). RESOLVED (2026-07-17
   review): (b) — local fix in this epic unconditionally (Task 3;
   it is a drop-emission bug, exactly this epic's subject) AND
   remote symmetry inside this epic (Task 7: the owner-linearized
   commit bit through existing reply/cancel-ack messages,
   cancel-vs-commit race rows, payload obligation with the caller
   until owner commit). The epic is deliberately expanded rather
   than leaving the local/remote asymmetry behind.

## Planned Tasks

Dependency edges (everything else may overlap):
`glue (shipped) → T4 → T5 → T6/T7 → T8`; `T5 requires the
RV2-DEBT-047 fix (its own row inside T5)`; `T3 (local select-send
semantics) → T7`. T1, T2, and T3 are parallel lanes with no inbound
edges; T2 feeds T8 only if T8's census set includes loop-based
programs. All census rows carry an execution witness (RV2-DEBT-049).

- **Task 1 — Epic 19 closeout** (parallel lane; doc + bench, no
  code): alloc/free steady-state bench vs the leak model, DEBT ledger
  updates, `RUNTIME_V2.md` Phase 4 status sync (Epics 14-19 reflected;
  worded so it does NOT claim Epic 20's activation is done).
- **Task 2 — RV2-DEBT-040** (parallel lane): reclaim the
  per-iteration `Option<T>` box (consumed-envelope semantics per
  fork 3) and the iterator cursor on LLVM; valgrind definitely-lost
  independent of iteration count for array-for and range-for; verify
  or re-ledger the fixed-width `Range<T>` raw-pointer residual
  (`emit_iter.go:78`, RV2-DEBT-037 tail); census harness rows.
- **Task 3 — Local select send-arm ownership (RV2-DEBT-048;
  parallel lane, mandatory):** send-arm payloads join move tracking
  with per-branch semantics (winner branch moved, other arm bodies
  still owned, maybe-moved after the join with a kind diagnostic);
  `own` marker required as in direct send; winner path frees exactly
  once, loser path reclaims at branch end; accept+reject goldens;
  valgrind rows with runtime-built payloads and execution witnesses.
- **Task 4 — Drop-fn registration + dispatch:** emit compiled drop
  functions for shipped crossing states, deterministic sorted id
  allocation (fork 1), populate the `__surge_drop_call` switch,
  replace the id-0 default-panic-only proof rows with real-id rows;
  negative control keeps the panic arm for unregistered ids; retry
  paths keep `(id=0, state=null)`.
- **Task 5 — Handoff contract + spawn-on activation:** fix
  RV2-DEBT-047 first (kind-complete shutdown release + per-family
  queued-at-shutdown rows). Then state the NAMED linearization point
  for "pending owns → body owns" and prove spawn-on abandon edges
  with dispatch-hit + census rows: cancel before dispatch; cancel
  after task creation but before publication; cancel after
  publication but before first poll (compiled-side reclamation per
  fork 5); refusal; ACK/reply enqueue failure after publication;
  forced races around publication/handoff and first poll. Stale
  generation gets THREE distinct rows: stale request before body
  creation (pending remains sole owner, drops once); stale/duplicate
  message after handoff (must NOT drop body-owned state); stale
  ACK/reply after resolution (releases only its message reference).
  Owned RESULTS ride the same contract in reverse (the reply-edge
  obligation recorded on RV2-DEBT-034): a completed body's owned
  result must be reclaimed exactly once when the awaiting caller is
  gone (cancelled/released before consuming the reply).
- **Task 6 — Immediate-on activation:** placement `on` and anchored
  `on ch` on the Task 5 contract, with SEPARATE rows for anchor
  stale/pin/unpin and reply cancellation.
- **Task 7 — Remote select: symmetry + abandon coverage (fork 6
  resolved to (b)):** extend the Task 3 per-branch semantics to far
  send arms — one authoritative "send committed" bit through the
  existing reply / cancel-ack messages (owner-linearized per Model C),
  payload obligation stays with the CALLER until owner commit
  (pending release never drops payloads; refused/shutdown callers
  resume and their compiled branch drops); cancel-vs-commit race
  rows; plumbing-level abandon coverage (arm table, pins, pending)
  with census rows and execution witnesses.
- **Task 8 — Bench + debt + closeout:** crossing-path drop cost vs
  the leak model; census e2e over the Epics 16-18 crossing programs
  at `SURGE_SHARDS=1,2,8` (strict-zero per fork 4; loop-based
  programs included once Task 2 lands); RV2-DEBT-034 closed (or
  re-scoped with evidence); RV2-DEBT-047 closed; vertical 3
  (owner-routed frees, Phase 5) scoped as the next epic.

Rules: the established task rules apply unchanged (expand only the
next task, test-first rows, per-task gates incl. committed-tree
Sentrux comparisons, `make check` before completion,
behavior-named identifiers only, gatecheck wiring for every new
tagged test).
