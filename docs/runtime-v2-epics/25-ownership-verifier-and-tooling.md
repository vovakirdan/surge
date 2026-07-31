# Epic 25 — Ownership Verifier And Investigation Tooling

Status: proposed, not started. Drafted 2026-07-31 after a single session closed
five ownership rows (RV2-DEBT-097/098/099/100 and RV2-DEBT-052's mixed-arm
residual) that were all one shape of defect, found and fixed the slow way —
valgrind, MIR read by eye, minimal reproducers built by hand. This epic invests
in not doing that again.

**Not part of the Epic 22→23→24 detour chain and does not block it.** Epic 23
Phase 2 has exactly one preflight item left (the places/references and
frame-slot storage-model document; see `23-value-composites.md` and the
Detour Chain section of `README.md`) and this epic does not touch it. It is a
deliberate, owner-chosen detour taken ahead of that document rather than a
dependency of it — either could be worked first, and this one was chosen
because the tooling pays for itself on every ownership-shaped epic that
follows, Phase 2 very much included.

This is the onboarding brief — read it end to end before touching anything.

## Why This Epic Exists

Every one of the five rows closed in the preceding session was the same
defect, in different clothing: **a place the compiler had already decided owed
a release was filled with a value it did not own.** None were logic errors —
the arithmetic, the control flow, the surface semantics were all correct. What
was wrong was bookkeeping: whether a given RValue MINTS a fresh reference,
merely ALIASES one someone else holds, or TRANSFERS one that already existed.

Every one of the five needed at least one sema- or HIR-level fix alongside
whatever MIR did — this is worth being precise about, because it is tempting
to read "MIR-level verifier" (the settled decision below) as meaning most of
these were MIR bugs, and re-deriving each one's actual fix commit says
otherwise:

- **RV2-DEBT-097** — sema classified an identity cast as a producer (a fresh
  value nothing owns); MIR read its source as an alias. Both were locally
  reasonable; the disagreement was the bug, and the real fix touched both:
  `noteTempCandidate` in sema, `lowerCastExpr` in MIR.
- **RV2-DEBT-099** — not one bug but two independent ones in the same
  direction, which is why fixing only the obvious half made things worse
  before it made them better: MIR's deref-through-a-reference didn't retain
  its pointee, AND sema's alias-marking predicate
  (`projectionReadAliasesItsSource`, then unnamed) didn't exclude
  reference-counted scalars from "this binding never owns what it read."
  Landing only the MIR retain would have left the sema mis-classification
  free to leak every such binding; both had to move together.
- **RV2-DEBT-100** — HIR's own `compareScrutineeOwnership` already knew a
  `compare *arg` subject was borrowed; sema's payload-obligation code never
  asked it, and registered an obligation on borrowed storage. Sema-only fix.
- **RV2-DEBT-098** — an arm's payload obligation didn't follow the value when
  the arm handed it to the compare's own result. Sema-only fix.
- **RV2-DEBT-052's residual** — a partially-bound tag arm had no release
  shape that covered its own field set. HIR-only fix.

So the honest claim is not "most of these were MIR bugs" — it is that **every
one of them, whichever pass was actually wrong, is observable in MIR as the
same shape: a released local whose reaching definition has no minting or
transferring step.** MIR is where the compiler's decisions all converge into
one instruction stream right before they become real memory operations, which
is what makes it the right level to CHECK at, independent of which earlier
pass made the wrong call — see "Settled Decisions" for why this still catches
sema-rooted bugs like 100, not only MIR-rooted ones like 097/099.

**How each one was actually found and fixed**, because the cost is the point:
write a `.sg` reproducer by hand, compile it, run it under `valgrind`, read
the MIR dump by eye to find the wrong operand, form a hypothesis, patch it,
re-run valgrind, repeat for every shape in a hand-built matrix (discarded /
bound / argument / literal / looped / borrowed / `@copy` / non-owning), and
only then trust the fix. That loop cost real hours per row and it is not
getting cheaper as the compiler grows — every one of Epic 23 Phase 1, Epic 24,
and Epic 22's remaining crossing barriers is exactly this class of question
asked about a new representation.

The corpus already carries the mechanism to answer it cheaper. **Every temp
holding a reference-counted scalar is registered for a release the moment it
is created** (`registerRefCountedTemp`, `internal/mir/lower.go`), and HIR
carries an explicit transfer mode on field reads (`FieldAccess.MoveOut`,
closed out for the one live generated-read site this session — see
`24-partial-moves.md`). The information the ownership question needs is
already flowing through the compiler at build time. It is just never checked
against itself, and the only thing that currently checks it is a human
running `valgrind` after the fact.

## What This Epic Delivers (Definition of Done)

An ownership question that today costs a hand-built reproducer, `valgrind`,
and an eyeballed MIR dump must cost a failed build with a file:line and a
reason, for the class of defect this epic targets: **a released local whose
definition chain contains no minting or transferring event.**

Concretely, all of the following must be true before this epic closes:

1. **A verifier exists and runs on real code.** It walks compiled MIR and
   flags every local that is released (dropped whole, or handed to an
   envelope release, transferred out of the function, or consumed into an
   aggregate literal/call argument/channel send by move) whose reaching
   definition is purely aliasing — no call, no literal, no representation-
   changing cast, no explicit retain, no move-of-something-owned — for at
   least the shapes this session measured by hand: identity casts,
   deref-through-a-reference, compare-arm payloads, borrowed compare
   subjects, AND a `@copy` value-composite read that clones rather than
   transfers (RV2-DEBT-052's exact residual — see Step 0's `OperandCopyValue`
   note below; a fixture set that only covers move-only-heap shapes is
   provably not enough, since that is exactly the axis this session's own
   regression needed to slip through).
2. **It is proven against the defects that motivated it.** Minimal MIR
   fixtures reconstructing the exact pre-fix shape of RV2-DEBT-097, 098, 099,
   100, and 052's `@copy` residual are checked in as the verifier's own test
   corpus. The verifier must flag every one of them and pass clean on the
   current (fixed) shape. This is the acceptance test for the whole epic, not
   an afterthought — if the verifier cannot re-derive today's closed bugs
   from their reproducers, it has not earned trust for tomorrow's. ONE MORE
   fixture is required beyond the five historical bugs, found during this
   document's own review rather than in production code: a `compare` whose
   SUBJECT is a `@copy` union read through a deref (`scrutineeDuplicated` —
   `reads_copy_payload` in `runtime_v2_borrowed_compare_payload_e2e_test.go`
   is the existing e2e version of this shape). The compare's own subject
   temp genuinely MINTS (a fresh clone), but its payload extraction must
   still retain, exactly like the borrowed case, because the clone is
   deep-dropped later rather than shallow-released — this is the
   counterexample that disproved this document's first "subject resolves to
   MINTS implies payload transfers" design (see Step 0's `TagPayload`
   prerequisite and Trap 7) and must stay a green MUST-FLAG-IF-BROKEN
   fixture so that regression cannot be reintroduced silently. A SECOND
   additional fixture, also found during review: a struct/tuple/array
   literal, or a tag-constructor call, whose field/argument position holds
   a reference-counted scalar (`float`) filled with a bare unretained alias
   — a `STORE`-shaped sink position, per Step 0, that an earlier draft's
   sink rule would have wrongly excluded (it copied
   `paramTransfersOwnership`'s "not a reference-counted scalar" exclusion,
   which is specific to ordinary parameter BORROWING, onto storage
   positions where it does not apply).
3. **It runs where it can actually stop a bad commit.** Wired into `surge
   build --dev` (which today parses the flag but only uses it to reject
   `--release --dev` together — it does not yet reach `CompileRequest` at
   all) and into `make check` via a Go-level test in the style of this
   session's `internal/crossinggate` MIR-invariant tests, on a small,
   representative, fast sample — cheap, no `SURGE_SKIP_TIMEOUT_TESTS` gate,
   no `valgrind` dependency. The FULL-corpus run (item 4) is a separate,
   longer-timeout Makefile target, matching how `runtime-v2-crossing-check`
   and its siblings are already kept out of `make check`'s own `test` target
   for exactly this reason (see Step 2's scale note).
4. **The existing corpus passes clean, or every exception is on the record.**
   Every fixture that successfully produces MIR — golden fixtures (excluding
   the ~409 deliberately-invalid ones under `testdata/golden/*/invalid`,
   which by construction never reach MIR), showcases, `core/`, `stdlib/` —
   compiles through the verifier with zero findings, or every finding is
   either fixed or recorded in a machine-readable allowlist cross-referenced
   from `DEBT.md` with the exact reason it is excluded and why that exclusion
   is safe (see Step 2).
5. **A future ownership row costs less to investigate.** A shared e2e harness
   (`ownershipGate`) exists and is used by at least the newest four tests
   from this session. Its value is not primarily line count — those tests
   already reuse `buildRuntimeV2CrossingSource`/`runBinaryUnderValgrind`/
   `parseValgrindDefinitelyLost`/`runProgramFromSource`, and their own
   per-test plumbing is already a lean ~20 lines, not the ~200 an earlier
   draft of this document overstated. The value is that the harness makes
   item 6 a MECHANICAL requirement instead of a convention.
6. **A regression like the `@copy` one cannot hide as easily, and this is
   enforced by the type system, not by a comment.** The harness's own API
   requires a caller to supply probes for all 4 axes (move-only heap /
   `@copy` value-composite / reference-counted scalar / non-owning) — e.g.
   four required struct fields or named parameters, so that omitting one is
   a compile error, not a missed convention. This is the exact axis a
   string-only census missed on RV2-DEBT-052's first cut, and "requires a
   comment" would not have caught it either; "fails to compile" would have.

## What We May Not Lose

This is tooling and analysis. It must not change what compiled programs do.

- **No codegen change from the verifier itself, and this is a stated gate, not
  just a promise.** It reads MIR and reports; it does not rewrite
  instructions, does not change which operand kind a lowering picks, does not
  change drop emission. Step 1's own test suite includes a before/after
  structural-equality check (serialize the MIR of a representative module,
  run the verifier, serialize again, diff) so "read-only" is something a test
  fails on, not only something this paragraph claims. If a real bug turns up
  in the corpus (item 4 above), fixing it is a normal, separately-reviewed
  ownership fix — same rigor as the five this session closed — not something
  the verifier does automatically.
- **`make check` stays green and the native e2e baseline is unchanged.** The
  ten pre-existing native failures recorded this session (see
  `session-2026-07-31-handoff.md` in the memory index) are the baseline;
  nothing in this epic may add to that set. Verify in a `git worktree` at
  this epic's own starting commit exactly the way this session did, not from
  memory of what the count was.
- **Default `--emit-mir` output is byte-identical**, and so are every
  existing `testdata/golden/mir/*.mir` fixture. The MIR-dump ownership
  annotation (step 4) is opt-in through a separate flag; it must never be the
  default, because the alternative is 26 golden files churning for a
  cosmetic change with no behavioral content.
- **Release builds carry zero overhead.** The verifier runs at `--dev`/test
  time only. The per-site heap census (step 6) is the one item that touches
  the runtime allocator's behavior directly (by its preferred TLS design,
  not its call sites — see Step 6) and must be
  compiled out or feature-gated in release — this is the highest-risk item
  in the epic for exactly that reason, which is why it is ordered last.
- **No language or public API surface change.** Nothing here is user-facing:
  no new syntax, no new attribute, no new diagnostic code visible to Surge
  programs. `--dev` already exists as a CLI flag; this epic gives it teeth,
  it does not invent a new one. If implementation surfaces a case where the
  cleanest fix would touch the language surface, STOP and ask — this is
  exactly the class of decision reserved for the owner.
- **Existing gates keep passing as they are, checked by name, not by
  assumption.** `make check`, `make golden-check`, and `make runtime-v2-check`
  (which is what actually composes `runtime-v2-crossing-check`,
  `-heap-check`, `-waiter-check`, `-fd-registry-check`, and the rest — `make
  check` alone does not invoke it, so "existing gates" means running this
  target explicitly, not assuming `make check`'s green covers it) all stay
  green before any step in this epic is called done. `internal/gatecheck`
  itself (which keeps the Makefile's gates honest) must still pass after any
  new gate is added, including the new ones this epic adds in Step 2/3.

## Steps

Numbered and independently gated, in the order that keeps every step provable
before the next one depends on it: build the classification, prove the
verifier against known shapes in report-only mode, run it over real code
while it still cannot break a build, THEN make it a hard gate — never the
reverse.

### Step 0 — RValue/Operand ownership-effect classification

**Prerequisite compiler change, not just verifier design: `mir.TagPayload`
needs an explicit `MoveOut bool` field, mirroring `FieldAccess.MoveOut`
exactly.** An earlier draft of this document tried to have the verifier
DERIVE a tag-payload read's ownership purely from whether its subject
resolves to MINTS, and that is unsound: HIR's `compareScrutineeOwnership`
(`internal/hir/normalize_compare_release.go`) has THREE outcomes, not two —
`scrutineeBorrowed`, `scrutineeMoved`, and `scrutineeDuplicated` (a `@copy`
union read through a deref, which the compare clones into a fresh,
genuinely MINTED envelope). `SubjectBorrowed` is set true for BOTH the
borrowed AND the duplicated case (`owned != scrutineeMoved` —
`internal/hir/normalize_compare.go`), because the DUPLICATED envelope is
later deep-dropped as an ordinary composite, field included, so its payload
read must still retain, exactly like the borrowed case — only the MOVED
case's envelope gets the narrowed shallow release that makes an unretained
transfer safe. A verifier asking only "does the subject resolve to MINTS"
cannot tell "moved, shallow-released later" from "duplicated, deep-dropped
later" — both mint the subject — and would wrongly wave through a missing
retain on the duplicated path (RV2-DEBT-052's own shape,
`reads_copy_payload` in `runtime_v2_borrowed_compare_payload_e2e_test.go`,
is exactly this counterexample). The fix is the same one `FieldAccess`
already has: HIR already computes the right three-way answer, so carry it
one field further into MIR instead of discarding it after deciding whether
to emit a trailing retain. `RValueTagPayload` then reads its OWN flag,
directly, the same way `RValueField` does, to decide the ONE question the
old subject-provenance design got wrong — is this extraction ALIASES or
TRANSFERS — without re-deriving it from the subject's own resolution.
**That is the only thing the flag replaces.** Once the flag says TRANSFERS,
it is an ORDINARY TRANSFERS like any other — it still recurses into its
`Value` operand's place exactly the way `RValueField{MoveOut:true}` does,
because the flag records what HIR decided the extraction DOES, not a
guarantee that the subject was actually built correctly; if some other bug
made a "moved" subject not actually resolve to MINTS in MIR, that mismatch
between what HIR believed and what MIR actually produced is precisely the
class of disagreement this verifier exists to surface (Trap 4), and only
recursing into the subject can catch it. This is a small, targeted schema
change (one field, set at one lowering site, `internal/hir/
normalize_compare.go`/`internal/mir/lower_expr_misc.go`), not a redesign,
and it should land as this step's first, separately-reviewed commit before
any classification table is written against it.

**The central design decision for everything else: TRANSFERS is not a
terminal answer alongside MINTS/ALIASES/N-A — it is a PASS-THROUGH that
inherits the ownership status of whatever is being moved, resolved
recursively through the SAME judgment this verifier is computing.** An
`OperandMove` of a local that was only ever ALIASES-defined does not become
owned by being moved — moving relocates the alias, it does not mint one.
RV2-DEBT-100's actual pre-fix shape (a borrowed payload read handed onward
through several ordinary moves before the drop) is exactly the
counterexample a non-recursive "TRANSFERS always satisfies the invariant"
rule would miss, and IS correctly caught once TRANSFERS resolves back to
its ultimate source. The same recursion, separately, is what makes an
aggregate literal's field operands sound (below) — two different shapes,
one mechanism.

**Prerequisite compiler change, second one: `InstrCall`/`CallInstr` needs an
explicit per-argument ownership-contract fact, computed once at lowering
time, for the same reason `TagPayload` needed `MoveOut`.** The lowering
already knows, for every argument of every call it emits, whether that
position BORROWS (an ordinary by-value parameter — a reference-counted
scalar argument is not owned by the callee, the caller keeps its reference
alive for the call) or STORES it. **`STORE` is a SEMANTIC category, defined
by the destination outliving the operation — not a syntactic one defined by
which `RValueKind`s happen to build aggregates.** An earlier draft of this
document enumerated `STORE` only as "tag constructors and aggregate-literal
fields," found by which `RValueKind` the operand feeds, and that
enumeration is INCOMPLETE: the same "this position keeps a reference alive
past the current instruction, and a later drop of the container is what
releases it" property shows up in at least four more places, none of them
an `RValueStructLit`/`ArrayLit`/`TupleLit` or a tag-constructor call:

- **Aggregate-literal fields and tag-constructor arguments** — the two
  cases already identified: `calleeStoresArguments`/`symbols.SymbolTag`
  (`internal/mir/lower_expr_calls.go`) for tags, `placeOperand`'s
  `consume=true`/`OperandRetain` path (`internal/mir/lower_expr_helpers.go`)
  for aggregate fields.
- **Projected assignment** (`arr[i] = x`, `p.field = x`) — `lowerAssignExpr`
  (`internal/mir/lower_expr_assign.go`) lowers the right-hand side as a
  CONSUMING read into a place that is itself already inside a live
  container. This is a distinct MIR shape (`InstrAssign` with a projected
  `Dst`) from constructing a fresh aggregate, and needs its own sink check:
  the NEW value being written in is a `STORE` sink regardless of whether
  the place being assigned into is a struct field, an array element, or a
  map entry.
- **Crossing and blocking captures** — become fields of a synthesized state
  struct that outlives the current function entirely (it travels to
  another shard or across a suspension) and is later recursively dropped,
  reference-counted fields included (`internal/mir/lower_expr_crossing.go`,
  `internal/mir/lower_blocking.go`, `emit_drop_glue.go`'s recursive field
  drop). Every capture is documented as "a CONSUMING read, including a copy
  capture" for exactly this reason — this is `STORE`, not
  `RValueStructLit`, and needs its own enumeration in the lowering sites
  that build these synthetic states.
- **Container-mutating runtime intrinsics** — `rt_array_push`,
  `rt_map_insert`, and any sibling that stores ONE argument into a
  container while merely BORROWING another (the receiver). These are
  ordinary `InstrCall`s to a named `CalleeValue`, not resolvable through
  `Module.FuncBySym`, and `calleeStoresArguments` today recognizes only
  tags — it does not generalize to "any intrinsic whose N-th argument is
  stored." Per-intrinsic argument classification here is NOT something a
  general rule can infer; it needs an explicit, audited table (which
  argument position of which named intrinsic is `BORROW` vs `STORE`),
  reviewed the same way the rest of this classification is.

A verifier trying to re-derive "does this position own its argument" from
callee symbol lookups alone cannot do it soundly for every callee shape MIR
can call — a direct, `Module.FuncBySym`-resolvable function exposes its
parameter types directly, but an extern, intrinsic, or runtime-helper call
(`CalleeValue` by name) and an indirect function-value call do not
uniformly expose one from MIR alone, and `CallInstr` today carries only
callee identity and arguments, no contract. So: lowering records the
contract explicitly, per argument, the moment it is decided (`BORROW` /
`TRANSFER-OWNED` / `STORE`) — the same "carry the fact forward instead of
re-deriving it" pattern `TagPayload.MoveOut` uses, extended to cover the
audited intrinsic table above, not only ordinary function/tag calls. A
callee shape the lowering itself cannot classify (which should not happen
once every lowering site above is audited, but the verifier must not
assume that) falls back to an explicit UNRESOLVED marker, which the
verifier treats as a finding, never a silent skip. **Gate addition:** the
per-argument contract slice's length must equal the call's argument count
(`len(ArgContracts) == len(Args)`, checked by the same test that checks
exhaustiveness), and Step 1's acceptance fixtures include one call with
MIXED contracts at different argument positions (a `BORROW` receiver
alongside a `STORE` value, the exact `rt_array_push` shape) to prove the
per-argument granularity is real, not per-call.

**A consuming sink is identified by the DESTINATION'S CONTRACT, never by
which operand kind the source happened to use.** This matters more than it
looks: if lowering has a bug and passes `OperandCopy` where a callee's
by-value owned parameter should have received `OperandMove`, that IS
exactly the alias-laundering shape this verifier exists to catch — and a
design that only treats "arguments already marked Move" as sinks would
never even look at that position, because the bug makes it stop looking
like one. So, using the per-argument contract above: a call-argument
position is a sink whenever it is `TRANSFER-OWNED` (per
`paramTransfersOwnership`'s predicate — a droppable, non-reference-counted-
scalar by-value parameter) OR `STORE` (a reference-counted scalar argument
to a tag constructor or an aggregate-literal field, which needs a retained
reference rather than a transferred one, but is EQUALLY a sink — an
unretained alias there is the same defect, just resolved by MINTS via
`OperandRetain` rather than by a move); it is NOT a sink when `BORROW` (an
ordinary reference-counted-scalar by-value parameter, which the callee
never owns). This is a real correction from an earlier draft, which wrongly
copied `paramTransfersOwnership`'s exclusion of reference-counted scalars
onto aggregate/tag-constructor STORAGE too — that exclusion is specific to
ordinary parameter BORROWING and does not apply to a position that stores
its argument past the call, which a reference-counted scalar field
genuinely does. A return, channel-send, or select-arm value is a sink
whenever the destination type is droppable, independent of the
reference-counted-scalar exception (nothing "stores past the call" there —
the value simply becomes the new place's whole content, so the same
TRANSFER-OWNED-style rule as an owned parameter applies, not the STORE
variant). The operand actually occupying a sink position is then resolved
by Table A/B
as normal — if it resolves to ALIASES with nothing bridging it, THAT
mismatch (contract says owned, value is an unretained alias) is the
finding, not a shape the verifier silently skips.

With the TagPayload flag and the sink-by-contract rule stated, the rest is
a flat table:

**Table A — `RValueKind`s whose classification does not depend on
recursion:**

- **MINTS** — `RValueStructLit`/`ArrayLit`/`TupleLit` (the container itself;
  each owns-heap field position is ALSO a sink per the rule above, which is
  a separate check, not a second classification of this same RValue),
  `RValueBinaryOp` on a magic operator (a real call underneath), `RValueCast`
  that changes representation, `RValueIterInit` (allocates the iterator
  state box directly — `emitIterInit`'s own `rt_alloc` call,
  `internal/backend/llvm/emit_iter.go` — an earlier draft of this document
  wrongly called this N/A), `RValueIterNext` (mints a fresh `Option<T>`
  envelope per call, which HIR releases explicitly via
  `StmtEnvelopeRelease` in `internal/hir/normalize_for.go` — also wrongly
  N/A in an earlier draft).
- **ALIASES** — `RValueField` with `MoveOut: false`, `RValueIndex` on a plain
  element (not a view — a view/slice mints its own owned header),
  `RValueUnaryOp` dereferencing a `&`/`&mut` reference, `RValueCast` that
  does not change representation (identity — RV2-DEBT-097's exact shape).
- **Reads its own `MoveOut` flag directly, like `RValueField`, to decide
  WHICH of the next two rows applies — the flag replaces subject-provenance
  derivation for this one decision only, and normal recursion still applies
  once the decision is made** — `RValueTagPayload`: ALIASES when `MoveOut`
  is false (needs a subsequent retain, same as any ALIASES read), TRANSFERS
  when true (recurses into the `Value` operand's place exactly like the
  next row, so a subject that MIR did not actually build as MINTS is still
  caught), per the prerequisite change above.
- **TRANSFERS (inherits from source, recursively)** — `RValueField` with
  `MoveOut: true`, `RValueTagPayload` with `MoveOut: true` (see above),
  `RValueUnaryOp` dereferencing an `own` pointer.
- **N/A (no heap involved)** — `RValueTagTest`, `RValueTypeTest`,
  `RValueHeirTest`, non-magic `RValueBinaryOp` (arithmetic/comparison on
  non-owning types).

**Table B — `RValueUse{Use: Operand}`**, which is not its own Table A row:
`RValueUse` is a pure pass-through, and its destination's status is entirely
the classification of the wrapped `Operand.Kind`:

- **MINTS** — `OperandRetain` (a fresh, independently-releasable reference —
  see the idiom note below), `OperandCopyValue` (a fresh, independent clone
  — this is the exact operand RV2-DEBT-052's `@copy` residual needed
  classified correctly: cloning is MINTING, never TRANSFERS, and a
  classification that let a clone's SOURCE count as released-through-this-op
  is precisely that bug), `OperandConst` whenever the constant's TYPE owns
  heap — not "is a reference-counted scalar," which is too narrow and
  leaves a real gap: a `float`/bignum constant is materialized via
  `materializeOwnedConst` into an explicit temp MIR registers for release
  automatically, but a STRING constant is not — `isRefCountedScalar`
  excludes strings, so `materializeOwnedConst` skips them entirely, and the
  backend allocates the string directly at the use site
  (`emitStringConst`/`rt_string_from_bytes`,
  `internal/backend/llvm/emit_term.go`) with no MIR-level temp step at all.
  Different mechanism, same ownership outcome: whoever consumes either kind
  of constant operand gets a genuinely fresh, owned value, so both classify
  MINTS.
- **ALIASES** — `OperandCopy` of a place whose type owns heap (a bare copy of
  an owning place with no retain is precisely the shape every one of this
  session's five defects turned out to be).
- **TRANSFERS (inherits from source)** — `OperandMove`.
- **N/A** — `OperandConst` of a non-heap-owning type, `OperandCopy`/`Move` of
  a non-owning type, `OperandAddrOf`/`AddrOfMut` (these produce a reference,
  not an owned value — the destination is never something this verifier
  tracks a release obligation for).

**A local reassigned by a retain right after an aliasing read is not a
special case.** `L25 = tag_payload copy L3.X[0]; L25 = retain L25` (the
idiom `retainExtractedValue` and every borrowed-payload fix this session
emits) is two ordinary definitions of the same local — the first ALIASES,
the second, read purely as `RValueUse{OperandRetain}` against Table B,
MINTS. A reaching-definitions dataflow that asks "does EVERY definition
reaching this release resolve — recursively through TRANSFERS — to MINTS"
handles this idiom for free, with no separate "bridging" concept required.

**Entry axioms — a function's parameters are definitions with no `RValue`
at all, and are a TERMINAL root, not something that recurses further
back.** They need a seeded starting classification rather than being
invisible to the dataflow, and it must not be labeled TRANSFERS — this
document's own definition of TRANSFERS is strictly "inherits from an
earlier MIR definition," and a parameter has none; calling it TRANSFERS
would either dead-end the recursion or require an undocumented special
case. Seed it as a distinct terminal class, OWNED-AT-ENTRY (same standing
as MINTS for everything downstream, just with a different origin): a
by-value, droppable (owns-heap) parameter is OWNED-AT-ENTRY UNLESS its type
is a reference-counted scalar, mirroring sema's own
`paramTransfersOwnership` precisely — the exclusion there is deliberately
"reference-counted scalar," not "`Copy`" in general, because a `@copy`
value-composite argument is CLONED at the call site and the callee
genuinely owns that clone; only a reference-counted scalar borrows. A
reference-counted-scalar parameter is NOT owned at entry (the callee holds
a borrow-shaped value the caller still holds too) and needs the same
explicit retain any other borrowed read does before something may release
it.

**Instructions whose destination is not produced through `InstrAssign`/RValue
still need a classification.** Rather than hand-enumerate `InstrKind`s and
risk missing one (an earlier draft of this document did exactly that), state
the rule structurally: every `InstrKind` other than `InstrAssign` that
carries a destination place (`InstrCall.Dst` when `HasDst`, `InstrSpawn`,
`InstrPoll`, `InstrJoinAll`, `InstrChanRecv`, `InstrAwait`, `InstrCrossing`,
`InstrBlocking`, `InstrTimeout`, `InstrSelect` — whichever of these actually
carry a `Dst`/`HasDst` field, verified against `internal/mir/instr.go` at
implementation time, not assumed from this list) classifies MINTS by
default, since each is a call-shaped operation producing a value nothing
else holds. This is a general rule with an exhaustiveness test behind it
(below), not a hand-picked list this document is trusted to have gotten
complete.

**Gate:** a test iterates `RValueKind`/`OperandKind` by their known value
range (both are small `uint8` enums with contiguous values from 0 to a
final sentinel — either add a `*Count` sentinel constant to each, matching a
pattern already usable in Go without reflection, or adopt the `exhaustive`
linter rule for every `switch` over these types with no `default` case) and
fails if any value maps to "unclassified" rather than one of
MINTS/ALIASES/TRANSFERS/OWNED-AT-ENTRY/N-A. A second test does the same for
`InstrKind` against a `HasDst`-style field check. New MIR kinds must not be
addable without extending this table — that exhaustiveness check is the
whole point, and it is cheap insurance against the next kind arriving
unclassified. A third test asserts the `mir.TagPayload.MoveOut` prerequisite
change produces the right flag for all three of `compareScrutineeOwnership`'s
outcomes (borrowed/moved/duplicated), since that is exactly the fact this
whole step depends on getting right before any classification table matters.
A fourth test asserts the `CallInstr` per-argument contract prerequisite
covers every callee shape the lowering can actually emit — direct
`Module.FuncBySym`-resolvable functions, extern/intrinsic calls, tag
constructors, and indirect function values — with none of them silently
falling through to a classification other than `BORROW`/`TRANSFER-OWNED`/
`STORE`/explicit-`UNRESOLVED`.

### Step 1 — Verifier core (report-only)

A forward dataflow pass over each function's CFG: reaching-definitions per
local, restricted to locals whose type owns heap (`OwnsHeap`, already
computed), seeded with Step 0's entry axioms for parameters. Sinks are found
by the DESTINATION'S CONTRACT (Step 0's `TRANSFER-OWNED`/`STORE`
classification), not by which operand kind the source happens to use:
`InstrDrop` with no projection, `InstrEnvelopeRelease`, a
`TermReturn`/`TermAsyncReturn` value whose function-result type is
droppable, an `InstrCall` argument at a `TRANSFER-OWNED` or `STORE`
position per the call's own per-argument contract (regardless of whether
the caller supplied `OperandMove`/`OperandRetain` or something else — a
bare alias at either kind of position is itself a finding, not an excuse to
skip the position; a `BORROW` position is correctly never a sink), an
owns-heap `RValueStructLit`/`ArrayLit`/`TupleLit` field position, a
projected-assignment right-hand side (`arr[i] = x`, `p.field = x` —
`InstrAssign` with a projected `Dst`, a distinct MIR shape from
constructing a fresh aggregate), a crossing/blocking capture becoming a
field of its synthesized state struct, a container-mutating intrinsic's
audited `STORE` argument (`rt_array_push`'s second argument, `rt_map_insert`'s
key/value, per Step 0's audited table — includes reference-counted
scalars, unlike a `BORROW` parameter or receiver), a channel-send value, a
select-arm value. At every one of these, the set of definitions reaching
the consuming operand must EVERY ONE resolve, recursively through
any TRANSFERS steps back to their ultimate source, to MINTS or
OWNED-AT-ENTRY.
Not "at least one" — an unconditional release downstream of a branch where
only one arm minted and the other merely aliased is exactly the shape
RV2-DEBT-096 ("a branch can mint sometimes") was, and requiring only one
reaching definition to qualify would make the verifier blind to it by
construction. And not "any TRANSFERS step is good enough on its own" —
RV2-DEBT-100's actual pre-fix shape was a borrowed union whose payload was
read (ALIASES) and then handed onward through ordinary moves before the
drop; every one of those moves is individually `OperandMove`-shaped, and
only recursive resolution back to the original ALIASES read (never bridged
by a retain) tells them apart from a genuinely owned chain.

**The recursion must terminate on a real algorithm, not an implied one, and
MIR is not SSA — a local can be reassigned, and a loop back-edge can build a
cycle in the def-use graph** (`L1 ← move L2 ← move L1` is a shape the
verifier must not infinite-loop on). Step 1's own design note must state,
before any code: the resolution is a fixpoint over (local, program-point)
pairs with a visited set per query, and a cycle that never reaches a
terminal MINTS/OWNED-AT-ENTRY root — the visited set closes without adding a
new terminal — resolves to ALIASES (unresolved), never to MINTS. This is the
same "err toward flagging, never toward silently accepting" rule the branch
case above already states, extended to cover the recursion's own
termination, not only the reaching-definition SET's computation. An earlier
draft of this document's Open Questions section allowed a "conservative
approximation that may miss some loop-carried shapes" — that direction is
wrong and is corrected here: an approximation may find MORE candidate
reaching definitions than a fully exact one strictly needs to (costing
false positives), but it may never treat a definition it could not
positively resolve to a terminal root as though it were one (which would
cost false negatives — exactly the failure mode this epic exists to close).

**Guarded drops are handled by trusting a NARROWLY RECOGNIZED pattern, not
any boolean-looking guard.** The compiler already encodes "release only on
the paths that minted" as a boolean raised at the minting site and tested
at the drop (`emitGuardedTempDrop`, `ChoiceReleaseGuards` — see Trap 3). For
v1, a drop reached through that EXACT, canonically-recognized shape (the
guard local is written `false` before the branch, written `true` only on
paths whose OWN reaching definitions independently resolve to MINTS, and
read immediately before the drop with no intervening write) is accepted
without requiring the drop's own reaching-definition set to independently
resolve — the verifier trusts that specific construction rather than
re-proving it, and a negative fixture (a hand-built "guard" local that does
NOT follow this exact shape — for instance, raised on an aliasing path) must
be in Step 1's acceptance corpus to prove the recognizer is exact rather
than pattern-matching loosely on "any boolean read before a drop." Fully
verifying guard correctness in the general case (proving the guard is true
on exactly the minting paths, no more and no less, for an arbitrarily
constructed guard) remains real additional work, named as an open question
below rather than folded into v1's scope.

Report-only: it collects findings (function, local, defining instruction,
releasing/consuming instruction, span) and returns them as data. It changes
no behavior and fails nothing on its own — steps 2 and 3 decide what to do
with what it finds.

Write the exact algorithm as a short design note in this file's own commit
before writing the pass, in the same spirit as Epic 24 step 0's "specify
before code" — the termination and reaching-definition rules above are the
minimum that note must state precisely, not a starting sketch to loosen
under implementation pressure.

**Gate:** the fixtures from DoD item 2 (minimal MIR reconstructions of
RV2-DEBT-097/098/099/100's pre-fix shapes, AND RV2-DEBT-052's `@copy`
residual — an ignored payload position whose read is `OperandCopyValue`
must never be treated as though the clone's SOURCE were released) are the
acceptance test, plus the before/after MIR structural-equality check from
"What We May Not Lose." Red before this step exists by definition; must be
green — flagged as findings — the moment the pass runs, and clean on the
current, fixed MIR shape for the same programs.

### Step 2 — Corpus run, triage every finding

Run the report-only verifier over every fixture that SUCCESSFULLY PRODUCES
MIR. That set is DISCOVERED, not approximated by directory naming: attempt
to compile every `.sg` fixture under `testdata/golden`, `showcases/`,
`core/`, `stdlib/`, and keep the successfully-compiling subset as the
corpus. Excluding paths under `*/invalid` gets most of the way there (of
966 `.sg` files, roughly 409 sit under such directories, deliberately
ill-typed and never reaching MIR) but is not the same claim as "produces
MIR" — a handful of fixtures OUTSIDE those directories fail to compile for
unrelated reasons (deferred-feature crossing fixtures, import-context
fixtures, fixtures `scripts/golden_update.sh` already knows to skip), and
scoping the corpus by a path filter alone would either crash Step 2's own
tooling on those or silently miss them. Discover the corpus by attempting
compilation and recording successes, the same "measure, don't assume"
discipline this document asks of everything else. Every finding gets one of
three outcomes, written down:

1. **A real bug.** Fix it with the same rigor as this session's five rows —
   measure before and after, gate with a regression test, get it reviewed by
   `codex review` (see the "Codex In The Loop" section below).
2. **A verifier false positive.** Fix the classification table or the
   dataflow, not the code the verifier flagged. Add the shape that fooled it
   to step 1's fixture corpus so it stays fixed.
3. **A deliberate, narrow exclusion.** Recorded in a machine-readable
   allowlist (keyed narrowly enough that it cannot silently cover a shape it
   was never meant to — a function name plus a local name plus a short
   reason string, not a blanket per-file or per-package suppression) and
   cross-referenced from `DEBT.md` with the exact shape, why it is safe, and
   — if it is safe only because of something the verifier cannot see (a
   runtime invariant, a backend-specific guarantee) — what would have to
   change for the exclusion to stop being safe. A test proves BOTH that the
   allowlist suppresses exactly the entries it names and that an entry which
   no longer produces a finding (the underlying code changed and the
   exclusion is now dead weight) is itself flagged as stale, so the list
   cannot silently outlive its own reasons.

Do not start step 3 until this list is empty or every remaining item is
category 3 with a written reason. A verifier that ships with silent
exceptions is worse than no verifier: it teaches the team to trust a build
that can still lie.

**Scale, decided up front rather than discovered under a timeout.**
`testdata/golden` alone is 966 `.sg` files, and `make check`'s `test` target
runs under a 90-second PER-PACKAGE timeout (`Makefile`, the plain `test`
target) — compiling hundreds of fixtures through the full pipeline inside
that budget, stacked against whatever else already runs in the same
package, is not a safe assumption. This is not a new problem this epic
invented: the repo already solves it by keeping expensive checks OUT of
`make check`'s `test` target and into named, separately-timed targets
composed under `runtime-v2-check` (`runtime-v2-crossing-check`,
`-heap-check`, `-waiter-check`, `-fd-registry-check`, each its own
`go test -run '^Test...$' --timeout Ns` line in the Makefile). This epic
follows the same shape: the fast, representative sample that Step 3 wires
into `make check` stays small by design (seconds, a couple dozen fixtures
covering Step 0's classification space), and the full-corpus run becomes a
new `runtime-v2-ownership-check` Makefile target, composed into
`runtime-v2-check` with its own generous timeout — not squeezed into a
budget it was never sized for.

**Gate:** zero un-triaged findings across the corpus; `DEBT.md` entries plus
allowlist entries for every category-3 exclusion, both directions tested
(suppresses what it should, flags what has gone stale).

### Step 3 — Promote to a hard gate

**`mir.Validate` is not test-only today — an earlier draft of this document
said it was, and that was wrong.** `buildpipeline.Compile`
(`internal/buildpipeline/compile.go`) already calls
`mir.ValidateWithOptions` unconditionally, on every real build, right after
async lowering. What is actually missing, verified against
`cmd/surge/build.go` and `internal/buildpipeline/{compile,build}.go`, is
narrower and more mechanical than "wire an unused validator in":

- **`--dev` is parsed by the CLI and then goes nowhere.** `build.go` reads
  the flag only to reject it alongside `--release`; there is no `Dev` field
  on `CompileRequest` or `BuildRequest` for it to set. Step 3 adds one, and
  threads it from the CLI flag through to `Compile`.
- **The new ownership verifier is the thing that actually needs wiring in,
  and it stays behind the now-real `--dev` field for the whole scope of
  this epic — not "until Step 2 is clean," which an earlier draft of this
  document said and which contradicted "What We May Not Lose"'s own "release
  builds carry zero overhead" promise.** `mir.Validate`'s existing
  invariants (block termination, valid targets, valid local IDs, return-type
  matching, `EndBorrow` well-formedness) say nothing about ownership — that
  gap is this epic's whole reason to exist, and the new verifier is the
  piece with no call site yet. Whether it should ever run unconditionally,
  the way `mir.Validate` itself already does in every build including
  release, is a separate decision this epic does not make — it is named in
  "Out Of Scope" rather than assumed as a natural next step, precisely
  because that promise is binding and this document should not quietly
  contradict it in one section while stating it in another.
- **`make check`** gets a new Go test, in the same package/style as this
  session's `internal/crossinggate/capture_unpack_transfer_test.go` and
  `state_envelope_protocol_test.go`, running the fast representative sample
  from Step 2's scale note — cheap, no `SURGE_SKIP_TIMEOUT_TESTS` gate, no
  `valgrind` dependency, and small enough to live inside the existing
  90-second package budget rather than needing its own target.

**Gate:** flip a known-clean fixture to a known-broken one and confirm both
wiring points fail — but the negative probe has to be a shape THIS verifier
actually examines. "Comment out a release" removes the very sink the
verifier pattern-matches against, so it would go quiet for the wrong reason
(nothing to check, not "checked and passed"). The valid negative controls
are: swap a MINTS-classified definition for an ALIASES one reaching the same
release (e.g. replace a `StructLit` with a bare field read of the same
type), or delete a compiler-inserted retain that an ALIASES read depends on.
Then confirm `make check`, `make golden-check`, and `make runtime-v2-check`
are all green on the real corpus, and the native e2e baseline
(worktree-verified, not remembered) is unchanged.

### Step 4 — MIR dump ownership annotation (opt-in)

A local carrying an unmet release obligation prints as
`L8: string [owns_heap, owes_release]` instead of `L8: string [owns_heap]`,
and the RHS of an assignment optionally prints its effect class
(`mint`/`alias`/`transfer`). Behind a flag the default `--emit-mir` path never
sets — `--emit-mir-annotated` or an equivalent verbosity level — so every
existing golden `.mir` fixture stays byte-identical without special-casing.

This directly replaces the manual work this session did over and over:
reading `registerRefCountedTemp` and reconstructing by hand which locals owed
a release. The annotation makes that fact visible in the dump instead of
requiring it to be re-derived.

**Gate:** `make golden-check` unchanged with zero new fixtures touched;
a new, separate golden fixture (or inline test) for the annotated output
path only.

### Step 5 — Shared ownership e2e harness + type matrix

Extract `ownershipGate(t, source)` (or similarly named) into a shared test
helper in `internal/vm`: compiles both backends, runs native under `valgrind`
asserting invalid-ops AND `definitely lost` at strict zero in the same
assertion (this session's fixes traded one for the other twice — 097's fix
briefly regressed a doubled release, 052's first cut regressed a `@copy`
leak — so both columns must be checked together, every time), and runs the
interpreter asserting a clean exit with no runtime error.

**Be honest about what the harness actually saves.** The four tests named
below already reuse `buildRuntimeV2CrossingSource`, `runBinaryUnderValgrind`,
`parseValgrindDefinitelyLost`, and `runProgramFromSource` — their own
per-test plumbing is already a lean ~20 lines (13 for the native/valgrind
half, 8 for the interpreter half), not the ~200 an earlier draft of this
document claimed. `ownershipGate` folding those two functions into one call
saves real but modest lines. **That is not the point of Step 5** — the point
is DoD item 6: the helper's API must REQUIRE — not merely permit, and not
merely comment-request — the caller to supply a probe for all four type-
matrix axes (move-only heap / `@copy` value-composite / reference-counted
scalar / non-owning), mechanically. That means a Go FUNCTION with four
named, non-optional parameters — Go enforces that all of a function's
parameters are supplied at every call site, which a struct literal does
NOT: `Foo{A: x}` compiles fine even when `Foo` has fields `B`/`C`/`D`, so a
struct-literal-based API (considered and rejected here) would not actually
be the compile-time guarantee this step claims, only a runtime-checkable
one. A comment convention is exactly what this session's own `@copy`
regression walked past — every union in the existing census carried a
`string`, and nothing forced a reviewer to notice the missing axis. Four
required function parameters are what actually forces it.

Migrate this session's newest ownership tests
(`runtime_v2_identity_cast_e2e_test.go`,
`runtime_v2_borrowed_scalar_e2e_test.go`,
`runtime_v2_compare_payload_handoff_e2e_test.go`,
`runtime_v2_borrowed_compare_payload_e2e_test.go`) onto the shared helper as
the demonstration that the mandatory-axis API is real, not that the line
count drops dramatically — it will not, and DoD item 5 says so honestly. A
full retrofit of every existing ownership e2e test in `internal/vm` is
explicitly OUT OF SCOPE — too large a diff for the value it adds; new tests
use the helper going forward by convention, not by a mass rewrite.

**Gate:** the four migrated tests pass; a synthetic negative probe
(constructing a harness call that omits one type-matrix axis) fails to
compile or fails at test setup, not merely "would have been nice to
notice."

### Step 6 — Per-site heap allocation census (dev-only)

Make a leak or double-free report name `main.sg:42:9` instead of an
anonymous `fn.63` in a stack trace read by hand — without editing
`rt_alloc`'s call sites (~67-70 of them across 36 native `.c` files) at all,
which was this document's original design and is now the LAST-RESORT option
rather than the plan.

**Preferred design: a thread-local "current site" set only at
COMPILER-GENERATED call boundaries, read by `rt_alloc` but never passed as a
parameter.** The runtime gains one TLS variable. LLVM codegen — a small,
fully-controlled set of emission points the compiler already owns, not the
36 native files — emits a call to set that variable immediately before each
user-visible allocating operation it generates (a struct/array/tuple
literal, a string op, a cast that allocates). `rt_alloc` itself does not
change signature; it reads whatever site was last set. Every native-internal
helper that calls `rt_alloc` on the runtime's own behalf (the 67-70 sites)
inherits whatever site the compiler last set, which is exactly the
granularity wanted — attribution to the `.sg` call site, not to which C
helper eventually allocated on its behalf — and needs no individual editing.

**An LLVM-debug-info-based design (emitting real `!dbg`/`DILocation`
metadata so `valgrind` or a symbolizer resolves locations natively) was
considered and set aside for this step, not because it is a bad idea but
because it is a BIGGER one:** the LLVM backend emits no debug information
today at all (`internal/backend/llvm` has no `DILocation`/`!dbg` machinery
to extend), so this path means building a new subsystem rather than reusing
one, and is not obviously cheaper than the TLS design for this epic's actual
need. Worth revisiting as a follow-up if TLS-level granularity (one site per
compiler-generated call boundary, not per individual native helper) proves
insufficient once step 6 is in use — not a reason to hold this step back.

**Scope this honestly: a thread-local "current site" belongs to an OS
WORKER, not to a logical task, and this runtime's own async model makes
that distinction real, not theoretical.** A task suspends at a poll
boundary and may resume on a DIFFERENT worker; workers drain queued
remote-spawn or other deferred messages before polling user code again
(`runtime/native/rt_worker_turn.c`, `rt_remote_spawn.c`), and those paths
allocate on the runtime's own behalf with no compiler-generated "set the
current site" call anywhere near them. A leak in that class of allocation
would report a STALE site (left over from whatever unrelated task last set
one on that worker) or none at all — not a wrong crash, but a misleading
answer, which is worse for an investigation tool than an honest "unknown."
Step 6's TLS design is correct and useful for its actual common case —
attributing a SYNCHRONOUS, straight-line allocation to the `.sg` construct
that caused it, which is the exact pattern this session's own
reproduce-by-hand loop needed over and over — and is explicitly NOT claimed
to correctly attribute allocations that cross a suspension or worker
boundary. That gap is accepted, named, and not silently papered over; if it
ever blocks a real investigation, the harder fix is carrying an explicit
site id as a field in the async state envelope itself (the same structure
this session's Epic 24 step-0 tail work already threads locals through
across suspension), not deepening the TLS mechanism to chase a boundary it
was never designed to cross.

This is still the highest-risk, lowest-priority item in the epic:

- It must be **zero-cost in release builds** — compiled out entirely
  (`#ifdef`/build-tag gated) or a no-op behind a runtime flag that defaults
  off, confirmed by a benchmark showing no measurable regression in a release
  build with the feature compiled out.
- `mir.Local`/`mir.Func` already carry `Span source.Span` (`internal/mir/
  types.go`) — the site ids the compiler emits are read directly from
  information already flowing through the compiler, the same "it's already
  there, just never checked/surfaced" pattern the rest of this epic rests
  on, not a new bookkeeping structure.

If step 6 turns out to cost more than steps 0-5 combined for a payoff that
step 5's harness already covers well enough, that is a legitimate reason to
close the epic without it and record the deferral in `DEBT.md` — this step
earns its place only if it demonstrably shortens investigation time on a real
future row, not on faith.

**Gate:** a deliberately introduced leak in a throwaway fixture reports a
source location, not just a stack trace; a release build's binary size and a
representative benchmark (`scripts/bench_native_channels.sh` or similar) are
unchanged with the feature compiled out.

## Codex In The Loop

Per the working agreement recorded in memory (`use-codex-actively`), every
commit in this epic gets `codex review --base <sha>`, and any step whose
design is non-obvious gets a scoped `codex exec` second opinion before
implementation, not after. Concretely for this epic:

- **The epic document itself** goes through `codex exec` review before
  implementation starts — the "align through iterations" the owner asked for.
- Give `codex exec` a HARD timeout, `< /dev/null` when backgrounded, and an
  explicit "do not run the full test suite" instruction — `codex review`
  hung for 36 minutes this session doing exactly that, and a scoped `exec`
  with a diff on disk found a real regression in under ten minutes. See
  `use-codex-actively.md` for the full accounting.
- Step 1's dataflow design note and step 6's ABI note both get a scoped
  `codex exec` pass before code, matching how this session's negative-probe
  method (deliberately break a fixture, confirm the gate catches it) is
  itself a discipline worth stating once and reusing.

## Traps To Know Before Writing Code

1. **The verifier proves internal consistency, not runtime correctness.** It
   checks that the compiler's own bookkeeping is self-consistent — a released
   local's definition resolves, recursively through any TRANSFERS steps, to
   a MINTS. It cannot see a bug in `rt_bignum`'s reference counting, or a
   genuine double-free the MIR correctly describes as two independent,
   individually correct releases of two different locals that happen to
   alias at runtime for a reason the type system does not track. `valgrind`
   and the native e2e suite remain necessary; this epic makes them a second
   line of defense instead of the only one.
2. **The rule is EVERY reaching definition, not ANY — Step 1 already settles
   this, so treat a design that checks only one path as a regression of the
   spec, not a stylistic choice.** What loops make genuinely hard is not the
   rule but computing the reaching-def SET exactly across a back-edge (a
   local reassigned in the loop body reaches the release on some iterations
   and not others depending on the CFG shape) — that is a real fixpoint
   computation, which is why the "exact vs. conservative approximation"
   open question below exists. A conservative approximation must still err
   toward FLAGGING more, never toward silently accepting fewer reaching
   defs than actually exist, or the pass becomes unsound in exactly the
   direction that lets a bug through.
3. **A guarded/conditional release (`emitGuardedTempDrop`,
   `ChoiceReleaseGuards`) is not the same shape as an unconditional one.**
   The existing MIR already encodes "release only on the paths that minted"
   via boolean guards raised at the minting site — the verifier's dataflow
   must recognize that pattern rather than flagging every guarded drop as
   suspicious, or step 2's corpus run will drown in noise from correct code.
4. **Do not let the verifier become a second source of truth for ownership
   rules.** If it disagrees with what sema/HIR/MIR actually decided, the
   BUG is the disagreement (as with 097/099/100) — the fix belongs in
   whichever pass was wrong, never in loosening the verifier to agree with
   whichever pass happened to run last.
5. **`internal/gatecheck` will itself flag a new Makefile gate if its
   `-run` selection does not match a real test** — run it locally before
   assuming step 3's wiring is correct.
6. **TRANSFERS-as-pass-through is the load-bearing design decision of this
   whole epic — do not simplify it back to a flat per-kind table under
   deadline pressure.** It is tempting, once the exhaustiveness test is
   green, to treat `OperandMove` and `RValueField{MoveOut:true}` as
   unconditionally satisfying MINTS/TRANSFERS the way a flat table would. A
   move of an alias is still an alias, only relocated — RV2-DEBT-100's
   actual pre-fix shape was exactly a chain of ordinary moves downstream of
   one unretained borrowed read, and a flat, non-recursive table would not
   have caught it (this was found and fixed once already, in review, before
   this document's first implementation step; do not lose it again). The
   same recursion is what makes the aggregate-literal field-sink rule (Step 0)
   sound instead of a second, disconnected special case.
7. **`RValueTagPayload`'s `MoveOut` flag replaces subject-provenance
   DERIVATION for exactly one decision — ALIASES vs. TRANSFERS — and
   nothing more; it does NOT exempt `RValueTagPayload` from Trap 6's
   recursion once that decision is TRANSFERS.** An earlier design tried to
   derive a tag-payload read's ownership purely from whether its subject
   resolves to MINTS, and review found a real counterexample:
   `compareScrutineeOwnership` has THREE outcomes (borrowed/moved/
   duplicated), not two, and a `@copy` subject read through a deref MINTS a
   fresh, cloned envelope while still needing its payload extraction to
   retain — the clone is deep-dropped later, unlike the moved case's
   narrowed shallow release. "Subject resolves to MINTS" cannot tell those
   two apart. The settled design is a real, small MIR schema addition
   (`TagPayload.MoveOut`, mirroring `FieldAccess.MoveOut` exactly, set by
   the lowering from HIR's own three-way answer) rather than asking the
   verifier to re-derive a decision the compiler already made correctly and
   then discarded. A SECOND draft of this design then overstated its own
   scope — "no recursion needed for this kind at all" — which would let a
   `MoveOut:true` extraction satisfy MINTS/TRANSFERS without checking that
   its subject was actually built as owned in MIR, silently trusting HIR's
   belief over MIR's own evidence, which is exactly the class of
   cross-pass disagreement this whole verifier exists to catch (Trap 4).
   The flag decides the ALIASES-vs-TRANSFERS QUESTION only; once it says
   TRANSFERS, ordinary recursion into the `Value` operand's place applies,
   the same as any other TRANSFERS. If a future refactor is tempted to
   remove the flag and go back to inferring it from the subject's
   provenance, OR to treat `MoveOut:true` as a non-recursing terminal,
   re-read this trap first — the `@copy`-duplicated-subject counterexample
   is checked in as one of Step 1's acceptance fixtures specifically so
   neither mistake has to be rediscovered by hand a second time.

## Settled Decisions

- **MIR-level, not HIR/sema-level — and this catches sema/HIR-rooted bugs
  too, not only MIR-rooted ones, which matters because "Why This Epic
  Exists" now shows every one of the five rows needed a sema- or HIR-side
  fix, not only the two most MIR-visible ones.** Take RV2-DEBT-100, rooted
  purely in sema (`registerComparePayloadDroppables` registered a drop
  obligation for a binding extracted from a borrowed union): it is tempting
  to think an MIR-only verifier misses that class. It does not. Sema's wrong
  obligation still has to become a real `InstrDrop` on a real local to
  matter at runtime, and by Step 0's `TagPayload.MoveOut` flag (set from
  HIR's own correct three-way answer, `scrutineeBorrowed` in this case) that
  local's definition classifies ALIASES, with no retain reaching it, and the
  local is released unconditionally. The verifier flags it regardless of
  which earlier pass is actually at fault; it detects the SYMPTOM at the
  level where the memory operation happens, which is a different question
  from where the FIX belongs (Trap 4), and MIR is where every one of this
  session's five rows was ultimately observable, root cause notwithstanding.
- **Report-only before hard-gate, always.** Every step that could break a
  build ships in report-only form first (step 1 before step 3), matching this
  session's own "measure first" discipline and this repo's standing pattern
  across every prior epic.
- **`--dev` gets real behavior; no new flag invented for the verifier.** The
  flag already exists and already does nothing but validate mutual exclusion
  with `--release`. Extending it is the smaller surface change and matches
  its stated purpose ("development build with extra checks") better than a
  second flag would.
- **Step 6 is explicitly the epic's lowest-priority, highest-risk item** and
  may be dropped without reopening the epic if steps 0-5 already deliver the
  DoD's measurable claims.

## Open Questions (non-blocking, revisit during implementation)

- Whether the verifier's dataflow should be an exact fixpoint (full
  reaching-definitions with loop-aware cycle handling, per Step 1) or a
  cheaper conservative approximation that computes a SUPERSET of the true
  reaching-definition set for a loop-carried local — costing more false
  positives, in exchange for a simpler, faster, more auditable
  implementation. Both are sound by Step 1's own rule (an approximation may
  over-count candidate definitions; it may never treat an unresolved one as
  satisfying MINTS). Not open: whether a loop-carried shape may be silently
  missed — it may not, under either choice. Lean conservative-and-simple
  first, tighten later if Step 2 shows the false-positive rate is costly in
  practice.
- Fully verifying guard correctness in the general case (Step 1's v1
  carve-out trusts a narrowly recognized canonical `ChoiceReleaseGuards`
  shape rather than proving an arbitrary guard is true on exactly the
  minting paths). Worth doing if Step 2's corpus run ever turns up a
  guard-shaped release this document's canonical-pattern recognizer cannot
  confidently classify either way.
- Whether step 4's annotation belongs in `--emit-mir-annotated` as a
  separate flag or as a `--mir-verbosity` level shared with future dump
  detail. Either is compatible with the "opt-in, no golden impact" contract;
  pick whichever reads better once `internal/driver`'s existing flag surface
  is in front of you.

## Out Of Scope

- Retrofitting the entire existing `internal/vm` ownership e2e suite onto the
  step 5 harness. Only this session's newest four tests migrate, as proof.
- Any HIR- or sema-level verifier. This epic is MIR-only; a sema-level
  ownership checker (which would need to model `movedPlaces`,
  `choiceOwnsItsValue`, `ArmDropsExpr` and the rest of sema's own bookkeeping)
  is a different, larger epic if it is ever wanted.
- Fixing RV2-DEBT-101 (borrowed tuple compare) or any other currently-open
  debt row as part of this epic, even if the verifier flags it in step 2 —
  triage it the same as any other finding, but closing it is separately
  scoped work with its own commit and its own review, not folded into this
  epic's diff.
- Any change to the language surface, new attributes, or new user-visible
  diagnostics. See "What We May Not Lose."
- **Promoting the ownership verifier to run unconditionally (in every build,
  including release, the way `mir.Validate` itself already does).** This
  epic keeps it behind `--dev` for its whole scope, matching "release builds
  carry zero overhead." Whether it should ever graduate to unconditional —
  once enough real-world use has built confidence in it beyond this epic's
  own corpus run — is a separate, later decision, not something this epic
  commits to or schedules.
- Fully verifying guard correctness for an arbitrary (non-canonical) guard
  shape. See the matching Open Question.

## Related Ledger Rows

RV2-DEBT-097, RV2-DEBT-098, RV2-DEBT-099, RV2-DEBT-100, and RV2-DEBT-052's
mixed-arm residual are this epic's motivating evidence, not its scope — all
five are already closed. RV2-DEBT-101 (borrowed tuple compare) is the epic's
first candidate finding once step 2's corpus run reaches it, but is not this
epic's to close.
