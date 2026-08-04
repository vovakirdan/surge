"""LLVM/source proofs for Runtime V2 carrier benchmark fixtures."""

from __future__ import annotations

import re
import shutil
from pathlib import Path
from typing import Mapping

from runtime_v2_carrier_bench_host import run_checked
from runtime_v2_carrier_bench_model import GateFailure

MARKER_SYMBOL = "rt_array_debug_deferred_base_drops"
TIMER_SYMBOL = "rt_monotonic_now"
CARRIER_SYMBOL_PREFIX = "rt_carrier_bench_"

_IR_NAME = r'(?:"(?:[^"\\]|\\.)+"|[-$._A-Za-z0-9]+)'
_IR_DEFINE_RE = re.compile(
    rf"(?m)^define\b[^\n]*@(?P<name>{_IR_NAME})\([^\n]*\)[^\n]*\{{\s*$"
)
_IR_CALL_RE = re.compile(
    rf"\b(?:call|invoke)\b[^@\n]*@(?P<name>{_IR_NAME})\("
)


def verify_fixture_source(source: Path, source_path: str) -> None:
    package_sources = list(source.parent.rglob("*.sg"))
    shared = source.parent.parent / "shared"
    if shared.is_dir():
        package_sources.extend(shared.rglob("*.sg"))
    package_sources = sorted(package_sources)
    if source not in package_sources:
        raise GateFailure(f"{source_path} is missing from its fixture package")
    texts = [path.read_text(encoding="utf-8") for path in package_sources]
    text = "\n".join(texts)
    marker_declaration = f"@intrinsic fn {MARKER_SYMBOL}() -> uint64;"
    timer_declaration = f"@intrinsic fn {TIMER_SYMBOL}() -> int64;"
    if text.count(marker_declaration) != 1:
        raise GateFailure(f"{source_path} must declare the marker exactly once")
    if text.count(timer_declaration) != 1:
        raise GateFailure(
            f"{source_path} package must declare the native timer exactly once"
        )
    marker_statements = re.findall(
        rf"(?m)^\s*{re.escape(MARKER_SYMBOL)}\(\);\s*$", text
    )
    if len(marker_statements) != 2:
        raise GateFailure(
            f"{source_path} must contain exactly two standalone marker calls; "
            f"found={len(marker_statements)}"
        )
    if re.search(r"\bDuration\b", text):
        raise GateFailure(
            f"{source_path} must use the base-compatible native timer, not boxed Duration"
        )


def verify_fixture_ir(binary: Path, source_path: str) -> None:
    llvm_ir = binary.parent / ".tmp" / binary.name / "out.ll"
    if not llvm_ir.is_file():
        raise GateFailure(f"{source_path} build did not retain emitted LLVM IR")
    clang = shutil.which("clang")
    if clang is None:
        raise GateFailure("clang is required to prove the carrier marker under -O3")
    optimized_ir = llvm_ir.with_name("out.o3.ll")
    run_checked(
        [clang, "-O3", "-S", "-emit-llvm", str(llvm_ir), "-o", str(optimized_ir)],
        cwd=llvm_ir.parent,
        timeout_seconds=120,
    )
    verify_emitted_ir(
        llvm_ir.read_text(encoding="utf-8"),
        optimized_ir.read_text(encoding="utf-8"),
        source_path,
    )


def verify_carrier_binary(binary: Path, source_path: str, capture_kind: str) -> None:
    result = run_checked(
        ["nm", "-a", str(binary)],
        cwd=binary.parent,
        timeout_seconds=30,
    )
    verify_carrier_symbols(result.stdout, source_path, capture_kind)


def verify_carrier_symbols(symbols: str, source_path: str, capture_kind: str) -> None:
    present = {
        line.split()[-1]
        for line in symbols.splitlines()
        if line.split() and line.split()[-1].startswith(CARRIER_SYMBOL_PREFIX)
    }
    required = {
        "rt_carrier_bench_init",
        "rt_carrier_bench_finish",
        "rt_carrier_bench_marker",
        "rt_carrier_bench_record_copy",
    }
    if capture_kind == "timing" and present:
        raise GateFailure(
            f"{source_path} timing binary contains carrier benchmark symbols: "
            f"{sorted(present)}"
        )
    if capture_kind == "resource" and not required <= present:
        raise GateFailure(
            f"{source_path} resource binary is missing carrier benchmark symbols: "
            f"{sorted(required - present)}"
        )
    if capture_kind not in {"timing", "resource"}:
        raise GateFailure(f"unknown carrier capture kind {capture_kind!r}")


def verify_emitted_ir(llvm_ir: str, optimized_ir: str, source_path: str) -> None:
    for label, text in (("emitted", llvm_ir), ("O3", optimized_ir)):
        _verify_entrypoint_marker_multiplicity(text, source_path, label)

    timer_calls = _ir_call_count(llvm_ir, TIMER_SYMBOL)
    direct_timer_calls = len(
        re.findall(
            rf"\b(?:tail\s+|musttail\s+|notail\s+)?call\s+i64\s+"
            rf"@{re.escape(TIMER_SYMBOL)}\(\)",
            llvm_ir,
        )
    )
    if timer_calls == 0 or direct_timer_calls != timer_calls:
        raise GateFailure(
            f"{source_path} timer must lower only to direct i64 native calls"
        )

    functions = _ir_functions(llvm_ir, source_path)
    observers = [
        name
        for name, body in functions.items()
        if any(call == "rt_heap_stats" for call in _ir_calls(body))
    ]
    if len(observers) != 1:
        raise GateFailure(
            f"{source_path} emitted IR must contain exactly one allocation observer; "
            f"found={len(observers)}"
        )
    _verify_observer_graph(functions, observers[0], source_path)


def _verify_entrypoint_marker_multiplicity(
    text: str, source_path: str, label: str
) -> None:
    functions = _ir_functions(text, source_path)
    owners = [
        name
        for name, body in functions.items()
        if _ir_call_count(body, MARKER_SYMBOL) != 0
    ]
    marker_calls = _ir_call_count(text, MARKER_SYMBOL)
    if len(owners) != 1 or marker_calls != 2:
        raise GateFailure(
            f"{source_path} {label} LLVM IR has {marker_calls} marker calls in "
            f"owners={owners}, want exactly 2 in one entrypoint owner"
        )
    owner = owners[0]
    entrypoint = "__surge_start"
    if entrypoint not in functions:
        raise GateFailure(f"{source_path} {label} LLVM IR has no {entrypoint}")
    if owner == entrypoint:
        return
    entrypoint_calls = _ir_calls(functions[entrypoint]).count(owner)
    all_owner_calls = sum(_ir_calls(body).count(owner) for body in functions.values())
    if entrypoint_calls != 1 or all_owner_calls != 1:
        raise GateFailure(
            f"{source_path} {label} entrypoint must invoke marker owner {owner} "
            f"exactly once; entrypoint_calls={entrypoint_calls} "
            f"all_calls={all_owner_calls}"
        )


def _ir_call_count(text: str, symbol: str) -> int:
    return sum(1 for match in _IR_CALL_RE.finditer(text) if _ir_name(match) == symbol)


def _ir_functions(text: str, source_path: str) -> dict[str, str]:
    functions: dict[str, str] = {}
    for match in _IR_DEFINE_RE.finditer(text):
        end = text.find("\n}", match.end())
        if end < 0:
            raise GateFailure(f"{source_path} emitted malformed LLVM function")
        name = _ir_name(match)
        if name in functions:
            raise GateFailure(f"{source_path} emitted duplicate LLVM function {name}")
        functions[name] = text[match.start() : end + 2]
    if not functions:
        raise GateFailure(f"{source_path} emitted no LLVM functions")
    return functions


def _ir_calls(body: str) -> tuple[str, ...]:
    return tuple(_ir_name(match) for match in _IR_CALL_RE.finditer(body))


def _ir_name(match: re.Match[str]) -> str:
    name = match.group("name")
    return name[1:-1] if name.startswith('"') else name


def _verify_observer_graph(
    functions: Mapping[str, str], root: str, source_path: str
) -> None:
    pending = [root]
    visited: set[str] = set()
    while pending:
        name = pending.pop()
        if name in visited:
            continue
        visited.add(name)
        for call in _ir_calls(functions[name]):
            if (
                call.startswith("rt_carrier_bench_")
                or call == MARKER_SYMBOL
                or call == TIMER_SYMBOL
            ):
                raise GateFailure(
                    f"{source_path} allocation observer graph calls forbidden {call}"
                )
            if call in functions and call not in visited:
                pending.append(call)
