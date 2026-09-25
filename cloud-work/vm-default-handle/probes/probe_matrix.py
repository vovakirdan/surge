#!/usr/bin/env python3
"""Run every probe program through the gate and both backends; print a table.

Columns per program:
  gate@base  : does the gated build (surge-base) accept it (return-origin)?
  vm-before  : VM run with the unfixed compiler, gate bypassed (surge-bypass-base)
  vm-after   : VM run with the fixed compiler, gate bypassed (surge-bypass-fix)
  native     : LLVM run (the fix does not touch the native path), gate bypassed
  parity     : vm-after and native agree on exit code, stdout and refusal words
Each run is summarised as rc/stdout/refusal, where the refusal is the words after
`panic VMnnnn: ` (VM) or `surge: fatal [PANIC]: ` (native).
"""
import os
import re
import subprocess
import sys
from typing import Dict, List, Tuple

# The three compilers are built by the reader (see PACKET.md section 5.3):
#   surge-base         the base tree
#   surge-bypass-base  the base tree + gate-bypass.patch
#   surge-bypass-fix   the fixed tree + gate-bypass.patch
# and live in $SURGE_BIN_DIR. $SURGE_STDLIB must name a checkout.
SP = os.environ["SURGE_BIN_DIR"]
PROBES = os.path.dirname(os.path.abspath(__file__))
ENV: Dict[str, str] = dict(os.environ, SURGE_THREADS="1")
VM_FAULT = re.compile(r"^panic (VM\d+): (.*)$", re.M)
NATIVE_FAULT = re.compile(r"^surge: fatal \[PANIC\]: (.*)$", re.M)


def run(binary: str, backend: str, program: str) -> Tuple[int, str, str]:
    proc = subprocess.run(
        [f"{SP}/{binary}", "run", "--backend", backend, program],
        cwd=PROBES, env=ENV, capture_output=True, text=True, timeout=120, check=False,
    )
    return proc.returncode, proc.stdout, proc.stderr


def summary(rc: int, out: str, err: str) -> Tuple[str, str]:
    """Return (display text, comparable key)."""
    if "return-origin analysis unfinished" in err:
        return "REFUSED by gate", "refused"
    m = VM_FAULT.search(err)
    fault = f"{m.group(1)} {m.group(2)}" if m else ""
    words = m.group(2) if m else ""
    n = NATIVE_FAULT.search(err)
    if n:
        fault, words = n.group(1), n.group(1)
    if not fault and err.strip():
        first = err.strip().splitlines()[0]
        fault, words = first[:90], first[:90]
    shown_out = out.replace("\n", "|")
    return f"rc={rc} out=[{shown_out}] {fault}".strip(), f"{rc}|{out}|{words}"


def main() -> None:
    programs: List[str] = sorted(p for p in os.listdir(PROBES) if p.endswith(".sg"))
    if len(sys.argv) > 1:
        programs = [p for p in programs if p in sys.argv[1:]]
    for program in programs:
        gate_rc, _, gate_err = run("surge-base", "vm", program)
        gate = "refused" if "return-origin analysis unfinished" in gate_err else "accepted"
        before, _ = summary(*run("surge-bypass-base", "vm", program))
        after, after_key = summary(*run("surge-bypass-fix", "vm", program))
        native, native_key = summary(*run("surge-bypass-fix", "llvm", program))
        parity = "SAME" if after_key == native_key else "DIFF"
        print(f"{program}\tgate@base={gate}\n"
              f"    vm-before : {before}\n"
              f"    vm-after  : {after}\n"
              f"    native    : {native}\n"
              f"    parity    : {parity}", flush=True)


if __name__ == "__main__":
    main()
