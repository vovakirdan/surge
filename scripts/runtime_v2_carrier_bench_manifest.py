"""Strict loader for the frozen Runtime V2 carrier benchmark manifest."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
from typing import Any, Sequence, cast

from runtime_v2_carrier_bench_model import (
    Aggregation,
    AllocationControl,
    AvailabilityStatus,
    CrossRelation,
    CrossRowInvariant,
    FileDigest,
    Invariant,
    LivenessAvailability,
    LivenessProbe,
    LivenessStatus,
    Manifest,
    ManifestError,
    Metric,
    MetricAvailability,
    MetricSource,
    Operator,
    PayloadRole,
    Protocol,
    ReferenceHost,
    Reduction,
    Row,
    Side,
    StrictJSONError,
    strict_json_loads,
)
from runtime_v2_carrier_bench_manifest_validation import validate_manifest
from runtime_v2_carrier_bench_manifest_values import (
    _boolean,
    _choice,
    _commit,
    _integer,
    _keys,
    _number,
    _object,
    _relative_path,
    _string,
    _unique_strings,
)


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
            "blocking_threads",
            "metrics",
            "allocation_control",
            "harness_files",
            "fixtures",
            "rows",
            "cross_row_invariants",
            "liveness_probes",
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
        blocking_threads=_integer(
            root["blocking_threads"], "blocking_threads", 1
        ),
        metrics=_metrics(root["metrics"]),
        allocation_control=_allocation_control(root["allocation_control"]),
        harness_files=_file_digests(root["harness_files"], "harness_files"),
        fixtures=_file_digests(root["fixtures"], "fixtures"),
        rows=_rows(root["rows"]),
        cross_row_invariants=_cross_row_invariants(root["cross_row_invariants"]),
        liveness_probes=_liveness_probes(root["liveness_probes"]),
    )
    validate_manifest(manifest)
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
        "p95_cv_floor_ns",
        "placements",
        "short_row_budget_ns",
        "short_row_throughput_min_ratio",
    }
    _keys(obj, "protocol", fields)
    protocol = Protocol(
        warmups=_integer(obj["warmups"], "protocol.warmups", 0),
        measured_pairs=_integer(obj["measured_pairs"], "protocol.measured_pairs", 2),
        placements=_integer(obj["placements"], "protocol.placements", 1),
        short_row_budget_ns=_integer(
            obj["short_row_budget_ns"], "protocol.short_row_budget_ns", 1
        ),
        short_row_throughput_min_ratio=_number(
            obj["short_row_throughput_min_ratio"],
            "protocol.short_row_throughput_min_ratio",
        ),
        max_cv=_number(obj["max_cv"], "protocol.max_cv"),
        throughput_min_ratio=_number(
            obj["throughput_min_ratio"], "protocol.throughput_min_ratio"
        ),
        p95_max_ratio=_number(obj["p95_max_ratio"], "protocol.p95_max_ratio"),
        percentile_method=_choice(
            obj["percentile_method"], "protocol.percentile_method", {"nearest-rank"}
        ),
        cv_method=_choice(obj["cv_method"], "protocol.cv_method", {"sample-n-minus-1"}),
        p95_cv_floor_ns=_integer(obj["p95_cv_floor_ns"], "protocol.p95_cv_floor_ns", 1),
    )
    if not 0.0 < protocol.max_cv < 1.0:
        raise ManifestError("protocol.max_cv must be in (0, 1)")
    return protocol


# No transport byte budget: the slot transport owns no bytes to budget (owner
# rulings 2026-08-29 and 2026-09-03), and the four SURGE_CARRIER_BENCH_*_BYTES
# environment variables the runner used to export were read by nothing.


def _rows(raw: Any) -> tuple[Row, ...]:
    if not isinstance(raw, list) or not raw:
        raise ManifestError("rows must be a non-empty array")
    rows: list[Row] = []
    for index, value in enumerate(raw):
        label = f"rows[{index}]"
        obj = _object(value, label)
        fields = {
            "id",
            "workload_family",
            "payload_role",
            "fixture",
            "probe",
            "operations_per_batch",
            "batches",
            "payload_bytes",
            "timeout_seconds",
            "relative_performance",
            "expected_checksum",
            "candidate_structural_allocations_per_batch",
            "required_metrics",
            "invariants",
        }
        _keys(obj, label, fields)
        rows.append(
            Row(
                row_id=_string(obj["id"], f"{label}.id"),
                workload_family=_string(
                    obj["workload_family"], f"{label}.workload_family"
                ),
                payload_role=cast(
                    PayloadRole,
                    _choice(
                        obj["payload_role"],
                        f"{label}.payload_role",
                        {"zero", "scalar", "composite", "control"},
                    ),
                ),
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
                candidate_structural_allocations_per_batch=_integer(
                    obj["candidate_structural_allocations_per_batch"],
                    f"{label}.candidate_structural_allocations_per_batch",
                    0,
                ),
                required_metrics=_unique_strings(
                    obj["required_metrics"], f"{label}.required_metrics"
                ),
                invariants=_invariants(obj["invariants"], f"{label}.invariants"),
            )
        )
    return tuple(rows)


def _allocation_control(raw: Any) -> AllocationControl:
    obj = _object(raw, "allocation_control")
    _keys(
        obj,
        "allocation_control",
        {"fixture", "probe", "expected_checksum", "expected_allocation_count"},
    )
    return AllocationControl(
        fixture=_string(obj["fixture"], "allocation_control.fixture"),
        probe=_string(obj["probe"], "allocation_control.probe"),
        expected_checksum=_string(
            obj["expected_checksum"], "allocation_control.expected_checksum"
        ),
        expected_allocation_count=_integer(
            obj["expected_allocation_count"],
            "allocation_control.expected_allocation_count",
            1,
        ),
    )


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


def _cross_row_invariants(raw: Any) -> tuple[CrossRowInvariant, ...]:
    if not isinstance(raw, list):
        raise ManifestError("cross_row_invariants must be an array")
    out: list[CrossRowInvariant] = []
    fields = {
        "id",
        "relation",
        "metric",
        "left_row",
        "left_reduction",
        "operator",
        "right_row",
        "right_reduction",
        "side",
    }
    for index, value in enumerate(raw):
        label = f"cross_row_invariants[{index}]"
        item = _object(value, label)
        _keys(item, label, fields)
        out.append(
            CrossRowInvariant(
                invariant_id=_string(item["id"], f"{label}.id"),
                relation=cast(
                    CrossRelation,
                    _choice(
                        item["relation"],
                        f"{label}.relation",
                        {"paired_payload", "payload_proportional"},
                    ),
                ),
                metric=_string(item["metric"], f"{label}.metric"),
                left_row=_string(item["left_row"], f"{label}.left_row"),
                left_reduction=cast(
                    Reduction,
                    _choice(
                        item["left_reduction"],
                        f"{label}.left_reduction",
                        {"min", "max"},
                    ),
                ),
                operator=cast(
                    Operator,
                    _choice(item["operator"], f"{label}.operator", {"eq", "le", "ge"}),
                ),
                right_row=_string(item["right_row"], f"{label}.right_row"),
                right_reduction=cast(
                    Reduction,
                    _choice(
                        item["right_reduction"],
                        f"{label}.right_reduction",
                        {"min", "max"},
                    ),
                ),
                side=cast(
                    Side,
                    _choice(item["side"], f"{label}.side", {"base", "candidate"}),
                ),
            )
        )
    ids = tuple(item.invariant_id for item in out)
    if len(set(ids)) != len(ids) or tuple(sorted(ids)) != ids:
        raise ManifestError("cross_row_invariants ids must be unique and bytewise sorted")
    return tuple(out)


def _liveness_probes(raw: Any) -> tuple[LivenessProbe, ...]:
    # The list may be empty, and today it is: the two probes it carried
    # waited on a sync point nothing armed, and no code ever emitted the
    # record their parser reads (owner ruling 2026-09-04). The machinery
    # stays -- a probe that names a point the runtime reaches can be added
    # back by writing the row.
    if not isinstance(raw, list):
        raise ManifestError("liveness_probes must be an array")
    out: list[LivenessProbe] = []
    # No byte figures here, and that is the transport model rather than an
    # omission: a cross-shard message carries a POINTER into a refcount graph
    # the transport neither copies nor owns, so there is no per-message byte
    # cost to bound and the budget that exists is slots. A probe that named a
    # payload size and a peak-byte window was asserting a cost that is not
    # charged.
    fields = {
        "id",
        "fixture",
        "probe",
        "syncpoint",
        "timeout_seconds",
        "expected_reply_reserved",
        "expected_park_transitions",
        "wave_a",
        "final",
    }
    for index, value in enumerate(raw):
        label = f"liveness_probes[{index}]"
        item = _object(value, label)
        _keys(item, label, fields)
        out.append(
            LivenessProbe(
                probe_id=_string(item["id"], f"{label}.id"),
                fixture=_string(item["fixture"], f"{label}.fixture"),
                probe=_string(item["probe"], f"{label}.probe"),
                syncpoint=_string(item["syncpoint"], f"{label}.syncpoint"),
                timeout_seconds=_integer(
                    item["timeout_seconds"], f"{label}.timeout_seconds", 1
                ),
                expected_reply_reserved=_integer(
                    item["expected_reply_reserved"],
                    f"{label}.expected_reply_reserved",
                    0,
                ),
                expected_park_transitions=_integer(
                    item["expected_park_transitions"],
                    f"{label}.expected_park_transitions",
                    1,
                ),
                wave_a=_liveness_availability(item["wave_a"], f"{label}.wave_a"),
                final=_liveness_availability(item["final"], f"{label}.final"),
            )
        )
    ids = tuple(item.probe_id for item in out)
    if len(set(ids)) != len(ids) or tuple(sorted(ids)) != ids:
        raise ManifestError("liveness_probes ids must be unique and bytewise sorted")
    return tuple(out)


def _liveness_availability(raw: Any, label: str) -> LivenessAvailability:
    obj = _object(raw, label)
    status = cast(
        LivenessStatus,
        _choice(obj.get("status"), f"{label}.status", {"required", "deferred"}),
    )
    if status == "required":
        _keys(obj, label, {"status"})
        return LivenessAvailability(status="required")
    _keys(obj, label, {"status", "reason", "provenance_commit"})
    return LivenessAvailability(
        status="deferred",
        reason=_string(obj["reason"], f"{label}.reason"),
        provenance_commit=_commit(
            obj["provenance_commit"], f"{label}.provenance_commit"
        ),
    )


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
