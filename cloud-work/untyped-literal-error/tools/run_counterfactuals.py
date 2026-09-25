#!/usr/bin/env python3
"""Counterfactuals for SEM3219: change one thing, run the rows, restore."""
from __future__ import annotations

import os
import re
import subprocess
import sys

ROWS = "^(TestUntypedLiteralNeedsExpectedType|TestUntypedLiteralArgumentMessage)$"
SEMA = "internal/sema/"
CHECKER = [SEMA + f for f in ("check.go", "type_checker_core.go", "type_checker_bindings.go", "type_checker_returns.go",
                             "type_checker_walk.go", "type_expr.go", "type_expr_calls.go",
                             "type_expr_calls_method_resolution.go")]
NEW = SEMA + "untyped_literal.go"

EDITS = {
    "cf2_sweep_off": [(SEMA + "type_checker_core.go", "\ttc.reportUntypedLiterals()\n", "")],
    "cf3_argument_interception_off": [(SEMA + "type_expr_calls.go",
        "\tif tc.reportUntypedCallArguments(candidates, args) {", "\tif false {"),
        (SEMA + "type_expr_calls_method_resolution.go", "\tif tc.reportUntypedArguments(argExprs, args, nil) {", "\tif false {")],
    "cf4_receiver_interception_off": [(SEMA + "type_expr_calls_method_resolution.go",
        "\t\tif !tc.reportUntypedReceiver(recv, recvExpr) {", "\t\tif true {")],
    "cf5_covered_rule_off": [(NEW, "\t\tif reported.File == span.File && reported.Start < span.End && span.Start < reported.End {",
        "\t\tif reported.File == span.File && false {")],
    "cf6_return_in_block_off": [(SEMA + "type_checker_returns.go",
        "\t\tif expr.IsValid() && actual == types.NoTypeID && tc.applyExpectedType(expr, tc.enclosingFunctionReturnType()) {",
        "\t\tif false {")],
    "cf7_tuple_expected_off": [(SEMA + "type_checker_bindings.go",
        "\tcase ast.ExprTuple:\n\t\treturn tc.applyExpectedTupleType(expr, expected)\n", "")],
    "cf8_annotation_cover_off": [(SEMA + "type_checker_walk.go",
        "\t\t\t\t\ttc.coverUntypedLiteral(letStmt.Value) // the annotation's own error explains it\n",
        "\t\t\t\t\t_ = letStmt.Value\n")],
}


def run(name: str, outdir: str) -> None:
    out = subprocess.run(["go", "test", "./internal/sema/", "-run", ROWS, "-count=1", "-v"], capture_output=True, text=True)
    text = out.stdout + out.stderr
    open(os.path.join(outdir, name + ".log"), "w").write(text)
    print("==", name)
    for line in text.splitlines():
        if re.match(r"\s*--- FAIL", line) or re.match(r"^(ok|FAIL)\s", line) or "build failed" in line:
            print(line)
        elif "untyped_literal_test.go:" in line:
            print("   ", line.strip()[:230])


def main() -> int:
    outdir = sys.argv[1]
    only = set(sys.argv[2:])
    files = CHECKER + [NEW]
    saved = {p: open(p).read() for p in files}
    try:
        if not only or "cf1_checker_reverted" in only:
            for p in CHECKER:
                subprocess.run(["git", "checkout", "1b122bfa", "--", p], check=True)
            os.remove(NEW)
            run("cf1_checker_reverted", outdir)
        for p, t in saved.items():
            open(p, "w").write(t)
        subprocess.run(["git", "reset", "-q", "--"] + CHECKER, check=True)
        for name, edits in EDITS.items():
            if only and name not in only:
                continue
            for path, old, new in edits:
                text = open(path).read()
                assert text.count(old) == 1, (name, path, text.count(old))
                open(path, "w").write(text.replace(old, new))
            try:
                run(name, outdir)
            finally:
                for p, t in saved.items():
                    open(p, "w").write(t)
        run("control", outdir)
    finally:
        for p, t in saved.items():
            open(p, "w").write(t)
        subprocess.run(["git", "reset", "-q", "--"] + CHECKER, check=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
