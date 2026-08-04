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
    score_side,
    validate_row_results,
)
from runtime_v2_carrier_bench_runner import RunRecord


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
) -> tuple[dict[str, Any], GateFailure | None]:
    rows: list[dict[str, Any]] = []
    failure: GateFailure | None = None
    for row in manifest.rows:
        row_records = records[row.row_id]
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
        row_failure: str | None = None
        try:
            validate_row_results(
                manifest,
                row,
                [record.measured for record in base],
                [record.measured for record in candidate],
            )
        except GateFailure as err:
            row_failure = str(err)
            if failure is None:
                failure = err
        rows.append(
            {
                "id": row.row_id,
                "probe": row.probe,
                "fixture": row.fixture,
                "payload_bytes": row.payload_bytes,
                "relative_performance": row.relative_performance,
                "failure": row_failure,
                "scores": scores,
                "base_runs": [_run_json(record) for record in base],
                "candidate_runs": [_run_json(record) for record in candidate],
            }
        )
    report = {
        "schema_version": 1,
        "status": "failed" if failure is not None else "passed",
        "failure": str(failure) if failure is not None else None,
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
    }
    return report, failure


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
