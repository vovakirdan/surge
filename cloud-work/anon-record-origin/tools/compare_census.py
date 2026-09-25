#!/usr/bin/env python3
"""Compare two census runs (census.sh outputs): verdict per program and raw output bytes.

Usage: compare_census.py BASE_OUTDIR AFTER_OUTDIR
"""
from __future__ import annotations

import json
import os
import sys


def classes(census: dict, form: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for cls, paths in census[f"class_lists_{form}"].items():
        for path in paths:
            out[path] = cls
    return out


def rows(census: dict) -> dict[str, list]:
    return {p["path"]: sorted((r["source_key"], r["span"], r["reason"]) for r in p.get("rows", []))
            for p in census["unfinished_programs"]}


def main() -> int:
    base_dir, after_dir = sys.argv[1], sys.argv[2]
    base = json.load(open(os.path.join(base_dir, "census.json")))
    after = json.load(open(os.path.join(after_dir, "census.json")))
    print("commits:", base["meta"]["commit"][:10], "->", after["meta"]["commit"][:10])
    for form in ("user_form", "harness_form"):
        b, a = classes(base, form), classes(after, form)
        assert set(b) == set(a), "program sets differ"
        moved = sorted((p, b[p], a[p]) for p in b if b[p] != a[p])
        print(f"{form}: {len(b)} programs, totals base={base['totals'][form]['all_files']} after={after['totals'][form]['all_files']}")
        print(f"{form}: verdict changes: {len(moved)}")
        for p, x, y in moved:
            print(f"  {p}: {x} -> {y}")
    rb, ra = rows(base), rows(after)
    changed_rows = sorted(p for p in set(rb) & set(ra) if rb[p] != ra[p])
    print("unfinished programs whose rows changed:", len(changed_rows))
    for p in changed_rows:
        print("  ", p)
    for sub, label in (("out", "user form"), ("hout", "harness form")):
        names = sorted(os.listdir(os.path.join(base_dir, sub)))
        differ = [n for n in names if open(os.path.join(base_dir, sub, n), "rb").read() != open(os.path.join(after_dir, sub, n), "rb").read()]
        print(f"{label}: raw outputs that differ byte-for-byte: {len(differ)} of {len(names)}")
        for n in differ:
            print("  ", n)
    return 0


if __name__ == "__main__":
    sys.exit(main())
