#!/usr/bin/env python3
"""Counterfactuals for N-ANON-RECORD: apply one change, run the rows, restore.

Run from the repository root at the packet's code commit.
"""
from __future__ import annotations

import os
import re
import subprocess
import sys

ROWS = "^(TestDiagnoseAnonymousRecordReturnOrigins|TestAnalyzeAnonymousRecordLostRecordStaysFatal)$"
TAMPER_DRIVER = "^TestAnalyzeFarSelectors$"
TAMPER_SEMA = "^(TestReturnOriginDeferredMethodTargetEvidence|TestReturnOriginEnumVariantRecord|TestReturnOriginTypeTestOperands)$"
EXPR = "internal/sema/return_origin_expr.go"
CTOR = "internal/sema/return_origin_constructors.go"
UNCHECKED = "internal/sema/return_origin_unchecked.go"

EDITS: dict[str, list[tuple[str, str, str]]] = {
    "cf2_literal_borrow_free": [(CTOR,
        "if shape == returnOriginShapeUnknown && b.anonymousRecordLiteral(id) {\n\t\tshape = returnOriginCarriesRef",
        "if shape == returnOriginShapeUnknown && b.anonymousRecordLiteral(id) {\n\t\tshape = returnOriginRefFree")],
    "cf3_field_any_target": [(UNCHECKED, "if returnOriginProvenBorrowFree(out.value) {", "if true {")],
    "cf4_operator_any_operands": [(UNCHECKED,
        "if returnOriginProvenBorrowFree(left.value) && returnOriginProvenBorrowFree(right.value) {", "if true {")],
    "cf5_binding_value_dropped": [(UNCHECKED, "\t\t\tvalue := env.value(symID)\n", "\t\t\tvalue := returnOriginValueOf()\n")],
    "cf6_gate_removed": [(EXPR,
        "if node == nil || u.Sema.ExprTypes[id] == types.NoTypeID && !b.uncheckedByLiteral(id) {",
        "if node == nil {")],
}


def go_test(pkg: str, pattern: str) -> str:
    return subprocess.run(["go", "test", pkg, "-run", pattern, "-count=1", "-v"], capture_output=True, text=True).stdout


def summarize(name: str, out: str) -> None:
    print("==", name)
    for line in out.splitlines():
        if re.match(r"\s*--- FAIL", line) or re.match(r"^(ok|FAIL)\s", line):
            print(line)
        elif "return_origin_anonymous_record_test.go:" in line and ("want" in line or "diagnosis failed" in line):
            print("   ", re.sub(r"tmp/\S+/origin\.sg", "<tmp>/origin.sg", line.strip())[:220])


def run(name: str, tamper: bool) -> None:
    out = go_test("./internal/driver/", ROWS)
    if tamper:
        out += go_test("./internal/driver/", TAMPER_DRIVER) + go_test("./internal/sema/", TAMPER_SEMA)
    open(os.path.join(sys.argv[1], name + ".log"), "w").write(out)
    summarize(name, out)


def main() -> int:
    saved = {path: open(path).read() for path in (EXPR, CTOR, UNCHECKED)}
    try:
        # CF1: the whole fix reverted to the base.
        for path in (EXPR, CTOR):
            subprocess.run(["git", "checkout", "1b122bfa", "--", path], check=True)
        os.remove(UNCHECKED)
        run("cf1_fix_reverted", False)
        for path, text in saved.items():
            open(path, "w").write(text)
        subprocess.run(["git", "reset", "-q", "--", EXPR, CTOR], check=True)
        for name, edits in EDITS.items():
            for path, old, new in edits:
                text = saved[path]
                assert text.count(old) == 1, (name, path, text.count(old))
                open(path, "w").write(text.replace(old, new))
            try:
                run(name, name == "cf6_gate_removed")
            finally:
                for path, text in saved.items():
                    open(path, "w").write(text)
        run("control_fix_in_place", True)
    finally:
        for path, text in saved.items():
            open(path, "w").write(text)
        subprocess.run(["git", "reset", "-q", "--", EXPR, CTOR], check=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
