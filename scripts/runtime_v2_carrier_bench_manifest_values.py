"""Primitive value readers for the strict carrier manifest."""

from __future__ import annotations

import math
from pathlib import Path
from typing import Any, Mapping

from runtime_v2_carrier_bench_model import ManifestError


def _object(raw: Any, label: str) -> dict[str, Any]:
    if not isinstance(raw, dict):
        raise ManifestError(f"{label} must be an object")
    return raw


def _keys(obj: Mapping[str, Any], label: str, expected: set[str]) -> None:
    actual = set(obj)
    if actual != expected:
        raise ManifestError(
            f"{label} fields mismatch: missing={sorted(expected - actual)} "
            f"unknown={sorted(actual - expected)}"
        )


def _string(raw: Any, label: str) -> str:
    if not isinstance(raw, str) or not raw:
        raise ManifestError(f"{label} must be a non-empty string")
    return raw


def _relative_path(raw: Any, label: str) -> str:
    value = _string(raw, label)
    path = Path(value)
    if path.is_absolute() or ".." in path.parts or value != path.as_posix():
        raise ManifestError(f"{label} must be a canonical repository-relative path")
    return value


def _integer(raw: Any, label: str, minimum: int) -> int:
    if isinstance(raw, bool) or not isinstance(raw, int) or raw < minimum:
        raise ManifestError(f"{label} must be an integer >= {minimum}")
    return raw


def _number(raw: Any, label: str) -> float:
    if isinstance(raw, bool) or not isinstance(raw, (int, float)) or not math.isfinite(raw):
        raise ManifestError(f"{label} must be a finite number")
    return float(raw)


def _boolean(raw: Any, label: str) -> bool:
    if not isinstance(raw, bool):
        raise ManifestError(f"{label} must be a boolean")
    return raw


def _choice(raw: Any, label: str, choices: set[str]) -> str:
    value = _string(raw, label)
    if value not in choices:
        raise ManifestError(f"{label} must be one of {sorted(choices)}, got {value!r}")
    return value


def _unique_strings(raw: Any, label: str) -> tuple[str, ...]:
    if not isinstance(raw, list) or not raw:
        raise ManifestError(f"{label} must be a non-empty array")
    values = tuple(_string(value, f"{label}[]") for value in raw)
    if len(set(values)) != len(values) or tuple(sorted(values)) != values:
        raise ManifestError(f"{label} must contain unique bytewise-sorted strings")
    return values


def _commit(raw: Any, label: str) -> str:
    value = _string(raw, label)
    if len(value) != 40 or any(ch not in "0123456789abcdef" for ch in value):
        raise ManifestError(f"{label} must be a full lowercase commit SHA")
    return value
