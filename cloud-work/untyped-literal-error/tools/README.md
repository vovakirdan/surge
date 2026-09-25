# Tools for the SEM3219 packet

Run each tool from the repository root.

- `census.sh`: a golden census with a prebuilt compiler. It wraps `run_one.sh` and `classify.py` from branch `cloud/d2-unfinished-plan`, and needs `ROOT`, `SURGE`, `OUTDIR` and `D2TOOLS`. It is copied from `cloud/anon-record-origin`.
- `compare_census.py BASE_OUTDIR AFTER_OUTDIR`: compares two censuses. It lists the programs on one side only, then compares the rest by verdict, unfinished rows and raw output bytes.
- `extra_census.sh`: diagnoses `showcases/`, `benchmarks/`, `stdlib/` and `core/` with `BASE` and `AFTER`.
- `test_names.py BASE.json AFTER.json`: failing and passing test names from two `go test -json` runs.
- `run_probes.sh`: diagnoses `../probes/*.sg` and `../probes/cascade/*.sg` with `BASE` and `AFTER`.
- `run_counterfactuals.py LOGDIR [CF ...]`: CF1 to CF8 of PACKET.md section 5. It changes one thing at a time, restores the files, and writes one log per counterfactual.
