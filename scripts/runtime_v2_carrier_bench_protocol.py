"""Strict result/counter protocol for Runtime V2 carrier benchmarks."""

from __future__ import annotations

import json
import re
import stat
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, cast

from runtime_v2_carrier_bench_model import (
    GateFailure,
    LivenessProbe,
    Manifest,
    Row,
    Side,
    StrictJSONError,
    strict_json_loads,
)

RESULT_PREFIX = "SURGE_CARRIER_BENCH "
RUNTIME_COUNTER_PREFIX = "SURGE_CARRIER_COUNTERS "
LIVENESS_PREFIX = "SURGE_CARRIER_LIVENESS "


@dataclass(frozen=True, slots=True)
class BatchResult:
    elapsed_ns: int
    operation_latencies_ns: tuple[int, ...]
    checksum: str
    counters: Mapping[str, int | None]
    nonce: str = ""


@dataclass(frozen=True, slots=True)
class LivenessRecord:
    probe_id: str
    status: str
    syncpoint: str | None
    credit_balance: int | None
    peak_transport_bytes: int | None
    park_transitions: int | None
    reason: str | None = None
    provenance_commit: str | None = None


def _parse_liveness_record(
    stdout: str,
    stderr: str,
    probe: LivenessProbe,
    *,
    expected_nonce: str,
    expected_protocol_sha256: str,
) -> LivenessRecord:
    if stdout:
        raise GateFailure(
            f"liveness probe {probe.probe_id} emitted unexpected stdout:\n{stdout}"
        )
    output_lines = stderr.splitlines()
    records = [line for line in output_lines if line.startswith(LIVENESS_PREFIX)]
    if len(records) != 1 or len(output_lines) != 1:
        raise GateFailure(
            f"liveness probe {probe.probe_id} emitted {len(records)} records and "
            f"{len(output_lines)} stderr lines, want exactly one record\nstderr:\n{stderr}"
        )
    try:
        raw = strict_json_loads(records[0][len(LIVENESS_PREFIX) :])
    except (json.JSONDecodeError, StrictJSONError) as err:
        raise GateFailure(
            f"liveness probe {probe.probe_id} emitted malformed JSON: {err}"
        ) from err
    expected_keys = {
        "schema_version",
        "status",
        "probe",
        "nonce",
        "protocol_sha256",
        "syncpoint",
        "credit_balance",
        "peak_transport_bytes",
        "park_transitions",
        "error",
    }
    if not isinstance(raw, dict) or set(raw) != expected_keys:
        actual = set(raw) if isinstance(raw, dict) else set()
        raise GateFailure(
            f"liveness probe {probe.probe_id} fields mismatch: "
            f"missing={sorted(expected_keys - actual)} extra={sorted(actual - expected_keys)}"
        )
    schema_version = _non_negative_integer(
        raw["schema_version"], f"{probe.probe_id}.schema_version"
    )
    if (
        schema_version != 1
        or raw["status"] != "ok"
        or raw["error"] is not None
        or raw["probe"] != probe.probe
        or raw["nonce"] != expected_nonce
        or raw["protocol_sha256"] != expected_protocol_sha256
        or raw["syncpoint"] != probe.syncpoint
    ):
        raise GateFailure(f"liveness probe {probe.probe_id} identity/status mismatch")
    credit_balance = _non_negative_integer(
        raw["credit_balance"], f"{probe.probe_id}.credit_balance"
    )
    peak = _non_negative_integer(
        raw["peak_transport_bytes"], f"{probe.probe_id}.peak_transport_bytes"
    )
    parks = _non_negative_integer(
        raw["park_transitions"], f"{probe.probe_id}.park_transitions"
    )
    if credit_balance != probe.expected_credit_balance:
        raise GateFailure(
            f"liveness probe {probe.probe_id} credit balance {credit_balance}, "
            f"want {probe.expected_credit_balance}"
        )
    # `peak` is READ AND RECORDED but no longer bounded. The window it used to
    # be checked against was derived from a payload size plus a per-message
    # overhead, and pointer transport charges neither: the message carries a
    # pointer into a refcount graph the transport does not copy. The number
    # stays in the record because it is an observation the report can show; it
    # is not a budget, so nothing here refuses on it.
    if parks != probe.expected_park_transitions:
        raise GateFailure(
            f"liveness probe {probe.probe_id} park transitions {parks}, "
            f"want {probe.expected_park_transitions}"
        )
    return LivenessRecord(
        probe_id=probe.probe_id,
        status="passed",
        syncpoint=probe.syncpoint,
        credit_balance=credit_balance,
        peak_transport_bytes=peak,
        park_transitions=parks,
    )


def _parse_result(
    stdout: str, row: Row, expected_metrics: set[str] | None = None
) -> BatchResult:
    output_lines = stdout.splitlines()
    lines = [line for line in output_lines if line.startswith(RESULT_PREFIX)]
    if len(lines) != 1 or len(output_lines) != 1:
        raise GateFailure(
            f"{row.row_id} emitted {len(lines)} result lines and "
            f"{len(output_lines)} total lines, want exactly one\n"
            f"stdout:\n{stdout}"
        )
    try:
        raw = strict_json_loads(lines[0][len(RESULT_PREFIX) :])
    except (json.JSONDecodeError, StrictJSONError) as err:
        raise GateFailure(f"{row.row_id} emitted malformed benchmark JSON: {err}") from err
    expected_keys = {
        "schema_version",
        "probe",
        "operations",
        "elapsed_ns",
        "operation_latencies_ns",
        "checksum",
        "metrics",
    }
    if not isinstance(raw, dict) or set(raw) != expected_keys:
        actual = set(raw) if isinstance(raw, dict) else set()
        raise GateFailure(
            f"{row.row_id} result fields mismatch: "
            f"missing={sorted(expected_keys - actual)} extra={sorted(actual - expected_keys)}"
        )
    schema_version = _non_negative_integer(
        raw["schema_version"], f"{row.row_id}.schema_version"
    )
    if schema_version != 1 or raw["probe"] != row.probe:
        raise GateFailure(f"{row.row_id} result schema/probe mismatch")
    operations = _non_negative_integer(raw["operations"], f"{row.row_id}.operations")
    if operations != row.operations_per_batch:
        raise GateFailure(f"{row.row_id} result operation count drifted")
    elapsed = _non_negative_integer(raw["elapsed_ns"], f"{row.row_id}.elapsed_ns")
    if elapsed == 0:
        raise GateFailure(f"{row.row_id} elapsed_ns must be positive")
    latencies_raw = raw["operation_latencies_ns"]
    if not isinstance(latencies_raw, list) or len(latencies_raw) != row.operations_per_batch:
        raise GateFailure(
            f"{row.row_id} operation_latencies_ns must contain exactly "
            f"{row.operations_per_batch} samples"
        )
    latencies = tuple(
        _non_negative_integer(value, f"{row.row_id}.operation_latencies_ns[]")
        for value in latencies_raw
    )
    if any(value == 0 for value in latencies):
        raise GateFailure(f"{row.row_id} operation latencies must be positive")
    checksum = raw["checksum"]
    if not isinstance(checksum, str) or not checksum:
        raise GateFailure(f"{row.row_id} checksum must be a non-empty string")
    metrics_raw = raw["metrics"]
    if not isinstance(metrics_raw, dict):
        raise GateFailure(f"{row.row_id} metrics must be an object")
    metrics = {
        str(name): (
            None
            if value is None
            else _non_negative_integer(value, f"{row.row_id}.metrics.{name}")
        )
        for name, value in metrics_raw.items()
    }
    actual_metrics = set(metrics)
    required_metrics = (
        set(row.required_metrics) if expected_metrics is None else expected_metrics
    )
    if actual_metrics != required_metrics:
        raise GateFailure(
            f"{row.row_id} result metrics mismatch: "
            f"missing={sorted(required_metrics - actual_metrics)} "
            f"extra={sorted(actual_metrics - required_metrics)}"
        )
    return BatchResult(
        elapsed_ns=elapsed,
        operation_latencies_ns=latencies,
        checksum=checksum,
        counters=metrics,
    )


def _parse_runtime_counters(
    stderr: str,
    row: Row,
    manifest: Manifest,
    side: Side,
    *,
    expected_nonce: str,
    expected_protocol_sha256: str,
) -> dict[str, int | None]:
    metrics = [metric for metric in manifest.metrics if metric.source == "runtime_exit"]
    expected_numeric = {
        metric.name
        for metric in metrics
        if (metric.base if side == "base" else metric.candidate).status == "required"
    }
    unsupported = {metric.name for metric in metrics} - expected_numeric
    output_lines = stderr.splitlines()
    records = [line for line in output_lines if line.startswith(RUNTIME_COUNTER_PREFIX)]
    if not output_lines:
        if expected_numeric:
            raise GateFailure(
                f"{row.row_id} {side} emitted no required runtime counter record"
            )
        return {name: None for name in unsupported}
    if not expected_numeric:
        raise GateFailure(
            f"{row.row_id} {side} emitted an unexpected runtime counter record"
        )
    if len(records) != 1 or len(output_lines) != 1:
        raise GateFailure(
            f"{row.row_id} {side} emitted {len(records)} runtime counter records and "
            f"{len(output_lines)} stderr lines, want exactly one record\n"
            f"stderr:\n{stderr}"
        )
    try:
        raw = strict_json_loads(records[0][len(RUNTIME_COUNTER_PREFIX) :])
    except (json.JSONDecodeError, StrictJSONError) as err:
        raise GateFailure(
            f"{row.row_id} emitted malformed runtime counter JSON: {err}"
        ) from err
    expected_keys = {
        "schema_version",
        "status",
        "probe",
        "nonce",
        "protocol_sha256",
        "metrics",
        "error",
    }
    if not isinstance(raw, dict) or set(raw) != expected_keys:
        actual = set(raw) if isinstance(raw, dict) else set()
        raise GateFailure(
            f"{row.row_id} runtime counter fields mismatch: "
            f"missing={sorted(expected_keys - actual)} "
            f"extra={sorted(actual - expected_keys)}"
        )
    schema_version = _non_negative_integer(
        raw["schema_version"], f"{row.row_id}.runtime_schema_version"
    )
    if (
        schema_version != 1
        or raw["status"] != "ok"
        or raw["error"] is not None
        or raw["probe"] != row.probe
    ):
        raise GateFailure(f"{row.row_id} runtime counter schema/probe mismatch")
    if raw["nonce"] != expected_nonce:
        raise GateFailure(f"{row.row_id} runtime counter nonce mismatch")
    if raw["protocol_sha256"] != expected_protocol_sha256:
        raise GateFailure(f"{row.row_id} runtime counter protocol hash mismatch")
    values = raw["metrics"]
    if not isinstance(values, dict) or set(values) != expected_numeric:
        actual = set(values) if isinstance(values, dict) else set()
        raise GateFailure(
            f"{row.row_id} {side} runtime metrics mismatch: "
            f"missing={sorted(expected_numeric - actual)} "
            f"extra={sorted(actual - expected_numeric)}"
        )
    parsed = {
        str(name): _non_negative_integer(value, f"{row.row_id}.runtime_metrics.{name}")
        for name, value in values.items()
    }
    parsed.update({name: None for name in unsupported})
    return parsed


def _built_binary(stdout: str, package_copy: Path) -> Path:
    matches = re.findall(r"(?m)^built\s+(.+?)\s*$", stdout)
    target_dir = package_copy / "target"
    release_dir = target_dir / "release"
    if package_copy.is_symlink() or target_dir.is_symlink() or release_dir.is_symlink():
        raise GateFailure("copied fixture target/release path must not contain symlinks")
    try:
        package_root = package_copy.resolve(strict=True)
        release_root = release_dir.resolve(strict=True)
    except OSError as err:
        raise GateFailure(f"copied fixture release directory is unavailable: {err}") from err
    if not release_root.is_relative_to(package_root):
        raise GateFailure("copied fixture release directory escapes the copied package")
    executables: set[Path] = set()
    for match in matches:
        reported = Path(match)
        candidate = reported if reported.is_absolute() else package_copy / reported
        try:
            relative = candidate.relative_to(package_copy)
        except ValueError:
            continue
        current = package_copy
        contains_symlink = False
        for part in relative.parts:
            current /= part
            if current.is_symlink():
                contains_symlink = True
                break
        if contains_symlink:
            continue
        try:
            resolved = candidate.resolve(strict=True)
        except OSError:
            continue
        if not resolved.is_relative_to(release_root):
            continue
        mode = resolved.stat().st_mode
        if stat.S_ISREG(mode) and mode & 0o111:
            executables.add(resolved)
    if len(executables) != 1:
        raise GateFailure(
            f"cannot identify exactly one built fixture binary; "
            f"found={sorted(str(path) for path in executables)}\n{stdout}"
        )
    return next(iter(executables))


def _non_negative_integer(raw: object, label: str) -> int:
    if isinstance(raw, bool) or not isinstance(raw, int) or raw < 0:
        raise GateFailure(f"{label} must be a non-negative integer")
    return cast(int, raw)
