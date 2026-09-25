# How to reproduce

Run everything from the repository root at `7131fb2`, with Go 1.26 and Python 3.9 or later. The prototype packets are not committed, so this file reproduces the census and the base-compiler facts only. The prototype's measurements are recorded in `prototype_measurements.json`.

## 1. Rebuild census.json from scratch

```
git checkout 7131fb2e3d6e5d4be396e5c86da6fc40f4847e92
OUT=$(mktemp -d)
JOBS=8 cloud-work/d2-unfinished-plan/tools/run_census.sh "$OUT"
```

`run_census.sh` does the following:

1. Builds `/tmp/surge` with `go build -o /tmp/surge ./cmd/surge`.
2. Lists every `.sg` file under `testdata/golden` (1079 files).
3. Runs both command forms on each file with a 120 s timeout (`tools/run_one.sh`).
4. Classifies the results (`tools/classify.py`) and clusters them (`tools/clusters.py`).

It prints the totals. The expected totals are:

```
user form,    all files:     ok 520, unfinished 108, diagnostics 448, other 3
user form,    harness scope: ok 476, unfinished  99, diagnostics 422, other 3
harness form, all files:     ok 518, unfinished 107, diagnostics 451, other 3
harness form, harness scope: ok 474, unfinished  98, diagnostics 425, other 3
after every kind-27 row goes: 84 (user form) and 83 (harness form)
```

To compare a fresh run with the committed file, program by program:

```
python3 - "$OUT/census.json" <<'PY'
import json, sys
def load(p):
    d = json.load(open(p))
    cls = {x: c for c, xs in d["class_lists_user_form"].items() for x in xs}
    roots = {p["path"]: p["root_reasons"] for p in d["unfinished_programs"]}
    return cls, roots
a_cls, a_roots = load("cloud-work/d2-unfinished-plan/census.json")
b_cls, b_roots = load(sys.argv[1])
diff = [p for p in a_cls if a_cls[p] != b_cls.get(p) or a_roots.get(p) != b_roots.get(p)]
print("programs that differ:", len(diff), diff[:10])
PY
```

Expected output: `programs that differ: 0 []`. Two independent runs at `7131fb2` gave that.

To regenerate the tables in `census.md` and `clusters.md`:

```
python3 cloud-work/d2-unfinished-plan/tools/report.py census "$OUT/census.json"
python3 cloud-work/d2-unfinished-plan/tools/report.py clusters "$OUT/census.json" "$OUT/clusters.json"
```

## 2. Spot checks

Each check uses `tools/diag_rows.py`. It runs `/tmp/surge diag` with `SURGE_STDLIB` set to the repository root, and prints the class and the root-reason rows as `<file>:<line> <reason>`. Derived reasons are left out.

```
H=cloud-work/d2-unfinished-plan/tools/diag_rows.py
```

1. **A literal pattern.**
   ```
   $ python3 $H testdata/golden/hir/compare.sg
   class=unfinished rows=1 root_reasons=1
     testdata/golden/hir/compare.sg:3 compare pattern needs a precise matching transfer
   ```
2. **An import with no identity.**
   ```
   $ python3 $H testdata/golden/sema/valid/import_all.sg
   class=unfinished rows=1 root_reasons=1
     testdata/golden/sema/valid/import_all.sg:8 selected callable lacks its published callable authority
   ```
3. **Kind 27, owned by the in-flight packets.**
   ```
   $ python3 $H testdata/golden/crossing/block03/valid/spawn_on_positive_pool.sg
   class=unfinished rows=1 root_reasons=1
     testdata/golden/crossing/block03/valid/spawn_on_positive_pool.sg:7 expression kind 27 needs an origin transfer
   ```
4. **A Task made through a function value (owner question 2).**
   ```
   $ python3 $H testdata/golden/sema/valid/fn_type_async.sg
   class=unfinished rows=4 root_reasons=1
     testdata/golden/sema/valid/fn_type_async.sg:11 opaque result borrowed-state classification is unsupported
   ```
5. **A guarded arm with no arm left (owner question 4).**
   ```
   $ python3 $H testdata/golden/mir/compare_guard_await_release.sg
   class=unfinished rows=3 root_reasons=1
     testdata/golden/mir/compare_guard_await_release.sg:12 compare has an unproved unmatched continuation
   ```
6. **An anonymous record aborts the analysis.**
   ```
   $ python3 $H testdata/golden/sema/valid/user_record_type.sg
   class=other exit=1 codes=
   Error: diagnosis failed: return origins: expression 31 is not typed in testdata/golden/sema/valid/user_record_type.sg
   ```
7. **Lent temporaries and core concatenation (owner question 3(a)).**
   ```
   $ python3 $H testdata/golden/vm_strings/strings_std.sg
   class=unfinished rows=5 root_reasons=2
     core/string.sg:311 generic use disagrees with its original typed operation
     core/string.sg:319 generic use disagrees with its original typed operation
     core/string.sg:331 generic use disagrees with its original typed operation
     testdata/golden/vm_strings/strings_std.sg:46 implicit borrow lacks an admitted borrow for this expression
     testdata/golden/vm_strings/strings_std.sg:51 implicit borrow lacks an admitted borrow for this expression
   ```
8. **A core copy, and the real core as root (owner question 1).**
   ```
   $ python3 $H testdata/golden/core_stdlib/base.sg | head -2
   class=unfinished rows=305 root_reasons=14
     testdata/golden/core_stdlib/array.sg:6 mutable argument may replace reference-bearing contents
   $ python3 $H $PWD/core/base.sg | head -2
   class=unfinished rows=38 root_reasons=2
     core/array.sg:163 captured binding requires origin finalization
   ```
9. **The only program where the two forms disagree on being unfinished.**
   ```
   $ python3 $H testdata/golden/sema/invalid/directives/time_not_directive_module/main.sg | head -2
   class=unfinished rows=6 root_reasons=1
     stdlib/time/time.sg:16 opaque result borrowed-state classification is unsupported
   $ python3 $H testdata/golden/sema/invalid/directives/time_not_directive_module/main.sg --format short --directives=collect
   class=diagnostics exit=1 codes=SEM3120
   ```
10. **The owner's figures in the committed census.**
    ```
    $ python3 -c "import json; d=json.load(open('cloud-work/d2-unfinished-plan/census.json')); print(json.dumps(d['totals']['harness_form']['all_files']), json.dumps(d['after_kind27']['harness_form']['all_files']))"
    {"ok": 518, "unfinished": 107, "diagnostics": 451, "other": 3} {"unfinished_before": 107, "freed_by_kind27_only": 24, "unfinished_after": 83}
    ```

## 3. After the in-flight packets land

Re-run section 1 on the new head. Every program whose only root reason was kind 27 should be `ok`, which is 24 programs. The other 84 should keep exactly the root reasons listed in `census.json`. Any program that moves in some other way means one of those packets touched another reason, and `clusters.md` and `PLAN.md` need re-measuring.
