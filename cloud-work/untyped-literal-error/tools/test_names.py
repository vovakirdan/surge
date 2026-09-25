#!/usr/bin/env python3
"""Failing and passing test names from `go test -json` output.

Usage: test_names.py BASE.json AFTER.json
Prints the failing top-level tests and leaves of each run and their difference.
"""
from __future__ import annotations

import json
import sys


def outcomes(path: str) -> tuple[set[str], set[str], dict[str, str]]:
    fail: set[str] = set()
    ok: set[str] = set()
    packages: dict[str, str] = {}
    for line in open(path):
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        action, test, pkg = event.get("Action"), event.get("Test"), event.get("Package")
        if test is None and action in ("pass", "fail"):
            packages[pkg] = action
        elif test and action == "fail":
            fail.add(f"{pkg} {test}")
        elif test and action == "pass":
            ok.add(f"{pkg} {test}")
    return fail, ok, packages


def main() -> int:
    base_fail, base_ok, base_pkgs = outcomes(sys.argv[1])
    after_fail, after_ok, after_pkgs = outcomes(sys.argv[2])
    top = lambda names: {n for n in names if "/" not in n.split(" ", 1)[1]}
    print("packages base:", base_pkgs)
    print("packages after:", after_pkgs)
    print(f"base: {len(top(base_fail))} failing top-level tests, {len(base_fail)} failing entries, {len(base_ok)} passing entries")
    print(f"after: {len(top(after_fail))} failing top-level tests, {len(after_fail)} failing entries, {len(after_ok)} passing entries")
    print("failing in base only:", sorted(base_fail - after_fail))
    print("failing after only:", sorted(after_fail - base_fail))
    print("passing after only (new or fixed):", sorted(after_ok - base_ok))
    print("passing in base only (lost):", sorted(base_ok - after_ok))
    print("failing top-level tests (both runs):")
    for name in sorted(top(base_fail) & top(after_fail)):
        print("  ", name)
    return 0


if __name__ == "__main__":
    sys.exit(main())
