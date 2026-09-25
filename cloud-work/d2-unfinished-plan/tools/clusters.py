#!/usr/bin/env python3
"""Cluster the unfinished programs of census.json by their sets of root reasons.

Usage: clusters.py census.json [--keep-kind27] [--harness-scope]

By default every row with the kind-27 reason is dropped first (the in-flight
N-TASK-27S / TC-XB packets are assumed to remove all of them). Prints JSON:
the clusters before and after the drop, and a greedy set cover over the rest,
i.e. which root reason, answered next, frees the most programs.
"""
from __future__ import annotations

import json
import sys
from collections import Counter, defaultdict

KIND27: str = "expression kind 27 needs an origin transfer"


def project(programs: list[dict], drop: set[str], harness_scope: bool) -> dict[str, frozenset[str]]:
    """Return {path: remaining root reasons} for every unfinished program (user form)."""
    out: dict[str, frozenset[str]] = {}
    for p in programs:
        if p["class"] != "unfinished" or (harness_scope and p["harness_excluded"]):
            continue
        out[p["path"]] = frozenset(r for r in p["root_reasons"] if r not in drop)
    return out


def clusters_of(sets: dict[str, frozenset[str]]) -> list[dict]:
    groups: dict[str, list[str]] = defaultdict(list)
    for path, roots in sets.items():
        groups[" | ".join(sorted(roots)) if roots else "(no root reason left)"].append(path)
    return [{"root_set": k, "size": len(v), "programs": sorted(v)}
            for k, v in sorted(groups.items(), key=lambda kv: (-len(kv[1]), kv[0]))]


def greedy_cover(sets: dict[str, frozenset[str]]) -> list[dict]:
    """Repeatedly answer the reason that frees the most programs (ties: most carriers, then name)."""
    remaining = {p: set(s) for p, s in sets.items() if s}
    steps: list[dict] = []
    total = 0
    while remaining:
        carry: Counter = Counter()
        frees: Counter = Counter()
        for roots in remaining.values():
            for r in roots:
                carry[r] += 1
                if len(roots) == 1:
                    frees[r] += 1
        best = max(carry, key=lambda r: (frees[r], carry[r], -len(r), r))
        freed = sorted(p for p, s in remaining.items() if s == {best})
        for p in list(remaining):
            remaining[p].discard(best)
            if not remaining[p]:
                del remaining[p]
        total += len(freed)
        steps.append({"reason": best, "frees_now": len(freed), "carried_by": carry[best],
                      "cumulative_freed": total, "freed_programs": freed})
    return steps


def main() -> None:
    with open(sys.argv[1]) as fh:
        programs = json.load(fh)["unfinished_programs"]
    drop = set() if "--keep-kind27" in sys.argv else {KIND27}
    scope = "--harness-scope" in sys.argv
    before = project(programs, set(), scope)
    after = project(programs, drop, scope)
    result = {
        "dropped": sorted(drop),
        "harness_scope_only": scope,
        "unfinished_before": len(before),
        "freed_by_drop": sorted(p for p, s in after.items() if not s),
        "unfinished_after": sum(1 for s in after.values() if s),
        "clusters_before": clusters_of(before),
        "clusters_after": clusters_of({p: s for p, s in after.items() if s}),
        "greedy_cover_after": greedy_cover(after),
        "reason_carry_after": sorted(Counter(r for s in after.values() for r in s).items(),
                                     key=lambda x: (-x[1], x[0])),
    }
    keys = list(result)
    out = ["{"]
    for i, key in enumerate(keys):
        value, tail = result[key], ("," if i + 1 < len(keys) else "")
        if isinstance(value, list) and value and isinstance(value[0], (dict, list)):
            items = ",\n".join("  " + json.dumps(item) for item in value)
            out.append(f" {json.dumps(key)}: [\n{items}\n ]{tail}")
        else:
            out.append(f" {json.dumps(key)}: {json.dumps(value)}{tail}")
    out.append("}")
    print("\n".join(out))


if __name__ == "__main__":
    main()
