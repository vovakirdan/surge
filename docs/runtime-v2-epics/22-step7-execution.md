# Epic 22 Phase 2: Step 7 execution contract

Accepted 2026-09-11 after research rounds R1–R6. This document specifies the
implementation and its proof; it does not claim the work has passed. The owning
model is `docs/RUNTIME_V2.md`; `RULES.md` and `EPICS_CLOSEOUT_PLAN.md` apply.

## Boundaries and order

Start from `ab0a73950402b471f25c63bffa87747650d04da6`, tree
`678eb721c25aa5d72bbb72b7acf2a9e4f82415e7`. Preserve the older dirty `step7-s1`
and `step7-debt363` worktrees as research artifacts. Rebuild from the accepted
design; do not integrate those trees wholesale.

1. Recover the baseline harness and first-local-task peer wake; enforce the
   golden filesystem/index census; freeze the current Ryzen benchmark protocol.
2. Fix the equal heap-uint subtraction leak independently.
3. Implement numeric scope IDs, offered-copy send ownership and the explicit
   post-baseline carrier allowance infrastructure as separate prerequisites.
4. Prepare numeric loop ownership while `float` already exercises the protocol.
5. Integrate the `int`/`uint` predicate, LLVM lifecycle branches, language/runtime
   ABI fixes, diagnostics and fixtures in one atomic B commit (S1 + S1b).
6. Run the full census, semantics, ownership, liveness, sanitizer, structural,
   golden and paired performance gates; reconcile the owned debt and epic docs.

Every writer has an isolated worktree and approved Rule 9 plan. Lead reviews
artifacts, obtains independent non-author review from a clean worktree, and runs
all expensive checks on Ryzen. Candidate snapshots exist to pin measurements;
they are not accepted integrations until the required checks pass. The local
pre-commit workload also runs on Ryzen. Keep its generated `STATS.md`.

## Baseline recovery

Fresh full Go runs on the starting SHA selected all 2,152 top-level tests once
per backend, with `-count=1 -parallel=1 -p=1`, timeout tests enabled and timeout
scale 3. VM had one failure; LLVM had seven. Missing tests, skip, cache and absent
package completion are separate observer failures, not passing evidence.

- Timeout diagnostics: literal scale 1 → `1s`, scale 3 → `3s`; keep exit 7,
  empty stdout/stderr and the persisted artifact checks.
- Panic fixtures: VM's result stderr is empty; LLVM emits exactly
  `panic: boom\n`; both exit 1. The buffered payload test proves termination,
  not native reclamation after `_exit`.
- Temporary stdlib: carry an explicit `runOptions.stdlibRoot` through compile,
  execution and reproduction commands. Keep main, copied core and imported
  fixture module together in the failure artifact directory. Do not weaken the
  Surge source to work around compiler failures. CI selects LLVM explicitly.
- User `from_str`: if the resolved symbol has a real `FuncBySym` body, use the
  ordinary direct call instead of the name-based intrinsic. Retain the builtin
  parsing controls. Removing the guard must reproduce the user-parser failure.
- Peer wake: a requested publication to a local unpinned ready queue must signal
  when peers exist even if it is the queue's first task. Preserve the affine
  route and the single-worker rule. A real idle peer is held immediately before
  its zero-credit condition wait; producer stays in its poll until the receiver
  acknowledges execution on the peer. Restoring `local->len > 1` must produce a
  named bounded failure and still release the producer and shut down cleanly.

The two MT failures reproduced in isolation with their original 30-second
budget. Setting the existing channel injection control made the same binaries
exit successfully in 4.3 ms and 1.1 ms. This supports the bounded wake fix; it
does not close the whole credit model. Unrequested ready publication and credit
consumption after queue exhaustion remain separate research findings.

Before compiler-output edits, run the original `make golden-check` and retain
NUL-safe index/status records plus a complete path/kind/mode/content manifest.
The existing golden utility already regenerates twice and checks status and
content; extend it only with the missing index census. A frozen empty orphan
directory that Git status hides is the named preflight negative control.

## Numeric prerequisites

Equal heap uint subtraction uses one `bu_cmp`: equality returns canonical NULL
without allocation; only a negative comparison reports underflow. Both inputs
and their RC remain unchanged, including identical-pointer and distinct-equal
inputs. Unequal subtraction controls and a reverted-leak negative are required.
The separate historical sized-free/live-byte counter drift is not repaired by
changing object layout in this task.

Scope IDs are numeric end to end: `rt_scope_enter` returns `uint64_t`;
register/cancel/join/exit take `uint64_t`, and join writes `uint64_t *pending`.
Reuse current owner lookup and stale/zero behavior; do not add a registry. LLVM
and MIR carry i64 without heap ownership. VM accepts exact Uint64 bits, including
the high bit and max uint64, through the existing alias/reference handling; it
rejects wrong kinds, widths, signed/big values and resource pointers. Native C
stands adapt only their scope variables. Add the completed-child release test
explicitly to the lifecycle gate selection.

Offered-copy send adds `rt_channel_send_offer` and
`rt_channel_send_yield_offer`. Each call transfers or drops exactly one offered
reference; bool continues to mean Ready/Pending only. MIR retains a counted Copy
word once per poll while preserving the original frame owner. LLVM stores that
offer in an aligned disposable typed alloca, never in the live frame binding.
Runtime records whether any staging/direct-ring path took it; otherwise it drops
the offer outside the owner lock before unpinning. Its address never outlives
the call. Exercise ACK, existing stage, pool full, claim refusal and cancellation,
including a callback that clears the offered word. Keep the consuming API for
owning calls. Repeat cancellation at 1 and 8 workers and keep the t15 budget.

Carrier baseline count 683 and digest
`db5a0f475c32c2155aa82f3606800da0668392bd2e7a7aee917b742e76e58ee9`
stay immutable. Optional version-1 `post_baseline_allow` entries identify an
exact category/path/token/evidence/ordinal key and carry reason, safety and
invalidation. Reject duplicates, overlap, stale/missing keys and extra findings.
Keep historical exact comparison separate from live post-baseline comparison.
Infrastructure lands empty; the actual new ptrtoint allowance lands with B.
The old W8 inttoptr finding remains pinned; numeric scope needs no allowance.

## Loop ownership preparation

Use existing MIR lexical `tempDropFrames`; do not add an AST return walker.
Private generated-let metadata distinguishes counted scalar candidates from
numeric iterable resource candidates before monomorphization. After mono,
register only actual counted scalar locals or actual numeric Range/Array/fixed
Array resources, after successful initializer assignment and before temporary
flush. Resolve aliases but do not unwrap references.

Ordinary `InstrDrop` owns those entries. Suppress only a legacy whole-local
cursor release/drop for a currently registered resource; preserve projected,
scalar, caller-local and nonnumeric cleanup. Generic binding type falls back
from VarType to bindingType to element type. `Some` payload MoveOut already
transfers its value; do not add a second retain. Bounds-next transfers the old
start; LLVM array-next retains a counted scalar exactly once, VM already does.

Add internal `WhileData.Post` only for generated numeric-fast loops. Lower both
normal fallthrough and continue through a latch using the existing overwrite
drop sequence: materialize RHS, drop old, store. Propagate Post through HIR
normalization, printing and release traversal; mono cloning, substitution, type
collection, call/variable rewrites and DCE; MIR lowering. Remove the numeric
continue rewrite, retain the separate classic-for path.

Loop contexts keep the lexical temporary depth before the body. Break/continue
drop body-owned entries; current/end live outside that depth. Explicit and
implicit returns flush to the return context while detaching returned ownership
with existing return machinery. Initializer exits drop only completed bindings.
Cleanup order is x → cursor → hoisted source, or x → end → current. Existing
drop use/def drives suspension liveness, frame packing and cancellation cleanup.

Typed cursor Drop is valid on both backends. LLVM Range initialization already
allocates a copy and uses kind-aware `rt_range_free`; VM owns its Range/backing
reference and marks dropped resources so teardown skips them. Preserve original
caller ownership; do not claim equal cursor-position semantics across backends.
Prove float first, then integer after B: normal/break/continue/return, expression
continue, nested return, returning the loop value, initializer exits, generic
inference, suspend/cancel, stored/passed temporary ranges and named/temporary
arrays. Strict-zero checks run after releasing caller/results too.

## Atomic B

Make int/uint counted Copy scalars and implement their lifecycle together. For
int/uint, branch on `word != 0 && (word & 1) == 0` before every RC access or
lifecycle call. Fixnums execute no allocation, retain/release call or counter
access. Keep the float path and suffix RC offsets 0/4/8. Share full-precision
literal classification between MIR and LLVM: signed [-2^62, 2^62-1], unsigned
[0, 2^63-1]. Preserve per-kind release/clone/unshare behavior and aliasing.
Known integer-one bounds increments omit its release; float remains counted.
An int array crossing still walks potential heap leaves; do not invent metadata
to disguise that legitimate cost.

Entrypoint helper ownership follows OwnsHeap even for Copy. Handle Copy code,
argv length and exit code explicitly. Fixed64 opaque File/TcpListener/TcpConn/
Task/Placement representations retain their existing object/registry/lifecycle
meaning. Resize is `Resize(int64, int64)` end to end; C event payload and VM
event construction agree. `term_size` is unchanged.

Diagnostics recursively find counted union tag arguments and nested map key
culprits. Compile each suggested repair at the concrete failing site. Change
plain numeric fixtures to fixed-width only when preserving their subject and
add counted twins. Behind-handle fixtures assert the actual refusal arm. No
blanket golden reblessing, compatibility ABI, version/board bump or hidden Step 8
handle traversal is part of B.

## Acceptance and reporting

All heavy rows use clean detached exact SHAs on the marked Ryzen, exact-worktree
stdlib, cleared inherited SURGE variables, timeout tests enabled, no cache, and
one lead-owned workload. Check actual selected RUN/PASS/terminal inventories.

- Full Go VM + LLVM and the full `runtime_v2_pending` VM package; ownership and
  transport tag inventories separately. Explain every skip and its replacement.
- Aggregate roster with every START/verdict, behaviour VM + LLVM, and MT at
  workers 2/8 repeat 2. Include the explicit overwrite/liveness stress rows.
- Carrier sanitizer preflight with planted defects, strict expected rows,
  Valgrind, race and ASan/UBSan/TSan lanes. Require actual PASS.
- Required C checks, lint/check, effective file-size check against Step 7 base
  `819eb378`, and Sentrux same-path root/scoped evidence without regression.
- Review each golden proposal, commit only the explained exact set, then run two
  serialized `make golden-check` commands with identical reviewed manifests.
- Run all 46 paired performance rows under the frozen manifest, prebuilding all
  timing binaries on both sides and candidate resource binaries. Add numeric
  fixnum allocation/call/counter, heap-copy retain/no-clone and unshare controls.
- Attribute census reductions to the exact removed work, never blindly repin.
  Close only proved owned debts (035/068/357/363 and verified residual notes).
  Keep Step 8 and unrelated pre-existing debt explicit. Phase 2 completion does
  not mean every Runtime V2 debt is closed.

The performance base is closed 23b `ea50ca0b6f22164bd2c615d73aff5d31670f2611`,
distinct from the immediate `ab0a7395` baseline. Both are rebuilt at O0 while
DEBT-333 remains open. Use 24 placements, one warmup, five measured pairs and
the lower-median clean placement; CV ≤0.05, throughput ≥0.95 (≥0.90 for two
sub-ms batches), p95 ratio ≤1.10 only when both reach 1000 ns. Never score heap
reclamation against the leaking base as a relative performance obligation.
