"""Strict loader for the frozen Runtime V2 carrier benchmark manifest."""

from __future__ import annotations

import hashlib
import json
import math
from pathlib import Path
from typing import Any, Mapping, Sequence, cast

from runtime_v2_carrier_bench_model import (
    Aggregation,
    AvailabilityStatus,
    FileDigest,
    Invariant,
    Manifest,
    ManifestError,
    Metric,
    MetricAvailability,
    MetricSource,
    Operator,
    Protocol,
    ReferenceHost,
    Row,
    Side,
    StrictJSONError,
    strict_json_loads,
)

FROZEN_METRIC_CONTRACT: dict[
    str, tuple[MetricSource, Aggregation, AvailabilityStatus]
] = {
    "allocation_count": ("fixture", "sum", "required"),
    "bytes_copied": ("runtime_exit", "sum", "unsupported"),
    "bytes_moved": ("runtime_exit", "sum", "unsupported"),
    "callback_count": ("runtime_exit", "sum", "unsupported"),
    "credit_stalls": ("runtime_exit", "sum", "unsupported"),
    "peak_transport_bytes": ("runtime_exit", "max", "unsupported"),
}


def load_manifest(path: Path) -> Manifest:
    try:
        raw = strict_json_loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError, StrictJSONError) as err:
        raise ManifestError(f"cannot read benchmark manifest {path}: {err}") from err
    root = _object(raw, "manifest")
    _keys(
        root,
        "manifest",
        {
            "schema_version",
            "epic_base",
            "reference_host",
            "protocol",
            "backend",
            "profile",
            "shards",
            "threads",
            "metrics",
            "harness_files",
            "fixtures",
            "rows",
        },
    )
    manifest = Manifest(
        schema_version=_integer(root["schema_version"], "schema_version", 1),
        epic_base=_commit(root["epic_base"], "epic_base"),
        reference=_reference(root["reference_host"]),
        protocol=_protocol(root["protocol"]),
        backend=_choice(root["backend"], "backend", {"llvm"}),
        profile=_choice(root["profile"], "profile", {"release"}),
        shards=_integer(root["shards"], "shards", 1),
        threads=_integer(root["threads"], "threads", 1),
        metrics=_metrics(root["metrics"]),
        harness_files=_file_digests(root["harness_files"], "harness_files"),
        fixtures=_file_digests(root["fixtures"], "fixtures"),
        rows=_rows(root["rows"]),
    )
    _validate_manifest(manifest)
    return manifest


def verify_file_digests(root: Path, entries: Sequence[FileDigest], label: str) -> None:
    for entry in entries:
        path = root / entry.path
        if path.is_symlink():
            raise ManifestError(f"{label} must not be a symlink: {entry.path}")
        try:
            digest = hashlib.sha256(path.read_bytes()).hexdigest()
        except OSError as err:
            raise ManifestError(f"cannot hash {label} {entry.path}: {err}") from err
        if digest != entry.sha256:
            raise ManifestError(
                f"stale {label} {entry.path}: sha256={digest}, expected={entry.sha256}"
            )


def _reference(raw: Any) -> ReferenceHost:
    obj = _object(raw, "reference_host")
    fields = {
        "system",
        "machine",
        "kernel_contains",
        "cpu_model",
        "logical_cpus",
        "cpuset",
        "go_version",
        "clang_version",
    }
    _keys(obj, "reference_host", fields)
    return ReferenceHost(
        system=_string(obj["system"], "reference_host.system"),
        machine=_string(obj["machine"], "reference_host.machine"),
        kernel_contains=_string(obj["kernel_contains"], "reference_host.kernel_contains"),
        cpu_model=_string(obj["cpu_model"], "reference_host.cpu_model"),
        logical_cpus=_integer(obj["logical_cpus"], "reference_host.logical_cpus", 1),
        cpuset=_string(obj["cpuset"], "reference_host.cpuset"),
        go_version=_string(obj["go_version"], "reference_host.go_version"),
        clang_version=_string(obj["clang_version"], "reference_host.clang_version"),
    )


def _protocol(raw: Any) -> Protocol:
    obj = _object(raw, "protocol")
    fields = {
        "warmups",
        "measured_pairs",
        "max_cv",
        "throughput_min_ratio",
        "p95_max_ratio",
        "percentile_method",
        "cv_method",
    }
    _keys(obj, "protocol", fields)
    protocol = Protocol(
        warmups=_integer(obj["warmups"], "protocol.warmups", 0),
        measured_pairs=_integer(obj["measured_pairs"], "protocol.measured_pairs", 2),
        max_cv=_number(obj["max_cv"], "protocol.max_cv"),
        throughput_min_ratio=_number(
            obj["throughput_min_ratio"], "protocol.throughput_min_ratio"
        ),
        p95_max_ratio=_number(obj["p95_max_ratio"], "protocol.p95_max_ratio"),
        percentile_method=_choice(
            obj["percentile_method"], "protocol.percentile_method", {"nearest-rank"}
        ),
        cv_method=_choice(obj["cv_method"], "protocol.cv_method", {"sample-n-minus-1"}),
    )
    if not 0.0 < protocol.max_cv < 1.0:
        raise ManifestError("protocol.max_cv must be in (0, 1)")
    return protocol


def _rows(raw: Any) -> tuple[Row, ...]:
    if not isinstance(raw, list) or not raw:
        raise ManifestError("rows must be a non-empty array")
    rows: list[Row] = []
    for index, value in enumerate(raw):
        label = f"rows[{index}]"
        obj = _object(value, label)
        _keys(
            obj,
            label,
            {
                "id",
                "fixture",
                "probe",
                "operations_per_batch",
                "batches",
                "payload_bytes",
                "timeout_seconds",
                "relative_performance",
                "expected_checksum",
                "required_metrics",
                "invariants",
            },
        )
        rows.append(
            Row(
                row_id=_string(obj["id"], f"{label}.id"),
                fixture=_string(obj["fixture"], f"{label}.fixture"),
                probe=_string(obj["probe"], f"{label}.probe"),
                operations_per_batch=_integer(
                    obj["operations_per_batch"], f"{label}.operations_per_batch", 1
                ),
                batches=_integer(obj["batches"], f"{label}.batches", 2),
                payload_bytes=_integer(obj["payload_bytes"], f"{label}.payload_bytes", 0),
                timeout_seconds=_integer(obj["timeout_seconds"], f"{label}.timeout_seconds", 1),
                relative_performance=_boolean(
                    obj["relative_performance"], f"{label}.relative_performance"
                ),
                expected_checksum=_string(
                    obj["expected_checksum"], f"{label}.expected_checksum"
                ),
                required_metrics=_unique_strings(
                    obj["required_metrics"], f"{label}.required_metrics"
                ),
                invariants=_invariants(obj["invariants"], f"{label}.invariants"),
            )
        )
    return tuple(rows)


def _invariants(raw: Any, label: str) -> tuple[Invariant, ...]:
    if not isinstance(raw, list):
        raise ManifestError(f"{label} must be an array")
    out: list[Invariant] = []
    for index, value in enumerate(raw):
        item_label = f"{label}[{index}]"
        item = _object(value, item_label)
        _keys(item, item_label, {"metric", "operator", "value", "side"})
        operator = _choice(item["operator"], f"{item_label}.operator", {"eq", "le", "ge"})
        side = _choice(item["side"], f"{item_label}.side", {"base", "candidate"})
        out.append(
            Invariant(
                metric=_string(item["metric"], f"{item_label}.metric"),
                operator=cast(Operator, operator),
                value=_integer(item["value"], f"{item_label}.value", 0),
                side=cast(Side, side),
            )
        )
    return tuple(out)


def _metrics(raw: Any) -> tuple[Metric, ...]:
    if not isinstance(raw, list) or not raw:
        raise ManifestError("metrics must be a non-empty array")
    out: list[Metric] = []
    for index, value in enumerate(raw):
        label = f"metrics[{index}]"
        item = _object(value, label)
        _keys(item, label, {"name", "aggregation", "source", "base", "candidate"})
        aggregation = _choice(
            item["aggregation"], f"{label}.aggregation", {"sum", "max", "last"}
        )
        out.append(
            Metric(
                name=_string(item["name"], f"{label}.name"),
                aggregation=cast(Aggregation, aggregation),
                source=cast(
                    MetricSource,
                    _choice(item["source"], f"{label}.source", {"fixture", "runtime_exit"}),
                ),
                base=_availability(item["base"], f"{label}.base"),
                candidate=_availability(item["candidate"], f"{label}.candidate"),
            )
        )
    names = tuple(metric.name for metric in out)
    if len(set(names)) != len(names) or tuple(sorted(names)) != names:
        raise ManifestError("metrics must have unique bytewise-sorted names")
    return tuple(out)


def _availability(raw: Any, label: str) -> MetricAvailability:
    obj = _object(raw, label)
    status = cast(
        AvailabilityStatus,
        _choice(obj.get("status"), f"{label}.status", {"required", "unsupported"}),
    )
    if status == "required":
        _keys(obj, label, {"status"})
        return MetricAvailability(status="required")
    _keys(obj, label, {"status", "reason", "provenance_commit"})
    return MetricAvailability(
        status="unsupported",
        reason=_string(obj["reason"], f"{label}.reason"),
        provenance_commit=_commit(
            obj["provenance_commit"], f"{label}.provenance_commit"
        ),
    )


def _file_digests(raw: Any, label: str) -> tuple[FileDigest, ...]:
    if not isinstance(raw, list) or not raw:
        raise ManifestError(f"{label} must be a non-empty array")
    out: list[FileDigest] = []
    for index, value in enumerate(raw):
        item = _object(value, f"{label}[{index}]")
        _keys(item, f"{label}[{index}]", {"path", "sha256"})
        digest = _string(item["sha256"], f"{label}[{index}].sha256")
        if len(digest) != 64 or any(ch not in "0123456789abcdef" for ch in digest):
            raise ManifestError(f"{label}[{index}].sha256 is not lowercase SHA-256")
        out.append(FileDigest(_relative_path(item["path"], f"{label}[{index}].path"), digest))
    paths = tuple(entry.path for entry in out)
    if len(set(paths)) != len(paths) or tuple(sorted(paths)) != paths:
        raise ManifestError(f"{label} paths must be unique and bytewise sorted")
    return tuple(out)


def _validate_manifest(manifest: Manifest) -> None:
    if manifest.schema_version != 1:
        raise ManifestError(f"unsupported schema_version {manifest.schema_version}")
    protocol = manifest.protocol
    if protocol.warmups != 2 or protocol.measured_pairs != 7:
        raise ManifestError("protocol must freeze exactly 2 warmups and 7 measured pairs")
    if protocol.max_cv != 0.05:
        raise ManifestError("protocol.max_cv must be exactly 0.05")
    if protocol.throughput_min_ratio != 0.95 or protocol.p95_max_ratio != 1.10:
        raise ManifestError("protocol relative budgets must be exactly 0.95 throughput / 1.10 p95")
    metric_set = {metric.name for metric in manifest.metrics}
    if metric_set != set(FROZEN_METRIC_CONTRACT):
        raise ManifestError(
            "metrics must match the frozen six-metric contract: "
            f"missing={sorted(set(FROZEN_METRIC_CONTRACT) - metric_set)} "
            f"extra={sorted(metric_set - set(FROZEN_METRIC_CONTRACT))}"
        )
    for metric in manifest.metrics:
        source, aggregation, base_status = FROZEN_METRIC_CONTRACT[metric.name]
        if metric.source != source or metric.aggregation != aggregation:
            raise ManifestError(
                f"metric {metric.name} must use source={source} aggregation={aggregation}"
            )
        if metric.base.status != base_status or metric.candidate.status != "required":
            raise ManifestError(
                f"metric {metric.name} availability must be "
                f"base={base_status} candidate=required"
            )
        if (
            metric.base.status == "unsupported"
            and metric.base.provenance_commit != manifest.epic_base
        ):
            raise ManifestError(
                f"metric {metric.name} unsupported provenance must equal epic_base"
            )
    harness_set = {entry.path for entry in manifest.harness_files}
    if len(harness_set) != len(manifest.harness_files):
        raise ManifestError("harness file paths must be unique")
    fixture_set = {entry.path for entry in manifest.fixtures}
    if len(fixture_set) != len(manifest.fixtures):
        raise ManifestError("fixture paths must be unique")
    row_ids: set[str] = set()
    for row in manifest.rows:
        if row.row_id in row_ids:
            raise ManifestError(f"duplicate row id {row.row_id}")
        row_ids.add(row.row_id)
        if row.fixture not in fixture_set:
            raise ManifestError(f"row {row.row_id} references unknown fixture {row.fixture}")
        if set(row.required_metrics) != metric_set:
            raise ManifestError(f"row {row.row_id} must require the complete metric schema")
        for invariant in row.invariants:
            if invariant.metric not in metric_set:
                raise ManifestError(
                    f"row {row.row_id} invariant references unknown metric {invariant.metric}"
                )
            metric = next(item for item in manifest.metrics if item.name == invariant.metric)
            availability = metric.base if invariant.side == "base" else metric.candidate
            if availability.status == "unsupported":
                raise ManifestError(
                    f"row {row.row_id} invariant references unsupported "
                    f"{invariant.side} metric {invariant.metric}"
                )


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
