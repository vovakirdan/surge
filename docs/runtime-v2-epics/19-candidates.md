# Next Direction Candidates (after the migration epic)

**Status:** decision note, 2026-07-14. Epics 13-18 closed the entire
post-cleanup candidate list (`16-candidates.md`: share, remote select,
migration all shipped). Three directions compared; the choice is a
product call.

## A. Debt tails (RV2-DEBT-031 / 032 / 033)

What the ledger actually permits today:

- **033 (multi-owner select, Model B slow path)** is profile-gated: no
  workload where the single-owner restriction binds exists. Building B
  now is building ahead of the profile — against the row's own close
  condition and against the review ruling that shaped it.
- **031 (credit backpressure)** is metric-gated: `credit_stalls` is
  instrumented on every shard and asserted structurally zero. The
  moment it starts mattering is observable; today it is silent.
- **032 (force-close capability)** is the ONE actionable item: its
  gate IS a design review ("who holds the close-for-all right: the
  minting lease, an owner capability object, or a quorum") — a cheap,
  product-flavored track with no implementation commitment. It can run
  in parallel with any other direction.
- **034 (dormant crossing drop plumbing)** is language-gated — it is
  activated BY direction B below, not by itself.

Verdict: not a standalone epic. Run the 032 design review as a side
track; leave 031/033 to their gates.

## B. Drop emission (owned-value reclamation)

The biggest lie in the language today: compiled code frees NO owned
heap value at scope exit anywhere — `InstrDrop` is a backend no-op
(`internal/backend/llvm/emit_instr.go:41`), nothing synthesizes drops
at scope exits, no drop glue exists, and the runtime has only raw
`rt_free` (no per-type free helpers). Every string, array, and
heap-owning struct leaks by design (the contract's recorded pre-1.0
malloc/free model).

**No new surface syntax is needed.** `@drop expr;` already exists end
to end: parser (`internal/parser/stmt_parser.go:319`, attr catalog
`internal/ast/attr_catalog.go:93`), `hir.StmtDrop` → `mir.InstrDrop`
(`internal/mir/lower_stmt.go:317-366`, copy/reference/non-copy split
already implemented). The work is compiler-internal plus runtime:

- sema: a drop-obligation pass over scope exits, reusing
  `move_tracking.go` (moved-out values are not dropped) and the borrow
  machinery's implicit end-of-scope `EvDrop` events
  (`internal/hir/borrow.go:74`);
- HIR: synthesize `StmtDrop` at scope exits, reassignment overwrites,
  and early exits (return/break/continue) — today only explicit
  `@drop` produces the node;
- MIR: recursive drop glue per type (nothing exists, dead or alive);
- LLVM: replace the `InstrDrop` no-op with glue/free calls;
- runtime: per-type free helpers (string, array, struct fields).

What requires the user is SEMANTICS, not grammar (design review before
Task 1, the established gate): when drops run (scope exit vs eager
last-use), reassignment semantics, `@drop` interplay, and the
behavior-change management (programs that accidentally relied on
liveness-past-scope).

Strategic weight — three unlocks in one arc:

1. activates RV2-DEBT-034: the entire crossing drop-obligation
   machinery built in Epic 18 (dispatch, pending metadata, single drop
   site, refusal drops) is in place, row-proven, and waiting at id 0;
2. unblocks allocator Phase 5 (`RUNTIME_V2.md` §11: shard pools,
   owner-routed frees) — routing frees is meaningless while nothing
   frees;
3. makes census (alloc/free balance) a REAL gate for every future
   epic's leak rows — today census proves only runtime-internal
   allocations.

Shape: an arc of 2-3 epics (local drop emission → crossing activation
+ glue-edge matrix from the Epic 18 record → owner-routed frees), each
with the usual vertical rhythm.

## C. Further RUNTIME_V2 features

The remaining capability-table and phase-plan surface:

- **Owned payloads through far channels** (cap table: "Cross-shard
  send (`own` shard-movable value, unbounded) — future remote channel
  operation"): the natural next crossing increment, composing directly
  with Epic 18's capture lift (channel elements are plain-copy-only
  today). Honest but with a caveat: its leak semantics — values parked
  in cross-shard buffers at close/teardown — are only PROVABLE after
  drop emission exists; shipping it first repeats Epic 18's "no new
  leak class" argument a second time, thinning it.
- **Allocator Phase 5** (shard-local pools, owner-routed frees):
  depends on B outright.
- **I/O-skew migration control plane** (fat-connection migration,
  trigger heuristic recorded open) and **Io boundary / io_uring
  backends** (§12-13): heavy, independent tracks; nothing currently
  blocked on them.
- **Tier 2 stealing lowering** (CPU skew): independent scheduler work.

Verdict: owned-payload channels is the best of this direction and the
first candidate AFTER the drop arc, when its census rows become
provable.

## Recommendation

**B (drop emission) as the next major arc**, with the 032 force-close
design review as a cheap parallel track. Rationale: B is the enabling
dependency for half of everything else (DEBT-034 activation, allocator
Phase 5, honest leak gates, owned-payload channels' proofs), it needs
no syntax (the user gates only the semantics review), and delaying it
makes every subsequent epic's leak arguments weaker. Owned-payload
channels slot in naturally as the first post-drop epic.
