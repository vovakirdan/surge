"""Build and execute frozen Runtime V2 carrier benchmark fixtures."""

from __future__ import annotations

import json
import re
import secrets
import shutil
import stat
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence, cast

from runtime_v2_carrier_bench_host import run_checked
from runtime_v2_carrier_bench_model import (
    GateFailure,
    Manifest,
    MeasuredRun,
    Row,
    Side,
    StrictJSONError,
    TimingSample,
    aggregate_counters,
    paired_order,
    strict_json_loads,
)

RESULT_PREFIX = "SURGE_CARRIER_BENCH "
RUNTIME_COUNTER_PREFIX = "SURGE_CARRIER_COUNTERS "


@dataclass(frozen=True, slots=True)
class BatchResult:
    elapsed_ns: int
    operation_latencies_ns: tuple[int, ...]
    checksum: str
    counters: Mapping[str, int | None]
    nonce: str = ""


@dataclass(frozen=True, slots=True)
class RunRecord:
    measured: MeasuredRun
    batches: tuple[BatchResult, ...]


@dataclass(frozen=True, slots=True)
class BuiltFixture:
    binary: Path
    source_path: str


def build_surge(root: Path, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    run_checked(
        ["go", "build", "-trimpath", "-o", str(output), "./cmd/surge"],
        cwd=root,
        timeout_seconds=600,
    )


def build_fixtures(
    *,
    side_root: Path,
    harness_root: Path,
    surge: Path,
    manifest: Manifest,
    build_root: Path,
) -> dict[str, BuiltFixture]:
    fixtures: dict[str, BuiltFixture] = {}
    source_paths = sorted({row.fixture for row in manifest.rows})
    for index, source_path in enumerate(source_paths):
        source = harness_root / source_path
        package_source = source.parent
        package_copy = build_root / f"fixture-{index:02d}"
        shutil.copytree(package_source, package_copy)
        copied_source = package_copy / source.name
        if not copied_source.is_file():
            raise GateFailure(f"fixture source is missing after copy: {source_path}")
        result = run_checked(
            [
                str(surge),
                "build",
                "--release",
                f"--backend={manifest.backend}",
                "--ui=off",
                str(package_copy),
            ],
            cwd=side_root,
            timeout_seconds=600,
            environment={"SURGE_STDLIB": str(side_root)},
        )
        binary = _built_binary(result.stdout, package_copy)
        fixtures[source_path] = BuiltFixture(binary=binary, source_path=source_path)
    return fixtures


def execute_manifest(
    manifest: Manifest,
    binaries: Mapping[Side, Mapping[str, BuiltFixture]],
    events: list[dict[str, object]],
    protocol_sha256: str,
) -> dict[str, dict[Side, tuple[RunRecord, ...]]]:
    records: dict[str, dict[Side, tuple[RunRecord, ...]]] = {}
    for row_index, row in enumerate(manifest.rows):
        _run_warmups(
            manifest, row_index, row, binaries, events, protocol_sha256
        )
        per_side: dict[Side, list[RunRecord]] = {"base": [], "candidate": []}
        for pair_index in range(manifest.protocol.measured_pairs):
            for side in paired_order(row_index, pair_index):
                fixture = binaries[side][row.fixture]
                per_side[side].append(
                    _run_measured(
                        manifest,
                        row,
                        side,
                        pair_index,
                        fixture,
                        events,
                        protocol_sha256,
                    )
                )
        records[row.row_id] = {
            "base": tuple(per_side["base"]),
            "candidate": tuple(per_side["candidate"]),
        }
    return records


def _run_warmups(
    manifest: Manifest,
    row_index: int,
    row: Row,
    binaries: Mapping[Side, Mapping[str, BuiltFixture]],
    events: list[dict[str, object]],
    protocol_sha256: str,
) -> None:
    for warmup_index in range(manifest.protocol.warmups):
        for side in paired_order(row_index, warmup_index):
            fixture = binaries[side][row.fixture]
            for batch_index in range(row.batches):
                _run_recorded_batch(
                    manifest,
                    row,
                    side,
                    fixture,
                    events,
                    phase="warmup",
                    run_index=warmup_index,
                    batch_index=batch_index,
                    protocol_sha256=protocol_sha256,
                )


def _run_measured(
    manifest: Manifest,
    row: Row,
    side: Side,
    pair_index: int,
    fixture: BuiltFixture,
    events: list[dict[str, object]],
    protocol_sha256: str,
) -> RunRecord:
    batches = tuple(
        _run_recorded_batch(
            manifest,
            row,
            side,
            fixture,
            events,
            phase="measured",
            run_index=pair_index,
            batch_index=batch_index,
            protocol_sha256=protocol_sha256,
        )
        for batch_index in range(row.batches)
    )
    counters = aggregate_counters(
        manifest.metrics, [batch.counters for batch in batches], side
    )
    measured = MeasuredRun(
        side=side,
        pair_index=pair_index,
        timing=TimingSample(
            elapsed_ns=sum(batch.elapsed_ns for batch in batches),
            operation_latencies_ns=tuple(
                latency
                for batch in batches
                for latency in batch.operation_latencies_ns
            ),
            operations=row.operations_per_batch * row.batches,
        ),
        counters=counters,
    )
    return RunRecord(measured=measured, batches=batches)


def _run_recorded_batch(
    manifest: Manifest,
    row: Row,
    side: Side,
    fixture: BuiltFixture,
    events: list[dict[str, object]],
    *,
    phase: str,
    run_index: int,
    batch_index: int,
    protocol_sha256: str,
) -> BatchResult:
    event: dict[str, object] = {
        "row": row.row_id,
        "phase": phase,
        "side": side,
        "run_index": run_index,
        "batch_index": batch_index,
        "status": "started",
    }
    events.append(event)
    context = (
        f"row={row.row_id} phase={phase} side={side} "
        f"run={run_index} batch={batch_index}"
    )
    try:
        result = _run_batch(
            manifest, row, side, fixture, protocol_sha256
        )
    except GateFailure as err:
        event["status"] = "failed"
        event["failure"] = str(err)
        raise GateFailure(f"{context}: {err}") from err
    event["status"] = "passed"
    event["result"] = {
        "elapsed_ns": result.elapsed_ns,
        "operation_latencies_ns": list(result.operation_latencies_ns),
        "checksum": result.checksum,
        "nonce": result.nonce,
        "counters": dict(sorted(result.counters.items())),
    }
    return result


def _run_batch(
    manifest: Manifest,
    row: Row,
    side: Side,
    fixture: BuiltFixture,
    protocol_sha256: str,
) -> BatchResult:
    nonce = secrets.token_hex(16)
    command = [
        "taskset",
        "-c",
        manifest.reference.cpuset,
        str(fixture.binary),
        row.probe,
    ]
    result = run_checked(
        command,
        cwd=fixture.binary.parent,
        timeout_seconds=row.timeout_seconds,
        environment={
            "SURGE_CARRIER_BENCH_COUNTERS": "1",
            "SURGE_CARRIER_BENCH_PROBE": row.probe,
            "SURGE_CARRIER_BENCH_NONCE": nonce,
            "SURGE_CARRIER_BENCH_PROTOCOL_SHA256": protocol_sha256,
            "SURGE_SHARDS": str(manifest.shards),
            "SURGE_THREADS": str(manifest.threads),
        },
    )
    fixture_metrics = {
        metric.name for metric in manifest.metrics if metric.source == "fixture"
    }
    parsed = _parse_result(result.stdout, row, fixture_metrics)
    runtime_metrics = _parse_runtime_counters(
        result.stderr,
        row,
        manifest,
        side,
        expected_nonce=nonce,
        expected_protocol_sha256=protocol_sha256,
    )
    counters = {**parsed.counters, **runtime_metrics}
    if set(counters) != set(row.required_metrics):
        raise GateFailure(f"{row.row_id} combined metric schema drifted")
    if parsed.checksum != row.expected_checksum:
        raise GateFailure(
            f"{row.row_id} checksum {parsed.checksum!r}, want {row.expected_checksum!r}"
        )
    return BatchResult(
        nonce=nonce,
        elapsed_ns=parsed.elapsed_ns,
        operation_latencies_ns=parsed.operation_latencies_ns,
        checksum=parsed.checksum,
        counters=counters,
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
