#!/usr/bin/env python3
"""Compare failing-test name sets between two `go test -json` runs, per leg.

Usage: failing_sets.py <results-dir> <base-label> <after-label>
Legs: {vm,llvm} x {plain,pending}; files are <label>-<backend>-<tags>.json.
"""
import json
import sys
from collections import Counter
from typing import Dict, List, Set, Tuple


def load(path: str) -> Tuple[Dict[str, str], List[str], bool]:
    """Return (final action per test, tests started but never finished, package failed)."""
    final: Dict[str, str] = {}
    started: Set[str] = set()
    package_failed = False
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            test = event.get("Test")
            action = event.get("Action")
            if test is None:
                if action == "fail":
                    package_failed = True
                continue
            if action == "run":
                started.add(test)
            elif action in ("pass", "fail", "skip"):
                final[test] = action
    unfinished = sorted(started - set(final))
    return final, unfinished, package_failed


def main() -> None:
    results, base_label, after_label = sys.argv[1], sys.argv[2], sys.argv[3]
    for backend in ("vm", "llvm"):
        for tags in ("plain", "pending"):
            leg = f"{backend}-{tags}"
            base, base_unfinished, _ = load(f"{results}/{base_label}-{leg}.json")
            after, after_unfinished, _ = load(f"{results}/{after_label}-{leg}.json")
            base_fail = {t for t, a in base.items() if a == "fail"}
            after_fail = {t for t, a in after.items() if a == "fail"}
            print(f"== leg {leg} (SURGE_BACKEND={backend}, {'-tags runtime_v2_pending' if tags == 'pending' else 'no tag'})")
            print(f"   base : {dict(Counter(base.values()))} unfinished={len(base_unfinished)}")
            print(f"   after: {dict(Counter(after.values()))} unfinished={len(after_unfinished)}")
            print(f"   failing: base={len(base_fail)} after={len(after_fail)}")
            print("   failing at base, not after:")
            for t in sorted(base_fail - after_fail):
                print(f"     - {t}  (after: {after.get(t, 'absent')})")
            print("   failing after, not at base:")
            for t in sorted(after_fail - base_fail):
                print(f"     + {t}  (base: {base.get(t, 'absent')})")
            new_tests = sorted(set(after) - set(base))
            print(f"   tests present only after ({len(new_tests)}):")
            for t in new_tests:
                print(f"     * {t}: {after[t]}")
            if base_unfinished or after_unfinished:
                print(f"   unfinished base={base_unfinished} after={after_unfinished}")


if __name__ == "__main__":
    main()
