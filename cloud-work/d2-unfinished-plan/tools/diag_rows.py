#!/usr/bin/env python3
"""Diagnose one file and print its class and its root-reason rows, one per line.

Usage: diag_rows.py <file.sg> [extra surge diag flags...]
Env:   SURGE (default /tmp/surge); SURGE_STDLIB defaults to the repository root.
Rows print as "<source key>:<line> <reason>", deduplicated, derived reasons left out.
"""
from __future__ import annotations

import os
import re
import subprocess
import sys

DERIVED: set[str] = {"function result contains an unproved source", "callee returned an unproved source",
                     "outgoing reference has unresolved or captured provenance"}
ROW_RE = re.compile(r"\{SourceKey:(\S+) Span:\d+:(\d+)-\d+ Reason:(.*?)\}(?= \{|\]$)")


def main() -> None:
    root = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True).stdout.strip()
    env = dict(os.environ)
    env.setdefault("SURGE_STDLIB", root)
    res = subprocess.run([os.environ.get("SURGE", "/tmp/surge"), "diag", *sys.argv[2:], sys.argv[1]],
                         cwd=root, env=env, capture_output=True, text=True, timeout=300)
    out = res.stdout + res.stderr
    m = re.search(r"return-origin analysis unfinished: (\[.*\])\s*$", out, re.M)
    if not m:
        codes = sorted(set(re.findall(r"(?:ERROR |^error )([A-Z]+\d+)", out, re.M)))
        first = out.strip().split("\n")[0] if out.strip() else ""
        kind = "ok" if res.returncode == 0 else ("diagnostics" if codes else "other")
        print(f"class={kind} exit={res.returncode} codes={','.join(codes)}")
        if kind == "other":
            print(first[:200])
        return
    rows = ROW_RE.findall(m.group(1))
    seen: list[str] = []
    for key, start, reason in rows:
        if reason in DERIVED:
            continue
        try:
            data = open(os.path.join(root, key), "rb").read()
            line = data.count(b"\n", 0, int(start)) + 1
        except OSError:
            line = 0
        text = f"{key}:{line} {reason}"
        if text not in seen:
            seen.append(text)
    roots = sorted({r for _, _, r in rows if r not in DERIVED})
    print(f"class=unfinished rows={len(rows)} root_reasons={len(roots)}")
    for text in seen[:12]:
        print("  " + text)
    if len(seen) > 12:
        print(f"  ... {len(seen) - 12} more root rows")


if __name__ == "__main__":
    main()
