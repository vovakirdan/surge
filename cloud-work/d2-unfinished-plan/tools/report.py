#!/usr/bin/env python3
"""Print the markdown tables of census.md and clusters.md from the JSON files.

Usage: report.py census   census.json
       report.py clusters census.json clusters.json
"""
from __future__ import annotations

import json
import sys

SLUGS: dict[str, str] = {
    "expression kind 27 needs an origin transfer": "on-expression-kind-27",
    "opaque result borrowed-state classification is unsupported": "opaque-result-classification",
    "index requires a non-scalar index transfer": "index-non-scalar",
    "projected borrowed payload needs precise origin facts": "projected-borrowed-payload",
    "index requires its selected container transfer": "index-selected-container",
    "generic index lacks its finalized concrete use": "generic-index-finalized-use",
    "selected callable lacks its published callable authority": "selected-callable-authority",
    "call needs an exact body, canonical core contract, or opaque declaration promise": "call-exact-body",
    "callable identifier lacks concrete source facts": "callable-identifier-facts",
    "callable value needs its concrete original type and alias authority": "callable-value-authority",
    "conversion retains its actual expression for origin finalization": "conversion-retains-expression",
    "binary callable needs an exact origin contract": "binary-callable-contract",
    "mutable argument may replace reference-bearing contents": "mutable-argument",
    "opaque call may change reference-bearing or callable contents": "opaque-call-effects",
    "captured binding requires origin finalization": "captured-binding",
    "deferred clone requires a live local storage referent": "deferred-clone-live-referent",
    "generic use lacks its exact original callable declarations": "generic-use-original-declarations",
    "parameter requires concrete type or callable provenance": "parameter-concrete-type",
    "range literal lacks its original builtin constructor certificate": "range-literal-certificate",
    "tag constructor lacks its exact owning source declaration": "tag-constructor-owning-declaration",
    "generic original call argument disagrees with its substituted source signature": "generic-argument-disagrees",
    "compare pattern needs a precise matching transfer": "compare-pattern",
    "generic opaque use requires its type-dependent effect transfer": "generic-opaque-use",
    "store through a place needs reference-content transfer": "store-through-place",
    "borrowed temporary has no proven storage owner": "borrowed-temporary",
    "expression kind 12 needs an origin transfer": "tuple-index-kind-12",
    "generic tag use disagrees with its original typed call": "generic-tag-use",
    "an `async` or `blocking` block that captures a value which can hold a reference, a storage loan or a task needs its capture origin": "task-block-capture",
    "destructuring needs projected origin facts": "destructuring",
    "expression kind 9 needs an origin transfer": "map-literal-kind-9",
    "generic use disagrees with its original typed operation": "generic-use-typed-operation",
    "implicit borrow lacks an admitted borrow for this expression": "implicit-borrow-temporary",
}


def short(path: str) -> str:
    return path.removeprefix("testdata/golden/")


def reason_cell(reason: str, prefix: str = "reasons/") -> str:
    slug = SLUGS.get(reason)
    text = reason.replace("|", "\\|")
    return f"[{text}]({prefix}{slug}.md)" if slug else text


def census(data: dict) -> None:
    t = data["totals"]
    print("| Form | Scope | ok | unfinished | diagnostics | other | total |")
    print("|---|---|---:|---:|---:|---:|---:|")
    for form in ("user_form", "harness_form"):
        for scope in ("all_files", "harness_scope"):
            row = t[form][scope]
            print(f"| {form.replace('_', ' ')} | {scope.replace('_', ' ')} | {row['ok']} | {row['unfinished']} | "
                  f"{row['diagnostics']} | {row['other']} | {sum(row.values())} |")
    print()
    print("| Form | Scope | unfinished now | freed if every kind-27 row goes | unfinished after |")
    print("|---|---|---:|---:|---:|")
    for form in ("user_form", "harness_form"):
        for scope in ("all_files", "harness_scope"):
            row = data["after_kind27"][form][scope]
            print(f"| {form.replace('_', ' ')} | {scope.replace('_', ' ')} | {row['unfinished_before']} | "
                  f"{row['freed_by_kind27_only']} | {row['unfinished_after']} |")
    print()
    print("### Root reasons\n")
    print("| Root reason | programs carrying it | programs where it is the only root | rows |")
    print("|---|---:|---:|---:|")
    for r in data["reason_stats"]:
        if not r["derived"]:
            print(f"| {reason_cell(r['reason'])} | {r['programs']} | {r['only_root_in']} | {r['rows']} |")
    print("\n### Derived reasons (propagate a source raised elsewhere; never counted as roots)\n")
    print("| Derived reason | programs carrying it | rows |")
    print("|---|---:|---:|")
    for r in data["reason_stats"]:
        if r["derived"]:
            print(f"| {r['reason']} | {r['programs']} | {r['rows']} |")
    print("\n### Unfinished programs (user form)\n")
    print("| Program | harness form | excluded from harness | rows | root reasons |")
    print("|---|---|---|---:|---|")
    for p in data["unfinished_programs"]:
        roots = "; ".join(SLUGS.get(r, r) for r in p["root_reasons"])
        print(f"| {short(p['path'])} | {p['harness_class']} | {p['harness_excluded'] or ''} | {p['row_count']} | {roots} |")


def clusters(data: dict, cl: dict) -> None:
    print(f"Unfinished (user form, all files): {cl['unfinished_before']}; freed by the kind-27 drop: "
          f"{len(cl['freed_by_drop'])}; left: {cl['unfinished_after']}.\n")
    print("### Clusters after the kind-27 drop\n")
    print("| # | size | root-reason set | programs |")
    print("|---:|---:|---|---|")
    for i, c in enumerate(cl["clusters_after"], 1):
        roots = "; ".join(SLUGS.get(r, r) for r in c["root_set"].split(" | "))
        progs = ", ".join(short(p) for p in c["programs"])
        print(f"| {i} | {c['size']} | {roots} | {progs} |")
    print("\n### Greedy set cover after the kind-27 drop\n")
    print("| step | root reason answered | carried by | frees now | freed so far | programs freed at this step |")
    print("|---:|---|---:|---:|---:|---|")
    for i, s in enumerate(cl["greedy_cover_after"], 1):
        progs = ", ".join(short(p) for p in s["freed_programs"])
        print(f"| {i} | {SLUGS.get(s['reason'], s['reason'])} | {s['carried_by']} | {s['frees_now']} | "
              f"{s['cumulative_freed']} | {progs} |")


def main() -> None:
    with open(sys.argv[2]) as fh:
        data = json.load(fh)
    if sys.argv[1] == "census":
        census(data)
    else:
        with open(sys.argv[3]) as fh:
            clusters(data, json.load(fh))


if __name__ == "__main__":
    main()
