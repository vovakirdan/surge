"""Machine-readable report rendering for Runtime V2 carrier benchmarks."""

from __future__ import annotations

import hashlib
import json
import os
import uuid
from pathlib import Path
from typing import Any, Mapping

from runtime_v2_carrier_bench_model import (
    GateFailure,
    Manifest,
    MetricAvailability,
    ReferenceHost,
    Side,
    row_invariant_failures,
    score_side,
    validate_row_protocol,
)
from runtime_v2_carrier_bench_runner import LivenessRecord, RunRecord


def manifest_digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def harness_digest(manifest: Manifest) -> str:
    digest = hashlib.sha256()
    for entry in manifest.harness_files:
        digest.update(entry.path.encode("utf-8"))
        digest.update(b"\0")
        digest.update(entry.sha256.encode("ascii"))
        digest.update(b"\0")
    for entry in manifest.fixtures:
        digest.update(entry.path.encode("utf-8"))
        digest.update(b"\0")
        digest.update(entry.sha256.encode("ascii"))
        digest.update(b"\0")
    return digest.hexdigest()


def render_report(
    *,
    attempt_id: str,
    started_at: str,
    ended_at: str,
    manifest: Manifest,
    manifest_sha256: str,
    base_commit: str,
    candidate_commit: str,
    actual_host: ReferenceHost,
    records: Mapping[str, Mapping[Side, tuple[RunRecord, ...]]],
    benchmark_phase: str,
    liveness_records: tuple[LivenessRecord, ...],
) -> tuple[dict[str, Any], GateFailure | None]:
    rows: list[dict[str, Any]] = []
    failure: GateFailure | None = None
    protocol_failed = False
    endpoint_failed = False
    for row in manifest.rows:
        row_records = records.get(row.row_id)
        if row_records is None or "base" not in row_records or "candidate" not in row_records:
            raise GateFailure(f"report is missing base/candidate records for {row.row_id}")
        base = row_records["base"]
        candidate = row_records["candidate"]
        base_score = score_side([record.measured for record in base])
        candidate_score = score_side([record.measured for record in candidate])
        scores: dict[str, Any] = {
            "base": _score_json(base_score),
            "candidate": _score_json(candidate_score),
            "throughput_ratio": candidate_score.throughput / base_score.throughput,
            "p95_ratio": candidate_score.p95_ns / base_score.p95_ns,
        }
        protocol_failure: str | None = None
        try:
            validate_row_protocol(
                manifest,
                row,
                [record.measured for record in base],
                [record.measured for record in candidate],
            )
        except GateFailure as err:
            protocol_failure = str(err)
            protocol_failed = True
            if failure is None:
                failure = err
        invariant_failures = list(
            row_invariant_failures(
                row,
                [record.measured for record in base],
                [record.measured for record in candidate],
            )
        )
        if invariant_failures:
            endpoint_failed = True
            if failure is None:
                failure = GateFailure(invariant_failures[0])
        rows.append(
            {
                "id": row.row_id,
                "probe": row.probe,
                "fixture": row.fixture,
                "payload_bytes": row.payload_bytes,
                "relative_performance": row.relative_performance,
                "failure": protocol_failure or next(iter(invariant_failures), None),
                "protocol_status": "failed" if protocol_failure else "passed",
                "protocol_failure": protocol_failure,
                "invariant_status": "failed" if invariant_failures else "passed",
                "invariant_failures": invariant_failures,
                "scores": scores,
                "base_runs": [_run_json(record) for record in base],
                "candidate_runs": [_run_json(record) for record in candidate],
            }
        )
    comparison_reports, comparison_failures = _cross_row_invariant_reports(
        manifest, records
    )
    if comparison_failures:
        endpoint_failed = True
        if failure is None:
            failure = GateFailure(comparison_failures[0])
    if benchmark_phase == "final" and any(
        record.status == "deferred" for record in liveness_records
    ):
        endpoint_failed = True
        deferred_failure = GateFailure("final benchmark phase contains deferred liveness")
        if failure is None:
            failure = deferred_failure
    report = {
        "schema_version": 1,
        "status": "failed" if failure is not None else "passed",
        "benchmark_phase": benchmark_phase,
        "failure": str(failure) if failure is not None else None,
        "protocol_status": "failed" if protocol_failed else "passed",
        "endpoint_invariant_status": "failed" if endpoint_failed else "passed",
        "attempt": {
            "id": attempt_id,
            "started_at": started_at,
            "ended_at": ended_at,
        },
        "base_commit": base_commit,
        "candidate_commit": candidate_commit,
        "manifest_sha256": manifest_sha256,
        "harness_sha256": harness_digest(manifest),
        "epic_base": manifest.epic_base,
        "backend": manifest.backend,
        "profile": manifest.profile,
        "shards": manifest.shards,
        "threads": manifest.threads,
        "blocking_threads": manifest.blocking_threads,
        "transport_budget": {
            "data_bytes": manifest.transport.data_bytes,
            "control_bytes": manifest.transport.control_bytes,
            "jumbo_threshold_bytes": manifest.transport.jumbo_threshold_bytes,
            "max_inline_overhead_bytes": manifest.transport.max_inline_overhead_bytes,
        },
        "reference_host": {
            "system": manifest.reference.system,
            "machine": manifest.reference.machine,
            "kernel_contains": manifest.reference.kernel_contains,
            "cpu_model": manifest.reference.cpu_model,
            "logical_cpus": manifest.reference.logical_cpus,
            "cpuset": manifest.reference.cpuset,
            "go_version": manifest.reference.go_version,
            "clang_version": manifest.reference.clang_version,
        },
        "actual_host": {
            "system": actual_host.system,
            "machine": actual_host.machine,
            "kernel_release": actual_host.kernel_contains,
            "cpu_model": actual_host.cpu_model,
            "logical_cpus": actual_host.logical_cpus,
            "cpuset": actual_host.cpuset,
            "go_version": actual_host.go_version,
            "clang_version": actual_host.clang_version,
        },
        "protocol": {
            "warmups": manifest.protocol.warmups,
            "measured_pairs": manifest.protocol.measured_pairs,
            "max_cv": manifest.protocol.max_cv,
            "throughput_min_ratio": manifest.protocol.throughput_min_ratio,
            "p95_max_ratio": manifest.protocol.p95_max_ratio,
            "percentile_method": manifest.protocol.percentile_method,
            "cv_method": manifest.protocol.cv_method,
        },
        "metrics": [
            {
                "name": metric.name,
                "aggregation": metric.aggregation,
                "source": metric.source,
                "base": _availability_json(metric.base),
                "candidate": _availability_json(metric.candidate),
            }
            for metric in manifest.metrics
        ],
        "rows": rows,
        "cross_row_invariants": comparison_reports,
        "liveness_probes": [_liveness_json(record) for record in liveness_records],
    }
    return report, failure


def _cross_row_invariant_reports(
    manifest: Manifest,
    records: Mapping[str, Mapping[Side, tuple[RunRecord, ...]]],
) -> tuple[list[dict[str, Any]], list[str]]:
    reports: list[dict[str, Any]] = []
    failures: list[str] = []
    rows = {row.row_id: row for row in manifest.rows}
    for invariant in manifest.cross_row_invariants:
        failure: str | None = None
        comparisons: list[dict[str, Any]] = []
        left_value: int | None = None
        right_value: int | None = None
        left_raw_value: int | None = None
        right_raw_value: int | None = None
        try:
            left_samples = _batch_metric_samples(
                records, invariant.left_row, invariant.side, invariant.metric
            )
            right_samples = _batch_metric_samples(
                records, invariant.right_row, invariant.side, invariant.metric
            )
        except GateFailure as err:
            failure = str(err)
            left_samples = []
            right_samples = []
        coordinates = [(item["pair_index"], item["batch_index"]) for item in left_samples]
        right_coordinates = [
            (item["pair_index"], item["batch_index"]) for item in right_samples
        ]
        if failure is None and coordinates != right_coordinates:
            failure = (
                f"cross-row invariant {invariant.invariant_id} has unmatched "
                "pair/batch coordinates"
            )
        if failure is None and any(
            item["value"] is None for item in (*left_samples, *right_samples)
        ):
            failure = (
                f"cross-row invariant {invariant.invariant_id} uses unsupported batch metric"
            )
        elif failure is None and invariant.relation == "payload_proportional":
            left_scale = rows[invariant.right_row].payload_bytes
            right_scale = rows[invariant.left_row].payload_bytes
            for left, right in zip(left_samples, right_samples, strict=True):
                left_scaled = int(left["value"]) * left_scale
                right_scaled = int(right["value"]) * right_scale
                passed = left_scaled == right_scaled
                comparisons.append(
                    {
                        "pair_index": left["pair_index"],
                        "batch_index": left["batch_index"],
                        "left_scaled": left_scaled,
                        "right_scaled": right_scaled,
                        "status": "passed" if passed else "failed",
                    }
                )
                if not passed and failure is None:
                    failure = (
                        f"cross-row invariant {invariant.invariant_id} pair="
                        f"{left['pair_index']} batch={left['batch_index']}: "
                        f"scaled left={left_scaled} eq scaled right={right_scaled} failed"
                    )
        elif failure is None:
            numeric_left = [int(item["value"]) for item in left_samples]
            numeric_right = [int(item["value"]) for item in right_samples]
            left_raw_value = _reduce_values(numeric_left, invariant.left_reduction)
            right_raw_value = _reduce_values(numeric_right, invariant.right_reduction)
            left_value = left_raw_value
            right_value = right_raw_value
            passed = {
                "eq": left_value == right_value,
                "le": left_value <= right_value,
                "ge": left_value >= right_value,
            }[invariant.operator]
            if not passed:
                failure = (
                    f"cross-row invariant {invariant.invariant_id}: "
                    f"{invariant.left_reduction}({invariant.left_row}."
                    f"{invariant.metric})={left_value} {invariant.operator} "
                    f"{invariant.right_reduction}({invariant.right_row}."
                    f"{invariant.metric})={right_value} failed"
                )
        if failure is not None:
            failures.append(failure)
        reports.append(
            {
                "id": invariant.invariant_id,
                "relation": invariant.relation,
                "metric": invariant.metric,
                "side": invariant.side,
                "left": {
                    "row": invariant.left_row,
                    "reduction": invariant.left_reduction,
                    "value": left_value,
                    "raw_value": left_raw_value,
                    "batch_values": left_samples,
                },
                "operator": invariant.operator,
                "right": {
                    "row": invariant.right_row,
                    "reduction": invariant.right_reduction,
                    "value": right_value,
                    "raw_value": right_raw_value,
                    "batch_values": right_samples,
                },
                "pointwise_comparisons": comparisons,
                "status": "failed" if failure else "passed",
                "failure": failure,
            }
        )
    return reports, failures


def _liveness_json(record: LivenessRecord) -> dict[str, Any]:
    return {
        "id": record.probe_id,
        "status": record.status,
        "syncpoint": record.syncpoint,
        "credit_balance": record.credit_balance,
        "peak_transport_bytes": record.peak_transport_bytes,
        "park_transitions": record.park_transitions,
        "reason": record.reason,
        "provenance_commit": record.provenance_commit,
    }


def _batch_metric_values(
    records: Mapping[str, Mapping[Side, tuple[RunRecord, ...]]],
    row: str,
    side: Side,
    metric: str,
) -> list[int | None]:
    return [
        sample["value"]
        for sample in _batch_metric_samples(records, row, side, metric)
    ]


def _batch_metric_samples(
    records: Mapping[str, Mapping[Side, tuple[RunRecord, ...]]],
    row: str,
    side: Side,
    metric: str,
) -> list[dict[str, int | None]]:
    row_records = records.get(row)
    if row_records is None or side not in row_records:
        raise GateFailure(
            f"cross-row invariant is missing records for row={row} side={side}"
        )
    runs = row_records[side]
    if not runs:
        raise GateFailure(f"cross-row invariant has no runs for row={row} side={side}")
    values: list[dict[str, int | None]] = []
    for run_index, record in enumerate(runs):
        if not record.batches:
            raise GateFailure(
                f"cross-row invariant has no batches for row={row} side={side} "
                f"run={run_index}"
            )
        for batch_index, batch in enumerate(record.batches):
            if metric not in batch.counters:
                raise GateFailure(
                    f"cross-row invariant metric {metric} is missing from row={row} "
                    f"side={side} run={run_index} batch={batch_index}"
                )
            values.append(
                {
                    "pair_index": record.measured.pair_index,
                    "batch_index": batch_index,
                    "value": batch.counters[metric],
                }
            )
    return values


def _reduce_values(values: list[int], reduction: str) -> int:
    return min(values) if reduction == "min" else max(values)


def render_aborted_report(
    *,
    attempt_id: str,
    started_at: str,
    ended_at: str,
    phase: str,
    failure: BaseException,
    candidate_root: Path,
    manifest_path: Path,
    manifest_sha256: str | None,
    manifest: Manifest | None,
    actual_host: ReferenceHost | None,
    base_commit: str | None,
    candidate_commit: str | None,
    events: list[dict[str, object]],
) -> dict[str, Any]:
    return {
        "schema_version": 1,
        "status": "aborted",
        "attempt": {
            "id": attempt_id,
            "started_at": started_at,
            "ended_at": ended_at,
        },
        "phase": phase,
        "failure": str(failure),
        "candidate_root": str(candidate_root),
        "manifest_path": str(manifest_path),
        "manifest_sha256": manifest_sha256,
        "harness_sha256": harness_digest(manifest) if manifest is not None else None,
        "epic_base": manifest.epic_base if manifest is not None else None,
        "base_commit": base_commit,
        "candidate_commit": candidate_commit,
        "reference_host": (
            _reference_host_json(manifest.reference) if manifest is not None else None
        ),
        "actual_host": _actual_host_json(actual_host) if actual_host is not None else None,
        "metrics": (
            [
                {
                    "name": metric.name,
                    "aggregation": metric.aggregation,
                    "source": metric.source,
                    "base": _availability_json(metric.base),
                    "candidate": _availability_json(metric.candidate),
                }
                for metric in manifest.metrics
            ]
            if manifest is not None
            else None
        ),
        "events": events,
        "rows": [],
    }


def write_report(path: Path, report: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    encoded = json.dumps(report, indent=2, sort_keys=True) + "\n"
    temporary = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")
    try:
        with temporary.open("x", encoding="utf-8") as stream:
            stream.write(encoded)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def _run_json(record: RunRecord) -> dict[str, Any]:
    measured = record.measured
    return {
        "pair_index": measured.pair_index,
        "throughput": measured.timing.throughput(),
        "p50_ns": measured.timing.p50_ns(),
        "p95_ns": measured.timing.p95_ns(),
        "elapsed_ns": measured.timing.elapsed_ns,
        "operations": measured.timing.operations,
        "operation_latencies_ns": list(measured.timing.operation_latencies_ns),
        "counters": dict(sorted(measured.counters.values.items())),
        "batches": [
            {
                "nonce": batch.nonce,
                "elapsed_ns": batch.elapsed_ns,
                "operation_latencies_ns": list(batch.operation_latencies_ns),
                "checksum": batch.checksum,
                "counters": dict(sorted(batch.counters.items())),
            }
            for batch in record.batches
        ],
    }


def _score_json(score: Any) -> dict[str, float]:
    return {
        "throughput": score.throughput,
        "p50_ns": score.p50_ns,
        "p95_ns": score.p95_ns,
        "throughput_cv": score.throughput_cv,
        "p95_cv": score.p95_cv,
    }


def _availability_json(value: MetricAvailability) -> dict[str, str]:
    rendered = {"status": value.status}
    if value.reason is not None:
        rendered["reason"] = value.reason
    if value.provenance_commit is not None:
        rendered["provenance_commit"] = value.provenance_commit
    return rendered


def _reference_host_json(host: ReferenceHost) -> dict[str, str | int]:
    return {
        "system": host.system,
        "machine": host.machine,
        "kernel_contains": host.kernel_contains,
        "cpu_model": host.cpu_model,
        "logical_cpus": host.logical_cpus,
        "cpuset": host.cpuset,
        "go_version": host.go_version,
        "clang_version": host.clang_version,
    }


def _actual_host_json(host: ReferenceHost) -> dict[str, str | int]:
    rendered = _reference_host_json(host)
    rendered["kernel_release"] = rendered.pop("kernel_contains")
    return rendered
