#!/usr/bin/env python3
"""Classify the outputs of run_one.sh (both command forms) into census.json.

Usage: classify.py ROOT OUTDIR DEST [COMMIT]

Classes (per form):
  ok           exit status 0
  unfinished   "return-origin analysis unfinished: [...]" printed
  diagnostics  exit status 1 with at least one ERROR diagnostic code
  other        anything else (internal errors, timeouts, crashes)
Rows are split into root reasons and the three derived reasons, which only
propagate an unproved source raised elsewhere.
"""
from __future__ import annotations

import json
import os
import re
import sys
from collections import Counter, defaultdict
from typing import Any

DERIVED: set[str] = {
    "function result contains an unproved source",
    "callee returned an unproved source",
    "outgoing reference has unresolved or captured provenance",
}
KIND27: str = "expression kind 27 needs an origin transfer"  # ast.ExprOn, owned by the in-flight N-TASK-27S / TC-XB packets
ROW_RE = re.compile(r"\{SourceKey:(?P<key>\S+) Span:(?P<file>\d+):(?P<start>\d+)-(?P<end>\d+) Reason:(?P<reason>.*?)\}(?= \{|\]$)")
UNFINISHED_RE = re.compile(r"return-origin analysis unfinished: (\[.*\])\s*$", re.M)
PRETTY_DIAG_RE = re.compile(r"^[^:\n]+:\d+:\d+: (?P<sev>ERROR|WARNING) (?P<code>[A-Z]+\d+):", re.M)
SHORT_DIAG_RE = re.compile(r"^(?P<sev>error|warning) (?P<code>[A-Z]+\d+) ", re.M)


def key_of(rel: str) -> str:
    return rel.replace("/", "_")


def harness_excluded(rel: str) -> str | None:
    """Mirror the selection of scripts/golden_update.sh:258-268."""
    if rel.startswith("testdata/golden/spec_audit/"):
        return "spec_audit"
    if rel.startswith("testdata/golden/crossing/crosses_deferred/"):
        return "crosses_deferred"
    if os.path.basename(rel).startswith("_"):
        return "underscore helper"
    return None


class Source:
    """Byte offsets to line/column and snippets, cached per file."""

    def __init__(self, root: str) -> None:
        self.root = root
        self.cache: dict[str, bytes] = {}

    def data(self, path: str) -> bytes:
        if path not in self.cache:
            try:
                with open(os.path.join(self.root, path), "rb") as fh:
                    self.cache[path] = fh.read()
            except OSError:
                self.cache[path] = b""
        return self.cache[path]

    def line_col(self, path: str, offset: int) -> tuple[int | None, int | None]:
        data = self.data(path)
        if not data or offset > len(data):
            return None, None
        line = data.count(b"\n", 0, offset) + 1
        return line, offset - (data.rfind(b"\n", 0, offset) + 1) + 1

    def snippet(self, path: str, start: int, end: int) -> str:
        text = self.data(path)[start:end].decode("utf-8", "replace")
        return " ".join(text.split())[:120]


def read_output(path: str) -> tuple[str, int]:
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        text = fh.read()
    lines = text.rstrip("\n").split("\n")
    m = re.match(r"exit_status=(\d+)", lines[-1] if lines else "")
    if not m:
        return text, -1
    return "\n".join(lines[:-1]), int(m.group(1))


def classify_text(body: str, status: int, src: Source, rel: str, with_rows: bool) -> dict[str, Any]:
    um = UNFINISHED_RE.search(body)
    if um:
        rows: list[dict[str, Any]] = []
        for rm in ROW_RE.finditer(um.group(1)):
            key, start, end = rm.group("key"), int(rm.group("start")), int(rm.group("end"))
            line, col = src.line_col(key, start)
            rows.append({"source_key": key, "span": f"{rm.group('file')}:{start}-{end}", "line": line,
                         "col": col, "reason": rm.group("reason"), "text": src.snippet(key, start, end)})
        reasons = [r["reason"] for r in rows]
        roots = sorted({r for r in reasons if r not in DERIVED})
        out: dict[str, Any] = {"class": "unfinished", "exit_status": status, "row_count": len(rows),
                               "root_reasons": roots,
                               "derived_reasons": sorted({r for r in reasons if r in DERIVED}),
                               "only_root_reason": roots[0] if len(roots) == 1 else None,
                               "foreign_row_sources": sorted({r["source_key"] for r in rows if r["source_key"] != rel})}
        if with_rows:
            out["rows"] = rows
        return out
    found = PRETTY_DIAG_RE.findall(body) + SHORT_DIAG_RE.findall(body)
    errors = sorted({c for s, c in found if s.lower() == "error"})
    warnings = sorted({c for s, c in found if s.lower() == "warning"})
    if status == 0:
        return {"class": "ok", "exit_status": 0, "warning_codes": warnings}
    if status == 1 and errors:
        return {"class": "diagnostics", "exit_status": 1, "error_codes": errors}
    first = body.strip().split("\n")[0][:300] if body.strip() else ""
    return {"class": "other", "exit_status": status, "first_line": first, "error_codes": errors}


def reason_table(programs: list[dict[str, Any]], scope_ok) -> list[dict[str, Any]]:
    carriers: dict[str, set[str]] = defaultdict(set)
    only: dict[str, set[str]] = defaultdict(set)
    rows: Counter = Counter()
    for p in programs:
        if p["class"] != "unfinished" or not scope_ok(p):
            continue
        for r in p["rows"]:
            rows[r["reason"]] += 1
            carriers[r["reason"]].add(p["path"])
        if p["only_root_reason"]:
            only[p["only_root_reason"]].add(p["path"])
    table = []
    for reason in sorted(carriers, key=lambda r: (-len(carriers[r]), r)):
        table.append({"reason": reason, "derived": reason in DERIVED, "programs": len(carriers[reason]),
                      "only_root_in": len(only.get(reason, ())), "rows": rows[reason],
                      "program_list": sorted(carriers[reason]), "only_root_list": sorted(only.get(reason, ()))})
    return table


def totals(programs: list[dict[str, Any]], field: str, scope_ok) -> dict[str, int]:
    count = Counter(p[field] for p in programs if scope_ok(p))
    return {k: count.get(k, 0) for k in ("ok", "unfinished", "diagnostics", "other")}


def after_kind27(programs: list[dict[str, Any]], roots_field: str, class_field: str, scope_ok) -> dict[str, int]:
    """Unfinished programs left if every kind-27 row disappears and nothing else changes."""
    unfinished = [p for p in programs if p[class_field] == "unfinished" and scope_ok(p)]
    freed = [p for p in unfinished if set(p[roots_field]) == {KIND27}]
    return {"unfinished_before": len(unfinished), "freed_by_kind27_only": len(freed),
            "unfinished_after": len(unfinished) - len(freed)}


def main() -> None:
    root, outdir, dest = sys.argv[1], sys.argv[2], sys.argv[3]
    commit = sys.argv[4] if len(sys.argv) > 4 else ""
    src = Source(root)
    with open(os.path.join(outdir, "files.txt")) as fh:
        files = [line.strip() for line in fh if line.strip()]
    programs: list[dict[str, Any]] = []
    for rel in files:
        body, status = read_output(os.path.join(outdir, "out", key_of(rel) + ".txt"))
        user = classify_text(body, status, src, rel, with_rows=True)
        hbody, hstatus = read_output(os.path.join(outdir, "hout", key_of(rel) + ".txt"))
        harness = classify_text(hbody, hstatus, src, rel, with_rows=False)
        entry: dict[str, Any] = {"path": rel, "harness_excluded": harness_excluded(rel),
                                 "expected_invalid": "/invalid/" in rel}
        entry.update(user)
        entry["harness_form"] = harness
        entry["harness_class"] = harness["class"]
        entry["harness_root_reasons"] = harness.get("root_reasons", [])
        programs.append(entry)

    everywhere = lambda p: True  # noqa: E731
    in_scope = lambda p: p["harness_excluded"] is None  # noqa: E731
    classes = ("ok", "unfinished", "diagnostics", "other")
    result = {
        "meta": {
            "commit": commit,
            "user_form": "SURGE_STDLIB=$ROOT surge diag <file>",
            "harness_form": "SURGE_STDLIB=$ROOT surge diag --format short --directives=<off|collect> <file>",
            "per_file_timeout_seconds": 120,
            "programs_run": len(programs),
            "harness_scope": "files scripts/golden_update.sh diagnoses: not spec_audit/, not crossing/crosses_deferred/, no leading underscore",
            "derived_reasons": sorted(DERIVED),
            "kind27_reason": KIND27,
            "rows_and_reason_stats_come_from": "user form",
        },
        "totals": {
            "user_form": {"all_files": totals(programs, "class", everywhere),
                          "harness_scope": totals(programs, "class", in_scope)},
            "harness_form": {"all_files": totals(programs, "harness_class", everywhere),
                             "harness_scope": totals(programs, "harness_class", in_scope)},
        },
        "after_kind27": {
            "user_form": {"all_files": after_kind27(programs, "root_reasons", "class", everywhere),
                          "harness_scope": after_kind27(programs, "root_reasons", "class", in_scope)},
            "harness_form": {"all_files": after_kind27(programs, "harness_root_reasons", "harness_class", everywhere),
                             "harness_scope": after_kind27(programs, "harness_root_reasons", "harness_class", in_scope)},
        },
        "form_disagreements": [{"path": p["path"], "user_class": p["class"], "harness_class": p["harness_class"]}
                               for p in programs if p["class"] != p["harness_class"]],
        "harness_excluded": {p["path"]: p["harness_excluded"] for p in programs if p["harness_excluded"]},
        "class_lists_user_form": {c: [p["path"] for p in programs if p["class"] == c] for c in classes},
        "class_lists_harness_form": {c: [p["path"] for p in programs if p["harness_class"] == c] for c in classes},
        "error_codes_user_form": {p["path"]: p.get("error_codes", []) for p in programs
                                  if p["class"] in ("diagnostics", "other")},
        "other_first_lines": {p["path"]: p.get("first_line", "") for p in programs if p["class"] == "other"},
        "reason_stats": reason_table(programs, everywhere),
        "unfinished_programs": [{k: v for k, v in p.items() if k not in ("harness_form", "warning_codes")}
                                for p in programs if p["class"] == "unfinished"],
    }
    with open(dest, "w") as fh:
        dump(result, fh)
    print(json.dumps({"totals": result["totals"], "after_kind27": result["after_kind27"]}, indent=1))


def dump(result: dict[str, Any], fh) -> None:
    """Valid JSON with one line per top-level scalar group and per list element."""
    keys = list(result)
    fh.write("{\n")
    for i, key in enumerate(keys):
        value = result[key]
        tail = "," if i + 1 < len(keys) else ""
        if isinstance(value, list):
            fh.write(f" {json.dumps(key)}: [\n")
            for j, item in enumerate(value):
                fh.write("  " + json.dumps(item, ensure_ascii=False) + ("," if j + 1 < len(value) else "") + "\n")
            fh.write(f" ]{tail}\n")
        elif isinstance(value, dict) and key in ("totals", "after_kind27", "class_lists_user_form", "class_lists_harness_form"):
            fh.write(f" {json.dumps(key)}: {{\n")
            sub = list(value)
            for j, name in enumerate(sub):
                fh.write(f"  {json.dumps(name)}: " + json.dumps(value[name], ensure_ascii=False) + ("," if j + 1 < len(sub) else "") + "\n")
            fh.write(f" }}{tail}\n")
        else:
            fh.write(f" {json.dumps(key)}: " + json.dumps(value, ensure_ascii=False) + tail + "\n")
    fh.write("}\n")


if __name__ == "__main__":
    main()
