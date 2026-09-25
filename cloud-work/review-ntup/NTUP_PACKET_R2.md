# NTUP R2: tuple index (kind 12) in return-origin; a tuple pattern in `let` is a compile error

2026-09-25. Generation base **`045c1135`**, the D2 tip after N-TASK-27S. VMDH and LITX land before NTUP; the generator
regenerates on any descendant of `045c1135`.

**Why R2 exists.** The owner answered OQ-1 of R1 (`NTUP_PACKET_R1.md`): tuple destructuring in `let` is not part of the
language. LANGUAGE.md §2.10 said so ("not supported yet"), and the grammar's `Let` has no pattern. This mirrors the
anonymous-record ruling (LITX). R2 therefore:
- keeps the cloud agent's kind-12 transfer;
- refuses `let (a, b) = t;` in the checker with a new code;
- drops the cloud's destructuring transfer;
- rewrites or moves every program that destructured.

Bundle `impl-ntup/`: MANIFEST sha256 **`e67447d3988e3a99091f3e8170b5f4ae87304da2a61a1e202ece188daa8dba30`**. The script
pins it with `NTUP_MANIFEST=<prefix>`.
- Generator: `gen/gen_ntup.py --base <sha> --tree <tree> --siblings ~/.cache/surge-artifacts/step7-return-source`. It is
  deterministic: two runs give the same MANIFEST. The cloud code is taken by commit (`7fbfaa08`), and nothing under
  `cloud-work/` is used.
- Measurement: `measure/run-ntup.sh`, on ryzen only. The launch line in its header is `NTUP_BASE=045c1135`.

Evidence tags:
- [read]: read at `045c1135`.
- [local]: the WSL preflight in §10, never a measurement.

The kind-12 soundness review of R1 (§2 of `NTUP_PACKET_R1.md`: element rule, G6 premises, adversarial probes) still holds
for R2. The index code is the same bytes.

## 1. What the bundle changes

**Kind 12, taken by commit.**
- `return_origin_expr.go`: the cloud's `case ast.ExprTupleIndex`, re-anchored. It applies on 27S's version of the file.
- `return_origin_tuples.go`: the cloud's file with `gen/ntup_edits.py` applied, 83 effective lines:
  - `destructure`, `bindPatternUnknown`, `wildcardIdent`, the destructuring const and the `source` import are dropped;
  - the header says the checker refuses a `let` pattern;
  - `tupleElementValue` names its results. This is R1's lint fix.
- `return_origin_stmt.go` is **not** taken. The base's row "destructuring needs projected origin facts" stays as the
  second fence, and CF-NTUP-LETREFUSE shows that it holds (§5).

**The checker.**
- `internal/sema/let_forms.go`: `checkTupleLet` still types and moves the value. It then reports:
  - **`SemaLetTuplePattern`, SEM3220**, at the pattern: "a `let` binds one name; it cannot take a tuple apart";
  - note: "tuple patterns are written only in `compare` arms";
  - help: "bind the tuple and read its elements: `let t = value; let x = t.0;`".
- A new, silent `bindRefusedTuplePattern` gives each name its element type when the shapes agree. Nothing that reads a
  name reports again (rows in §3).
- `bindTuplePattern` is removed from `type_checker_bindings.go` (390 effective lines after). Its SEM3015 refusals for
  arity and non-identifier elements are never reached now.
- `internal/diag/codes.go`: `SemaLetTuplePattern Code = 3220` is registered like its neighbours, with the const, the
  comment and the `codeDescription` entry. **3219 is left to LITX** (`SemaLiteralNeedsType`), which lands first. The
  generator accepts 3219 only as free or as LITX's.
- **The parser needs nothing** [read + local]. `let (a, b) = v` already parses to a `LetStmt` with a `Pattern`. The
  variants the parser refuses today keep their codes:
  - an annotated pattern `let (a, b): T = v` is SYN2001, plus SEM3005 "cannot resolve" for later uses;
  - `let mut (a, b)` is SYN2102, plus SEM3005;
  - a pattern without a value is SYN2001.

  These are kept as they are. Each is already an error, and the parse fails before `checkTupleLet` sees a pattern.
  `(&a, b)` now gets SEM3220 plus the resolver's own SEM3005 for `&a`, which it cannot declare. `(1, b)` and an arity
  mismatch get SEM3220 only, where before they got SEM3015.

**LANGUAGE.md and LANGUAGE.ru.md (the ru file carries the English text).**
- Line 563 now reads: "Tuple destructuring in `let` bindings is not supported: a `let` binds one name, so
  `let (a, b) = t;` is a compile error (`SemaLetTuplePattern`, SEM3220). Bind the tuple and access fields via `.0`, `.1`,
  etc."
- The Limitations line gains "(a compile error, SEM3220)".
- The grammar line `Let := "let" ("mut")? Ident ...` is already right and is not changed.

**Tests.**
- `internal/sema/let_tuple_pattern_test.go` (new): `TestLetTuplePatternIsRefused` has 15 leaves:
  - 12 refused, each with exactly its SEM3220 at the pattern and no other error: a literal, an owning element read later,
    a call result, nested, `_`, `(a,)`, through `&`, an `own` subject, an arity mismatch, a non-identifier element, a value
    block, and two in a loop;
  - 3 controls: `t.0`/`own t.1`, `let _ = t`, and a `compare` arm tuple pattern.

  `TestLetTuplePatternMessage` pins the code, message, note and help.
- `internal/driver/return_origin_tuples_test.go` (new): the index rows, 29 leaves in 3 tests.
  - `TestAnalyzeTuples`, 14: the cloud's 12 index rows plus the review's `own` parameter and scalar-store rows.
  - `TestAnalyzeTupleRefusals`, 7: the cloud's 4 plus the review's `&mut` element store, window through `&`, and tuple
    field of a struct.
  - `TestTupleEscapeIsReported`, 8: the cloud's 3 index canaries plus the review's call result, generic instantiation,
    reassignment, compare join and scalar store beside the reference.
  - The cloud's destructuring rows are dropped. What they covered is now the sema rows above.
- Re-pinned to the new checker error: `rd_tuple_pattern_binding_returned` and `rds_tuple_pattern_binding_returned` (the
  task-check R-d probes) now want `SEM3139,SEM3220`. The task check still sees the carried borrow, because the refused
  names keep their types.
- `internal/vm/vm_term_intrinsics_test.go`: `TestVMTermSizeOverride` reads `size.0`/`size.1`. It is red at the base and
  after on stdlib/term's own unfinished rows; only its destructuring row goes away.
- `internal/mir/ownership_tuple_destructure_test.go` is **removed**, with the form it tested. Its three tests are
  `expected_gone_rows`. The HIR/MIR lowering of a `let` pattern (`lowerLetPattern`, `lowerTuplePatternFields`) stays in
  place, now unreachable from source. The ledger row records it as a later cleanup, not done here.
- `internal/symbols/resolve_test.go` (`TestResolveLetTuplePatternDeclaresBindings`) is unchanged. The resolver still
  declares the names, and that is what lets the checker refuse without a cascade.

## 2. Golden decisions

| golden | decision | why |
|---|---|---|
| `sema/valid/tuple_destructure.sg` | **moved to `sema/invalid/`**, with a comment that marks each error | its whole point is destructuring. Now 2× SEM3220 |
| `sema/valid/tuple_destructure_call.sg` | **moved to `sema/invalid/`** | the point is destructuring a call result. 1× SEM3220, and the three reads after it report nothing |
| `vm_tuples/tuple_literals.sg` | **rewritten to `.0`/`.1`** (`pair.0`, `own pair.1`, `nums.0`, `nums.1`), stays valid | its point is tuple literals and element access in the VM. Its `.out` is unchanged, byte-equal on both backends, Valgrind clean. The F-5 leak leaves the corpus |
| `sema/invalid/tuple_destruct_mismatch.sg` | **kept**, comment updated; `.diag` is now SEM3220 | still an invalid destructure; the arity is never compared now |
| `sema/invalid/concurrency/task_created_outside_scope.sg` | `destructured_outside` becomes `nested_element_outside` with `let same = own nested.1.0;` | with a `let` pattern, SEM3220 hid all five of its SEM3209 [local]. The index spelling keeps **the same five SEM3209, byte-identical `.diag`** |
| `spec_audit/s02_types_tuples.sg` | the Destructuring section reads by index | spec_audit is not generated. Its census row and its ownership allowlist signature (SEM3178 at 23:26 and 29:29, both before the edit) are unchanged |
| `showcases/21_bigint_stress/main.sg` (not a golden) | `fib_pair` results read by `.0`/`.1` | it still stays unfinished, on two implicit-borrow rows |

All outputs were regenerated with the after binary exactly as `scripts/golden_update.sh` generates them:
`measure/golden_regen_ntup.sh`, with the source passed by absolute path. The regenerated outputs of the untouched
`hir/tuples`, `tuple_access` and `t15` reproduce their committed files byte for byte [local].

**F-6 [local]. `make golden-check` is vacuous on D2.** `testdata/golden.expectations.json` freezes `entry_count` 5487, but
`045c1135` has 5498 entries: 5368 files plus the directories. The target stops at its preflight "golden preflight rejected
frozen corpus" on the base and on any change. NTUP does not update the expectations file. LITX does not either, and the
corpus is already stale at the base. The measurement reads S-GOLD as NOT-RUN when both sides stop there, and relies on
S-GOLDFILES. A golden-check that "passed" on D2 by comparing `.sg` lines compared nothing.

## 3. F-2, F-3, F-5: unreachable from source; F-4 stays [local]

Every R1 witness now stops at SEM3220 and never builds (roster run mode NOBUILD):
- F-5 (a pattern name is never dropped): f01, d09.
- F-2 (an owning element moved out of `&(..)`): e01, and the sema row `through_a_reference`.
- F-3 (MIR cannot lower `&`/`own` subjects): d03, d10, e01, and the sema row `owned_subject`.

`tuple_literals` no longer destructures and is Valgrind clean.

**F-4 is unchanged and not NTUP's.** `Some::<&T>(r)` faults at run time on the base (k01, k02 at both sides; a05, a13
after). **Ledger text for the coordinator**, with the id left for them to assign:

> | RV2-DEBT-<id> | **`Some::<&T>(r)` BUILT WITH AN EXPLICIT REFERENCE TYPE ARGUMENT FAULTS AT RUN TIME ON BOTH BACKENDS, IN A PROGRAM THE CHECKER AND RETURN-ORIGIN ACCEPT.** Found by the NTUP review (2026-09-25) and measured on D2 `1b122bfa` and `045c1135` alike: `fn keep(p: &int) -> Option<&int> { return Some::<&int>(p); }` with `compare keep(&x) { Some(r) => *r - 7; nothing => 1; }` diagnoses ok, then `surge run --backend=vm` stops `panic VM1999: storage: bigint is not a reference (type#279)` at the constructor and the native binary exits 255 with no output; `&string` gives "string is not a reference"; the same in a local (`let o: Option<&int> = Some::<&int>(&x);`). An `Option<&int>` that comes from a call (`m.get_ref(&k)`) runs correctly on both backends, and `Some(&x)` without the type argument is typed `Some<int>` (a copy), so the fault is the explicit reference type argument of the tag constructor. It is not in the ledger (`grep 'is not a reference'`: 0). NTUP's kind-12 transfer makes more programs of this shape diagnose ok (its rows `keep_index`, `index_of_a_reference_holding_parameter` are analysis-only). Witness programs: `20260925-ntup/gen/probes/k01_some_ref_param_fault_at_base_run.sg`, `k02_some_ref_local_fault_at_base_run.sg`, control `k03_get_ref_control_run.sg`. | Open | Tag construction with a reference type argument (VM storage, LLVM) | `Some::<&int>(p)` and `Some::<&string>(p)` build a payload that reads back on the VM and natively, with a VM row and a native e2e row that fail without the fix, and k01/k02 exit 0 on both backends. |

## 4. The runner matrix and the probes [local]

There are 63 probes in `impl-ntup/probes/roster.tsv`, with R2's expectations measured at `045c1135` and with the after
binary.
- 24 are NOBUILD. These are every leak form through a tuple (b01–b15), the CF escape witnesses (w01–w03), and every
  destructuring witness (d03, d09, d10, d11, e01, f01).
- 2 are RUN: f02 (the index form of f01) and k03. Each is VM and native 3/3 exit 0 and Valgrind clean.
- 4 are KNOWNFAULT or KNOWNFAULT-AB (F-4), RECORDED.

Judge: S-MATRIX, S-RUNNER and S-RUN-ROWS are all clear. The b-probes keep their SEM3021 or SEM3139, and the destructuring
spellings add SEM3220.

## 5. Counterfactuals

Seven CFs. Each is one contiguous region, passes cfguard, and builds. Each is judged over the driver rows (31, including
the 2 re-pinned probes) and the sema rows (16) [local, all PASS].

| CF | region | red | behaviour through a rebuilt binary |
|---|---|---|---|
| CF-NTUP-IDX | the `ExprTupleIndex` case removed (`return_origin_expr.go:86-87`) | 28 (every index row but `swap`) | — |
| CF-NTUP-FRESH | an element holds nothing | 13: 8 escape canaries, 5 origin-keeping rows | w01 builds; VM `panic VM3301: use-after-free`; native prints `peek 1`, not 424242; Valgrind error |
| CF-NTUP-LOAN | a loan carrier treated as fresh | 2 | w02 builds; VM `VM3301 stale reference`; native `panic VM3202`; Valgrind error |
| CF-NTUP-REFERENT | the through-reference refusal removed | 2 | — |
| CF-NTUP-STORAGE | an indirect element's referent storage removed | 1 | — |
| CF-NTUP-LETREFUSE | SEM3220 built but not emitted (`let_forms.go:26`) | 15: 12 refused sema rows, the message row, 2 re-pinned probes | **the second fence**: f01 is not refused by SEM3220 but stays unfinished on "destructuring needs projected origin facts" and does not build |
| CF-NTUP-QUIET | the refused names left untyped (`let_forms.go:27`) | 3: `in_a_value_block`, and the 2 probes, whose SEM3139 disappears | — |

## 6. Predicted census and other deltas [local: 1098 programs at `045c1135`, 546/472/77/3 after]

**Census.** These are the exact changes, and nothing else moves (1091 programs are equal in every strict field):
- unfinished → ok:
  - `hir/tuples`;
  - `sema/valid/tuple_access`;
  - `vm_async_suite/t15_fairness_round_robin`;
  - `vm_tuples/tuple_literals` (rewritten).
- diagnostics → diagnostics, `stdout_head` now SEM3220: `sema/invalid/tuple_destruct_mismatch`.
- Removed (unfinished at the base): `sema/valid/tuple_destructure` and `sema/valid/tuple_destructure_call`.
- Added, SEM3220 only: `sema/invalid/tuple_destructure` and `sema/invalid/tuple_destructure_call`.

**Wide diag** (245 files):
- 10 programs lose only kind-12, destructuring or derived rows and stay unfinished. The rewritten showcase is compared by
  its reasons.
- `testdata/runtime-v2-ordinary-storage/ops/storage_construct.sg` becomes ok. It prints `construct ok` on both backends
  and is Valgrind clean.

**Roster flip-ins**, each run FAIL at the base and PASS after:
- `TestVMAsyncSuiteGolden/t15_fairness_round_robin/vm`;
- `TestVMTuplesGolden/tuple_literals/vm` and `TestVMTuplesGolden`;
- `TestBehaviourCorpusMT/t15_fairness_round_robin`;
- `TestRuntimeV2OrdinaryStorageCorpusOnVM/storage_construct`;
- `TestRuntimeV2OrdinaryStorageParity/storage_construct`.

**Gone rows:** the three `TestOwnershipTupleDestructure*` tests.

**Roster lines:** `TestVMTermSizeOverride` is red on both sides and loses its destructuring row, so S-ROSTER-LINES is
READ.

**Runtime:**
- t15 is byte-equal on the VM and multiset-equal natively (sidecar `.order-backends`: `vm`).
- tuple_literals is byte-equal on both backends.
- **S-VALGRIND is predicted PASS**: t15, tuple_literals and storage_construct have 0 errors and no loss.

## 7. Ledger (commit L; `gen/hunks/`)

- **The RV2-DEBT-365 paragraph** goes at the end of the description cell, anchored on the cell, and ends with
  `@@NTUP-MEASURED@@`. It states the kind-12 rule and the named rows. It says a `let` pattern is refused (SEM3220; the new
  row) and that return-origin keeps the second fence. It says no residual names kind 12 as its fence, and that the R-d/R-f
  forms through a tuple element stop at the task check and never build, with their destructuring spellings now also at
  SEM3220. It closes with t15 now building and running.
- **A new Closed row**, `RV2-DEBT-<max+1>`: **382 at `045c1135`**. It becomes 386 after VMDH and LITX, or 387 if the F-4
  row lands first. It is inserted after the row with the highest id. It records:
  - the checker hole (a pattern passed a checker that the language does not allow);
  - F-5, F-2 and F-3 as found, and that they are now unreachable;
  - the owner ruling, the diagnostic, the golden decisions, the removed MIR test and the unreachable `lowerLetPattern`;
  - the rows.

  The generator fills the id into both texts.
- **The landing guard (stage 13)**:
  - no placeholder;
  - the paragraph once, naming as whole tokens: `census`, `golden`, `ST-RUNOUT`, `Valgrind`, `S-RUNNER`, `SEM3220`, the
    seven CF ids, `S-OWNERSHIP`, `ryzen`, and every READ/NOT-RUN/RECORDED/FAIL stop;
  - the new row once, Closed, placed right;
  - nothing else changed.

## 8. Measurement changes from R1 (`measure/run-ntup.sh`)

- **Stage 0**: S-BASE requires a base that descends from `045c1135`.
- **Stage 1**: at a moved base, S-R1 is READ when only base-dependent files differ. The new file, `let_forms.go`, the
  rows, the probes and the element/refusal CFs must equal the shipped bundle.
- **Stage 2**: Stage A runs the driver rows only. The sema rows name SEM3220 and do not compile on the base.
- **Stage 4**: applies the deletions (MANIFEST `deleted`, committed with `git add -A`), compiles `mir` and `diag` too, and
  vets `diag`, `mir` and `vm` too.
- **Stage 5**: rows B (driver + sema).
- **Stage 7**:
  - the census judge handles removed, added and changed programs, with `stdout_head` and `error` strict;
  - ST-K12 FAILs if any kind-12 or destructuring row remains;
  - S-GOLD handles the F-6 preflight;
  - S-GOLDFILES regenerates every golden NTUP adds or rewrites, and the three untouched kind-12 programs, and compares
    them byte for byte;
  - the wide diag runs the base binary on the BASE tree's copy of each file.
- **Stage 10**: CFs judged over both logs. Direct witnesses: FRESH w01 and LOAN w02, which must show VM3301 and a
  Valgrind error; LETREFUSE f01, which must show the second fence.
- **Predicted stops**:
  - S-GOLD NOT-RUN (F-6);
  - ST-FINDINGS RECORDED (F-4);
  - S-ROSTER-LINES READ (`TestVMTermSizeOverride`);
  - S-OWNERSHIP READ at most (moved goldens are scoped by `golden_rewritten`);
  - **S-VALGRIND PASS**.

## 9. Not verified

- Nothing ran on ryzen.
- The full rosters, tripwire, ownership, sanitizer gates, TestLLVMParity, the full MT lane and the 20× run-out were not
  run locally. Targeted runs are in §10.
- The generator was run only at `045c1135`. Its anchors were checked against LITX's after files (LANGUAGE edits,
  `codes.go` with 3219 taken, the `bindTuplePattern` block) and 27S is already in the base. It was not run on a commit
  that carries VMDH and LITX, because none exists yet.
- The ownership corpus lines for the moved goldens were predicted, not run.
- SEM3220 suppresses SEM3209 in the same file (task_created_outside_scope); that is why its spelling was rewritten. Which
  later checks an early SEM3220 suppresses in general was not surveyed: SEM3139 and SEM3021 still appear beside it in the
  probes.

## 10. Local preflight (`ulimit -v 16000000; GOMAXPROCS=4 timeout 900 taskset -c 0-3`, `-p 1`) [local]

- **Setup.** Worktree `~/.cache/surge-wt/ntup-pre` at `045c1135`, holding exactly the bundle (after, tests, the 11
  deletions). The base binary was built from `git archive 045c1135`.
- **Build and lint.**
  - `go build ./...`, `go vet` on sema, driver, diag, mir and vm, and the surge binary: clean.
  - golangci-lint on sema, driver, diag, mir and vm: 168 findings at base and after, **new 0**.
  - `check_file_sizes.sh`: exit 0.
- **Rows.**
  - Stage B (driver 31 + sema 16 + parents): 0 not as predicted.
  - Stage A (driver on the base): 0 not as predicted (30 red, `swap` green).
- **Packages:** `go test` for sema, diag, mir, symbols and parser all ok. `TestSyntheticSurgeStartInheritsRealEntrypointProvenance`
  (which uses tuple_literals) is ok.
- **Census:** base against after passes the judge. **Wide diag:** 245 files, passes the judge.
- **Goldens:** every changed golden's outputs and the three untouched kind-12 goldens regenerate byte-exact.
- **The kept runtime programs:**
  - t15 and tuple_literals: 3/3 per backend, byte-equal (t15 multiset natively).
  - `TestVMTuplesGolden` and `TestVMAsyncSuiteGolden/t15` on vm and llvm: PASS.
  - Valgrind clean for t15, tuple_literals and storage_construct.
- **Matrix:** 63 probes through the judge, all clear, with 6 F-4 notes.
- **CFs:** all 7 build and pass the leaf judge. The FRESH, LOAN and LETREFUSE binaries were rebuilt and their witnesses
  behave as in §5.
- **Script checks.**
  - All 9 helper self-tests pass.
  - `bash -n` is clean on both scripts.
  - Defined functions: 33 defined, 33 listed, and the script head was executed.
  - A launch without `NTUP_BASE` prints `NTUP-NO-BASE` as line 1.
- **Cleanup:** the worktree was removed and verified gone.
