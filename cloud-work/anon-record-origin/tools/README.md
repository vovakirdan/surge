# Tools for N-ANON-RECORD

Run each tool from the repository root.

- `census.sh`: a golden census with a prebuilt compiler. It wraps `run_one.sh` and `classify.py` from branch `cloud/d2-unfinished-plan`, and needs `ROOT`, `SURGE`, `OUTDIR` and `D2TOOLS`.
- `compare_census.py BASE_OUTDIR AFTER_OUTDIR`: compares two censuses by verdict, by unfinished rows and by raw output bytes.
- `test_names.py BASE.json AFTER.json`: failing and passing test names from two `go test -json` runs.
- `run_probes.sh`: diagnoses `../probes/*.sg` with `BASE` and `AFTER`, and runs the `@entrypoint` probes with `AFTER` on both backends.
- `run_counterfactuals.py OUTDIR`: CF1 to CF6 of PACKET.md section 3. It changes one thing at a time, restores the files afterwards, and writes one log per counterfactual.
- `type_dump_test.go.txt`: the checked-type dump used in PACKET.md section 1.2. Copy it into `internal/driver` to run it; it is not part of the suite.
